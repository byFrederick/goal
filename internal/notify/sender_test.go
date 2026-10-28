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
	if !strings.Contains(logs, `"msg":"notify.sent"`) || !strings.Contains(logs, `"id":1,`) ||
		!strings.Contains(logs, `"event":"approval"`) || !strings.Contains(logs, `"channel":"email"`) {
		t.Errorf("logs lack notify.sent with its row, event and channel:\n%s", logs)
	}

	assertNothingPrivate(t, logs)

	if n := h.work(t); n != 0 || len(h.mailer.Sent()) != 1 {
		t.Fatalf("a second round finished %d rows and sent %d messages; want 0 and still 1", n, len(h.mailer.Sent()))
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
