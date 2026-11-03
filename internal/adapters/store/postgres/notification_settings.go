package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.NotificationSettings = (*NotificationSettings)(nil)

// NotificationSettings implements ports.NotificationSettings over 00094's
// org_notification_settings, as goal_app under row-level security: every statement sees the
// organization in ctx. The Slack secret is stored by id and read by name, so a rename keeps it;
// an organization without a row has Slack off. A change appends its audit entry in the same
// transaction.
type NotificationSettings struct{ s *Store }

// NewNotificationSettings returns the settings on s's pool.
func NewNotificationSettings(s *Store) *NotificationSettings { return &NotificationSettings{s: s} }

// NotificationSettings implements ports.NotificationSettings.
func (n *NotificationSettings) NotificationSettings(ctx context.Context) (domain.NotificationSettings, error) {
	slack, err := fetch(ctx, n.s.read, func(q *db.Queries) (string, error) { return q.OrgNotificationSettings(ctx) })
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.NotificationSettings{}, nil
	}

	if err != nil {
		return domain.NotificationSettings{}, fmt.Errorf("get the notification settings: %w", err)
	}

	return domain.NotificationSettings{SlackSecret: slack}, nil
}

// SetNotificationSettings implements ports.NotificationSettings. The named secret is locked
// against its deletion until the commit, and the stored row for the change.
func (n *NotificationSettings) SetNotificationSettings(ctx context.Context, s domain.NotificationSettings, by domain.Actor) error {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.ErrNoOrg
	}

	err := n.s.inTx(ctx, func(q *db.Queries) error {
		var secret *uuid.UUID

		if s.SlackSecret != "" {
			id, err := q.SecretIDByName(ctx, s.SlackSecret)
			if notFound(err) {
				return fmt.Errorf("secret %q: %w", s.SlackSecret, ports.ErrNotFound)
			}

			if err != nil {
				return err
			}

			secret = &id
		}

		current, err := q.LockOrgNotificationSettings(ctx)
		if err != nil && !notFound(err) {
			return err
		}

		if sameID(current, secret) {
			return nil
		}

		if err := q.PutOrgNotificationSettings(ctx, db.PutOrgNotificationSettingsParams{SlackSecretID: secret, UpdatedBy: by.Ref()}); err != nil {
			return err
		}

		return appendAudit(ctx, q, domain.AuditEntry{
			Actor: by, Action: domain.AuditOrgNotificationsUpdated, TargetKind: domain.AuditTargetOrg, TargetID: string(org),
			Data: map[string]any{domain.AuditSlackSecret: s.SlackSecret},
		})
	})
	if err != nil {
		return fmt.Errorf("set the notification settings: %w", err)
	}

	return nil
}

// sameID reports whether a and b are both nil or the same id.
func sameID(a, b *uuid.UUID) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }
