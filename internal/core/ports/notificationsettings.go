package ports

import (
	"context"

	"github.com/byFrederick/goal/internal/core/domain"
)

// NotificationSettings are the organization's notification settings (ADR 0037 decision 14), for
// the organization in ctx. The Slack secret is referred to by id, so renaming it keeps the
// setting, which then reads the new name; OrgSecrets.Delete refuses while the setting uses it.
type NotificationSettings interface {
	// NotificationSettings are the organization's settings; an organization that never saved any
	// has Slack off (an empty SlackSecret).
	NotificationSettings(ctx context.Context) (domain.NotificationSettings, error)
	// SetNotificationSettings saves s, auditing domain.AuditOrgNotificationsUpdated in the same
	// transaction with the secret's name, never its value. An empty SlackSecret turns Slack off; a
	// name that is no secret is ErrNotFound and changes nothing. Saving what is already saved
	// writes nothing and audits nothing.
	SetNotificationSettings(ctx context.Context, s domain.NotificationSettings, by domain.Actor) error
}
