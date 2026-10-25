//go:build integration

package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/migrations"
)

func TestCheckAppRole_RefusesBypass(t *testing.T) {
	t.Parallel()

	db := newTestDB(t, OwnerRole)
	migrate(t, db.owner)

	for name, url := range map[string]string{"superuser": db.admin, "goal_owner": db.owner} {
		if err := CheckAppRole(t.Context(), openPool(t, url)); !errors.Is(err, ErrRoleBypassesRLS) {
			t.Errorf("CheckAppRole as %s = %v; want ErrRoleBypassesRLS", name, err)
		}
	}

	if err := CheckAppRole(t.Context(), openPool(t, db.app)); err != nil {
		t.Errorf("CheckAppRole as goal_app = %v; want nil", err)
	}
}

// A database from before the roles: the bootstrap superuser owns the schema's objects and has
// applied 00001-00003. goal db init hands them to goal_owner, which then migrates, and goald's
// goal_app reads and writes the existing rows.
func TestInitRoles_UpgradesADatabaseFromBeforeTheRoles(t *testing.T) {
	t.Parallel()

	db := newTestDB(t, "")
	admin := openPool(t, db.admin)
	migrateTo(t, admin, 3)

	before := insertRowsFromBefore(t, admin)
	ctx := domain.WithOrg(t.Context(), DefaultOrgID)

	var moved int

	err := pgx.BeginFunc(t.Context(), admin, func(tx pgx.Tx) error {
		if err := createRoles(t.Context(), tx); err != nil {
			return err
		}

		var err error

		if moved, err = grantAndTransfer(t.Context(), tx); err != nil {
			return err
		}

		// The extension 00054 needs, which only the administrator creates.
		return createExtensions(t.Context(), tx)
	})
	if err != nil {
		t.Fatal(err)
	}

	// factories, work_items, leases, agent_runs, item_events, andon_pulls, station_wip,
	// goose_db_version and notify_item_event(); sequences move with their tables.
	if moved != 9 {
		t.Errorf("moved %d objects to goal_owner; want 9", moved)
	}

	// The extension's own objects (pgvector's types and functions) stay the administrator's.
	var left int
	if err := admin.QueryRow(t.Context(), `
		SELECT (SELECT count(*) FROM pg_class c
		        WHERE relnamespace = current_schema()::text::regnamespace AND relowner = current_user::text::regrole
		          AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.classid = 'pg_class'::regclass AND d.objid = c.oid AND d.deptype = 'e'))
		     + (SELECT count(*) FROM pg_proc p
		        WHERE pronamespace = current_schema()::text::regnamespace AND proowner = current_user::text::regrole
		          AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.classid = 'pg_proc'::regclass AND d.objid = p.oid AND d.deptype = 'e'))`).
		Scan(&left); err != nil {
		t.Fatal(err)
	}

	if left != 0 {
		t.Errorf("the bootstrap user still owns %d objects; want 0", left)
	}

	checkDatabasePrivileges(t, admin)

	upgrade(t, db.owner)

	app := openPool(t, db.app)
	if err := CheckAppRole(t.Context(), app); err != nil {
		t.Fatal(err)
	}

	s := New(app)

	got, err := s.Item(ctx, before.item)
	if err != nil || got.Title != "from before" {
		t.Fatalf("Item as goal_app = %+v, %v; want the existing item", got, err)
	}

	if _, err := s.CreateItem(ctx, domain.NewItem{
		FactoryID: before.factory, Type: typeTask, Title: "after", Station: stWork, State: domain.StateQueued,
		Class: domain.ClassStandard, CreatedAt: time.Now(),
	}); err != nil {
		t.Errorf("CreateItem as goal_app = %v", err)
	}
}

// migrateTo applies the migrations up to and including version, as pool's role.
func migrateTo(t *testing.T, pool *pgxpool.Pool, version int64) {
	t.Helper()

	sqlDB := stdlib.OpenDBFromPool(pool)

	t.Cleanup(func() { _ = sqlDB.Close() })

	p, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := p.UpTo(t.Context(), version); err != nil {
		t.Fatal(err)
	}
}

// publicRole is PostgreSQL's PUBLIC, every role, as the privilege functions name it.
const publicRole = "public"

// checkDatabasePrivileges checks what goal db init leaves on the database: goal_owner, goal_app
// and goal_auth connect by their own grants, and PUBLIC may neither connect (so no other role of the cluster
// LISTENs to or NOTIFYs goald's channels) nor create temporary tables (review 2.2 H1, M4).
func checkDatabasePrivileges(t *testing.T, admin *pgxpool.Pool) {
	t.Helper()

	const connect, temporary = "CONNECT", "TEMPORARY"

	for _, c := range []struct {
		role, priv string
		want       bool
	}{
		{publicRole, connect, false},
		{publicRole, temporary, false},
		{OwnerRole, connect, true},
		{OwnerRole, "CREATE", true},
		{OwnerRole, temporary, false},
		{AppRole, connect, true},
		{AppRole, temporary, false},
		{AuthRole, connect, true},
		{AuthRole, temporary, false},
	} {
		var has bool
		if err := admin.QueryRow(t.Context(), "SELECT has_database_privilege($1, current_database(), $2)", c.role, c.priv).
			Scan(&has); err != nil || has != c.want {
			t.Errorf("%s %s on the database = %v, %v; want %v", c.role, c.priv, has, err, c.want)
		}
	}
}
