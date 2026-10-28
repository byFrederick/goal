package porttest

import (
	"context"
	"sync"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.NotificationPreferences = (*NotificationPreferences)(nil)

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
