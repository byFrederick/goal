package porttest

import (
	"context"
	"fmt"
	"sync"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.NotificationSettings = (*NotificationSettings)(nil)

// NotificationSettings keeps each organization's notification settings in memory with the
// database's rules: Slack off until a setting names one of secrets' secrets, the setting following
// the secret's renames, the secret not deleted while the setting uses it, and an
// org.notifications_updated entry through audit for each change.
type NotificationSettings struct {
	secrets *OrgSecrets
	audit   ports.Audit

	mu    sync.Mutex
	slack map[domain.OrgID]string // the Slack secret's name
}

// NewNotificationSettings returns settings that name secrets' secrets, audited through audit (a
// Tenancy that knows the organizations).
func NewNotificationSettings(secrets *OrgSecrets, audit ports.Audit) *NotificationSettings {
	n := &NotificationSettings{secrets: secrets, audit: audit, slack: map[domain.OrgID]string{}}

	secrets.d.mu.Lock()
	defer secrets.d.mu.Unlock()

	secrets.d.notifies = func(org domain.OrgID, name string) bool {
		n.mu.Lock()
		defer n.mu.Unlock()

		return name != "" && n.slack[org] == name
	}
	secrets.d.notifyRenamed = func(org domain.OrgID, from, to string) {
		n.mu.Lock()
		defer n.mu.Unlock()

		if n.slack[org] == from {
			n.slack[org] = to
		}
	}

	return n
}

// NotificationSettings implements ports.NotificationSettings.
func (n *NotificationSettings) NotificationSettings(ctx context.Context) (domain.NotificationSettings, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.NotificationSettings{}, domain.ErrNoOrg
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	return domain.NotificationSettings{SlackSecret: n.slack[org]}, nil
}

// SetNotificationSettings implements ports.NotificationSettings. The secrets' lock is held
// throughout, as the database locks the secret, so a Delete cannot pass between the check and
// the save.
func (n *NotificationSettings) SetNotificationSettings(ctx context.Context, s domain.NotificationSettings, by domain.Actor) error {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.ErrNoOrg
	}

	n.secrets.d.mu.Lock()
	defer n.secrets.d.mu.Unlock()

	if s.SlackSecret != "" {
		if _, err := n.secrets.rowLocked(org, s.SlackSecret); err != nil {
			return fmt.Errorf("set the notification settings: %w", err)
		}
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	if n.slack[org] == s.SlackSecret {
		return nil
	}

	err := n.audit.Append(ctx, domain.AuditEntry{
		Actor: by, Action: domain.AuditOrgNotificationsUpdated, TargetKind: domain.AuditTargetOrg, TargetID: string(org),
		Data: map[string]any{domain.AuditSlackSecret: s.SlackSecret},
	})
	if err != nil {
		return err
	}

	n.slack[org] = s.SlackSecret

	return nil
}
