//go:build integration

package postgres

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Migration 00132 moves each run's heads into agent_run_repositories: one row per run with a
// head, on the item's first repository when it has one and without a repository otherwise, that
// wrote when it pushed a commit other than its branch head. A run without heads gets no row, and
// agent_runs loses the three columns.
func TestMigrate_RunHeadsBecomeRunRepositories(t *testing.T) {
	t.Parallel()

	db := newTestDB(t, OwnerRole)
	owner := openPool(t, db.owner)
	migrateTo(t, owner, 130)

	at := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	a, b, c := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	factory, withRepo, withoutRepo := uuid.NewString(), uuid.NewString(), uuid.NewString()
	pushed, bare, same, retry := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()

	tx, err := owner.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = tx.Rollback(t.Context()) }() // a no-op after Commit

	exec := func(sql string, args ...any) {
		t.Helper()

		if _, err := tx.Exec(t.Context(), sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}

	exec(`SELECT set_config('goal.org_id', $1, true)`, string(DefaultOrgID))
	exec(`INSERT INTO factories (id, org_id, name, definition, definition_sha256) VALUES ($1, goal_org(), 'acme', '{}', 'sha')`, factory)

	for i, id := range []string{withRepo, withoutRepo} {
		exec(`INSERT INTO work_items (id, factory_id, number, type, title, station, state, created_at, updated_at)
			VALUES ($1, $2, $3, 'task', 'item', 'inbox', 'queued', $4, $4)`, id, factory, i+1, at)
	}

	exec(`INSERT INTO work_item_repositories (item_id, repo, joined, joined_at, ordinal) VALUES
		($1, 'github.com/acme/docs', 'filing', $2, 1), ($1, 'github.com/acme/app', 'filing', $2, 0)`, withRepo, at)

	run := func(id, item string, branch, base, push any) {
		exec(`INSERT INTO agent_runs (id, factory_id, item_id, station, agent, model, status, started_at, branch_head, base_head, pushed_head)
			VALUES ($1, $2, $3, 'build', 'engineer', 'model', 'succeeded', $4, $5, $6, $7)`, id, factory, item, at, branch, base, push)
	}

	run(pushed, withRepo, a, b, c)
	run(bare, withRepo, nil, nil, nil)
	run(same, withoutRepo, a, b, a)
	run(retry, withoutRepo, nil, nil, c)

	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}

	migrate(t, db.owner)

	rows, err := owner.Query(t.Context(), `SELECT run_id::text, coalesce(repo, '-'), coalesce(base_commit, ''), coalesce(branch_head, ''),
		coalesce(pushed_head, ''), wrote FROM agent_run_repositories WHERE run_id = ANY ($1::uuid[]) ORDER BY array_position($1::uuid[], run_id)`,
		[]string{pushed, bare, same, retry})
	if err != nil {
		t.Fatal(err)
	}

	type row struct {
		run, repo, base, branch, pushed string
		wrote                           bool
	}

	var got []row

	for rows.Next() {
		var r row
		if err := rows.Scan(&r.run, &r.repo, &r.base, &r.branch, &r.pushed, &r.wrote); err != nil {
			t.Fatal(err)
		}

		got = append(got, r)
	}

	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	want := []row{
		{pushed, "github.com/acme/app", b, a, c, true},
		{same, "-", b, a, a, false},
		{retry, "-", "", "", c, true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("agent_run_repositories after 00132 =\n%+v\nwant\n%+v", got, want)
	}

	cols := queryStrings(t, owner, `SELECT column_name::text FROM information_schema.columns
		WHERE table_name = 'agent_runs' AND column_name IN ('branch_head', 'base_head', 'pushed_head')`)
	if len(cols) != 0 {
		t.Errorf("agent_runs still has %v after 00132", cols)
	}
}
