//go:build integration

package postgres

import (
	"io/fs"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/db/migrations"
)

// Migration 00062 moves every done assembly to the station of its most recently done part,
// within its own factory, and nothing else: not a cancelled assembly, not an open item, no span.
// A second run changes no row.
func TestMigrate_MovesDoneAssembliesToTheDoneStation(t *testing.T) {
	t.Parallel()

	db := newTestDB(t, OwnerRole)
	owner := openPool(t, db.owner)
	migrateTo(t, owner, 61)

	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	acme, other := uuid.NewString(), uuid.NewString()
	done, cancelled, open, otherDone := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()

	// Done parts end at their factory's last station; of two, the later done one decides.
	tx, err := owner.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := tx.Exec(t.Context(), `SELECT set_config('goal.org_id', $1, true)`, string(DefaultOrgID)); err != nil {
		t.Fatal(err)
	}

	for _, row := range []struct {
		sql  string
		args []any
	}{
		{
			`INSERT INTO factories (id, org_id, name, definition, definition_sha256) VALUES ($1, goal_org(), 'acme-a', '{}', 'sha'), ($2, goal_org(), 'acme-b', '{}', 'sha')`,
			[]any{acme, other},
		},
		{
			`INSERT INTO work_items (id, factory_id, number, type, title, station, state, created_at, updated_at, done_at) VALUES
		 ($3, $1, 1, 'task', 'done assembly', 'plan', 'done', $7, $7, $7),
		 ($4, $1, 2, 'task', 'cancelled assembly', 'plan', 'cancelled', $7, $7, NULL),
		 ($5, $1, 3, 'task', 'open item', 'plan', 'queued', $7, $7, NULL),
		 ($6, $2, 1, 'task', 'done assembly', 'plan', 'done', $7, $7, $7)`,
			[]any{acme, other, done, cancelled, open, otherDone, at},
		},
		{
			`INSERT INTO work_items (id, factory_id, number, parent_id, part_key, type, title, station, state, created_at, updated_at, done_at) VALUES
		 (goal_uuidv7(), $1, 4, $3, 'a', 'task', 'a', 'shipped', 'done', $5, $5, $5::timestamptz + interval '1 hour'),
		 (goal_uuidv7(), $1, 5, $3, 'b', 'task', 'b', 'merged', 'done', $5, $5, $5),
		 (goal_uuidv7(), $1, 6, $3, 'c', 'task', 'c', 'plan', 'cancelled', $5, $5, NULL),
		 (goal_uuidv7(), $1, 7, $4, 'a', 'task', 'a', 'shipped', 'cancelled', $5, $5, NULL),
		 (goal_uuidv7(), $2, 2, $6, 'a', 'task', 'a', 'finished', 'done', $5, $5, $5)`,
			[]any{acme, other, done, cancelled, at, otherDone},
		},
	} {
		if _, err := tx.Exec(t.Context(), row.sql, row.args...); err != nil {
			t.Fatalf("%s: %v", row.sql, err)
		}
	}

	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}

	const (
		stations = `SELECT station FROM work_items WHERE id = ANY ($1::uuid[]) ORDER BY array_position($1::uuid[], id)`
		spans    = `SELECT count(*)::text FROM item_spans`
	)

	ids := []string{done, cancelled, open, otherDone}
	spansBefore := queryStrings(t, owner, spans)

	migrate(t, db.owner)

	if got, want := queryStrings(t, owner, stations, ids), []string{"shipped", "plan", "plan", "finished"}; !slices.Equal(got, want) {
		t.Errorf("stations after 00062 = %v; want %v (done, cancelled, open, other factory's done)", got, want)
	}

	if got := queryStrings(t, owner, spans); !slices.Equal(got, spansBefore) {
		t.Errorf("item spans after 00062 = %v; want %v, unchanged", got, spansBefore)
	}

	backfill, err := fs.ReadFile(migrations.FS, "00062_done_assemblies_at_the_done_station.sql")
	if err != nil {
		t.Fatal(err)
	}

	again, err := owner.Exec(t.Context(), string(backfill))
	if err != nil {
		t.Fatal(err)
	}

	if n := again.RowsAffected(); n != 0 {
		t.Errorf("00062 run again changed %d rows; want 0", n)
	}
}
