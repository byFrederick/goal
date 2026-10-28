//go:build integration

package postgres

import (
	"context"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// digestRecipients are the recipients of o, by user.
func digestRecipients(t *testing.T, dg *Digests, o outboxOrg) map[domain.UserID]ports.DigestRecipient {
	t.Helper()

	rs, err := dg.DigestRecipients(o.ctx(t))
	if err != nil {
		t.Fatal(err)
	}

	out := map[domain.UserID]ports.DigestRecipient{}
	for _, r := range rs {
		out[r.UserID] = r
	}

	return out
}

// The digest recipients are the members with the digest on, with their time zone (UTC without a
// saved one), and Slack only while the organization has a Slack connection.
func TestDigests_Recipients(t *testing.T) {
	t.Parallel()

	o := newOutboxOrg(t)
	dg := NewDigests(o.d.s)

	// The viewer turns the digest off; the approver lives in Berlin and wants it on Slack too.
	o.prefer(t, o.viewer, domain.NotifyDigest, domain.NotificationChannels{})
	berlinOnSlack(t, o)

	rs := digestRecipients(t, dg, o)
	if _, ok := rs[domain.UserID(o.viewer)]; ok || len(rs) != 6 {
		t.Fatalf("recipients = %+v; want the six members with the digest on, not the viewer", rs)
	}

	if r := rs[domain.UserID(o.owner)]; r.TimeZone != "UTC" || !r.Email || r.Slack {
		t.Errorf("owner = %+v; want the defaults: UTC, email, no Slack", r)
	}

	if r := rs[domain.UserID(o.approver)]; r.TimeZone != "Europe/Berlin" || !r.Email || r.Slack {
		t.Errorf("approver without a Slack connection = %+v; want Berlin, email and no Slack", r)
	}

	o.slackConnection(t)

	if r := digestRecipients(t, dg, o)[domain.UserID(o.approver)]; !r.Slack {
		t.Errorf("approver with a Slack connection = %+v; want Slack", r)
	}
}

// berlinOnSlack has o's approver live in Berlin with the digest on for email and Slack.
func berlinOnSlack(t *testing.T, o outboxOrg) {
	t.Helper()

	p := domain.DefaultNotificationPreferences()
	p.TimeZone = "Europe/Berlin"
	p.Events[domain.NotifyDigest] = domain.NotificationChannels{Email: true, Slack: true}

	if err := NewNotificationPreferences(o.d.s).SetNotificationPreferences(t.Context(), domain.UserID(o.approver), p); err != nil {
		t.Fatal(err)
	}
}

// queueDigests queues ds, due at the given time, and fails unless it queued want of them.
func queueDigests(t *testing.T, dg *Digests, at time.Time, want int, ds ...ports.NewDigest) {
	t.Helper()

	if n, err := dg.QueueDigests(t.Context(), ds, at); err != nil || n != want {
		t.Fatalf("QueueDigests at %v = %d, %v; want %d", at, n, err, want)
	}
}

// claimDigests claims the due digests of channel at now and fails unless there are want.
func claimDigests(t *testing.T, dg *Digests, channel string, now time.Time, want int) []ports.Digest {
	t.Helper()

	got, err := dg.ClaimDigests(t.Context(), channel, now, time.Minute, 10)
	if err != nil || len(got) != want {
		t.Fatalf("ClaimDigests of %s at %v = %+v, %v; want %d", channel, now, got, err, want)
	}

	return got
}

// finishDigest marks digest id sent at the given time under attempt attempts and fails unless the finish
// reports want.
func finishDigest(t *testing.T, dg *Digests, id int64, attempts int, at time.Time, want bool) {
	t.Helper()

	ok, err := dg.FinishDigest(t.Context(), id, attempts, ports.NotificationOutcome{State: ports.NotificationSent, At: at})
	if err != nil || ok != want {
		t.Fatalf("FinishDigest(%d, attempt %d) = %v, %v; want %v", id, attempts, ok, err, want)
	}
}

// A digest is queued once per person, local date, channel and organization whatever the calls,
// claimed under a lease and finished under its claim's attempt, and the next one covers the time
// since the last sent one.
func TestDigests_QueueOncePerLocalDate(t *testing.T) {
	t.Parallel()

	o := newOutboxOrg(t)
	dg := NewDigests(o.d.s)

	at := time.Date(2026, 10, 28, 7, 0, 0, 0, time.UTC)
	email := ports.NewDigest{UserID: domain.UserID(o.approver), LocalDate: "2026-10-28", Channel: ports.ChannelEmail}
	slack := ports.NewDigest{UserID: domain.UserID(o.approver), LocalDate: "2026-10-28", Channel: ports.ChannelSlack, OrgID: o.org}

	queueDigests(t, dg, at, 2, email, slack, email)
	queueDigests(t, dg, at.Add(5*time.Minute), 0, email, slack)

	g := claimDigests(t, dg, ports.ChannelEmail, at, 1)[0]
	if g.UserID != email.UserID || g.LocalDate != email.LocalDate || g.OrgID != "" || g.Attempts != 1 ||
		!g.PeriodStart.Equal(at.Add(-24*time.Hour)) || !g.PeriodEnd.Equal(at) {
		t.Fatalf("claimed %+v; want the approver's first email digest, covering the 24 hours before it", g)
	}

	claimDigests(t, dg, ports.ChannelEmail, at, 0) // leased

	finishDigest(t, dg, g.ID, 2, at, false) // another attempt's
	finishDigest(t, dg, g.ID, 1, at, true)

	if s := claimDigests(t, dg, ports.ChannelSlack, at, 1)[0]; s.OrgID != o.org {
		t.Fatalf("claimed Slack digest %+v; want acme's", s)
	}

	// The next day's digest covers the time since the sent one.
	next := at.Add(24 * time.Hour)
	email.LocalDate = "2026-10-29"
	queueDigests(t, dg, next, 1, email)

	if g := claimDigests(t, dg, ports.ChannelEmail, next, 1)[0]; !g.PeriodStart.Equal(at) || !g.PeriodEnd.Equal(next) {
		t.Fatalf("claimed the next day %+v; want it to cover [%v, %v)", g, at, next)
	}

	if _, err := dg.QueueDigests(t.Context(), []ports.NewDigest{{UserID: email.UserID, LocalDate: "28/10/2026", Channel: "email"}}, at); err == nil {
		t.Error("QueueDigests with a malformed date succeeded")
	}
}

// What a digest says of an organization: what Needs you lists for the person (decisions on the
// factories they may decide on), and nothing for someone who is no member.
func TestDigests_SummaryNeedsYou(t *testing.T) {
	t.Parallel()

	o := newOutboxOrg(t)
	dg := NewDigests(o.d.s)
	ctx := o.ctx(t)

	if err := o.toWaiting(t, o.item(t), nil, domain.EventWaitingHuman); err != nil {
		t.Fatal(err)
	}

	until := time.Now().Add(time.Minute)
	since := until.Add(-24 * time.Hour)

	for _, c := range []struct {
		user      string
		decisions int
	}{{o.owner, 1}, {o.approver, 1}, {o.editor, 1}, {o.viewer, 0}, {o.otherApprover, 0}} {
		s, err := dg.DigestSummary(ctx, domain.UserID(c.user), since, until)
		if err != nil {
			t.Fatal(err)
		}

		want := ports.DigestSummary{
			Member: true, OrgRole: s.OrgRole, Address: s.Address, OrgSlug: s.OrgSlug, OrgName: s.OrgName, NeedsYouDecisions: c.decisions,
		}
		if !reflect.DeepEqual(s, want) || s.Address == "" || s.OrgSlug == "" {
			t.Errorf("summary of %s = %+v; want a member with %d decisions and nothing else", c.user, s, c.decisions)
		}
	}

	stranger := o.d.user("Stan")

	s, err := dg.DigestSummary(ctx, domain.UserID(stranger), since, until)
	if err != nil || s.Member || s.Address != "" || !s.Empty() {
		t.Errorf("summary of a stranger = %+v, %v; want no member and nothing", s, err)
	}
}

// The digest lock is held by one session at a time: a second caller is skipped while the first
// runs, and takes it once the first is done.
func TestDigests_LockHeldByOneSession(t *testing.T) {
	t.Parallel()

	o := newOutboxOrg(t)
	first, second := NewDigests(o.d.s), NewDigests(New(o.d.app))

	var inner []bool

	ran, err := first.WithDigestLock(t.Context(), func(ctx context.Context) error {
		r, err := second.WithDigestLock(ctx, func(context.Context) error {
			t.Error("the second caller ran while the first held the lock")

			return nil
		})
		inner = append(inner, r)

		return err
	})
	if err != nil || !ran || !slices.Equal(inner, []bool{false}) {
		t.Fatalf("WithDigestLock = %v, %v (inner %v); want the first to run and the second skipped", ran, err, inner)
	}

	ran, err = second.WithDigestLock(t.Context(), func(context.Context) error { return nil })
	if err != nil || !ran {
		t.Fatalf("WithDigestLock after the first = %v, %v; want it to run", ran, err)
	}
}
