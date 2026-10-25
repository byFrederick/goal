//go:build integration

package main

import (
	"context"
	"crypto/rand"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/byFrederick/goal/internal/adapters/postgres"
	"github.com/byFrederick/goal/internal/definitions"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

// These tests run goald's definition wiring on PostgreSQL 18 (a fresh schema of the database
// at GOAL_TEST_DATABASE_URL, migrated as goal_owner and used as goal_app) with the in-memory
// bucket; s3store's own integration tests cover the bucket against RustFS.

const acmeID domain.FactoryID = "8d4f2b1a-3c5e-4f60-9a7b-1c2d3e4f5a6b"

// acme is a small valid factory whose prompt is prompt.
func acme(prompt string) map[string][]byte {
	return map[string][]byte{
		"factory.yaml": []byte(`apiVersion: goal/v1
kind: Factory
metadata: { id: ` + string(acmeID) + `, name: acme-notes }
spec:
  runtime: { modelConnection: claude }
  agents:
    - { name: writer, prompt: ./agents/writer.md }
  board:
    stations:
      - { id: write, kind: agent, agent: writer }
      - { id: done, kind: done }
`),
		"agents/writer.md": []byte(prompt),
	}
}

// database is a migrated database of its own (migration 00040 creates schema auth, which a
// database holds once): pool connects as goal_app, admin as the bootstrap superuser.
type database struct {
	pool  *pgxpool.Pool
	admin *pgxpool.Pool
}

func newDatabase(t *testing.T) database {
	t.Helper()

	base := os.Getenv("GOAL_TEST_DATABASE_URL")
	if base == "" {
		t.Fatal("GOAL_TEST_DATABASE_URL is not set; run `task test-integration`")
	}

	admin := openPool(t, base)

	// The roles of roles.sql, created when missing and otherwise left as they are.
	for _, role := range []string{
		"goal_owner NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOCREATEDB",
		"goal_app NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOCREATEDB NOINHERIT",
		"goal_auth NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOCREATEDB NOINHERIT",
	} {
		exec(t, admin, "DO $$ BEGIN CREATE ROLE "+role+"; EXCEPTION WHEN duplicate_object OR unique_violation THEN NULL; END $$")
	}

	name := "goal_test_" + strings.ToLower(rand.Text()[:12])
	exec(t, admin, "CREATE DATABASE "+name)
	t.Cleanup(func() {
		// WITH (FORCE) ends the connections still open; under load that and removing the files can
		// take a while (task test-integration sweeps what a killed run leaves).
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		if _, err := admin.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)"); err != nil {
			t.Errorf("drop %s: %v", name, err)
		}
	})

	// What goal db init grants.
	db := openPool(t, withParams(t, base, name, ""))
	exec(t, db, "REVOKE TEMPORARY, CONNECT ON DATABASE "+name+" FROM PUBLIC; "+
		"GRANT CONNECT, CREATE ON DATABASE "+name+" TO goal_owner; GRANT CONNECT ON DATABASE "+name+" TO goal_app, goal_auth; "+
		"GRANT USAGE, CREATE ON SCHEMA public TO goal_owner; GRANT USAGE ON SCHEMA public TO goal_app, goal_auth; CREATE EXTENSION IF NOT EXISTS vector")

	if err := postgres.Migrate(t.Context(), openPool(t, withParams(t, base, name, postgres.OwnerRole))); err != nil {
		t.Fatal(err)
	}

	return database{pool: openPool(t, withParams(t, base, name, postgres.AppRole)), admin: db}
}

// withParams is base on database, switching to role unless it is "".
func withParams(t *testing.T, base, database, role string) string {
	t.Helper()

	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}

	u.Path = "/" + database

	if role != "" {
		q := u.Query()
		q.Set("role", role)
		u.RawQuery = q.Encode()
	}

	return u.String()
}

func openPool(t *testing.T, url string) *pgxpool.Pool {
	t.Helper()

	pool, err := postgres.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(pool.Close)

	return pool
}

func exec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()

	if _, err := pool.Exec(t.Context(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

// One goald publishes; another, listening, runs the new version within a second.
func TestDefinitions_AnotherGoaldReloadsWithinASecondOfAPublish(t *testing.T) {
	t.Parallel()

	db := newDatabase(t)
	objects := porttest.NewObjectStore()
	store := postgres.New(db.pool)
	publisher := postgres.NewDefinitions(db.pool, objects, nil)
	ctx := domain.WithOrg(t.Context(), postgres.DefaultOrgID)

	var (
		mu    sync.Mutex
		seen  = map[domain.DefinitionVersionID]time.Time{}
		added = make(chan struct{}, 1)
	)

	cache := definitions.New(postgres.NewDefinitions(db.pool, objects, nil), store, func(_ context.Context, lines []domain.Line) error {
		mu.Lock()
		for _, l := range lines {
			if _, ok := seen[l.VersionID]; !ok {
				seen[l.VersionID] = time.Now()
			}
		}
		mu.Unlock()

		select {
		case added <- struct{}{}:
		default:
		}

		return nil
	}, definitions.Config{}, nil)

	listenCtx, stop := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() {
		done <- cache.Listen(listenCtx, postgres.NewDefinitionNotices(postgres.New(db.pool), nil))
	}()

	t.Cleanup(func() {
		stop()

		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			t.Error(err)
		}
	})

	<-added // the first resync: the listener is connected

	for i, prompt := range []string{"# writer\n", "# writer, v2\n"} {
		v, err := porttest.Publish(ctx, publisher, acmeID, "acme-notes", acme(prompt))
		if err != nil {
			t.Fatal(err)
		}

		published := time.Now()
		deadline := time.After(time.Second)

		for {
			mu.Lock()
			at, ok := seen[v.ID]
			mu.Unlock()

			if ok {
				t.Logf("v%d running %s after its publish", i+1, at.Sub(published).Round(time.Millisecond))
				break
			}

			select {
			case <-added:
			case <-deadline:
				t.Fatalf("v%d not running within 1s of its publish", i+1)
			}
		}
	}
}
