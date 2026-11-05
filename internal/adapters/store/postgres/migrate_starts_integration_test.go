//go:build integration

package postgres

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Migration 00113 backfills started_at from when each item left its first span: an item that
// moved on twice started when it first moved, and one still in its first span, or with none,
// has not started. No item gets a start_at.
func TestMigrate_BackfillsItemStartedAt(t *testing.T) {
	t.Parallel()

	db := newTestDB(t, OwnerRole)
	owner := openPool(t, db.owner)
	migrateTo(t, owner, 112)

	at := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	factory := uuid.NewString()
	ids := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}

	tx, err := owner.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = tx.Rollback(t.Context()) }() // a no-op after Commit

	if _, err := tx.Exec(t.Context(), `SELECT set_config('goal.org_id', $1, true)`, string(DefaultOrgID)); err != nil {
		t.Fatal(err)
	}

	for _, row := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO factories (id, org_id, name, definition, definition_sha256) VALUES ($1, goal_org(), 'acme', '{}', 'sha')`, []any{factory}},
		{
			`INSERT INTO work_items (id, factory_id, number, type, title, station, state, created_at, updated_at) VALUES
		 ($2, $1, 1, 'task', 'moved twice', 'inbox', 'queued', $5, $5),
		 ($3, $1, 2, 'task', 'waiting', 'inbox', 'queued', $5, $5),
		 ($4, $1, 3, 'task', 'cancelled', 'inbox', 'cancelled', $5, $5)`,
			[]any{factory, ids[0], ids[1], ids[2], at},
		},
		{`UPDATE work_items SET station = 'build', updated_at = $2 WHERE id = $1`, []any{ids[0], at.Add(time.Hour)}},
		{`UPDATE work_items SET station = 'merge', updated_at = $2 WHERE id = $1`, []any{ids[0], at.Add(2 * time.Hour)}},
	} {
		if _, err := tx.Exec(t.Context(), row.sql, row.args...); err != nil {
			t.Fatalf("%s: %v", row.sql, err)
		}
	}

	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}

	migrate(t, db.owner)

	const starts = `SELECT coalesce(to_char(started_at AT TIME ZONE 'UTC', 'HH24:MI'), '') || '/' || coalesce(start_at::text, '')
		FROM work_items WHERE id = ANY ($1::uuid[]) ORDER BY array_position($1::uuid[], id)`

	if got, want := queryStrings(t, owner, starts, ids), []string{"10:00/", "/", "/"}; !slices.Equal(got, want) {
		t.Errorf("started_at/start_at after 00113 = %v; want %v", got, want)
	}
}
