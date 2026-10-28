package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.NotificationPreferences = (*NotificationPreferences)(nil)

// NotificationPreferences implements ports.NotificationPreferences over 00078's tables. They
// belong to a person, not an organization, and have no row-level security, so its transactions
// bind no organization and every statement names the person (db/queries/notification_preferences.sql).
type NotificationPreferences struct {
	s *Store
}

// NewNotificationPreferences returns each person's notification preferences on s's pool.
func NewNotificationPreferences(s *Store) *NotificationPreferences { return &NotificationPreferences{s: s} }

// userTx runs fn in a transaction bound to no organization, with opts.
func (n *NotificationPreferences) userTx(ctx context.Context, opts pgx.TxOptions, fn func(q *db.Queries) error) error {
	return pgx.BeginTxFunc(ctx, n.s.pool, opts, func(tx pgx.Tx) error { return fn(n.s.q.WithTx(tx)) })
}

// NotificationPreferences implements ports.NotificationPreferences: the stored rows over the
// defaults, read from one snapshot.
func (n *NotificationPreferences) NotificationPreferences(ctx context.Context, user domain.UserID) (domain.NotificationPreferences, error) {
	id, err := parseID("user", string(user))
	if err != nil {
		return domain.NotificationPreferences{}, err
	}

	p := domain.DefaultNotificationPreferences()

	err = n.userTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(q *db.Queries) error {
		events, err := q.NotificationPreferences(ctx, id)
		if err != nil {
			return fmt.Errorf("notification preferences of %s: %w", user, err)
		}

		for _, e := range events {
			p.Events[domain.NotificationEvent(e.Event)] = domain.NotificationChannels{InGoal: e.InGoal, Email: e.Email, Slack: e.Slack}
		}

		settings, err := q.NotificationSettings(ctx, id)
		if err != nil {
			return fmt.Errorf("notification settings of %s: %w", user, err)
		}

		for _, s := range settings {
			p.Email, p.TimeZone = s.Email, s.TimeZone
		}

		return nil
	})

	return p, err
}

// SetNotificationPreferences implements ports.NotificationPreferences: Validate requires every
// event, so writing each one's row replaces them all.
func (n *NotificationPreferences) SetNotificationPreferences(ctx context.Context, user domain.UserID, p domain.NotificationPreferences) error {
	if err := p.Validate(); err != nil {
		return err
	}

	id, err := parseID("user", string(user))
	if err != nil {
		return err
	}

	err = n.userTx(ctx, pgx.TxOptions{}, func(q *db.Queries) error {
		for _, e := range domain.NotificationEvents {
			c := p.Events[e]

			err := q.PutNotificationPreference(ctx, db.PutNotificationPreferenceParams{
				UserID: id, Event: string(e), InGoal: c.InGoal, Email: c.Email, Slack: c.Slack,
			})
			if err != nil {
				return fmt.Errorf("event %s: %w", e, err)
			}
		}

		if err := q.PutNotificationSettings(ctx, db.PutNotificationSettingsParams{UserID: id, Email: p.Email, TimeZone: p.TimeZone}); err != nil {
			return fmt.Errorf("settings: %w", err)
		}

		return nil
	})

	switch {
	case isForeignKeyViolation(err):
		return fmt.Errorf("set notification preferences of %s: %w", user, ports.ErrNotFound)
	case err != nil:
		return fmt.Errorf("set notification preferences of %s: %w", user, err)
	}

	return nil
}
