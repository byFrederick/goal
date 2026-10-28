//go:build integration

package postgres

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The digest recipients are the members with the digest on, with their time zone (UTC without a
// saved one) and Slack only while the organization has a Slack connection; a digest is queued
// once per person, local date, channel and organization whatever the calls, and the next one
// covers the time since the last sent one.
func TestDigests_QueueOncePerLocalDate(t *testing.T) {
	t.Parallel()

	o := newOutboxOrg(t)
	dg := NewDigests(o.d.s)
	ctx := o.ctx(t)

	// The viewer turns the digest off; the approver lives in Berlin and wants it on Slack too.
	o.prefer(t, o.viewer, domain.NotifyDigest, domain.NotificationChannels{})

	berlin := domain.DefaultNotificationPreferences()
	berlin.TimeZone = "Europe/Berlin"
	berlin.Events[domain.NotifyDigest] = domain.NotificationChannels{Email: true, Slack: true}

	if err := NewNotificationPreferences(o.d.s).SetNotificationPreferences(t.Context(), domain.UserID(o.approver), berlin); err != nil {
		t.Fatal(err)
	}

	recipients := func() map[domain.UserID]ports.DigestRecipient {
		t.Helper()

		rs, err := dg.DigestRecipients(ctx)
		if err != nil {
			t.Fatal(err)
		}

		out := map[domain.UserID]ports.DigestRecipient{}
		for _, r := range rs {
			out[r.UserID] = r
		}

		return out
	}

	rs := recipients()
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

	if r := recipients()[domain.UserID(o.approver)]; !r.Slack {
		t.Errorf("approver with a Slack connection = %+v; want Slack", r)
	}

	at := time.Date(2026, 10, 28, 7, 0, 0, 0, time.UTC)
	email := ports.NewDigest{UserID: domain.UserID(o.approver), LocalDate: "2026-10-28", Channel: ports.ChannelEmail}
	slack := ports.NewDigest{UserID: domain.UserID(o.approver), LocalDate: "2026-10-28", Channel: ports.ChannelSlack, OrgID: o.org}

	if n, err := dg.QueueDigests(t.Context(), []ports.NewDigest{email, slack, email}, at); err != nil || n != 2 {
		t.Fatalf("QueueDigests = %d, %v; want the email and the Slack digest", n, err)
	}

	if n, err := dg.QueueDigests(t.Context(), []ports.NewDigest{email, slack}, at.Add(5*time.Minute)); err != nil || n != 0 {
		t.Fatalf("QueueDigests again = %d, %v; want nothing", n, err)
	}

	claimed, err := dg.ClaimDigests(t.Context(), ports.ChannelEmail, at, time.Minute, 10)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("ClaimDigests = %+v, %v; want the email digest", claimed, err)
	}

	g := claimed[0]
	if g.UserID != email.UserID || g.LocalDate != "2026-10-28" || g.OrgID != "" || g.Attempts != 1 ||
		!g.PeriodStart.Equal(at.Add(-24*time.Hour)) || !g.PeriodEnd.Equal(at) {
		t.Fatalf("claimed %+v; want the approver's first email digest, covering the 24 hours before it", g)
	}

	if again, err := dg.ClaimDigests(t.Context(), ports.ChannelEmail, at, time.Minute, 10); err != nil || len(again) != 0 {
		t.Fatalf("ClaimDigests while leased = %+v, %v; want none", again, err)
	}

	if ok, err := dg.FinishDigest(t.Context(), g.ID, 2, ports.NotificationOutcome{State: ports.NotificationSent, At: at}); err != nil || ok {
		t.Fatalf("FinishDigest under another attempt = %v, %v; want false", ok, err)
	}

	if ok, err := dg.FinishDigest(t.Context(), g.ID, 1, ports.NotificationOutcome{State: ports.NotificationSent, At: at}); err != nil || !ok {
		t.Fatalf("FinishDigest = %v, %v; want true", ok, err)
	}

	slackRows, err := dg.ClaimDigests(t.Context(), ports.ChannelSlack, at, time.Minute, 10)
	if err != nil || len(slackRows) != 1 || slackRows[0].OrgID != o.org {
		t.Fatalf("ClaimDigests of Slack = %+v, %v; want acme's", slackRows, err)
	}

	// The next day's digest covers the time since the sent one.
	next := at.Add(24 * time.Hour)
	email.LocalDate = "2026-10-29"

	if n, err := dg.QueueDigests(t.Context(), []ports.NewDigest{email}, next); err != nil || n != 1 {
		t.Fatalf("QueueDigests the next day = %d, %v; want one", n, err)
	}

	claimed, err = dg.ClaimDigests(t.Context(), ports.ChannelEmail, next, time.Minute, 10)
	if err != nil || len(claimed) != 1 || !claimed[0].PeriodStart.Equal(at) || !claimed[0].PeriodEnd.Equal(next) {
		t.Fatalf("claimed the next day %+v, %v; want it to cover [%v, %v)", claimed, err, at, next)
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

		if !s.Member || s.Address == "" || s.OrgSlug == "" || s.NeedsYouDecisions != c.decisions || s.Done != 0 || len(s.Spend) != 0 {
			t.Errorf("summary of %s = %+v; want a member with %d decisions", c.user, s, c.decisions)
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
