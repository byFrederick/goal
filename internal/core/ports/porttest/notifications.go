package porttest

import (
	"context"
	"sync"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var (
	_ ports.NotificationPreferences = (*NotificationPreferences)(nil)
	_ ports.NotificationOutbox      = (*NotificationOutbox)(nil)
)

// NotificationPreferences keeps each person's notification preferences in memory with the
// database's rules: the defaults until a save, and saves checked with Validate.
type NotificationPreferences struct {
	mu   sync.Mutex
	rows map[domain.UserID]domain.NotificationPreferences
}

// NewNotificationPreferences returns preferences in which nobody has saved any.
func NewNotificationPreferences() *NotificationPreferences {
	return &NotificationPreferences{rows: map[domain.UserID]domain.NotificationPreferences{}}
}

// NotificationPreferences implements ports.NotificationPreferences.
func (n *NotificationPreferences) NotificationPreferences(_ context.Context, user domain.UserID) (domain.NotificationPreferences, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	p, ok := n.rows[user]
	if !ok {
		return domain.DefaultNotificationPreferences(), nil
	}

	return p.Clone(), nil
}

// SetNotificationPreferences implements ports.NotificationPreferences.
func (n *NotificationPreferences) SetNotificationPreferences(_ context.Context, user domain.UserID, p domain.NotificationPreferences) error {
	if err := p.Validate(); err != nil {
		return err
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	n.rows[user] = p.Clone()

	return nil
}

// BudgetNotification is one budget event a NotificationOutbox queued.
type BudgetNotification struct {
	FactoryID           domain.FactoryID
	SpentUSD, WeeklyUSD float64
	At                  time.Time
}

// NotificationOutbox keeps the budget events queued in memory with the database's rule: at
// most one per factory per domain.BudgetWindow. Err, when set, fails every call.
type NotificationOutbox struct {
	mu     sync.Mutex
	budget []BudgetNotification
	calls  int
	Err    error
}

// NewNotificationOutbox returns an outbox with nothing queued.
func NewNotificationOutbox() *NotificationOutbox { return &NotificationOutbox{} }

// QueueBudgetNotification implements ports.NotificationOutbox.
func (o *NotificationOutbox) QueueBudgetNotification(_ context.Context, factory domain.FactoryID, spentUSD, weeklyUSD float64, at time.Time) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.calls++

	if o.Err != nil {
		return o.Err
	}

	for _, b := range o.budget {
		if b.FactoryID == factory && !b.At.Before(at.Add(-domain.BudgetWindow)) {
			return nil
		}
	}

	o.budget = append(o.budget, BudgetNotification{FactoryID: factory, SpentUSD: spentUSD, WeeklyUSD: weeklyUSD, At: at})

	return nil
}

// Budget returns the budget events queued so far, in order.
func (o *NotificationOutbox) Budget() []BudgetNotification {
	o.mu.Lock()
	defer o.mu.Unlock()

	return append([]BudgetNotification(nil), o.budget...)
}

// Calls counts the QueueBudgetNotification calls, queued or not.
func (o *NotificationOutbox) Calls() int {
	o.mu.Lock()
	defer o.mu.Unlock()

	return o.calls
}
