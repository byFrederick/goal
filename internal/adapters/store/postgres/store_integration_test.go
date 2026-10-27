//go:build integration

package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

	"github.com/byFrederick/goal/db/migrations"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest/storetest"
)

func TestStore_Conformance(t *testing.T) {
	t.Parallel()

	storetest.Run(t, func(t *testing.T) storetest.Harness {
		t.Helper()

		s := New(newTestPool(t))

		return storetest.Harness{
			Store: s,
			Flow:  s,
			Spend: s,
			Audit: s,
			LeaseSpans: func(t *testing.T, org domain.OrgID, id domain.ItemID) []domain.LeaseSpan {
				t.Helper()

				return s.leaseSpans(domain.WithOrg(t.Context(), org), t, id)
			},
			NewOrg: func(t *testing.T) domain.OrgID { t.Helper(); return newOrgID(t, s) },
			NewFactory: func(t *testing.T, org domain.OrgID) domain.FactoryID {
				t.Helper()

				return saveFactory(domain.WithOrg(t.Context(), org), t, s)
			},
			SetSlotCaps: func(t *testing.T, org domain.OrgID, maxSlots int, factories map[domain.FactoryID]int) {
				t.Helper()

				u := ports.OrgUpdate{MaxSlots: &maxSlots, FactorySlots: factories}
				if _, err := s.UpdateOrg(domain.WithOrg(t.Context(), org), domain.LocalUser, u); err != nil {
					t.Fatalf("UpdateOrg slot caps: %v", err)
				}
			},
			NewVersion: func(t *testing.T, org domain.OrgID, factory domain.FactoryID) domain.DefinitionVersionID {
				t.Helper()

				return newPublishedVersion(t, s.pool, org, factory)
			},
		}
	})
}

func TestMigrate_RepeatableAndSafeToRunConcurrently(t *testing.T) {
	t.Parallel()

	// More callers than the pool has connections, on a database none has migrated yet: a
	// migration that needed a pool connection while it held the lock would deadlock here, and
	// the deadline turns that into a failure instead of a hung run.
	db := newTestDB(t, OwnerRole)
	pool := openPool(t, withPoolSize(t, db.owner, 2))

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	var wg sync.WaitGroup

	for range 4 {
		wg.Go(func() {
			if err := Migrate(ctx, pool); err != nil {
				t.Error(err)
			}
		})
	}

	wg.Wait()

	var version int64
	if err := pool.QueryRow(ctx, "SELECT max(version_id) FROM goose_db_version").Scan(&version); err != nil {
		t.Fatal(err)
	}

	if want := latestMigration(t); version != want {
		t.Errorf("schema version = %d; want %d", version, want)
	}
}

// withPoolSize is dbURL with a pool of at most size connections.
func withPoolSize(t *testing.T, dbURL string, size int) string {
	t.Helper()

	u, err := url.Parse(dbURL)
	if err != nil {
		t.Fatal(err)
	}

	q := u.Query()
	q.Set("pool_max_conns", strconv.Itoa(size))
	u.RawQuery = q.Encode()

	return u.String()
}

// latestMigration is the number of the newest file in db/migrations/.
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

// newTestPool returns a goal_app pool on a fresh database, migrated from zero as goal_owner: the
// roles goald runs with.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	db := newTestDB(t, OwnerRole)
	migrate(t, db.owner)

	return openPool(t, db.app)
}

// testDB is a fresh database of the PostgreSQL at GOAL_TEST_DATABASE_URL (PostgreSQL 18 from
// compose.yaml; `task test-integration` sets it up), dropped when the test ends: a database, not
// a schema of a shared one, since migration 00040 creates schema auth, which a database holds
// once. Goal's tables are in its public schema. Its URLs connect as the bootstrap superuser of
// that URL (admin) or switch to goal_owner, goal_app or goal_auth with the role startup
// parameter, so the tests never need, set or change the roles' passwords.
type testDB struct {
	schema, admin, owner, app, auth string
}

// newTestDB creates the roles (roles.sql: idempotent, without LOGIN) and a database. With
// schemaOwner OwnerRole it is as after `goal db init`: goal_owner may create in it and in its
// public schema, the other roles may use them, and PUBLIC may neither connect nor create
// temporary tables. With "" it is as before the roles: only the bootstrap user may create there.
func newTestDB(t *testing.T, schemaOwner string) testDB {
	t.Helper()

	base := os.Getenv("GOAL_TEST_DATABASE_URL")
	if base == "" {
		t.Fatal("GOAL_TEST_DATABASE_URL is not set; run `task test-integration`")
	}

	execSQL(t, base, "BEGIN; SELECT pg_advisory_xact_lock(hashtext('goal.roles')); "+rolesSQL+"; COMMIT")

	name := "goal_test_" + randomHex(t)
	execSQL(t, base, "CREATE DATABASE "+name)
	t.Cleanup(func() { execSQL(t, base, "DROP DATABASE "+name+" WITH (FORCE)") })

	db := testDB{
		schema: publicSchema,
		admin:  withParams(t, base, name, ""),
		owner:  withParams(t, base, name, OwnerRole),
		app:    withParams(t, base, name, AppRole),
		auth:   withParams(t, base, name, AuthRole),
	}

	if schemaOwner != "" {
		execSQL(t, db.admin, "REVOKE TEMPORARY, CONNECT ON DATABASE "+name+" FROM PUBLIC; "+
			"GRANT CONNECT, CREATE ON DATABASE "+name+" TO "+schemaOwner+"; "+
			"GRANT CONNECT ON DATABASE "+name+" TO goal_app, goal_auth; "+
			"GRANT USAGE, CREATE ON SCHEMA public TO "+schemaOwner+"; GRANT USAGE ON SCHEMA public TO goal_app, goal_auth; CREATE EXTENSION IF NOT EXISTS vector")
	}

	return db
}

// publicSchema holds Goal's tables in every test database.
const publicSchema = "public"

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

// upgrade migrates a database from an older migration the way an install upgrades: the previous
// release migrates it up to 00049 and its goald ends every legacy import, then this release
// migrates the rest.
func upgrade(t *testing.T, ownerURL string) {
	t.Helper()

	owner := openPool(t, ownerURL)
	migrateTo(t, owner, legacyImportDropped-1)
	endLegacyImports(t, owner)
	migrate(t, ownerURL)
}

// endLegacyImports ends every organization's legacy import, as the previous release's goald did
// at its start.
func endLegacyImports(t *testing.T, owner *pgxpool.Pool) {
	t.Helper()

	if _, err := owner.Exec(t.Context(), `DELETE FROM legacy_import_pending`); err != nil {
		t.Fatal(err)
	}
}

// execSQL runs one statement on its own connection, which outlives the test's context. Its two
// minutes are for DROP DATABASE … WITH (FORCE), which ends the connections still open and, with a
// busy server, takes a while to remove the files (task test-integration sweeps what a killed run
// leaves).
func execSQL(t *testing.T, url, sql string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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

// saveFactory registers a new factory in the organization of ctx.
func saveFactory(ctx context.Context, t *testing.T, s *Store) domain.FactoryID {
	t.Helper()

	org, _ := domain.OrgFrom(ctx)
	id := domain.FactoryID(newUUID(t))
	legacyFactory(t, s.pool, org, id, "acme-"+string(id[len(id)-12:]))

	return id
}

// legacyFactory registers a factory row with no definition version in org, as goald did before
// versions: the row items and runs reference.
func legacyFactory(t *testing.T, pool *pgxpool.Pool, org domain.OrgID, id domain.FactoryID, name string) {
	t.Helper()

	err := inOrgTx(t, pool, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO factories (id, name, definition, definition_sha256) VALUES ($1, $2, '{}', $3)`,
			string(id), name, strings.Repeat("0", 64))

		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// newPublishedVersion inserts the next published definition version of factory in org, with an
// empty manifest: what a run's definition_version_id references.
func newPublishedVersion(t *testing.T, pool *pgxpool.Pool, org domain.OrgID, factory domain.FactoryID) domain.DefinitionVersionID {
	t.Helper()

	id := newUUID(t)

	err := inOrgTx(t, pool, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `
			INSERT INTO definition_versions (id, factory_id, state, number, manifest, created_by, updated_by, published_by, published_at)
			SELECT $1, $2, 'published', coalesce(max(number), 0) + 1, '{}', 'user:local', 'user:local', 'user:local', now()
			FROM definition_versions WHERE factory_id = $2`, id, string(factory))

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return domain.DefinitionVersionID(id)
}

// leaseSpans reads an item's lease spans straight from lease_spans in the organization of ctx, in
// the order they ended; no port reads them one item at a time.
func (s *Store) leaseSpans(ctx context.Context, t *testing.T, id domain.ItemID) []domain.LeaseSpan {
	t.Helper()

	org, _ := domain.OrgFrom(ctx)

	var spans []domain.LeaseSpan

	err := inOrgTx(t, s.pool, org, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT item_id::text, station, agent, slot, rework_round, acquired_at, ended_at, outcome
			FROM lease_spans WHERE item_id = $1 ORDER BY ended_at, id`, string(id))
		if err != nil {
			return err
		}

		spans, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.LeaseSpan, error) {
			var l domain.LeaseSpan

			err := row.Scan(&l.ItemID, &l.Station, &l.Agent, &l.Slot, &l.ReworkRound, &l.AcquiredAt, &l.EndedAt, &l.Outcome)

			return l, err
		})

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return spans
}

// A note is audited in its transaction, by its length: the text stays on the item only.
func TestStore_NoteIsAuditedByLength(t *testing.T) {
	t.Parallel()

	s := New(newTestPool(t))
	ctx := domain.WithOrg(t.Context(), newOrgID(t, s))
	f := saveFactory(ctx, t, s)

	it, err := s.CreateItem(ctx, domain.NewItem{
		FactoryID: f, Type: "task", Title: "an item", Station: stWork, State: domain.StateQueued, Class: domain.ClassStandard,
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = s.AppendNote(ctx, domain.Event{
		FactoryID: f, ItemID: it.ID, At: time.Now(), Kind: domain.EventNote, Station: stWork, Actor: domain.LocalUser.Name,
		Data: map[string]any{"text": "Check the pool. ✓", "actor": domain.LocalUser.Ref()},
	}, domain.LocalUser)
	if err != nil {
		t.Fatal(err)
	}

	entries, err := s.AuditLog(ctx, ports.AuditQuery{Action: domain.AuditItemNoted, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 {
		t.Fatalf("item.noted entries = %+v, want one", entries)
	}

	e := entries[0]
	if e.TargetKind != domain.AuditTargetItem || e.TargetID != string(it.ID) || e.FactoryID != f || e.Actor != domain.LocalUser ||
		e.Data["length"] != float64(17) || len(e.Data) != 1 {
		t.Errorf("audit entry %+v, want the item, the local user and only the note's length (17)", e)
	}
}

// A transition's audit entry is written with it, and not when its guard refuses it.
func TestStore_TransitionAuditsWithTheChange(t *testing.T) {
	t.Parallel()

	s := New(newTestPool(t))
	ctx := domain.WithOrg(t.Context(), newOrgID(t, s))
	f := saveFactory(ctx, t, s)
	now := time.Now()

	it, err := s.CreateItem(ctx, domain.NewItem{
		FactoryID: f, Type: "task", Title: "an item", Station: stWork, State: domain.StateQueued, Class: domain.ClassStandard,
		CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	pause := ports.Transition{
		ItemID: it.ID, From: it.Position(), To: domain.Position{Station: stWork, State: domain.StateBlocked}, At: now,
		Audit: &domain.AuditEntry{
			At: now, Actor: domain.LocalUser, Action: domain.AuditItemPaused, TargetKind: domain.AuditTargetItem,
			TargetID: string(it.ID), FactoryID: f, Data: map[string]any{"station": stWork},
		},
	}

	stale := pause
	stale.From.State = domain.StateBlocked

	if _, err := s.Transition(ctx, stale); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("stale transition = %v; want ErrConflict", err)
	}

	if _, err := s.Transition(ctx, pause); err != nil {
		t.Fatal(err)
	}

	entries, err := s.AuditLog(ctx, ports.AuditQuery{Action: domain.AuditItemPaused, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 || entries[0].TargetID != string(it.ID) || entries[0].Data["station"] != stWork {
		t.Errorf("item.paused entries = %+v; want one, of the applied transition", entries)
	}
}
