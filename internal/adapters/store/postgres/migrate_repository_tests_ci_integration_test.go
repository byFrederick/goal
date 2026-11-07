//go:build integration

package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/byFrederick/goal/internal/core/domain"
)

// A repository test kept before migration 00140 has no CI answer: after the migration it reads
// with no CI check, never a failed one, and the next test of the repository records one.
func TestMigrate_RepositoryTestsCIAbsentBeforeMigration(t *testing.T) {
	t.Parallel()

	db := newTestDB(t, OwnerRole)
	owner := openPool(t, db.owner)
	migrateTo(t, owner, 139)

	at := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	factory := uuid.NewString()

	seedRepositoryTestBeforeCI(t, owner, factory, at)

	migrate(t, db.owner)

	tests := NewRepositoryTests(New(openPool(t, db.app)))
	ctx := domain.WithOrg(t.Context(), DefaultOrgID)

	got, err := tests.List(ctx, domain.FactoryID(factory))
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 || !got[0].OK || got[0].CITested || got[0].CIOK || got[0].CIReason != "" {
		t.Fatalf("List after the migration = %+v; want the passing test with no CI check", got)
	}

	// Testing the repository again, as goal_app, keeps its CI check on the old row.
	again := got[0]
	again.At, again.CITested, again.CIReason = at.Add(time.Hour), true, "the token has no read access to Checks and Commit statuses"

	if err := tests.Record(ctx, again); err != nil {
		t.Fatal(err)
	}

	if got, err = tests.List(ctx, domain.FactoryID(factory)); err != nil || len(got) != 1 || got[0] != again {
		t.Errorf("List after a new test = %+v, %v; want %+v", got, err, again)
	}
}

// seedRepositoryTestBeforeCI keeps, as goal_owner at migration 00133, a factory and a passing
// read test of acme/app in it, without a CI check.
func seedRepositoryTestBeforeCI(t *testing.T, owner *pgxpool.Pool, factory string, at time.Time) {
	t.Helper()

	tx, err := owner.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = tx.Rollback(t.Context()) }() // a no-op after Commit

	for _, row := range []struct {
		sql  string
		args []any
	}{
		{`SELECT set_config('goal.org_id', $1, true)`, []any{string(DefaultOrgID)}},
		{`INSERT INTO factories (id, org_id, name, definition, definition_sha256) VALUES ($1, goal_org(), 'acme', '{}', 'sha')`, []any{factory}},
		{
			`INSERT INTO repository_tests (factory_id, url, access, secret, at, tested, summary, ok, read_reason, read_ok, push_ok, push_reason)
			 VALUES ($1, 'github.com/acme/app', 'read', 'repo-token', $2, true, 'Can read', true, 'shown', true, false, 'no push')`,
			[]any{factory, at},
		},
	} {
		if _, err := tx.Exec(t.Context(), row.sql, row.args...); err != nil {
			t.Fatalf("%s: %v", row.sql, err)
		}
	}

	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
}
