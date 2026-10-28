package ports

import (
	"context"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// NotificationPreferences keeps each person's notification preferences (ADR 0023, decision 1).
// They belong to the person, not to an organization, so no method needs one in ctx; every call
// names the person whose preferences it reads or writes, and touches no one else's.
type NotificationPreferences interface {
	// NotificationPreferences are user's preferences: what they saved last, or
	// domain.DefaultNotificationPreferences when they never saved any.
	NotificationPreferences(ctx context.Context, user domain.UserID) (domain.NotificationPreferences, error)
	// SetNotificationPreferences replaces all of user's preferences with p, which must pass
	// Validate (domain.ErrInvalid, changing nothing). ErrNotFound when user is no person.
	SetNotificationPreferences(ctx context.Context, user domain.UserID, p domain.NotificationPreferences) error
}

// NotificationOutbox queues the notifications people hear about outside the app (ADR 0023,
// decision 3): one outbox row per recipient and per channel that is on. The store queues the
// approval, question and andon rows itself, in the transaction that records their event; this
// port is for the one event the engine notices on its tick, a weekly budget passing 80%.
type NotificationOutbox interface {
	// QueueBudgetNotification queues the budget event of factory, in the organization of ctx,
	// whose spend in the window ending at at is spentUSD of weeklyUSD, for the organization's
	// owners and admins. It queues at most once per factory per domain.BudgetWindow, whatever
	// the instance that calls: a call within the window of the last queued one does nothing.
	QueueBudgetNotification(ctx context.Context, factory domain.FactoryID, spentUSD, weeklyUSD float64, at time.Time) error
}
