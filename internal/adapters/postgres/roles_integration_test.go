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

	old := New(admin)
	factory := saveFactory(t, old)

	it, err := old.CreateItem(t.Context(), domain.NewItem{
		FactoryID: factory, Type: typeTask, Title: "from before", Station: stWork, State: domain.StateQueued,
		Class: domain.ClassStandard, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	var moved int

	err = pgx.BeginFunc(t.Context(), admin, func(tx pgx.Tx) error {
		if err := createRoles(t.Context(), tx); err != nil {
			return err
		}

		moved, err = grantAndTransfer(t.Context(), tx)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	// factories, work_items, leases, agent_runs, item_events, andon_pulls, station_wip,
	// goose_db_version and notify_item_event(); sequences move with their tables.
	if moved != 9 {
		t.Errorf("moved %d objects to goal_owner; want 9", moved)
	}

	var left int
	if err := admin.QueryRow(t.Context(), `
		SELECT (SELECT count(*) FROM pg_class
		        WHERE relnamespace = current_schema()::text::regnamespace AND relowner = current_user::text::regrole)
		     + (SELECT count(*) FROM pg_proc
		        WHERE pronamespace = current_schema()::text::regnamespace AND proowner = current_user::text::regrole)`).
		Scan(&left); err != nil {
		t.Fatal(err)
	}

	if left != 0 {
		t.Errorf("the bootstrap user still owns %d objects; want 0", left)
	}

	migrate(t, db.owner)

	app := openPool(t, db.app)
	if err := CheckAppRole(t.Context(), app); err != nil {
		t.Fatal(err)
	}

	s := New(app)

	got, err := s.Item(t.Context(), it.ID)
	if err != nil || got.Title != "from before" {
		t.Fatalf("Item as goal_app = %+v, %v; want the existing item", got, err)
	}

	if _, err := s.CreateItem(t.Context(), domain.NewItem{
		FactoryID: factory, Type: typeTask, Title: "after", Station: stWork, State: domain.StateQueued,
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
