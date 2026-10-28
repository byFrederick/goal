// Package notify sends the notification outbox's email rows (ADR 0023, decisions 3 and 4): every
// goald instance runs one Sender, which claims due rows of each live organization under a lease,
// renders each event's message and sends it through ports.Mailer. It keeps no state of its own,
// so a row whose sender died returns to the queue when its lease ends.
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
	// MaxAttempts is how many sends a row gets before it fails.
	MaxAttempts = 8
	// maxBackoff caps the wait before a retry.
	maxBackoff = time.Hour
)

// The reasons a row ends with, besides a ports.MailError's code. None names an address.
const (
	reasonNotQualified = "recipient-not-qualified"
	reasonUnknownEvent = "unknown-event"
	reasonMailError    = "mail-error"
)

// Orgs lists the live organizations; ports.Tenancy satisfies it.
type Orgs interface {
	ActiveOrgs(ctx context.Context) ([]domain.OrgID, error)
}

// Deps are what a Sender works through.
type Deps struct {
	Orgs   Orgs
	Outbox ports.NotificationDeliveries
	Mailer ports.Mailer
	Clock  ports.Clock
	// PublicURL is the origin every link starts with (GOAL_PUBLIC_URL).
	PublicURL string
	// Log may be nil.
	Log *slog.Logger
}

// Sender is the email sender job.
type Sender struct {
	d   Deps
	log *slog.Logger
}

// New returns a sender over d.
func New(d Deps) *Sender {
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &Sender{d: d, log: log}
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

// Work claims one batch of due email rows of every live organization and finishes each, and
// returns how many it finished. An organization that fails does not hold up the others.
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

		n, err := s.workOrg(domain.WithOrg(ctx, org))
		done += n

		if err != nil {
			errs = append(errs, fmt.Errorf("notify work of org %s: %w", org, err))
		}
	}

	return done, errors.Join(errs...)
}

// workOrg claims and finishes one batch of the organization in ctx.
func (s *Sender) workOrg(ctx context.Context) (int, error) {
	rows, err := s.d.Outbox.ClaimNotifications(ctx, ports.ChannelEmail, s.d.Clock.Now(), Lease, BatchSize)
	if err != nil {
		return 0, fmt.Errorf("claim: %w", err)
	}

	done := 0

	for _, n := range rows {
		if ctx.Err() != nil {
			// The lease returns the rest to the queue.
			break
		}

		o := s.deliver(ctx, n)

		finished, err := s.d.Outbox.FinishNotification(ctx, n.ID, n.Attempts, o)
		if err != nil {
			return done, fmt.Errorf("finish notification %d: %w", n.ID, err)
		}

		if !finished {
			// A later claim took the row after this lease ran out; it reports the row.
			continue
		}

		done++

		s.logOutcome(ctx, n, o)
	}

	return done, nil
}

// deliver sends n when its recipient still qualifies and says how its row ends.
func (s *Sender) deliver(ctx context.Context, n ports.Notification) ports.NotificationOutcome {
	if !n.Qualifies || n.Address == "" {
		return ports.NotificationOutcome{State: ports.NotificationSkipped, Reason: reasonNotQualified}
	}

	m, err := Render(n, s.d.PublicURL)
	if err != nil {
		return ports.NotificationOutcome{State: ports.NotificationFailed, Reason: reasonUnknownEvent}
	}

	err = s.d.Mailer.Send(ctx, m)
	now := s.d.Clock.Now()

	switch {
	case err == nil:
		return ports.NotificationOutcome{State: ports.NotificationSent, At: now}
	case errors.Is(err, ports.ErrMailPermanent) || n.Attempts >= MaxAttempts:
		return ports.NotificationOutcome{State: ports.NotificationFailed, Reason: reason(err)}
	default:
		return ports.NotificationOutcome{State: ports.NotificationPending, RetryAt: now.Add(backoff(n.Attempts)), Reason: reason(err)}
	}
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

// reason is err's short code: a MailError's, else a fixed one.
func reason(err error) string {
	if me, ok := errors.AsType[*ports.MailError](err); ok && me.Code != "" {
		return me.Code
	}

	return reasonMailError
}

// backoff is the wait after attempt attempt failed: 1, 2, 4… minutes, at most maxBackoff.
func backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}

	if attempt > 7 {
		return maxBackoff
	}

	return min(time.Minute<<(attempt-1), maxBackoff)
}
