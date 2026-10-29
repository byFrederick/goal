package porttest

import (
	"context"
	"slices"
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

var _ ports.NotificationDeliveries = (*NotificationDeliveries)(nil)

// DeliveryRow is one outbox row of a NotificationDeliveries: the claim's view of it and its
// delivery state.
type DeliveryRow struct {
	ports.Notification
	State         string
	NextAttemptAt time.Time
	LeaseUntil    time.Time // zero: no lease
	LastError     string
	SentAt        time.Time
}

// NotificationDeliveries keeps outbox rows in memory with the database's claim rules: per
// organization, due pending rows without a live lease, oldest due first, and a finish only under
// the attempt that claimed.
type NotificationDeliveries struct {
	mu     sync.Mutex
	rows   map[domain.OrgID][]*DeliveryRow
	nextID int64
}

// NewNotificationDeliveries returns an outbox with no rows.
func NewNotificationDeliveries() *NotificationDeliveries {
	return &NotificationDeliveries{rows: map[domain.OrgID][]*DeliveryRow{}}
}

// Queue adds a pending row of org, due at n.At, and returns its id. n.ID and n.Attempts are
// ignored; the claim's facts (Qualifies, Address, names) are what each claim returns.
func (d *NotificationDeliveries) Queue(org domain.OrgID, n ports.Notification) int64 {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.nextID++
	n.ID, n.Attempts = d.nextID, 0
	d.rows[org] = append(d.rows[org], &DeliveryRow{Notification: n, State: ports.NotificationPending, NextAttemptAt: n.At})

	return n.ID
}

// Row returns row id as it is now, and whether it exists.
func (d *NotificationDeliveries) Row(id int64) (DeliveryRow, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, rows := range d.rows {
		for _, r := range rows {
			if r.ID == id {
				return *r, true
			}
		}
	}

	return DeliveryRow{}, false
}

// ClaimNotifications implements ports.NotificationDeliveries.
func (d *NotificationDeliveries) ClaimNotifications(ctx context.Context, channel string, now time.Time, lease time.Duration, limit int) ([]ports.Notification, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, domain.ErrNoOrg
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	var due []*DeliveryRow

	for _, r := range d.rows[org] {
		if r.Channel == channel && r.State == ports.NotificationPending && !r.NextAttemptAt.After(now) &&
			(r.LeaseUntil.IsZero() || !r.LeaseUntil.After(now)) {
			due = append(due, r)
		}
	}

	slices.SortStableFunc(due, func(a, b *DeliveryRow) int {
		if c := a.NextAttemptAt.Compare(b.NextAttemptAt); c != 0 {
			return c
		}

		return int(a.ID - b.ID)
	})

	out := make([]ports.Notification, 0, min(limit, len(due)))

	for _, r := range due[:min(limit, len(due))] {
		r.LeaseUntil = now.Add(lease)
		r.Attempts++
		out = append(out, r.Notification)
	}

	return out, nil
}

// FinishNotification implements ports.NotificationDeliveries.
func (d *NotificationDeliveries) FinishNotification(ctx context.Context, id int64, attempts int, o ports.NotificationOutcome) (bool, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return false, domain.ErrNoOrg
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	for _, r := range d.rows[org] {
		if r.ID != id {
			continue
		}

		if r.State != ports.NotificationPending || r.Attempts != attempts {
			return false, nil
		}

		r.State, r.LeaseUntil, r.LastError = o.State, time.Time{}, o.Reason

		switch o.State {
		case ports.NotificationSent:
			r.SentAt = o.At
		case ports.NotificationPending:
			r.NextAttemptAt = o.RetryAt
		}

		return true, nil
	}

	return false, nil
}
