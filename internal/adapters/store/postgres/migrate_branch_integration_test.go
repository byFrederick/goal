//go:build integration

package postgres

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/core/domain"
)

// Migration 00130 backfills every existing item's branch with the one it has today,
// goal/<number>-<slug>, exactly as Item.Branch computes it: punctuation, non-ASCII letters (the
// two Go lowercases to ASCII included), an empty slug and a slug cut at 40 or 41 characters.
func TestMigrate_BackfillsItemBranch(t *testing.T) {
	t.Parallel()

	titles := []string{
		"Add a `goal version` subcommand",
		"  Fix: lease sweep (again)!  ",
		"Ünïcode only ✓",
		"???",
		"",
		"\u0130stanbul \u212Aelvin ÀÉ straße 日本語 émoji 🚀 end", // İ and the Kelvin sign lowercase to i and k in Go
		"a very long title that keeps going well past the limit of a branch",
		strings.Repeat("a", 39) + " b c",
		strings.Repeat("a", 38) + " b c",
		strings.Repeat("a", 40) + " b",
		strings.Repeat("x", 60),
		"--Leading and trailing--",
		"MiXeD CaSe 123 & 456",
	}

	db := newTestDB(t, OwnerRole)
	owner := openPool(t, db.owner)
	migrateTo(t, owner, 129)

	at := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	factory := uuid.NewString()
	ids := make([]string, len(titles))

	tx, err := owner.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = tx.Rollback(t.Context()) }() // a no-op after Commit

	if _, err := tx.Exec(t.Context(), `SELECT set_config('goal.org_id', $1, true)`, string(DefaultOrgID)); err != nil {
		t.Fatal(err)
	}

	if _, err := tx.Exec(t.Context(), `INSERT INTO factories (id, org_id, name, definition, definition_sha256) VALUES ($1, goal_org(), 'acme', '{}', 'sha')`, factory); err != nil {
		t.Fatal(err)
	}

	for i, title := range titles {
		ids[i] = uuid.NewString()

		_, err := tx.Exec(t.Context(), `INSERT INTO work_items (id, factory_id, number, type, title, station, state, created_at, updated_at)
			VALUES ($1, $2, $3, 'task', $4, 'inbox', 'queued', $5, $5)`, ids[i], factory, i+1, title, at)
		if err != nil {
			t.Fatalf("insert %q: %v", title, err)
		}
	}

	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}

	migrate(t, db.owner)

	got := queryStrings(t, owner, `SELECT branch FROM work_items WHERE id = ANY ($1::uuid[]) ORDER BY array_position($1::uuid[], id)`, ids)

	want := make([]string, len(titles))
	for i, title := range titles {
		want[i] = domain.Item{Number: int64(i + 1), Title: title}.Branch()
	}

	if !slices.Equal(got, want) {
		var b strings.Builder

		for i := range want {
			if i >= len(got) || got[i] != want[i] {
				fmt.Fprintf(&b, "\n%q: got %q, want %q", titles[i], at0(got, i), want[i])
			}
		}

		t.Errorf("branches after 00130 differ from Item.Branch:%s", b.String())
	}
}

// at0 is s[i], or "" past its end.
func at0(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}

	return ""
}
