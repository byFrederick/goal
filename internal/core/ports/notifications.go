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
	// whose spend in the window that ends at the time at is spentUSD of weeklyUSD, for the organization's
	// owners and admins. It queues at most once per factory per domain.BudgetWindow, whatever
	// the instance that calls: a call within the window of the last queued one does nothing.
	QueueBudgetNotification(ctx context.Context, factory domain.FactoryID, spentUSD, weeklyUSD float64, at time.Time) error
}

// The outbox's channels and the states a claimed row ends in.
const (
	ChannelEmail = "email"
	ChannelSlack = "slack"

	NotificationPending = "pending"
	NotificationSent    = "sent"
	NotificationFailed  = "failed"
	NotificationSkipped = "skipped"
)

// NotificationDeliveries is the sender job's side of the outbox (ADR 0023, decision 3), for the
// organization in ctx. A claim leases due rows; a finish ends the claim under the attempt it
// made, so a worker whose lease ran out and whose row another claim took changes nothing.
type NotificationDeliveries interface {
	// ClaimNotifications leases at most limit due rows of channel (pending, next attempt at or
	// before now, no lease live at now), oldest due first, skipping rows another transaction
	// holds: each gets a lease until now+lease and one more attempt. Each comes with what the
	// message needs and whether its recipient still qualifies.
	ClaimNotifications(ctx context.Context, channel string, now time.Time, lease time.Duration, limit int) ([]Notification, error)
	// FinishNotification ends the claim of row id that made attempt attempts with o, and
	// releases its lease. It reports false, changing nothing, when the row is no longer pending
	// at that attempt: a later claim took it.
	FinishNotification(ctx context.Context, id int64, attempts int, o NotificationOutcome) (bool, error)
}

// Notification is one claimed outbox row, with the names and facts its message shows, read when
// it was claimed. Payload facts an event lacks are zero.
type Notification struct {
	ID       int64
	EventID  string
	Event    domain.NotificationEvent
	Channel  string
	UserID   domain.UserID
	Attempts int // this claim's attempt, from 1
	At       time.Time
	// Qualifies is whether the recipient is still a member who would receive the event (by the
	// queue's recipient rule) with the channel on. Address is their notification email, else
	// their sign-in email; blank when they are no member.
	Qualifies bool
	Address   string

	OrgSlug      string
	FactoryID    domain.FactoryID
	FactoryName  string // the factory's name, which its pages' paths carry
	FactoryTitle string // its title, or its name without one
	ItemNumber   int64  // 0 without an item
	ItemTitle    string
	Station      string
	AndonKind    string
	SpentUSD     float64
	WeeklyUSD    float64
}

// NotificationOutcome is how a claimed row ends: sent at At, back to pending until RetryAt, or
// failed or skipped with Reason, a short code that names no address.
type NotificationOutcome struct {
	State   string
	At      time.Time
	RetryAt time.Time
	Reason  string
}
