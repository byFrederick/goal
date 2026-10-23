//go:build integration

package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest/storetest"
)

func TestStore_Conformance(t *testing.T) {
	t.Parallel()

	storetest.Run(t, func(t *testing.T) storetest.Harness {
		t.Helper()

		s := New(newTestPool(t))

		return storetest.Harness{
			Store:      s,
			NewFactory: func(t *testing.T) domain.FactoryID { t.Helper(); return saveFactory(t, s) },
			Runs:       func(t *testing.T, id domain.ItemID) []domain.Run { t.Helper(); return s.runs(t, id) },
		}
	})
}

func TestMigrate_RepeatableAndSafeToRunConcurrently(t *testing.T) {
	t.Parallel()

	pool := newTestPool(t) // already migrated once

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

	if version != 3 {
		t.Errorf("schema version = %d; want 3", version)
	}
}

func TestCreateItem_UnsavedFactoryNotFound(t *testing.T) {
	t.Parallel()

	s := New(newTestPool(t))

	_, err := s.CreateItem(t.Context(), domain.NewItem{
		FactoryID: domain.FactoryID(uuid.NewString()), Type: "task", Title: "x", Station: "work",
		State: domain.StateQueued, Class: domain.ClassStandard, CreatedAt: time.Now(),
	})
	if !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("CreateItem = %v; want ErrNotFound", err)
	}
}

func TestSaveFactory_UpdatesTheDefinition(t *testing.T) {
	t.Parallel()

	s := New(newTestPool(t))
	f := Factory{ID: domain.FactoryID(uuid.NewString()), Name: "acme-support", Definition: json.RawMessage(`{"v": 1}`)}

	if err := s.SaveFactory(t.Context(), f); err != nil {
		t.Fatal(err)
	}

	f.Name, f.Title, f.Definition = "acme-help", "Help desk", json.RawMessage(`{"v": 2}`)
	if err := s.SaveFactory(t.Context(), f); err != nil {
		t.Fatal(err)
	}

	var name, title, def string
	if err := s.pool.QueryRow(t.Context(), "SELECT name, title, definition::text FROM factories WHERE id = $1", string(f.ID)).
		Scan(&name, &title, &def); err != nil {
		t.Fatal(err)
	}

	if name != "acme-help" || title != "Help desk" || def != `{"v": 2}` {
		t.Errorf("factory = %q, %q, %s; want the second save", name, title, def)
	}
}

// newTestPool returns a pool on a fresh schema of the database at GOAL_TEST_DATABASE_URL
// (PostgreSQL 18 from compose.yaml; `task test-integration` sets it up), migrated from zero,
// and drops the schema when the test ends.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	base := os.Getenv("GOAL_TEST_DATABASE_URL")
	if base == "" {
		t.Fatal("GOAL_TEST_DATABASE_URL is not set; run `task test-integration`")
	}

	schema := "goal_test_" + randomHex(t)
	execSQL(t, base, "CREATE SCHEMA "+schema)
	t.Cleanup(func() { execSQL(t, base, "DROP SCHEMA "+schema+" CASCADE") })

	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}

	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()

	pool, err := Open(t.Context(), u.String())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(pool.Close)

	if err := Migrate(t.Context(), pool); err != nil {
		t.Fatal(err)
	}

	return pool
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

func saveFactory(t *testing.T, s *Store) domain.FactoryID {
	t.Helper()

	id := uuid.NewString()

	err := s.SaveFactory(t.Context(), Factory{
		ID: domain.FactoryID(id), Name: "acme-" + id[:8], Definition: json.RawMessage(`{"metadata": {"id": "` + id + `"}}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	return domain.FactoryID(id)
}

// runs reads an item's runs straight from agent_runs; the port has no reader yet.
func (s *Store) runs(t *testing.T, id domain.ItemID) []domain.Run {
	t.Helper()

	rows, err := s.pool.Query(t.Context(), `
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
