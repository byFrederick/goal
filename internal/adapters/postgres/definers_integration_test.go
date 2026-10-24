//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/byFrederick/goal/internal/domain"
)

// A SECURITY DEFINER function runs as goal_owner, so a table the caller can put in front of the
// real one would run goal_owner's statements on the caller's rows (review 2.2 H1). A temporary
// table named organizations, holding a live organization marked deleted, neither forges what
// goal_org_by_ref returns nor lets goal_purge_org wipe that organization.
func TestDefiners_IgnoreTheCallersTemporaryTables(t *testing.T) {
	t.Parallel()

	db := newTestDB(t, OwnerRole)
	migrate(t, db.owner)

	s := New(openPool(t, db.app))
	live := newOrgID(t, s)
	saveFactory(domain.WithOrg(t.Context(), live), t, s)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, db.admin)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = conn.Close(context.Background()) }()

	// The bootstrap superuser makes the table, since goal_app may not create temporary tables
	// at all (goal db init revokes TEMPORARY from PUBLIC), and shares it with goal_owner; the
	// session then continues as goal_app.
	for _, stmt := range []string{
		"CREATE TEMP TABLE organizations (LIKE " + db.schema + ".organizations)",
		"INSERT INTO pg_temp.organizations SELECT * FROM " + db.schema + ".organizations",
		"UPDATE pg_temp.organizations SET deleted_at = now() - interval '1 year'",
		"INSERT INTO pg_temp.organizations (id, slug, name, mark, created_at, updated_at) VALUES (gen_random_uuid(), 'forged', 'Forged', 'Fo', now(), now())",
		"GRANT ALL ON pg_temp.organizations TO goal_owner",
		"SET ROLE goal_app",
	} {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	var forged int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM goal_org_by_ref('forged')").Scan(&forged); err != nil || forged != 0 {
		t.Errorf("goal_org_by_ref of the temporary table's slug = %d rows, %v; want none", forged, err)
	}

	if _, err := conn.Exec(ctx, "SELECT goal_purge_org($1)", string(live)); err == nil {
		t.Error("goal_purge_org of a live organization through a temporary table = nil; want an error")
	}

	var factories int
	if err := openPool(t, db.admin).QueryRow(t.Context(), "SELECT count(*) FROM factories WHERE org_id = $1", string(live)).
		Scan(&factories); err != nil || factories != 1 {
		t.Errorf("factories of the live organization = %d, %v; want 1", factories, err)
	}
}
