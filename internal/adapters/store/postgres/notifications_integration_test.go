//go:build integration

package postgres

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest/notificationtest"
)

func TestNotificationPreferences_Conformance(t *testing.T) {
	t.Parallel()

	notificationtest.Run(t, func(t *testing.T) notificationtest.Harness {
		t.Helper()

		d := newIdentityDB(t)

		return notificationtest.Harness{
			Prefs:   NewNotificationPreferences(d.s),
			NewUser: func(t *testing.T) domain.UserID { t.Helper(); return domain.UserID(d.user("Ada")) },
		}
	})
}

// The table itself refuses the digest In Goal, whoever writes it; a person who does not exist
// has no preferences to set; and the rows go with the user.
func TestNotificationPreferences_Schema(t *testing.T) {
	t.Parallel()

	d := newIdentityDB(t)
	ada := d.user("Ada")

	_, err := d.app.Exec(t.Context(), `INSERT INTO notification_preferences (user_id, event, in_goal, email, slack)
		VALUES ($1, 'digest', true, false, false)`, ada)

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.ConstraintName != "notification_preferences_digest_not_in_goal" {
		t.Fatalf("digest in_goal insert = %v, want the digest check's violation", err)
	}

	n := NewNotificationPreferences(d.s)

	err = n.SetNotificationPreferences(t.Context(), "00000000-0000-7000-8000-000000000001", domain.DefaultNotificationPreferences())
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("set for no person = %v, want ErrNotFound", err)
	}

	if err := n.SetNotificationPreferences(t.Context(), domain.UserID(ada), domain.DefaultNotificationPreferences()); err != nil {
		t.Fatal(err)
	}

	d.exec(d.auth, `DELETE FROM auth."user" WHERE id = $1`, ada)

	if got := d.count(`SELECT count(*) FROM notification_preferences WHERE user_id = $1`, ada) +
		d.count(`SELECT count(*) FROM notification_settings WHERE user_id = $1`, ada); got != 0 {
		t.Fatalf("%d rows outlive their user", got)
	}
}
