package notify

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/connkind"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

var start = time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)

type orgList []domain.OrgID

func (o orgList) ActiveOrgs(context.Context) ([]domain.OrgID, error) { return o, nil }

// syncBuffer is a log sink the race detector accepts.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.b.String()
}

type senderHarness struct {
	orgs   orgList
	outbox *porttest.NotificationDeliveries
	mailer *porttest.Mailer
	slack  *porttest.DirectMessenger
	clock  *porttest.Clock
	logs   *syncBuffer
	sender *Sender
}

func newSender(t *testing.T, orgs ...domain.OrgID) senderHarness {
	t.Helper()

	if len(orgs) == 0 {
		orgs = []domain.OrgID{"org-a"}
	}

	h := senderHarness{
		orgs: orgs, outbox: porttest.NewNotificationDeliveries(), mailer: porttest.NewMailer(),
		clock: porttest.NewClock(start), logs: &syncBuffer{},
	}
	h.sender = New(Deps{
		Orgs: h.orgs, Outbox: h.outbox, Mailer: h.mailer, Clock: h.clock, PublicURL: publicURL,
		Log: slog.New(slog.NewJSONHandler(h.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})

	return h
}

func (h senderHarness) work(t *testing.T) int {
	t.Helper()

	n, err := h.sender.Work(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	return n
}

func (h senderHarness) row(t *testing.T, id int64) porttest.DeliveryRow {
	t.Helper()

	r, ok := h.outbox.Row(id)
	if !ok {
		t.Fatalf("row %d is gone", id)
	}

	return r
}

func (h senderHarness) queue(org domain.OrgID, n ports.Notification) int64 {
	n.At = start

	return h.outbox.Queue(org, n)
}

// A due row is sent to its recipient's address and marked sent; the log says so with the row,
// the event and the channel, and names no address and no body.
func TestSender_SendsAndMarksSent(t *testing.T) {
	t.Parallel()

	h := newSender(t)
	id := h.queue("org-a", itemNotification(domain.NotifyApproval))

	if n := h.work(t); n != 1 {
		t.Fatalf("work finished %d rows; want 1", n)
	}

	sent := h.mailer.Sent()
	if len(sent) != 1 || sent[0].To != "ann@example.com" || !strings.Contains(sent[0].Text, "needs your OK") {
		t.Fatalf("sent %+v; want the approval to ann@example.com", sent)
	}

	if r := h.row(t, id); r.State != ports.NotificationSent || !r.SentAt.Equal(start) || !r.LeaseUntil.IsZero() {
		t.Fatalf("row = %+v; want sent at %v with no lease", r, start)
	}

	logs := h.logs.String()
	assertLogged(t, logs, `"msg":"notify.sent"`, `"id":1,`, `"event":"approval"`, `"channel":"email"`)
	assertNothingPrivate(t, logs)

	if n := h.work(t); n != 0 || len(h.mailer.Sent()) != 1 {
		t.Fatalf("a second round finished %d rows and sent %d messages; want 0 and still 1", n, len(h.mailer.Sent()))
	}
}

// assertLogged fails unless logs hold every one of want.
func assertLogged(t *testing.T, logs string, want ...string) {
	t.Helper()

	for _, w := range want {
		if !strings.Contains(logs, w) {
			t.Errorf("logs lack %s:\n%s", w, logs)
		}
	}
}

// assertNothingPrivate fails when logs hold an address or a part of a message.
func assertNothingPrivate(t *testing.T, logs string) {
	t.Helper()

	for _, private := range []string{"@example.com", "Add a login page", "needs your OK", "smtp-secret"} {
		if strings.Contains(logs, private) {
			t.Errorf("logs hold %q:\n%s", private, logs)
		}
	}
}

// A temporary error puts the row back to pending with a backoff of 1, 2, 4… minutes capped at
// an hour; after 8 attempts it fails with a reason that names no address.
func TestSender_RetriesWithBackoffThenFails(t *testing.T) {
	t.Parallel()

	h := newSender(t)
	h.mailer.Fail = func(ports.Message) error {
		return errors.Join(&ports.MailError{Code: "smtp-dial"}, errors.New("dial ann@example.com smtp-secret"))
	}
	id := h.queue("org-a", itemNotification(domain.NotifyAndon))

	waits := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 32 * time.Minute, time.Hour}

	for i, wait := range waits {
		h.work(t)

		r := h.row(t, id)
		if r.State != ports.NotificationPending || r.Attempts != i+1 || !r.NextAttemptAt.Equal(h.clock.Now().Add(wait)) {
			t.Fatalf("after attempt %d: %+v; want pending, due in %v", i+1, r, wait)
		}

		// Not due yet: nothing is claimed.
		h.clock.Advance(wait - time.Second)

		if n := h.work(t); n != 0 {
			t.Fatalf("attempt %d was retried before its backoff", i+1)
		}

		h.clock.Advance(time.Second)
	}

	h.work(t)

	r := h.row(t, id)
	if r.State != ports.NotificationFailed || r.Attempts != MaxAttempts || r.LastError != "smtp-dial" {
		t.Fatalf("after %d attempts: %+v; want failed with smtp-dial", MaxAttempts, r)
	}

	logs := h.logs.String()
	if strings.Count(logs, `"msg":"notify.failed"`) != MaxAttempts {
		t.Errorf("logs hold %d notify.failed; want one per attempt:\n%s", strings.Count(logs, "notify.failed"), logs)
	}

	assertNothingPrivate(t, logs)
}

// A permanent failure fails the row at once.
func TestSender_PermanentFailureFailsAtOnce(t *testing.T) {
	t.Parallel()

	h := newSender(t)
	h.mailer.Fail = func(ports.Message) error { return &ports.MailError{Code: "smtp-rcpt-550", Permanent: true} }
	id := h.queue("org-a", itemNotification(domain.NotifyBudget))

	h.work(t)

	if r := h.row(t, id); r.State != ports.NotificationFailed || r.Attempts != 1 || r.LastError != "smtp-rcpt-550" {
		t.Fatalf("row = %+v; want failed after one attempt with smtp-rcpt-550", r)
	}
}

// An error that is no MailError is stored as a fixed reason, never as its text.
func TestSender_OtherErrorsStoreAFixedReason(t *testing.T) {
	t.Parallel()

	h := newSender(t)
	h.mailer.Fail = func(ports.Message) error { return errors.New("550 <ann@example.com> unknown") }
	id := h.queue("org-a", itemNotification(domain.NotifyApproval))

	h.work(t)

	if r := h.row(t, id); r.State != ports.NotificationPending || r.LastError != "mail-error" {
		t.Fatalf("row = %+v; want pending with mail-error", r)
	}
}

// A recipient who no longer qualifies (no longer a member, or email turned off) is skipped
// and sent nothing.
func TestSender_SkipsWhoNoLongerQualifies(t *testing.T) {
	t.Parallel()

	h := newSender(t)

	gone := itemNotification(domain.NotifyApproval)
	gone.Qualifies, gone.Address = false, ""
	off := itemNotification(domain.NotifyAndon)
	off.Qualifies = false

	ids := []int64{h.queue("org-a", gone), h.queue("org-a", off)}

	h.work(t)

	for _, id := range ids {
		if r := h.row(t, id); r.State != ports.NotificationSkipped {
			t.Errorf("row %d = %+v; want skipped", id, r)
		}
	}

	if len(h.mailer.Sent()) != 0 {
		t.Fatalf("sent %d messages; want none", len(h.mailer.Sent()))
	}
}

// A row whose lease is live is not claimed; once its lease has run out, the next round claims
// it, and the stale worker's finish changes nothing.
func TestSender_LeaseExpiryReclaims(t *testing.T) {
	t.Parallel()

	h := newSender(t)
	id := h.queue("org-a", itemNotification(domain.NotifyApproval))
	ctx := domain.WithOrg(t.Context(), "org-a")

	// A worker that died after its claim.
	stale, err := h.outbox.ClaimNotifications(ctx, ports.ChannelEmail, h.clock.Now(), Lease, BatchSize)
	if err != nil || len(stale) != 1 {
		t.Fatalf("claim = %v, %v; want the row", stale, err)
	}

	h.clock.Advance(Lease - time.Second)

	if n := h.work(t); n != 0 || len(h.mailer.Sent()) != 0 {
		t.Fatalf("a row under a live lease was sent")
	}

	h.clock.Advance(time.Second)

	if n := h.work(t); n != 1 || len(h.mailer.Sent()) != 1 {
		t.Fatalf("the row whose lease ran out was not sent")
	}

	if r := h.row(t, id); r.State != ports.NotificationSent || r.Attempts != 2 {
		t.Fatalf("row = %+v; want sent at attempt 2", r)
	}

	ok, err := h.outbox.FinishNotification(ctx, id, stale[0].Attempts, ports.NotificationOutcome{State: ports.NotificationFailed, Reason: "late"})
	if err != nil || ok {
		t.Fatalf("stale finish = %v, %v; want false", ok, err)
	}
}

// A batch whose sends are slow stops before its lease could run out mid-send, so no other
// instance can claim and mail a row this one is still sending; the rows it did not reach go
// back to the queue at once and the next round sends them.
func TestSender_StopsBeforeTheLeaseRunsOut(t *testing.T) {
	t.Parallel()

	h := newSender(t)

	var ids []int64
	for range 5 {
		ids = append(ids, h.queue("org-a", itemNotification(domain.NotifyApproval)))
	}

	leaseEnd := start.Add(Lease)

	var mu sync.Mutex

	var lateSend bool

	// Every send takes as long as one send may.
	h.mailer.Fail = func(ports.Message) error {
		mu.Lock()
		defer mu.Unlock()

		h.clock.Advance(SendTimeout)

		if h.clock.Now().After(leaseEnd) {
			lateSend = true
		}

		return nil
	}

	first := h.work(t)
	if first == 0 || first == len(ids) {
		t.Fatalf("first round finished %d rows; want some, not all %d", first, len(ids))
	}

	if lateSend {
		t.Fatal("a send ended after the claim's lease ran out")
	}

	for _, id := range ids[first:] {
		if r := h.row(t, id); r.State != ports.NotificationPending || !r.LeaseUntil.IsZero() || r.NextAttemptAt.After(h.clock.Now()) {
			t.Fatalf("row not reached = %+v; want pending, due now, with no lease", r)
		}
	}

	if n := h.work(t); n != len(ids)-first {
		t.Fatalf("second round finished %d rows; want the %d left", n, len(ids)-first)
	}

	if got := len(h.mailer.Sent()); got != len(ids) {
		t.Fatalf("sent %d messages; want each of the %d rows once", got, len(ids))
	}
}

// Each organization's rows are claimed in that organization, at most BatchSize per round.
func TestSender_ClaimsPerOrganizationInBatches(t *testing.T) {
	t.Parallel()

	h := newSender(t, "org-a", "org-b")
	for range BatchSize + 5 {
		h.queue("org-a", itemNotification(domain.NotifyApproval))
	}

	b := h.queue("org-b", itemNotification(domain.NotifyAndon))

	if n := h.work(t); n != BatchSize+1 {
		t.Fatalf("first round finished %d rows; want %d", n, BatchSize+1)
	}

	if r := h.row(t, b); r.State != ports.NotificationSent {
		t.Fatalf("org-b's row = %+v; want sent", r)
	}

	if n := h.work(t); n != 5 {
		t.Fatalf("second round finished %d rows; want the 5 left", n)
	}
}

// A row the renderer has no message for fails at once.
func TestSender_UnknownEventFails(t *testing.T) {
	t.Parallel()

	h := newSender(t)
	id := h.queue("org-a", itemNotification(domain.NotifyDigest))

	h.work(t)

	if r := h.row(t, id); r.State != ports.NotificationFailed || r.LastError != "unknown-event" {
		t.Fatalf("row = %+v; want failed with unknown-event", r)
	}
}

func TestBackoff_DoublesUpToAnHour(t *testing.T) {
	t.Parallel()

	for attempt, want := range map[int]time.Duration{1: time.Minute, 2: 2 * time.Minute, 3: 4 * time.Minute, 7: time.Hour, 30: time.Hour} {
		if got := backoff(attempt); got != want {
			t.Errorf("backoff(%d) = %v; want %v", attempt, got, want)
		}
	}
}

// slackHarness is a sender whose organization is a real one of a tenancy, so it can hold
// connections, and whose rows go to Slack.
type slackHarness struct {
	senderHarness
	org   domain.OrgID
	conns *porttest.Connections
}

func newSlackSender(t *testing.T, mailer bool) slackHarness {
	t.Helper()

	clock := porttest.NewClock(start)
	tenancy := porttest.NewTenancy(clock.Now)
	conns := porttest.NewConnections(porttest.NewMasterKeys().Key("k1"), tenancy)

	o, err := tenancy.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: "acme", Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}

	kinds, err := connkind.Platform()
	if err != nil {
		t.Fatal(err)
	}

	if err := conns.SyncPlatformKinds(t.Context(), kinds); err != nil {
		t.Fatal(err)
	}

	h := slackHarness{
		senderHarness: senderHarness{
			orgs: orgList{o.ID}, outbox: porttest.NewNotificationDeliveries(), mailer: porttest.NewMailer(),
			slack: porttest.NewDirectMessenger(), clock: clock, logs: &syncBuffer{},
		},
		org: o.ID, conns: conns,
	}

	d := Deps{
		Orgs: h.orgs, Outbox: h.outbox, Slack: h.slack, Connections: conns, Clock: clock, PublicURL: publicURL,
		Log: slog.New(slog.NewJSONHandler(h.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	if mailer {
		d.Mailer = h.mailer
	}

	h.sender = New(d)

	return h
}

// connect adds a Slack connection whose token is token, or a reference to a credential.
func (h slackHarness) connect(t *testing.T, name, kind, token string) {
	t.Helper()

	ctx := domain.WithOrg(t.Context(), h.org)
	if _, err := h.conns.Create(ctx, domain.NewConnection{Name: name, Kind: kind, Secrets: map[string]string{"token": token}}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}
}

func slackNotification(event domain.NotificationEvent) ports.Notification {
	n := itemNotification(event)
	n.Channel = ports.ChannelSlack

	return n
}

// A Slack row is sent to its recipient's email as a direct message, with the token of the
// organization's Slack connection and the email's text, and marked sent; the log names no
// address, no text and no token.
func TestSender_SlackSendsDirectMessage(t *testing.T) {
	t.Parallel()

	h := newSlackSender(t, false)
	h.connect(t, "slack", "slack", "xoxb-test-token")

	n := slackNotification(domain.NotifyApproval)
	id := h.queue(h.org, n)

	if done := h.work(t); done != 1 {
		t.Fatalf("work finished %d rows; want 1", done)
	}

	sent := h.slack.Sent()
	if len(sent) != 1 || sent[0].Token != "xoxb-test-token" || sent[0].Message.To != n.Address ||
		!strings.Contains(sent[0].Message.Text, "needs your OK") || !strings.Contains(sent[0].Message.Text, publicURL+"/o/acme/f/web-app/items/12") {
		t.Fatalf("sent %+v; want the approval to ann@example.com with the connection's token", sent)
	}

	if r := h.row(t, id); r.State != ports.NotificationSent || !r.SentAt.Equal(start) {
		t.Fatalf("row = %+v; want sent", r)
	}

	logs := h.logs.String()
	assertLogged(t, logs, `"msg":"notify.sent"`, `"channel":"slack"`)
	assertNothingPrivate(t, logs)

	if strings.Contains(logs, "xoxb-test-token") {
		t.Fatalf("logs hold the token:\n%s", logs)
	}
}

// Without a Slack connection, or with one whose token is a credential reference, a Slack row
// fails at once with a fixed reason and sends nothing.
func TestSender_SlackWithoutConnectionFails(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name    string
		connect func(t *testing.T, h slackHarness)
		reason  string
	}{
		{"no connection", func(t *testing.T, h slackHarness) {
			t.Helper()
			h.connect(t, "pager", "pagerduty", "pd-token")
		}, "slack-no-connection"},
		{"a credential reference", func(t *testing.T, h slackHarness) {
			t.Helper()
			h.connect(t, "slack", "slack", "{{slack-bot}}")
		}, "slack-token-unavailable"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			h := newSlackSender(t, false)
			c.connect(t, h)
			id := h.queue(h.org, slackNotification(domain.NotifyAndon))

			h.work(t)

			if r := h.row(t, id); r.State != ports.NotificationFailed || r.Attempts != 1 || r.LastError != c.reason {
				t.Fatalf("row = %+v; want failed after one attempt with %s", r, c.reason)
			}

			if len(h.slack.Sent()) != 0 {
				t.Fatal("a message was sent")
			}

			h.clock.Advance(maxBackoff)

			if n := h.work(t); n != 0 {
				t.Fatalf("a failed row was retried")
			}
		})
	}
}

// A person Slack does not find fails the row after one attempt, and it is not retried.
func TestSender_SlackUserNotFoundFailsOnce(t *testing.T) {
	t.Parallel()

	h := newSlackSender(t, false)
	h.connect(t, "slack", "slack", "xoxb-test-token")

	var tries int

	h.slack.Fail = func(string, ports.DirectMessage) error {
		tries++

		return &ports.DMError{Code: "slack-user-not-found", NotFound: true}
	}
	id := h.queue(h.org, slackNotification(domain.NotifyQuestion))

	h.work(t)
	h.clock.Advance(maxBackoff)
	h.work(t)

	if r := h.row(t, id); r.State != ports.NotificationFailed || r.Attempts != 1 || r.LastError != "slack-user-not-found" || tries != 1 {
		t.Fatalf("row = %+v after %d tries; want failed once with slack-user-not-found", r, tries)
	}

	assertNothingPrivate(t, h.logs.String())
}

// A temporary Slack error backs off as email does, storing its code; an error that is no
// DMError stores a fixed reason.
func TestSender_SlackTemporaryErrorsBackOff(t *testing.T) {
	t.Parallel()

	h := newSlackSender(t, false)
	h.connect(t, "slack", "slack", "xoxb-test-token")
	h.slack.Fail = func(string, ports.DirectMessage) error { return &ports.DMError{Code: "slack-rate-limited"} }
	id := h.queue(h.org, slackNotification(domain.NotifyBudget))

	h.work(t)

	if r := h.row(t, id); r.State != ports.NotificationPending || r.LastError != "slack-rate-limited" || !r.NextAttemptAt.Equal(start.Add(time.Minute)) {
		t.Fatalf("row = %+v; want pending a minute with slack-rate-limited", r)
	}

	h.slack.Fail = func(string, ports.DirectMessage) error { return errors.New("xoxb-test-token ann@example.com") }
	h.clock.Advance(time.Minute)
	h.work(t)

	if r := h.row(t, id); r.State != ports.NotificationPending || r.LastError != "slack-error" {
		t.Fatalf("row = %+v; want pending with slack-error", r)
	}
}

// With more than one Slack connection, the oldest one's token is used, whatever the names.
func TestSender_SlackUsesTheOldestConnection(t *testing.T) {
	t.Parallel()

	h := newSlackSender(t, false)
	h.connect(t, "zz-first", "slack", "xoxb-oldest")
	h.connect(t, "aa-second", "slack", "xoxb-newer")
	h.queue(h.org, slackNotification(domain.NotifyApproval))

	h.work(t)

	if sent := h.slack.Sent(); len(sent) != 1 || sent[0].Token != "xoxb-oldest" {
		t.Fatalf("sent %+v; want one message with the oldest connection's token", sent)
	}
}

// Without a Mailer the sender claims no email rows: they stay pending for an instance that can
// send them, and Slack rows still go out.
func TestSender_EmailWaitsWithoutAMailer(t *testing.T) {
	t.Parallel()

	h := newSlackSender(t, false)
	h.connect(t, "slack", "slack", "xoxb-test-token")
	email := h.queue(h.org, itemNotification(domain.NotifyApproval))
	slackRow := h.queue(h.org, slackNotification(domain.NotifyApproval))

	if n := h.work(t); n != 1 {
		t.Fatalf("work finished %d rows; want only the Slack one", n)
	}

	if r := h.row(t, email); r.State != ports.NotificationPending || r.Attempts != 0 {
		t.Fatalf("email row = %+v; want pending and never claimed", r)
	}

	if r := h.row(t, slackRow); r.State != ports.NotificationSent {
		t.Fatalf("slack row = %+v; want sent", r)
	}
}

// With both channels each row goes out on its own.
func TestSender_SendsBothChannels(t *testing.T) {
	t.Parallel()

	h := newSlackSender(t, true)
	h.connect(t, "slack", "slack", "xoxb-test-token")
	h.queue(h.org, itemNotification(domain.NotifyApproval))
	h.queue(h.org, slackNotification(domain.NotifyApproval))

	if n := h.work(t); n != 2 || len(h.mailer.Sent()) != 1 || len(h.slack.Sent()) != 1 {
		t.Fatalf("finished %d rows, %d emails, %d Slack messages; want 2, 1 and 1", n, len(h.mailer.Sent()), len(h.slack.Sent()))
	}
}
