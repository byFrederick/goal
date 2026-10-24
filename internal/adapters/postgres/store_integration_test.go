//go:build integration

package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest/storetest"
	"github.com/byFrederick/goal/migrations"
)

func TestStore_Conformance(t *testing.T) {
	t.Parallel()

	storetest.Run(t, func(t *testing.T) storetest.Harness {
		t.Helper()

		s := New(newTestPool(t))

		return storetest.Harness{
			Store:  s,
			NewOrg: func(t *testing.T) domain.OrgID { t.Helper(); return newOrgID(t, s) },
			NewFactory: func(t *testing.T, org domain.OrgID) domain.FactoryID {
				t.Helper()

				return saveFactory(domain.WithOrg(t.Context(), org), t, s)
			},
			Runs: func(t *testing.T, org domain.OrgID, id domain.ItemID) []domain.Run {
				t.Helper()

				return s.runs(domain.WithOrg(t.Context(), org), t, id)
			},
		}
	})
}

func TestMigrate_RepeatableAndSafeToRunConcurrently(t *testing.T) {
	t.Parallel()

	db := newTestDB(t, OwnerRole)
	migrate(t, db.owner)
	pool := openPool(t, db.owner)

	var wg sync.WaitGroup

	for range 4 {
		wg.Go(func() {
			if err := Migrate(t.Context(), pool); err != nil {
				t.Error(err)
			}
		})
	}

	wg.Wait()

	var version int64
	if err := pool.QueryRow(t.Context(), "SELECT max(version_id) FROM goose_db_version").Scan(&version); err != nil {
		t.Fatal(err)
	}

	if want := latestMigration(t); version != want {
		t.Errorf("schema version = %d; want %d", version, want)
	}
}

// latestMigration is the number of the newest file in migrations/.
func latestMigration(t *testing.T) int64 {
	t.Helper()

	files, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("migrations: %v, %d files", err, len(files))
	}

	v, err := strconv.ParseInt(strings.SplitN(files[len(files)-1], "_", 2)[0], 10, 64)
	if err != nil {
		t.Fatal(err)
	}

	return v
}

func TestCreateItem_UnsavedFactoryNotFound(t *testing.T) {
	t.Parallel()

	s := New(newTestPool(t))

	_, err := s.CreateItem(domain.WithOrg(t.Context(), DefaultOrgID), domain.NewItem{
		FactoryID: domain.FactoryID(uuid.NewString()), Type: typeTask, Title: "x", Station: "work",
		State: domain.StateQueued, Class: domain.ClassStandard, CreatedAt: time.Now(),
	})
	if !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("CreateItem = %v; want ErrNotFound", err)
	}
}

func TestSaveFactory_UpdatesTheDefinition(t *testing.T) {
	t.Parallel()

	s := New(newTestPool(t))
	ctx := domain.WithOrg(t.Context(), DefaultOrgID)
	f := Factory{ID: domain.FactoryID(uuid.NewString()), Name: "acme-support", Definition: json.RawMessage(`{"v": 1}`)}

	if err := s.SaveFactory(ctx, f); err != nil {
		t.Fatal(err)
	}

	f.Name, f.Title, f.Definition = "acme-help", "Help desk", json.RawMessage(`{"v": 2}`)
	if err := s.SaveFactory(ctx, f); err != nil {
		t.Fatal(err)
	}

	var name, title, def string
	if err := asOrg(t, s.pool, DefaultOrgID, "SELECT name, title, definition::text FROM factories WHERE id = '"+string(f.ID)+"'",
		&name, &title, &def); err != nil {
		t.Fatal(err)
	}

	if name != "acme-help" || title != "Help desk" || def != `{"v": 2}` {
		t.Errorf("factory = %q, %q, %s; want the second save", name, title, def)
	}
}

// newTestPool returns a goal_app pool on a fresh schema, migrated from zero as goal_owner: the
// roles goald runs with.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	db := newTestDB(t, OwnerRole)
	migrate(t, db.owner)

	return openPool(t, db.app)
}

// testDB is a fresh schema of the database at GOAL_TEST_DATABASE_URL (PostgreSQL 18 from
// compose.yaml; `task test-integration` sets it up), dropped when the test ends. Its URLs connect
// as the bootstrap superuser of that URL (admin) or switch to goal_owner or goal_app with the
// role startup parameter, so the tests never need, set or change the roles' passwords.
type testDB struct {
	schema, admin, owner, app string
}

// newTestDB creates the roles (roles.sql: idempotent, without LOGIN) and a schema owned by
// schemaOwner (OwnerRole as after `goal db init`, or "" for the bootstrap user as before it).
func newTestDB(t *testing.T, schemaOwner string) testDB {
	t.Helper()

	base := os.Getenv("GOAL_TEST_DATABASE_URL")
	if base == "" {
		t.Fatal("GOAL_TEST_DATABASE_URL is not set; run `task test-integration`")
	}

	execSQL(t, base, "BEGIN; SELECT pg_advisory_xact_lock(hashtext('goal.roles')); "+rolesSQL+"; COMMIT")

	schema := "goal_test_" + randomHex(t)
	create := "CREATE SCHEMA " + schema

	if schemaOwner != "" {
		create += " AUTHORIZATION " + schemaOwner + "; GRANT USAGE ON SCHEMA " + schema + " TO " + AppRole
	}

	execSQL(t, base, create)
	t.Cleanup(func() { execSQL(t, base, "DROP SCHEMA "+schema+" CASCADE") })

	return testDB{
		schema: schema,
		admin:  withParams(t, base, map[string]string{"search_path": schema}),
		owner:  withParams(t, base, map[string]string{"search_path": schema, "role": OwnerRole}),
		app:    withParams(t, base, map[string]string{"search_path": schema, "role": AppRole}),
	}
}

func withParams(t *testing.T, base string, params map[string]string) string {
	t.Helper()

	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}

	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}

	u.RawQuery = q.Encode()

	return u.String()
}

func openPool(t *testing.T, url string) *pgxpool.Pool {
	t.Helper()

	pool, err := Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(pool.Close)

	return pool
}

// migrate applies every migration as goal_owner, as goald does.
func migrate(t *testing.T, ownerURL string) {
	t.Helper()

	if err := Migrate(t.Context(), openPool(t, ownerURL)); err != nil {
		t.Fatal(err)
	}
}

// execSQL runs one statement on its own connection, which outlives the test's context.
func execSQL(t *testing.T, url, sql string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}

	_, err = conn.Exec(ctx, sql)
	if err := errors.Join(err, conn.Close(ctx)); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func randomHex(t *testing.T) string {
	t.Helper()

	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}

	return hex.EncodeToString(b)
}

// newOrgID creates an organization through the Tenancy port.
func newOrgID(t *testing.T, s *Store) domain.OrgID {
	t.Helper()

	o, err := s.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: "acme-" + randomHex(t), Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}

	return o.ID
}

// saveFactory saves a new factory in the organization of ctx.
func saveFactory(ctx context.Context, t *testing.T, s *Store) domain.FactoryID {
	t.Helper()

	id := uuid.NewString()

	err := s.SaveFactory(ctx, Factory{
		ID: domain.FactoryID(id), Name: "acme-" + id[:8], Definition: json.RawMessage(`{"metadata": {"id": "` + id + `"}}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	return domain.FactoryID(id)
}

// runs reads an item's runs straight from agent_runs in the organization of ctx; the port has
// no reader yet.
func (s *Store) runs(ctx context.Context, t *testing.T, id domain.ItemID) []domain.Run {
	t.Helper()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = tx.Rollback(ctx) }()

	org, _ := domain.OrgFrom(ctx)
	if _, err := tx.Exec(ctx, "SELECT set_config('goal.org_id', $1, true)", string(org)); err != nil {
		t.Fatal(err)
	}

	rows, err := tx.Query(ctx, `
		SELECT id::text, factory_id::text, item_id::text, station, agent, model, status, coalesce(session_id, ''),
		       turns, input_tokens, output_tokens, cost_usd, coalesce(error, ''), started_at, ended_at
		FROM agent_runs WHERE item_id = $1 ORDER BY id`, string(id))
	if err != nil {
		t.Fatal(err)
	}

	runs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Run, error) {
		var (
			r     domain.Run
			ended *time.Time
		)

		err := row.Scan(&r.ID, &r.FactoryID, &r.ItemID, &r.Station, &r.Agent, &r.Model, &r.Result.Status,
			&r.Result.SessionID, &r.Result.Turns, &r.Result.InputTokens, &r.Result.OutputTokens, &r.Result.CostUSD,
			&r.Result.Error, &r.StartedAt, &ended)

		r.StartedAt = r.StartedAt.UTC()
		if ended != nil {
			r.EndedAt = ended.UTC()
		}

		return r, err
	})
	if err != nil {
		t.Fatal(err)
	}

	return runs
}
