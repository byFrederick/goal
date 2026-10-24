//go:build integration

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"maps"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/byFrederick/goal/internal/adapters/envsecrets"
	"github.com/byFrederick/goal/internal/adapters/postgres"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/definitions"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/factorystore"
	"github.com/byFrederick/goal/internal/ports"
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
  runtime: { runner: docker, image: ghcr.io/acme/agent:1.0.0, auth: anthropic-api-key }
  knowledgeBase: { collections: [notes] }
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
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if _, err := admin.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)"); err != nil {
			t.Errorf("drop %s: %v", name, err)
		}
	})

	// What goal db init grants.
	db := openPool(t, withParams(t, base, name, ""))
	exec(t, db, "REVOKE TEMPORARY, CONNECT ON DATABASE "+name+" FROM PUBLIC; "+
		"GRANT CONNECT, CREATE ON DATABASE "+name+" TO goal_owner; GRANT CONNECT ON DATABASE "+name+" TO goal_app, goal_auth; "+
		"GRANT USAGE, CREATE ON SCHEMA public TO goal_owner; GRANT USAGE ON SCHEMA public TO goal_app, goal_auth")

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

// An install from before versions: the factory's files under the bucket's oldest layout, its
// row as goald registered it, and an item on its line. Startup imports it as version 1, once;
// the item stays and runs on that version.
func TestStartup_ImportsALegacyFactoryOnce(t *testing.T) {
	t.Parallel()

	db := newDatabase(t)
	objects := porttest.NewObjectStore()
	files := acme("# writer\r\nkept byte for byte  \n")

	for p, body := range files {
		if _, err := objects.Put(t.Context(), factorystore.LegacyRoot+string(acmeID)+"/"+p, body); err != nil {
			t.Fatal(err)
		}
	}

	exec(t, db.admin, `INSERT INTO factories (id, org_id, name, definition, definition_sha256) VALUES ($1, $2, 'acme-notes', '{}', $3)`,
		string(acmeID), string(postgres.DefaultOrgID), strings.Repeat("0", 64))

	store := postgres.New(db.pool)
	defs := postgres.NewDefinitions(db.pool, objects, nil)
	ctx := domain.WithOrg(t.Context(), postgres.DefaultOrgID)

	it, err := store.CreateItem(ctx, domain.NewItem{
		FactoryID: acmeID, Type: "task", Title: "Write the notes", Station: "write", State: domain.StateQueued,
		Class: domain.ClassStandard, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	s, err := settingsFrom(envOf(nil))
	if err != nil {
		t.Fatal(err)
	}

	authors := authoring.New(authoring.Deps{
		Store: defs, Loads: store, Templates: objects, Credentials: s.credentials(envsecrets.New(os.LookupEnv)),
	})

	startup := func() string {
		t.Helper()

		var logs bytes.Buffer
		if err := importLegacy(t.Context(), s, objects, defs, store, publishProblems(authors), slog.New(slog.NewTextHandler(&logs, nil))); err != nil {
			t.Fatal(err)
		}

		return logs.String()
	}

	if logs := startup(); !strings.Contains(logs, `msg="legacy definitions imported" factories=1 versions=1 drafts=0 present=0 problems=0`) ||
		!strings.Contains(logs, `msg="legacy bucket layout migrated" copied=2`) {
		t.Errorf("startup log does not report the move and one version:\n%s", logs)
	}

	v1 := importedV1(ctx, t, defs, files)

	// The default organization's import ended: the next start reads no legacy prefix.
	if logs := startup(); strings.Contains(logs, "layout migrated") || !strings.Contains(logs, "factories=0 versions=0") {
		t.Errorf("second startup read the legacy layout again:\n%s", logs)
	}

	if vs, err := defs.Versions(ctx, acmeID, 0, 10); err != nil || len(vs) != 1 {
		t.Errorf("versions after a second startup = %d (%v), want 1", len(vs), err)
	}

	runsOn(ctx, t, store, defs, it, v1)
}

// runsOn checks that the item's factory runs v1 and the item, still where it was, starts a
// run on it.
func runsOn(ctx context.Context, t *testing.T, store *postgres.Store, defs ports.DefinitionStore, it domain.Item, v1 domain.DefinitionVersion) {
	t.Helper()

	lines, err := definitions.New(defs, store, nil, definitions.Config{}, nil).Refresh(t.Context())
	if err != nil || len(lines) != 1 || lines[0].VersionID != v1.ID {
		t.Fatalf("lines = %+v (%v), want the imported v1", lines, err)
	}

	if got, err := store.Item(ctx, it.ID); err != nil || got.Position() != it.Position() {
		t.Errorf("item = %+v (%v), want it where it was", got, err)
	}

	if _, err := store.StartRun(ctx, ports.NewRun{
		FactoryID: it.FactoryID, DefinitionVersionID: v1.ID, ItemID: it.ID, Station: it.Station, Agent: "writer", StartedAt: time.Now(),
	}); err != nil {
		t.Errorf("a run of the item on v1: %v", err)
	}
}

// importedV1 checks that acme runs version 1 holding exactly files, byte for byte, and
// returns it.
func importedV1(ctx context.Context, t *testing.T, defs ports.DefinitionStore, files map[string][]byte) domain.DefinitionVersion {
	t.Helper()

	rec, err := defs.FactoryRecord(ctx, acmeID)
	if err != nil || rec.Current == nil || rec.Current.Number != 1 {
		t.Fatalf("factory = %+v (%v), want the adopted row on v1", rec, err)
	}

	v1, err := defs.Version(ctx, rec.Current.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := slices.Sorted(maps.Keys(v1.Manifest)), slices.Sorted(maps.Keys(files)); !slices.Equal(got, want) {
		t.Errorf("manifest = %v, want %v", got, want)
	}

	for p, body := range files {
		if got, err := defs.Blob(ctx, v1.Manifest[p]); err != nil || !bytes.Equal(got, body) {
			t.Errorf("%s = %q (%v), want the legacy bytes %q", p, got, err, body)
		}
	}

	return v1
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
