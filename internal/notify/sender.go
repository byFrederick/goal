// Package notify sends the notification outbox's email and Slack rows (ADR 0023, decisions 3, 4
// and 5): every goald instance runs one Sender, which claims due rows of each live organization
// under a lease, renders each event's message and sends it through ports.Mailer, or as a direct
// message through ports.DirectMessenger with the token held by the secret the organization's
// notification settings name (ADR 0037, Consequences). It keeps no state of its own, so a row whose sender died returns to the queue when
// its lease ends.
package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The claim and retry rules of ADR 0023, decision 3.
const (
	// BatchSize is how many rows one claim takes, per organization and round.
	BatchSize = 50
	// Lease is how long a claim holds its rows.
	Lease = 2 * time.Minute
	// SendTimeout is the longest one send may take, whatever the mailer's own timeout.
	SendTimeout = 30 * time.Second
	// finishTime is what a row's finish may take after its send, so the claim is recorded
	// before the lease ends.
	finishTime = 10 * time.Second
	// MaxAttempts is how many sends a row gets before it fails.
	MaxAttempts = 8
	// maxBackoff caps the wait before a retry.
	maxBackoff = time.Hour
)

// The reasons a row ends with, besides a ports.MailError's or ports.DMError's code. None names an
// address.
const (
	reasonNotQualified = "recipient-not-qualified"
	reasonUnknownEvent = "unknown-event"
	reasonMailError    = "mail-error"
	reasonSlackError   = "slack-error"
)

// The Slack outcomes that come from the organization's notification setting and secrets, not
// from Slack. None names the secret or holds its value. The first two are not retried, since an
// admin must choose or fix the secret first; a store error is.
var (
	errSlackNotSet = &ports.DMError{Code: "slack-not-set", Permanent: true}
	errSlackToken  = &ports.DMError{Code: "slack-token-unavailable", Permanent: true}
	errSlackSecret = &ports.DMError{Code: "slack-secret-error"}
)

// Orgs lists the live organizations; ports.Tenancy satisfies it.
type Orgs interface {
	ActiveOrgs(ctx context.Context) ([]domain.OrgID, error)
}

// Secrets opens the organization's secrets; ports.OrgSecrets satisfies it.
type Secrets interface {
	Resolve(ctx context.Context, names []string, use domain.SecretUse) ([]domain.ResolvedSecret, error)
}

// Deps are what a Sender works through. Email rows are claimed only with a Mailer, and Slack rows
// only with Slack, Settings and Secrets: the Slack token is the value of the secret Settings
// names, opened through Secrets. A channel without them stays pending for an instance that has
// them.
type Deps struct {
	Orgs     Orgs
	Outbox   ports.NotificationDeliveries
	Mailer   ports.Mailer
	Slack    ports.DirectMessenger
	Settings ports.NotificationSettings
	Secrets  Secrets
	Clock    ports.Clock
	// PublicURL is the origin every link starts with (GOAL_PUBLIC_URL).
	PublicURL string
	// Log may be nil.
	Log *slog.Logger
}

// Sender is the notification sender job.
type Sender struct {
	d        Deps
	log      *slog.Logger
	channels []string
}

// New returns a sender over d.
func New(d Deps) *Sender {
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	var channels []string
	if d.Mailer != nil {
		channels = append(channels, ports.ChannelEmail)
	}

	if slackReady(d.Slack, d.Settings, d.Secrets) {
		channels = append(channels, ports.ChannelSlack)
	}

	return &Sender{d: d, log: log, channels: channels}
}

// Run works the outbox at once and then each time every passes, until ctx ends; failures are
// logged and retried on the next round.
func (s *Sender) Run(ctx context.Context, every time.Duration) {
	t := s.d.Clock.NewTicker(every)
	defer t.Stop()

	for {
		if _, err := s.Work(ctx); err != nil && ctx.Err() == nil {
			s.log.WarnContext(ctx, "notify work", slog.Any("error", err))
		}

		select {
		case <-ctx.Done():
			return
		case <-t.C():
		}
	}
}

// Work claims one batch of due rows of each channel it can send, of every live organization, and
// finishes each, and returns how many it finished. An organization that fails does not hold up
// the others.
func (s *Sender) Work(ctx context.Context) (int, error) {
	orgs, err := s.d.Orgs.ActiveOrgs(ctx)
	if err != nil {
		return 0, fmt.Errorf("notify work: list organizations: %w", err)
	}

	var (
		done int
		errs []error
	)

	for _, org := range orgs {
		if ctx.Err() != nil {
			break
		}

		for _, channel := range s.channels {
			n, err := s.workOrg(domain.WithOrg(ctx, org), channel)
			done += n

			if err != nil {
				errs = append(errs, fmt.Errorf("notify work of org %s, channel %s: %w", org, channel, err))
			}
		}
	}

	return done, errors.Join(errs...)
}

// workOrg claims and finishes one batch of channel of the organization in ctx. It starts a row only when its
// send and finish fit inside the claim's lease, so no other instance can claim a row while this
// one may still be mailing it; the rows it does not reach go back to the queue at once.
func (s *Sender) workOrg(ctx context.Context, channel string) (int, error) {
	claimed := s.d.Clock.Now()
	leaseEnd := claimed.Add(Lease)

	rows, err := s.d.Outbox.ClaimNotifications(ctx, channel, claimed, Lease, BatchSize)
	if err != nil {
		return 0, fmt.Errorf("claim: %w", err)
	}

	done := 0
	token := &tokenOnce{}

	for i, n := range rows {
		if err := ctx.Err(); err != nil {
			// The lease returns the rest to the queue.
			return done, err
		}

		if s.d.Clock.Now().Add(SendTimeout + finishTime).After(leaseEnd) {
			return done, s.release(ctx, rows[i:])
		}

		finished, ferr := s.finish(ctx, n, token)
		if ferr != nil {
			return done, ferr
		}

		if finished {
			done++
		}
	}

	return done, nil
}

// release ends the claim of rows this round did not send, due again now, so the next round of any
// instance takes them without waiting for the lease. Their claim still counted as an attempt.
func (s *Sender) release(ctx context.Context, rows []ports.Notification) error {
	now := s.d.Clock.Now()

	for _, n := range rows {
		o := ports.NotificationOutcome{State: ports.NotificationPending, RetryAt: now}
		if _, err := s.d.Outbox.FinishNotification(ctx, n.ID, n.Attempts, o); err != nil {
			return fmt.Errorf("release notification %d: %w", n.ID, err)
		}
	}

	return nil
}

// finish delivers n and ends its claim, and reports whether the claim was still its own: a
// later claim that took the row after this lease ran out reports it instead.
func (s *Sender) finish(ctx context.Context, n ports.Notification, token *tokenOnce) (bool, error) {
	o := s.deliver(ctx, n, token)

	finished, err := s.d.Outbox.FinishNotification(ctx, n.ID, n.Attempts, o)
	if err != nil {
		return false, fmt.Errorf("finish notification %d: %w", n.ID, err)
	}

	if finished {
		logOutcome(ctx, s.log, n.ID, string(n.Event), n.Channel, n.Attempts, o)
	}

	return finished, nil
}

// deliver sends n when its recipient still qualifies and says how its row ends.
func (s *Sender) deliver(ctx context.Context, n ports.Notification, token *tokenOnce) ports.NotificationOutcome {
	if !n.Qualifies || n.Address == "" {
		return ports.NotificationOutcome{State: ports.NotificationSkipped, Reason: reasonNotQualified}
	}

	m, err := Render(n, s.d.PublicURL)
	if err != nil {
		return ports.NotificationOutcome{State: ports.NotificationFailed, Reason: reasonUnknownEvent}
	}

	sendCtx, cancel := context.WithTimeout(ctx, SendTimeout)
	if n.Channel == ports.ChannelSlack {
		err = s.sendSlack(sendCtx, m, token)
	} else {
		err = s.d.Mailer.Send(sendCtx, m)
	}

	cancel()

	return sendOutcome(n.Channel, n.Attempts, err, s.d.Clock.Now())
}

// sendOutcome is how a row of channel ends after the send of its attempt-th claim returned err
// at now: sent, failed when the error is permanent or the attempts are spent, else back to
// pending after the backoff.
func sendOutcome(channel string, attempt int, err error, now time.Time) ports.NotificationOutcome {
	switch {
	case err == nil:
		return ports.NotificationOutcome{State: ports.NotificationSent, At: now}
	case errors.Is(err, ports.ErrMailPermanent) || errors.Is(err, ports.ErrDMPermanent) || attempt >= MaxAttempts:
		return ports.NotificationOutcome{State: ports.NotificationFailed, Reason: reason(channel, err)}
	default:
		return ports.NotificationOutcome{State: ports.NotificationPending, RetryAt: now.Add(backoff(attempt)), Reason: reason(channel, err)}
	}
}

// sendSlack sends m's text to m.To as a direct message with the organization's Slack token.
func (s *Sender) sendSlack(ctx context.Context, m ports.Message, token *tokenOnce) error {
	t, err := token.get(ctx, s.d.Settings, s.d.Secrets)
	if err != nil {
		return err
	}

	return s.d.Slack.Send(ctx, t, ports.DirectMessage{To: m.To, Text: m.Text})
}

// slackReady reports whether a job can send Slack messages: it has a messenger, the
// notification settings and the secrets to open.
func slackReady(slack ports.DirectMessenger, settings ports.NotificationSettings, secrets Secrets) bool {
	return slack != nil && settings != nil && secrets != nil
}

// tokenOnce is the Slack token of one organization and round, looked up at its first Slack row.
type tokenOnce struct {
	done  bool
	token string
	err   error
}

func (o *tokenOnce) get(ctx context.Context, settings ports.NotificationSettings, secrets Secrets) (string, error) {
	if !o.done {
		o.token, o.err = slackTokenOf(ctx, settings, secrets)
		// A failed lookup is tried again at the next row; a missing setting or secret is not.
		o.done = o.err == nil || errors.Is(o.err, ports.ErrDMPermanent)
	}

	return o.token, o.err
}

// slackTokenOf is the value of the secret the Slack notification setting of the organization in
// ctx names (ADR 0037, Consequences), opened without a factory and audited as secret.used. A
// secret that is gone, open to some factories only, or that goald cannot open without a master
// key fails the row for good; any other error is temporary. No error names the secret.
func slackTokenOf(ctx context.Context, settings ports.NotificationSettings, secrets Secrets) (string, error) {
	set, err := settings.NotificationSettings(ctx)
	if err != nil {
		return "", errSlackSecret
	}

	if set.SlackSecret == "" {
		return "", errSlackNotSet
	}

	opened, err := secrets.Resolve(ctx, []string{set.SlackSecret}, domain.SecretUse{})

	switch {
	case errors.Is(err, ports.ErrNotFound) || errors.Is(err, ports.ErrSecretNotAllowed) || errors.Is(err, ports.ErrNoMasterKey):
		return "", errSlackToken
	case err != nil:
		return "", errSlackSecret
	case len(opened) != 1 || opened[0].Value == "":
		return "", errSlackToken
	default:
		return opened[0].Value, nil
	}
}

// logOutcome logs a sent or failed row by its id, event and channel only: never an address, a
// message or the error's text, which may quote the server's reply.
func logOutcome(ctx context.Context, log *slog.Logger, id int64, event, channel string, attempts int, o ports.NotificationOutcome) {
	attrs := []any{slog.Int64("id", id), slog.String("event", event), slog.String("channel", channel)}

	switch o.State {
	case ports.NotificationSent:
		log.InfoContext(ctx, "notify.sent", attrs...)
	case ports.NotificationFailed, ports.NotificationPending:
		log.WarnContext(ctx, "notify.failed", append(attrs, slog.String("reason", o.Reason), slog.Int("attempts", attempts),
			slog.Bool("retry", o.State == ports.NotificationPending))...)
	}
}

// reason is err's short code: a MailError's or a DMError's, else the channel's fixed one.
func reason(channel string, err error) string {
	if me, ok := errors.AsType[*ports.MailError](err); ok && me.Code != "" {
		return me.Code
	}

	if de, ok := errors.AsType[*ports.DMError](err); ok && de.Code != "" {
		return de.Code
	}

	if channel == ports.ChannelSlack {
		return reasonSlackError
	}

	return reasonMailError
}

// backoff is the wait after the given attempt failed: 1, 2, 4… minutes, at most maxBackoff.
func backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}

	if attempt > 7 {
		return maxBackoff
	}

	return min(time.Minute<<(attempt-1), maxBackoff)
}
