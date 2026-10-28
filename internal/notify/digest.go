package notify

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// DigestHour is the local hour at or after which a person's daily digest is due (ADR 0023,
// decision 6); the account screen fixes it.
const DigestHour = 8

// reasonNothingToReport ends a digest whose every line is zero: nobody gets an empty digest.
const reasonNothingToReport = "nothing-to-report"

// DigestDeps are what a DigestJob works through. Email digests are queued and sent only with a
// Mailer, and Slack digests only with Slack and Connections, as the Sender's rows are.
type DigestDeps struct {
	Orgs        Orgs
	Digests     ports.Digests
	Preferences ports.NotificationPreferences
	Mailer      ports.Mailer
	Slack       ports.DirectMessenger
	Connections SlackConnections
	Clock       ports.Clock
	// PublicURL is the origin every link starts with (GOAL_PUBLIC_URL).
	PublicURL string
	// Log may be nil.
	Log *slog.Logger
}

// DigestJob is the daily digest job (ADR 0023, decision 6). Every goald instance runs one; a
// round runs only in the instance that takes the digest lock, and the others skip it. A round
// queues the digest of every person whose local time has reached DigestHour on a date they have
// none for yet, then claims and sends the due digests under the outbox's lease and retry rules.
type DigestJob struct {
	d        DigestDeps
	log      *slog.Logger
	channels []string
}

// DigestRound is what one round did.
type DigestRound struct {
	Ran      bool // whether this instance held the lock
	Queued   int
	Finished int
}

// NewDigestJob returns the digest job over d.
func NewDigestJob(d DigestDeps) *DigestJob {
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	var channels []string
	if d.Mailer != nil {
		channels = append(channels, ports.ChannelEmail)
	}

	if d.Slack != nil && d.Connections != nil {
		channels = append(channels, ports.ChannelSlack)
	}

	return &DigestJob{d: d, log: log, channels: channels}
}

// Run runs a round at once and then each time every passes, until ctx ends; failures are logged
// and the next round tries again.
func (j *DigestJob) Run(ctx context.Context, every time.Duration) {
	t := j.d.Clock.NewTicker(every)
	defer t.Stop()

	for {
		if _, err := j.Round(ctx); err != nil && ctx.Err() == nil {
			j.log.WarnContext(ctx, "notify digest", slog.Any("error", err))
		}

		select {
		case <-ctx.Done():
			return
		case <-t.C():
		}
	}
}

// Round queues the due digests and sends a batch of each channel, under the digest lock.
func (j *DigestJob) Round(ctx context.Context) (DigestRound, error) {
	var r DigestRound

	ran, err := j.d.Digests.WithDigestLock(ctx, func(ctx context.Context) error {
		var qerr, serr error

		r.Queued, qerr = j.queue(ctx)
		r.Finished, serr = j.send(ctx)

		return errors.Join(qerr, serr)
	})
	r.Ran = ran

	if err != nil {
		return r, fmt.Errorf("digest round: %w", err)
	}

	return r, nil
}

// queue queues, for every member of every live organization with the digest on, the digest of
// their local date once their local time is at or past DigestHour: one email digest across their
// organizations and one Slack digest per organization with a Slack connection. The store keeps a
// digest already queued for the date, so every round may ask again.
func (j *DigestJob) queue(ctx context.Context) (int, error) {
	if len(j.channels) == 0 {
		return 0, nil
	}

	now := j.d.Clock.Now()

	orgs, err := j.d.Orgs.ActiveOrgs(ctx)
	if err != nil {
		return 0, fmt.Errorf("queue digests: list organizations: %w", err)
	}

	var (
		ds    []ports.NewDigest
		email = map[domain.UserID]bool{}
		errs  []error
	)

	for _, org := range orgs {
		if ctx.Err() != nil {
			break
		}

		rs, err := j.d.Digests.DigestRecipients(domain.WithOrg(ctx, org))
		if err != nil {
			errs = append(errs, fmt.Errorf("digest recipients of org %s: %w", org, err))

			continue
		}

		for _, r := range rs {
			date, due := localDate(now, r.TimeZone)
			if !due {
				continue
			}

			if r.Email && j.d.Mailer != nil && !email[r.UserID] {
				email[r.UserID] = true
				ds = append(ds, ports.NewDigest{UserID: r.UserID, LocalDate: date, Channel: ports.ChannelEmail})
			}

			if r.Slack && j.d.Slack != nil && j.d.Connections != nil {
				ds = append(ds, ports.NewDigest{UserID: r.UserID, LocalDate: date, Channel: ports.ChannelSlack, OrgID: org})
			}
		}
	}

	n, err := j.d.Digests.QueueDigests(ctx, ds, now)
	if err != nil {
		errs = append(errs, fmt.Errorf("queue digests: %w", err))
	}

	return n, errors.Join(errs...)
}

// localDate is the date at now in the IANA zone tz, UTC when tz is blank or one Go cannot load,
// and whether the local time there is at or past DigestHour. A day whose 08:00 a clock change
// skips is due from the first instant after it.
func localDate(now time.Time, tz string) (string, bool) {
	loc, err := time.LoadLocation(tz)
	if err != nil || tz == "" {
		loc = time.UTC
	}

	local := now.In(loc)

	return local.Format(time.DateOnly), local.Hour() >= DigestHour
}

// send claims one batch of due digests of each channel and finishes each.
func (j *DigestJob) send(ctx context.Context) (int, error) {
	var (
		done int
		errs []error
	)

	for _, channel := range j.channels {
		if ctx.Err() != nil {
			break
		}

		n, err := j.sendChannel(ctx, channel)
		done += n

		if err != nil {
			errs = append(errs, fmt.Errorf("send %s digests: %w", channel, err))
		}
	}

	return done, errors.Join(errs...)
}

// sendChannel claims and finishes one batch of channel, starting a digest only when its send
// and finish fit in the lease, as Sender.workOrg does.
func (j *DigestJob) sendChannel(ctx context.Context, channel string) (int, error) {
	claimed := j.d.Clock.Now()
	leaseEnd := claimed.Add(Lease)

	rows, err := j.d.Digests.ClaimDigests(ctx, channel, claimed, Lease, BatchSize)
	if err != nil {
		return 0, fmt.Errorf("claim: %w", err)
	}

	done := 0
	tokens := map[domain.OrgID]*tokenOnce{}

	for i, g := range rows {
		if err := ctx.Err(); err != nil {
			return done, err
		}

		if j.d.Clock.Now().Add(SendTimeout + finishTime).After(leaseEnd) {
			return done, j.release(ctx, rows[i:])
		}

		if tokens[g.OrgID] == nil {
			tokens[g.OrgID] = &tokenOnce{}
		}

		o, err := j.deliver(ctx, g, tokens[g.OrgID])
		if err != nil {
			// The lease returns it, and the rest, to the queue.
			return done, fmt.Errorf("digest %d: %w", g.ID, err)
		}

		finished, err := j.d.Digests.FinishDigest(ctx, g.ID, g.Attempts, o)
		if err != nil {
			return done, fmt.Errorf("finish digest %d: %w", g.ID, err)
		}

		if finished {
			done++

			logOutcome(ctx, j.log, g.ID, string(domain.NotifyDigest), g.Channel, g.Attempts, o)
		}
	}

	return done, nil
}

// release ends the claim of digests this round did not send, due again now.
func (j *DigestJob) release(ctx context.Context, rows []ports.Digest) error {
	now := j.d.Clock.Now()

	for _, g := range rows {
		o := ports.NotificationOutcome{State: ports.NotificationPending, RetryAt: now}
		if _, err := j.d.Digests.FinishDigest(ctx, g.ID, g.Attempts, o); err != nil {
			return fmt.Errorf("release digest %d: %w", g.ID, err)
		}
	}

	return nil
}

// deliver sends digest g when its person still has its channel on and is still a member, and
// it has something to report, and says how it ends. An error is the store's: the digest stays
// claimed until its lease ends.
func (j *DigestJob) deliver(ctx context.Context, g ports.Digest, token *tokenOnce) (ports.NotificationOutcome, error) {
	prefs, err := j.d.Preferences.NotificationPreferences(ctx, g.UserID)
	if err != nil {
		return ports.NotificationOutcome{}, fmt.Errorf("preferences: %w", err)
	}

	on := prefs.Events[domain.NotifyDigest]
	if (g.Channel == ports.ChannelEmail && !on.Email) || (g.Channel == ports.ChannelSlack && !on.Slack) {
		return ports.NotificationOutcome{State: ports.NotificationSkipped, Reason: reasonNotQualified}, nil
	}

	orgs := []domain.OrgID{g.OrgID}
	if g.Channel == ports.ChannelEmail {
		if orgs, err = j.d.Orgs.ActiveOrgs(ctx); err != nil {
			return ports.NotificationOutcome{}, fmt.Errorf("list organizations: %w", err)
		}
	}

	var sums []ports.DigestSummary

	for _, org := range orgs {
		s, err := j.d.Digests.DigestSummary(domain.WithOrg(ctx, org), g.UserID, g.PeriodStart, g.PeriodEnd)
		if err != nil {
			return ports.NotificationOutcome{}, fmt.Errorf("summary of org %s: %w", org, err)
		}

		if s.Member && s.Address != "" {
			sums = append(sums, s)
		}
	}

	if len(sums) == 0 {
		return ports.NotificationOutcome{State: ports.NotificationSkipped, Reason: reasonNotQualified}, nil
	}

	m, ok := RenderDigest(g, sums, j.d.PublicURL)
	if !ok {
		return ports.NotificationOutcome{State: ports.NotificationSkipped, Reason: reasonNothingToReport}, nil
	}

	sendCtx, cancel := context.WithTimeout(ctx, SendTimeout)
	if g.Channel == ports.ChannelSlack {
		err = j.sendSlack(domain.WithOrg(sendCtx, g.OrgID), m, token)
	} else {
		err = j.d.Mailer.Send(sendCtx, m)
	}

	cancel()

	return sendOutcome(g.Channel, g.Attempts, err, j.d.Clock.Now()), nil
}

// sendSlack sends m's text to m.To as a direct message with the token of the organization in
// ctx.
func (j *DigestJob) sendSlack(ctx context.Context, m ports.Message, token *tokenOnce) error {
	t, err := token.get(ctx, j.d.Connections)
	if err != nil {
		return err
	}

	return j.d.Slack.Send(ctx, t, ports.DirectMessage{To: m.To, Text: m.Text})
}

// RenderDigest returns digest g's message from the summaries of its organizations, to the
// address of the first, and false when no organization has anything to report: then nothing is
// sent. An organization with nothing to report is left out of it. Every link is publicURL plus a
// path of escaped segments; the HTML copy escapes every name and loads nothing.
func RenderDigest(g ports.Digest, sums []ports.DigestSummary, publicURL string) (ports.Message, bool) {
	base := strings.TrimRight(publicURL, "/")

	var (
		text strings.Builder
		body strings.Builder
		to   string
	)

	for _, s := range sums {
		if to == "" {
			to = s.Address
		}

		if s.Empty() {
			continue
		}

		org := base + "/o/" + url.PathEscape(s.OrgSlug)
		lines := []struct{ says, link string }{
			{"Needs you: " + count(s.NeedsYouDecisions, "decision") + " and " + count(s.NeedsYouAndons, "andon") + ".", org + "/needs-you"},
			{"Items done: " + strconv.Itoa(s.Done) + ".", org},
			{"Andons pulled: " + strconv.Itoa(s.AndonsPulled) + ".", org},
			{"Spend: " + spend(s.Spend) + ".", org + "/spend"},
		}

		name := clean(s.OrgName)
		text.WriteString("\n" + name + "\n")
		body.WriteString("<h2>" + html.EscapeString(name) + "</h2>\n<ul>\n")

		for _, l := range lines {
			text.WriteString("- " + l.says + " " + l.link + "\n")
			body.WriteString(`<li>` + html.EscapeString(l.says) + ` <a href="` + html.EscapeString(l.link) + `">Open</a></li>` + "\n")
		}

		body.WriteString("</ul>\n")
	}

	if text.Len() == 0 {
		return ports.Message{}, false
	}

	account := base + "/account"
	intro := "Your Goal digest for " + g.LocalDate + ", since " + g.PeriodStart.UTC().Format("2006-01-02 15:04") + " UTC."

	footer := "You get this email because of your notification settings."
	if g.Channel == ports.ChannelSlack {
		footer = "You get this message because of your notification settings."
	}

	return ports.Message{
		To:      to,
		Subject: "Your Goal digest for " + g.LocalDate,
		Text:    intro + "\n" + text.String() + "\n-- \n" + footer + " Change them at " + account + "\n",
		HTML: "<!doctype html>\n<html><body>\n<p>" + html.EscapeString(intro) + "</p>\n" + body.String() +
			"<p>" + html.EscapeString(footer) + ` <a href="` + html.EscapeString(account) + `">Change them</a>.</p>` + "\n" +
			"</body></html>\n",
	}, true
}

// count is "1 decision" or "n decisions".
func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}

	return strconv.Itoa(n) + " " + noun + "s"
}

// spend is the period's total and each factory's part, the largest first.
func spend(fs []ports.FactorySpend) string {
	if len(fs) == 0 {
		return "$0.00"
	}

	total := 0.0
	parts := make([]string, 0, len(fs))

	for _, f := range fs {
		total += f.USD
		parts = append(parts, fmt.Sprintf("%s $%.2f", clean(f.FactoryTitle), f.USD))
	}

	return fmt.Sprintf("$%.2f (%s)", total, strings.Join(parts, ", "))
}
