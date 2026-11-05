//go:build integration

package postgres

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Migration 00112 backfills origin and via from source: a part is plan, a trigger's source a
// trigger, people, cli and api a person through web, cli and api, and an item without a source a
// person with no via. No row gets a created_by.
func TestMigrate_BackfillsItemOrigins(t *testing.T) {
	t.Parallel()

	db := newTestDB(t, OwnerRole)
	owner := openPool(t, db.owner)
	migrateTo(t, owner, 111)

	at := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	factory := uuid.NewString()
	ids := make([]string, 7)

	for i := range ids {
		ids[i] = uuid.NewString()
	}

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
			`INSERT INTO work_items (id, factory_id, number, type, title, station, state, created_at, updated_at, source) VALUES
		 ($2, $1, 1, 'task', 'browser', 'plan', 'queued', $8, $8, 'people'),
		 ($3, $1, 2, 'task', 'cli', 'plan', 'queued', $8, $8, 'cli'),
		 ($4, $1, 3, 'task', 'key', 'plan', 'queued', $8, $8, 'api'),
		 ($5, $1, 4, 'task', 'schedule', 'plan', 'queued', $8, $8, 'schedule'),
		 ($6, $1, 5, 'task', 'webhook', 'plan', 'queued', $8, $8, 'github'),
		 ($7, $1, 6, 'task', 'old', 'plan', 'assembly', $8, $8, NULL)`,
			[]any{factory, ids[0], ids[1], ids[2], ids[3], ids[4], ids[5], at},
		},
		{
			`INSERT INTO work_items (id, factory_id, number, parent_id, part_key, type, title, station, state, created_at, updated_at) VALUES
		 ($2, $1, 7, $3, 'a', 'task', 'part', 'plan', 'queued', $4, $4)`,
			[]any{factory, ids[6], ids[5], at},
		},
	} {
		if _, err := tx.Exec(t.Context(), row.sql, row.args...); err != nil {
			t.Fatalf("%s: %v", row.sql, err)
		}
	}

	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}

	migrate(t, db.owner)

	const origins = `SELECT origin || '/' || coalesce(via, '') || '/' || coalesce(created_by, '') || '/' || generation || '/' || untrusted
		FROM work_items WHERE id = ANY ($1::uuid[]) ORDER BY array_position($1::uuid[], id)`

	want := []string{
		"person/web//0/false", "person/cli//0/false", "person/api//0/false", "trigger///0/false", "trigger///0/false",
		"person///0/false", "plan///0/false",
	}
	if got := queryStrings(t, owner, origins, ids); !slices.Equal(got, want) {
		t.Errorf("origins after 00112 = %v; want %v", got, want)
	}
}
