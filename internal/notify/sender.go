// Package notify sends the notification outbox's email and Slack rows (ADR 0023, decisions 3, 4
// and 5): every goald instance runs one Sender, which claims due rows of each live organization
// under a lease, renders each event's message and sends it through ports.Mailer, or as a direct
// message through ports.DirectMessenger with the token of the organization's oldest Slack
// connection. It keeps no state of its own, so a row whose sender died returns to the queue when
// its lease ends.
package notify

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
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

// The connection kind whose connections send Slack rows (internal/connkind/kinds/slack.yaml), and
// the field that holds its bot token.
const (
	slackKind  = "slack"
	slackToken = "token"
)

// The Slack outcomes that come from the organization's connections, not from Slack. Neither is
// retried: an admin must add or change a connection first.
var (
	errNoSlackConnection = &ports.DMError{Code: "slack-no-connection", Permanent: true}
	errSlackToken        = &ports.DMError{Code: "slack-token-unavailable", Permanent: true}
	errSlackConnection   = &ports.DMError{Code: "slack-connection-error"}
)

// Orgs lists the live organizations; ports.Tenancy satisfies it.
type Orgs interface {
	ActiveOrgs(ctx context.Context) ([]domain.OrgID, error)
}

// SlackConnections finds and opens the organization's Slack connection; ports.Connections
// satisfies it.
type SlackConnections interface {
	List(ctx context.Context) ([]domain.Connection, error)
	Open(ctx context.Context, name string) (domain.ResolvedConnection, error)
}

// Deps are what a Sender works through. Email rows are claimed only with a Mailer, and Slack rows
// only with Slack and Connections; a channel without them stays pending for an instance that has
// them.
type Deps struct {
	Orgs        Orgs
	Outbox      ports.NotificationDeliveries
	Mailer      ports.Mailer
	Slack       ports.DirectMessenger
	Connections SlackConnections
	Clock       ports.Clock
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

	if d.Slack != nil && d.Connections != nil {
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
		s.logOutcome(ctx, n, o)
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

	now := s.d.Clock.Now()

	switch {
	case err == nil:
		return ports.NotificationOutcome{State: ports.NotificationSent, At: now}
	case errors.Is(err, ports.ErrMailPermanent) || errors.Is(err, ports.ErrDMPermanent) || n.Attempts >= MaxAttempts:
		return ports.NotificationOutcome{State: ports.NotificationFailed, Reason: reason(n.Channel, err)}
	default:
		return ports.NotificationOutcome{State: ports.NotificationPending, RetryAt: now.Add(backoff(n.Attempts)), Reason: reason(n.Channel, err)}
	}
}

// sendSlack sends m's text to m.To as a direct message with the organization's Slack token.
func (s *Sender) sendSlack(ctx context.Context, m ports.Message, token *tokenOnce) error {
	t, err := token.get(ctx, s.d.Connections)
	if err != nil {
		return err
	}

	return s.d.Slack.Send(ctx, t, ports.DirectMessage{To: m.To, Text: m.Text})
}

// tokenOnce is the Slack token of one claimed batch, looked up at its first Slack row.
type tokenOnce struct {
	done  bool
	token string
	err   error
}

func (o *tokenOnce) get(ctx context.Context, conns SlackConnections) (string, error) {
	if !o.done {
		o.token, o.err = slackTokenOf(ctx, conns)
		// A failed lookup is tried again at the next row; a missing connection is not.
		o.done = o.err == nil || errors.Is(o.err, ports.ErrDMPermanent)
	}

	return o.token, o.err
}

// slackTokenOf is the bot token of the organization's oldest Slack connection (ADR 0023,
// decision 5). A token that refers to a credential is not opened here: the row fails.
func slackTokenOf(ctx context.Context, conns SlackConnections) (string, error) {
	all, err := conns.List(ctx)
	if err != nil {
		return "", errSlackConnection
	}

	all = slices.DeleteFunc(all, func(c domain.Connection) bool { return c.Kind != slackKind })
	if len(all) == 0 {
		return "", errNoSlackConnection
	}

	oldest := slices.MinFunc(all, func(a, b domain.Connection) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	})

	if _, ref := oldest.SecretRefs[slackToken]; ref {
		return "", errSlackToken
	}

	c, err := conns.Open(ctx, oldest.Name)
	if err != nil {
		return "", errSlackConnection
	}

	if _, ref := c.SecretRefs[slackToken]; ref || c.Values[slackToken] == "" {
		return "", errSlackToken
	}

	return c.Values[slackToken], nil
}

// logOutcome logs a sent or failed row by its id, event and channel only: never an address, a
// message or the error's text, which may quote the server's reply.
func (s *Sender) logOutcome(ctx context.Context, n ports.Notification, o ports.NotificationOutcome) {
	attrs := []any{slog.Int64("id", n.ID), slog.String("event", string(n.Event)), slog.String("channel", n.Channel)}

	switch o.State {
	case ports.NotificationSent:
		s.log.InfoContext(ctx, "notify.sent", attrs...)
	case ports.NotificationFailed, ports.NotificationPending:
		s.log.WarnContext(ctx, "notify.failed", append(attrs, slog.String("reason", o.Reason), slog.Int("attempts", n.Attempts),
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
