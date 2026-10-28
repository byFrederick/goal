package ports

import (
	"context"

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
