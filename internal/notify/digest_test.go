package notify

import (
	"context"
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

const (
	ann domain.UserID = "user-ann"
	bob domain.UserID = "user-bob"
)

type digestHarness struct {
	orgs    orgList
	digests *porttest.Digests
	prefs   *porttest.NotificationPreferences
	mailer  *porttest.Mailer
	slack   *porttest.DirectMessenger
	conns   *porttest.Connections
	clock   *porttest.Clock
	logs    *syncBuffer
	job     *DigestJob
}

// newDigestJob is a digest job over two real organizations of a tenancy, acme and labs, so they
// can hold connections, with email and Slack, its clock at now.
func newDigestJob(t *testing.T, now time.Time) digestHarness {
	t.Helper()

	clock := porttest.NewClock(now)
	tenancy := porttest.NewTenancy(clock.Now)
	conns := porttest.NewConnections(porttest.NewMasterKeys().Key("k1"), tenancy)

	var orgs orgList

	for _, slug := range []string{"acme", "labs"} {
		o, err := tenancy.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: slug, Name: strings.ToUpper(slug[:1]) + slug[1:]})
		if err != nil {
			t.Fatal(err)
		}

		orgs = append(orgs, o.ID)
	}

	kinds, err := connkind.Platform()
	if err != nil {
		t.Fatal(err)
	}

	if err := conns.SyncPlatformKinds(t.Context(), kinds); err != nil {
		t.Fatal(err)
	}

	h := digestHarness{
		orgs: orgs, digests: porttest.NewDigests(), prefs: porttest.NewNotificationPreferences(), mailer: porttest.NewMailer(),
		slack: porttest.NewDirectMessenger(), conns: conns, clock: clock, logs: &syncBuffer{},
	}
	h.job = h.newJob()

	return h
}

// newJob is another instance's job over the same database, mailer and Slack.
func (h digestHarness) newJob() *DigestJob {
	return NewDigestJob(DigestDeps{
		Orgs: h.orgs, Digests: h.digests, Preferences: h.prefs, Mailer: h.mailer, Slack: h.slack, Connections: h.conns,
		Clock: h.clock, PublicURL: publicURL,
		Log: slog.New(slog.NewJSONHandler(h.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
}

func (h digestHarness) round(t *testing.T) DigestRound {
	t.Helper()

	r, err := h.job.Round(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	return r
}

// member makes user a member of org with something to report there.
func (h digestHarness) member(org domain.OrgID, user domain.UserID, address string) {
	h.digests.SetSummary(org, user, ports.DigestSummary{
		Address: address, OrgSlug: map[domain.OrgID]string{h.orgs[0]: "acme", h.orgs[1]: "labs"}[org],
		OrgName: map[domain.OrgID]string{h.orgs[0]: "Acme", h.orgs[1]: "Labs"}[org], NeedsYouDecisions: 1,
	})
}

func emailRecipient(user domain.UserID, tz string) ports.DigestRecipient {
	return ports.DigestRecipient{UserID: user, TimeZone: tz, Email: true}
}

func (h digestHarness) emailDigests() []porttest.DigestRow {
	var out []porttest.DigestRow

	for _, r := range h.digests.Rows() {
		if r.Channel == ports.ChannelEmail {
			out = append(out, r)
		}
	}

	return out
}

// A person's digest is due once their local time reaches 08:00, in their own zone (UTC when
// blank or unknown), and is dated by their local date.
func TestDigest_DueAtEightLocal(t *testing.T) {
	t.Parallel()

	// 2026-10-28: Berlin is UTC+1 (summer time ended on the 25th), Los Angeles UTC-7 (until
	// November 1st), Auckland UTC+13.
	for _, c := range []struct {
		name, tz string
		before   time.Time // 07:59 local
		date     string
	}{
		{"berlin", "Europe/Berlin", time.Date(2026, 10, 28, 6, 59, 0, 0, time.UTC), "2026-10-28"},
		{"los angeles", "America/Los_Angeles", time.Date(2026, 10, 28, 14, 59, 0, 0, time.UTC), "2026-10-28"},
		{"utc", "UTC", time.Date(2026, 10, 28, 7, 59, 0, 0, time.UTC), "2026-10-28"},
		{"unset", "", time.Date(2026, 10, 28, 7, 59, 0, 0, time.UTC), "2026-10-28"},
		{"unknown zone", "Mars/Olympus", time.Date(2026, 10, 28, 7, 59, 0, 0, time.UTC), "2026-10-28"},
		{"auckland, a day ahead of utc", "Pacific/Auckland", time.Date(2026, 10, 28, 18, 59, 0, 0, time.UTC), "2026-10-29"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			h := newDigestJob(t, c.before)
			h.digests.SetRecipients(h.orgs[0], emailRecipient(ann, c.tz))
			h.member(h.orgs[0], ann, "ann@example.com")

			if r := h.round(t); !r.Ran || r.Queued != 0 || len(h.mailer.Sent()) != 0 {
				t.Fatalf("round at 07:59 local = %+v, sent %d; want nothing queued", r, len(h.mailer.Sent()))
			}

			h.clock.Advance(time.Minute)

			if r := h.round(t); r.Queued != 1 || r.Finished != 1 {
				t.Fatalf("round at 08:00 local = %+v; want one digest queued and sent", r)
			}

			rows := h.emailDigests()
			if len(rows) != 1 || rows[0].LocalDate != c.date || rows[0].State != ports.NotificationSent {
				t.Fatalf("digests = %+v; want one sent, dated %s", rows, c.date)
			}

			if sent := h.mailer.Sent(); len(sent) != 1 || sent[0].To != "ann@example.com" || !strings.Contains(sent[0].Subject, c.date) {
				t.Fatalf("sent %+v; want ann's digest of %s", sent, c.date)
			}
		})
	}
}

// A person gets one digest per local date: not again later that day, not after a restart, and
// again the next day, covering the time since the first.
func TestDigest_OncePerLocalDate(t *testing.T) {
	t.Parallel()

	eight := time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC)
	h := newDigestJob(t, eight)
	h.digests.SetRecipients(h.orgs[0], emailRecipient(ann, "UTC"))
	h.member(h.orgs[0], ann, "ann@example.com")

	if r := h.round(t); r.Queued != 1 || r.Finished != 1 {
		t.Fatalf("first round = %+v; want one digest", r)
	}

	h.clock.Advance(5 * time.Minute)

	if r := h.round(t); r.Queued != 0 || r.Finished != 0 {
		t.Fatalf("second round = %+v; want nothing", r)
	}

	h.clock.Advance(10 * time.Hour)
	h.job = h.newJob() // a restart: the job keeps nothing of its own

	if r := h.round(t); r.Queued != 0 || r.Finished != 0 {
		t.Fatalf("round after a restart = %+v; want nothing", r)
	}

	if n := len(h.mailer.Sent()); n != 1 {
		t.Fatalf("sent %d digests on one date; want 1", n)
	}

	// The next day, before and at 08:00.
	h.clock.Advance(eight.Add(24*time.Hour - time.Minute).Sub(h.clock.Now()))

	if r := h.round(t); r.Queued != 0 {
		t.Fatalf("round at 07:59 the next day = %+v; want nothing", r)
	}

	h.clock.Advance(time.Minute)

	if r := h.round(t); r.Queued != 1 || r.Finished != 1 {
		t.Fatalf("round at 08:00 the next day = %+v; want one digest", r)
	}

	rows := h.emailDigests()
	if len(rows) != 2 || rows[1].LocalDate != "2026-10-29" || !rows[1].PeriodStart.Equal(rows[0].PeriodEnd) {
		t.Fatalf("digests = %+v; want the next day's covering the time since the first", rows)
	}

	if !rows[0].PeriodStart.Equal(eight.Add(-24 * time.Hour)) {
		t.Errorf("first digest starts at %v; want 24 hours before it", rows[0].PeriodStart)
	}
}

// Two goald instances that run rounds at once queue and send each digest once: a round runs only
// in the instance holding the lock, and the other skips it.
func TestDigest_TwoInstancesRace(t *testing.T) {
	t.Parallel()

	h := newDigestJob(t, time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC))
	h.digests.SetRecipients(h.orgs[0], emailRecipient(ann, "UTC"), emailRecipient(bob, "UTC"))
	h.digests.SetRecipients(h.orgs[1], emailRecipient(ann, "UTC"))
	h.member(h.orgs[0], ann, "ann@example.com")
	h.member(h.orgs[0], bob, "bob@example.com")
	h.member(h.orgs[1], ann, "ann@example.com")

	other := h.newJob()

	// While one instance sends, the other's round is skipped.
	var (
		once    sync.Once
		skipped DigestRound
		err     error
	)

	h.mailer.Fail = func(ports.Message) error {
		once.Do(func() { skipped, err = other.Round(t.Context()) })

		return nil
	}

	if r := h.round(t); !r.Ran || r.Queued != 2 || r.Finished != 2 {
		t.Fatalf("round = %+v; want ann's and bob's digests", r)
	}

	if err != nil || skipped.Ran || skipped.Queued != 0 || skipped.Finished != 0 {
		t.Fatalf("the other instance's round during it = %+v, %v; want it skipped", skipped, err)
	}

	// Both instances racing, round after round, send nothing more.
	var wg sync.WaitGroup

	for _, j := range []*DigestJob{h.job, other} {
		wg.Go(func() {
			for range 20 {
				if _, err := j.Round(t.Context()); err != nil {
					t.Error(err)
				}
			}
		})
	}

	wg.Wait()

	to := map[string]int{}
	for _, m := range h.mailer.Sent() {
		to[m.To]++
	}

	if len(to) != 2 || to["ann@example.com"] != 1 || to["bob@example.com"] != 1 {
		t.Fatalf("sent %v; want one digest to each", to)
	}
}

// The email digest is one message across the person's organizations, with each one's counts and
// links from the public URL; a Slack digest covers its own organization only and goes as a
// direct message with that organization's token. The log names no address and no body.
func TestDigest_RenderLinksAndCounts(t *testing.T) {
	t.Parallel()

	h := newDigestJob(t, time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC))
	acme, labs := h.orgs[0], h.orgs[1]

	if err := h.prefs.SetNotificationPreferences(t.Context(), ann, slackDigestOn()); err != nil {
		t.Fatal(err)
	}

	ctx := domain.WithOrg(t.Context(), acme)
	if _, err := h.conns.Create(ctx, domain.NewConnection{Name: "slack", Kind: "slack", Secrets: map[string]string{"token": "xoxb-test-token"}}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	h.digests.SetRecipients(acme, ports.DigestRecipient{UserID: ann, TimeZone: "UTC", Email: true, Slack: true})
	h.digests.SetRecipients(labs, emailRecipient(ann, "UTC"))
	h.digests.SetSummary(acme, ann, ports.DigestSummary{
		Address: "ann@example.com", OrgSlug: "acme", OrgName: "Acme", NeedsYouDecisions: 2, NeedsYouAndons: 1, Done: 5, AndonsPulled: 1,
		Spend: []ports.FactorySpend{{FactoryName: "docs", FactoryTitle: "Docs <site>", USD: 10}, {FactoryName: "web", FactoryTitle: "Web", USD: 2.3}},
	})
	h.digests.SetSummary(labs, ann, ports.DigestSummary{Address: "ann@example.com", OrgSlug: "labs", OrgName: "Labs", Done: 3})

	if r := h.round(t); r.Queued != 2 || r.Finished != 2 {
		t.Fatalf("round = %+v; want ann's email and acme's Slack digest", r)
	}

	sent := h.mailer.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent %d emails; want 1", len(sent))
	}

	m := sent[0]
	for _, want := range []string{
		"Your Goal digest for 2026-10-28",
		"Acme\n",
		"- Needs you: 2 decisions and 1 andon. " + publicURL + "/o/acme/needs-you",
		"- Items done: 5. " + publicURL + "/o/acme",
		"- Andons pulled: 1.",
		"- Spend: $12.30 (Docs <site> $10.00, Web $2.30). " + publicURL + "/o/acme/spend",
		"Labs\n",
		"- Items done: 3. " + publicURL + "/o/labs",
		"Change them at " + publicURL + "/account",
	} {
		if !strings.Contains(m.Text, want) {
			t.Errorf("email text lacks %q:\n%s", want, m.Text)
		}
	}

	if m.To != "ann@example.com" || !strings.Contains(m.HTML, "Docs &lt;site&gt;") || strings.Contains(m.HTML, "<site>") ||
		!strings.Contains(m.HTML, `href="`+publicURL+`/o/labs"`) {
		t.Errorf("email = %+v; want it to ann with the names escaped in the HTML and the links", m)
	}

	dms := h.slack.Sent()
	if len(dms) != 1 || dms[0].Token != "xoxb-test-token" || dms[0].Message.To != "ann@example.com" {
		t.Fatalf("direct messages %+v; want acme's digest to ann with acme's token", dms)
	}

	if text := dms[0].Message.Text; !strings.Contains(text, publicURL+"/o/acme/needs-you") || strings.Contains(text, "Labs") ||
		!strings.Contains(text, "You get this message") {
		t.Errorf("Slack digest = %q; want acme's lines only", text)
	}

	logs := h.logs.String()
	if strings.Count(logs, `"msg":"notify.sent"`) != 2 || !strings.Contains(logs, `"event":"digest"`) ||
		strings.Contains(logs, "ann@example.com") || strings.Contains(logs, "xoxb-") || strings.Contains(logs, "Needs you") {
		t.Errorf("logs = %s; want two notify.sent digest lines naming no address, token or body", logs)
	}
}

func slackDigestOn() domain.NotificationPreferences {
	p := domain.DefaultNotificationPreferences()
	d := p.Events[domain.NotifyDigest]
	d.Slack = true
	p.Events[domain.NotifyDigest] = d

	return p
}

// A digest with nothing to report on any line is skipped and not sent; so is one whose person
// turned the digest off, or left every organization, after it was queued.
func TestDigest_NothingToReportIsSkipped(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name   string
		setup  func(t *testing.T, h digestHarness)
		reason string
	}{
		{"nothing to report", func(_ *testing.T, h digestHarness) {
			h.digests.SetSummary(h.orgs[0], ann, ports.DigestSummary{Address: "ann@example.com", OrgSlug: "acme", OrgName: "Acme"})
		}, reasonNothingToReport},
		{"digest turned off", func(t *testing.T, h digestHarness) {
			h.member(h.orgs[0], ann, "ann@example.com")

			p := domain.DefaultNotificationPreferences()
			d := p.Events[domain.NotifyDigest]
			d.Email = false
			p.Events[domain.NotifyDigest] = d

			if err := h.prefs.SetNotificationPreferences(t.Context(), ann, p); err != nil {
				t.Fatal(err)
			}
		}, reasonNotQualified},
		{"no longer a member", func(*testing.T, digestHarness) {}, reasonNotQualified},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			h := newDigestJob(t, time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC))
			h.digests.SetRecipients(h.orgs[0], emailRecipient(ann, "UTC"))
			c.setup(t, h)

			if r := h.round(t); r.Queued != 1 || r.Finished != 1 {
				t.Fatalf("round = %+v; want one digest queued and finished", r)
			}

			if rows := h.emailDigests(); len(rows) != 1 || rows[0].State != ports.NotificationSkipped || rows[0].LastError != c.reason {
				t.Fatalf("digests = %+v; want one skipped with %s", rows, c.reason)
			}

			if n := len(h.mailer.Sent()); n != 0 {
				t.Errorf("sent %d emails; want none", n)
			}
		})
	}
}

// A send that fails for now goes back to the queue with the outbox's backoff and is sent on a
// later round; a permanent failure fails the digest.
func TestDigest_RetriesLikeTheOutbox(t *testing.T) {
	t.Parallel()

	h := newDigestJob(t, time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC))
	h.digests.SetRecipients(h.orgs[0], emailRecipient(ann, "UTC"), emailRecipient(bob, "UTC"))
	h.member(h.orgs[0], ann, "ann@example.com")
	h.member(h.orgs[0], bob, "bob@example.com")

	fails := 1
	h.mailer.Fail = func(m ports.Message) error {
		if m.To == "bob@example.com" {
			return &ports.MailError{Code: "smtp-rcpt-550", Permanent: true}
		}

		if fails > 0 {
			fails--

			return &ports.MailError{Code: "smtp-421"}
		}

		return nil
	}

	h.round(t)

	rows := h.emailDigests()
	if len(rows) != 2 || rows[0].State != ports.NotificationPending || !rows[0].NextAttemptAt.Equal(h.clock.Now().Add(time.Minute)) ||
		rows[1].State != ports.NotificationFailed || rows[1].LastError != "smtp-rcpt-550" {
		t.Fatalf("digests = %+v; want ann's back in a minute and bob's failed", rows)
	}

	h.clock.Advance(time.Minute)
	h.round(t)

	if rows := h.emailDigests(); rows[0].State != ports.NotificationSent || len(h.mailer.Sent()) != 1 {
		t.Fatalf("digests = %+v; want ann's sent on the retry", rows)
	}
}

// Without a mailer nothing is queued for email, so no stale digests wait for one; without Slack
// nothing is queued for Slack.
func TestDigest_NoChannelQueuesNothing(t *testing.T) {
	t.Parallel()

	h := newDigestJob(t, time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC))
	h.digests.SetRecipients(h.orgs[0], ports.DigestRecipient{UserID: ann, TimeZone: "UTC", Email: true, Slack: true})

	job := NewDigestJob(DigestDeps{Orgs: h.orgs, Digests: h.digests, Preferences: h.prefs, Clock: h.clock, PublicURL: publicURL})

	r, err := job.Round(context.Background())
	if err != nil || r.Queued != 0 || len(h.digests.Rows()) != 0 {
		t.Fatalf("round without channels = %+v, %v; want nothing queued", r, err)
	}
}
