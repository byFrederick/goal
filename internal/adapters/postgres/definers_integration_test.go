//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

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

	var purged bool
	if err := conn.QueryRow(ctx, "SELECT goal_purge_org($1, now())", string(live)).Scan(&purged); err == nil && purged {
		t.Error("goal_purge_org of a live organization through a temporary table = true; want it refused")
	}

	var factories int
	if err := openPool(t, db.admin).QueryRow(t.Context(), "SELECT count(*) FROM factories WHERE org_id = $1", string(live)).
		Scan(&factories); err != nil || factories != 1 {
		t.Errorf("factories of the live organization = %d, %v; want 1", factories, err)
	}
}

// An organization is purged only once it was deleted before the retention cutoff the purge
// passes, whoever calls the function (review 2.2 M1): soft-deleting an organization and purging
// it in the same transaction purges nothing.
func TestPurgeOrg_RefusesAnOrgDeletedAfterTheCutoff(t *testing.T) {
	t.Parallel()

	pool := newTestPool(t)
	s := New(pool)
	org := newOrgID(t, s)
	saveFactory(domain.WithOrg(t.Context(), org), t, s)

	var purged bool

	err := inOrgTx(t, pool, org, func(tx pgx.Tx) error {
		if _, err := tx.Exec(t.Context(), "UPDATE organizations SET deleted_at = now() WHERE id = goal_org()"); err != nil {
			return err
		}

		return tx.QueryRow(t.Context(), "SELECT goal_purge_org(goal_org(), now())").Scan(&purged)
	})
	if err != nil || purged {
		t.Errorf("goal_purge_org of an org deleted in the same transaction = %v, %v; want false", purged, err)
	}

	var factories int
	if err := asOrg(t, pool, org, "SELECT count(*) FROM factories", &factories); err != nil || factories != 1 {
		t.Errorf("factories after the refused purge = %d, %v; want 1", factories, err)
	}

	var old *string
	if err := pool.QueryRow(t.Context(), "SELECT to_regprocedure('goal_purge_org(uuid)')::text").Scan(&old); err != nil || old != nil {
		t.Errorf("goal_purge_org(uuid), which takes no cutoff = %v, %v; want it gone", old, err)
	}
}

// Published versions outlive a soft delete: until the purge, nobody deletes one, not even
// goal_app in the deleted organization (review 2.2 L1), so a restore would find its history.
func TestDeletedOrg_KeepsItsPublishedVersionsUntilThePurge(t *testing.T) {
	t.Parallel()

	f := newDefinitionsFixture(t)

	if err := f.s.DeleteOrg(domain.WithOrg(t.Context(), f.org), domain.LocalUser, time.Now()); err != nil {
		t.Fatal(err)
	}

	// Nothing but the trigger stands in the way: the factory points at no version, and the draft
	// based on v1 is gone.
	err := inOrgTx(t, f.pool, f.org, func(tx pgx.Tx) error {
		for _, stmt := range []string{
			"UPDATE factories SET current_version_id = NULL",
			"DELETE FROM definition_versions WHERE state = 'draft'",
		} {
			if _, err := tx.Exec(t.Context(), stmt); err != nil {
				t.Fatalf("%s: %v", stmt, err)
			}
		}

		_, err := tx.Exec(t.Context(), "DELETE FROM definition_versions WHERE id = $1", string(f.v1.ID))

		return err
	})

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != checkViolation {
		t.Errorf("goal_app deleting a published version of a deleted organization = %v; want the trigger's check_violation", err)
	}
}

// No definer function tells one organization's transaction about another organization (review
// 2.2 L7): factory counts come from each organization's own rows under row-level security.
func TestOrgs_FactoryCountsComeFromEachOrgsOwnRows(t *testing.T) {
	t.Parallel()

	pool := newTestPool(t)
	s := New(pool)
	acme, labs := newOrgID(t, s), newOrgID(t, s)
	saveFactory(domain.WithOrg(t.Context(), labs), t, s)
	saveFactory(domain.WithOrg(t.Context(), labs), t, s)

	var n int
	if err := asOrg(t, pool, acme, "SELECT goal_org_factory_count('"+string(labs)+"')", &n); err == nil {
		t.Errorf("goal_org_factory_count of another org from acme = %d; want no such function", n)
	}

	orgs, err := s.Orgs(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	counts := map[domain.OrgID]int{}
	for _, o := range orgs {
		counts[o.ID] = o.FactoryCount
	}

	if counts[acme] != 0 || counts[labs] != 2 {
		t.Errorf("Orgs factory counts = acme %d, labs %d; want 0 and 2", counts[acme], counts[labs])
	}
}
