//go:build integration

package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/byFrederick/goal/internal/adapters/masterkey"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest/connectiontest"
)

// The organization kind and connection seedConnections makes.
const (
	seededKind       = "acme-api"
	seededConnection = "acme"
	seededField      = "key"
)

// runUse is one run's use of connections.
var runUse = domain.ConnectionUse{Run: "run-1"}

// testKey is master keys ids with secrets derived from each id, so an id is the same key in every
// call.
func testKey(t *testing.T, ids ...string) ports.MasterKey {
	t.Helper()

	keys := make([]masterkey.Key, 0, len(ids))
	for _, id := range ids {
		sum := sha256.Sum256([]byte("goal-test-key/" + id))
		keys = append(keys, masterkey.Key{ID: id, Secret: sum[:]})
	}

	k, err := masterkey.New(keys...)
	if err != nil {
		t.Fatal(err)
	}

	return k
}

// connectionsDB is a migrated database with goald's store (goal_app) and goal db rekey's
// (goal_owner).
type connectionsDB struct {
	db    testDB
	store *Store
	owner *Store
	admin *pgxpool.Pool
}

func newConnectionsDB(t *testing.T) connectionsDB {
	t.Helper()

	db := newTestDB(t, OwnerRole)
	migrate(t, db.owner)

	return connectionsDB{db: db, store: New(openPool(t, db.app)), owner: New(openPool(t, db.owner)), admin: openPool(t, db.admin)}
}

func TestConnections_Conformance(t *testing.T) {
	t.Parallel()

	connectiontest.Run(t, func(t *testing.T) connectiontest.Harness {
		t.Helper()

		d := newConnectionsDB(t)

		return connectiontest.Harness{
			Connections: func(key ports.MasterKey) ports.Connections { return NewConnections(d.store, key) },
			Key:         func(ids ...string) ports.MasterKey { return testKey(t, ids...) },
			Audit:       d.store,
			NewOrg:      func(t *testing.T) domain.OrgID { t.Helper(); return newOrgID(t, d.store) },
			Rekey:       func(ctx context.Context, key ports.MasterKey) (int, error) { return Rekey(ctx, d.owner, key) },
		}
	})
}

// seedConnections gives org an organization kind and a sealed connection of it, "acme", whose
// secret is secret.
func seedConnections(t *testing.T, c *Connections, org domain.OrgID, secret string) domain.Connection {
	t.Helper()

	ctx := domain.WithOrg(t.Context(), org)
	kind := domain.ConnectionKind{Name: seededKind, Title: "Acme API", Spec: domain.KindSpec{
		Fields: []domain.KindField{{Name: seededField, Title: "Key", Secret: true, Required: true}, {Name: "region", Title: "Region"}},
		Env:    map[string]string{"ACME_KEY": "{{key}}"},
	}}

	if err := c.PutKind(ctx, kind, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	conn, err := c.Create(ctx, domain.NewConnection{
		Name: seededConnection, Kind: seededKind, Fields: map[string]string{"region": "eu"}, Secrets: map[string]string{seededField: secret},
	}, domain.LocalUser)
	if err != nil {
		t.Fatal(err)
	}

	return conn
}

// Spec 4 §11.2 row 1: as goal_app, organization B neither reads nor writes A's connections and
// kinds, nor builds a connection on A's kind; both see the platform's kinds.
func TestConnections_RLSAcrossOrgs(t *testing.T) {
	t.Parallel()

	d := newConnectionsDB(t)
	c := NewConnections(d.store, testKey(t, "k1"))
	acme, labs := newOrgID(t, d.store), newOrgID(t, d.store)

	if err := c.SyncPlatformKinds(t.Context(), []domain.ConnectionKind{{Name: "acme-docs", Title: "Docs", Spec: domain.KindSpec{
		Fields: []domain.KindField{{Name: "url", Title: "URL"}}, MCP: &domain.KindMCP{URL: "{{url}}"},
	}}}); err != nil {
		t.Fatal(err)
	}

	seedConnections(t, c, acme, "acme-secret")

	var acmeKind string
	if err := d.admin.QueryRow(t.Context(), `SELECT id::text FROM connection_kinds WHERE org_id = $1`, string(acme)).Scan(&acmeKind); err != nil {
		t.Fatal(err)
	}

	if err := inOrgTx(t, d.store.pool, labs, func(tx pgx.Tx) error { return seesOnlyPlatformKinds(t, tx) }); err != nil {
		t.Fatal(err)
	}

	for what, stmt := range map[string]string{
		"a connection into acme": `INSERT INTO connections (id, org_id, name, kind_id, created_by, updated_by)
			VALUES (gen_random_uuid(), '` + string(acme) + `', 'forged', '` + acmeKind + `', 'user:x', 'user:x')`,
		"a connection on acme's kind": `INSERT INTO connections (id, name, kind_id, created_by, updated_by)
			VALUES (gen_random_uuid(), 'forged', '` + acmeKind + `', 'user:x', 'user:x')`,
		"a platform kind": `INSERT INTO connection_kinds (id, org_id, name, spec, spec_sha256, created_by)
			VALUES (gen_random_uuid(), NULL, 'forged', '{}', repeat('a', 64), 'user:x')`,
	} {
		err := inOrgTx(t, d.store.pool, labs, func(tx pgx.Tx) error { _, err := tx.Exec(t.Context(), stmt); return err })
		if err == nil {
			t.Errorf("labs wrote %s", what)
		}
	}

	if got, err := c.List(domain.WithOrg(t.Context(), acme)); err != nil || len(got) != 1 || got[0].Description != "" {
		t.Errorf("acme's connections after labs tried = %+v, %v; want its one, unchanged", got, err)
	}
}

// seesOnlyPlatformKinds checks, in an organization without connections and kinds of its own,
// that it sees no connection and only the platform's kind, and changes nothing.
func seesOnlyPlatformKinds(t *testing.T, tx pgx.Tx) error {
	t.Helper()

	var conns, kinds, platform int
	if err := tx.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM connections), (SELECT count(*) FROM connection_kinds),
		(SELECT count(*) FROM connection_kinds WHERE org_id IS NULL)`).Scan(&conns, &kinds, &platform); err != nil {
		return err
	}

	if conns != 0 || kinds != 1 || platform != 1 {
		t.Errorf("labs sees %d connections and %d kinds (%d the platform's); want none of acme's and the platform's one", conns, kinds, platform)
	}

	for _, stmt := range []string{
		`UPDATE connections SET description = 'taken' WHERE true`,
		`DELETE FROM connections WHERE true`,
		`UPDATE connection_kinds SET spec = '{}' WHERE true`,
		`DELETE FROM connection_kinds WHERE true`,
	} {
		if tag, err := tx.Exec(t.Context(), stmt); err != nil || tag.RowsAffected() != 0 {
			t.Errorf("%s as labs: %v rows, %v; want none", stmt, tag.RowsAffected(), err)
		}
	}

	return nil
}

// Spec 4 §11.2 row 2: no column of any table, the audit log included, holds a secret in the
// clear, and the ciphertext does not contain it either.
func TestConnections_NoPlaintextInAnyColumn(t *testing.T) {
	t.Parallel()

	d := newConnectionsDB(t)
	c := NewConnections(d.store, testKey(t, "k1"))
	org := newOrgID(t, d.store)
	ctx := domain.WithOrg(t.Context(), org)

	const first, second = "sk-first-plaintext-4f1c", "sk-second-plaintext-9b2e"

	seedConnections(t, c, org, first)

	if _, err := c.Update(ctx, seededConnection, domain.ConnectionPatch{Secrets: map[string]string{seededField: second}}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	if _, err := c.Resolve(ctx, []string{seededConnection}, runUse); err != nil {
		t.Fatal(err)
	}

	if err := c.RecordTest(ctx, seededConnection, domain.ConnectionTest{OK: false, Error: "401"}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	tables := queryStrings(t, d.admin, `SELECT c.relname::text FROM pg_class c
		WHERE c.relnamespace = current_schema()::text::regnamespace AND c.relkind IN ('r', 'p')`)

	for _, table := range tables {
		var hits int
		// Every column as text, bytea included (its hex and its escape form), searched for either value.
		err := d.admin.QueryRow(t.Context(), `SELECT count(*) FROM `+pgx.Identifier{table}.Sanitize()+` r
			WHERE row_to_json(r)::text LIKE '%' || $1 || '%' OR row_to_json(r)::text LIKE '%' || $2 || '%'
			   OR row_to_json(r)::text LIKE '%' || encode(convert_to($1, 'UTF8'), 'hex') || '%'
			   OR row_to_json(r)::text LIKE '%' || encode(convert_to($2, 'UTF8'), 'hex') || '%'`, first, second).Scan(&hits)
		if err != nil {
			t.Fatalf("%s: %v", table, err)
		}

		if hits != 0 {
			t.Errorf("table %s has %d rows holding a secret value", table, hits)
		}
	}

	var (
		key    string
		cipher []byte
	)

	if err := d.admin.QueryRow(t.Context(), `SELECT key_id, ciphertext FROM connections`).Scan(&key, &cipher); err != nil {
		t.Fatal(err)
	}

	if key != "k1" || strings.Contains(string(cipher), second) || strings.Contains(string(cipher), seededConnection) {
		t.Errorf("sealed under %q, ciphertext %q; want k1 and nothing readable", key, cipher)
	}
}

// Spec 4 §11.2 row 2: a ciphertext copied to another connection, of the same organization or
// another, does not open there, and neither does one relabelled with another key id.
func TestConnections_AADMismatchRefused(t *testing.T) {
	t.Parallel()

	d := newConnectionsDB(t)
	key := testKey(t, "k2", "k1")
	c := NewConnections(d.store, key)
	acme, labs := newOrgID(t, d.store), newOrgID(t, d.store)

	seedConnections(t, c, acme, "acme-secret")
	seedConnections(t, c, labs, "labs-secret")

	acmeCtx := domain.WithOrg(t.Context(), acme)
	if _, err := c.Create(acmeCtx, domain.NewConnection{Name: "other", Kind: seededKind, Secrets: map[string]string{seededField: "other-secret"}}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	copySealed := func(t *testing.T, fromOrg, toOrg domain.OrgID, from, to string) {
		t.Helper()

		_, err := d.admin.Exec(t.Context(), `UPDATE connections dst SET key_id = src.key_id, nonce = src.nonce, ciphertext = src.ciphertext
			FROM connections src WHERE src.org_id = $1 AND src.name = $2 AND dst.org_id = $3 AND dst.name = $4`,
			string(fromOrg), from, string(toOrg), to)
		if err != nil {
			t.Fatal(err)
		}
	}

	copySealed(t, acme, acme, seededConnection, "other")

	if got, err := c.Resolve(acmeCtx, []string{"other"}, runUse); err == nil {
		t.Errorf("another connection's ciphertext opened: %+v", got)
	}

	copySealed(t, acme, labs, seededConnection, seededConnection)

	if got, err := c.Resolve(domain.WithOrg(t.Context(), labs), []string{seededConnection}, runUse); err == nil {
		t.Errorf("another organization's ciphertext opened: %+v", got)
	}

	// The key id is part of the associated data: relabelled k2, a value sealed under k1 does not
	// open, even though goald has both keys.
	if _, err := d.admin.Exec(t.Context(), `UPDATE connections SET key_id = 'k1' WHERE org_id = $1 AND name = $2`, string(acme), seededConnection); err != nil {
		t.Fatal(err)
	}

	got, err := c.Resolve(acmeCtx, []string{seededConnection}, runUse)
	if err == nil {
		t.Errorf("a relabelled ciphertext opened: %+v", got)
	} else if strings.Contains(err.Error(), "acme-secret") {
		t.Errorf("Resolve's refusal %v names the secret", err)
	}
}

// Platform kinds sync once per goald start; instances starting together take turns, and a kind a
// connection uses stays when a release drops it.
func TestSyncPlatformKinds_Concurrent(t *testing.T) {
	t.Parallel()

	d := newConnectionsDB(t)
	c := NewConnections(d.store, testKey(t, "k1"))
	kinds := []domain.ConnectionKind{{Name: "acme-docs", Title: "Docs", Spec: domain.KindSpec{
		Fields: []domain.KindField{{Name: "url", Title: "URL"}}, MCP: &domain.KindMCP{URL: "{{url}}"},
	}}}

	errs := make(chan error, 4)

	for range 4 {
		go func() { errs <- c.SyncPlatformKinds(t.Context(), kinds) }()
	}

	for range 4 {
		if err := <-errs; err != nil {
			t.Errorf("SyncPlatformKinds: %v", err)
		}
	}

	var n int
	if err := d.admin.QueryRow(t.Context(), `SELECT count(*) FROM connection_kinds WHERE org_id IS NULL`).Scan(&n); err != nil || n != 1 {
		t.Errorf("%d platform kinds, %v; want one", n, err)
	}
}

// An organization's purge removes its kinds and connections in one cascade.
func TestPurgeOrg_RemovesConnections(t *testing.T) {
	t.Parallel()

	d := newConnectionsDB(t)
	c := NewConnections(d.store, testKey(t, "k1"))
	deleted := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	org := newOrgID(t, d.store)
	seedConnections(t, c, org, "s")

	if err := d.store.DeleteOrg(domain.WithOrg(t.Context(), org), domain.LocalUser, deleted); err != nil {
		t.Fatal(err)
	}

	if ok, err := d.store.PurgeOrg(t.Context(), org, deleted.Add(time.Second), nil); !ok || err != nil {
		t.Fatalf("PurgeOrg = %v, %v", ok, err)
	}

	var n int
	if err := d.admin.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM connections) + (SELECT count(*) FROM connection_kinds WHERE org_id IS NOT NULL)`).
		Scan(&n); err != nil || n != 0 {
		t.Errorf("%d connection rows left, %v; want none", n, err)
	}
}

// goal db rekey refuses nothing it cannot open silently: a connection sealed under a key it lacks
// stops it with an error naming the connection, never a value.
func TestRekey_UnknownKeyStops(t *testing.T) {
	t.Parallel()

	d := newConnectionsDB(t)
	org := newOrgID(t, d.store)
	conn := seedConnections(t, NewConnections(d.store, testKey(t, "k1")), org, "s3cret")

	_, err := Rekey(t.Context(), d.owner, testKey(t, "k3", "k2"))
	if err == nil || !strings.Contains(err.Error(), string(conn.ID)) || strings.Contains(err.Error(), "s3cret") ||
		errors.Is(err, ports.ErrNoMasterKey) {
		t.Errorf("Rekey without the old key = %v; want an error naming the connection", err)
	}

	ids, err := ConnectionKeyIDs(t.Context(), d.store)
	if err != nil || len(ids) != 1 || ids[0] != "k1" {
		t.Errorf("ConnectionKeyIDs = %v, %v; want [k1] still", ids, err)
	}
}

// Spec 4 §8.3: each change and test of a connection is the organization's stream event
// connection, sent on commit and read back from the change's audit entry, so it names the
// connection as it is now; a forged one for a connection the organization has no such entry of
// (another organization's, an unknown one) is dropped.
func TestNotices_ConnectionChangesAreConnectionEvents(t *testing.T) {
	t.Parallel()

	d := newConnectionsDB(t)
	c := NewConnections(d.store, testKey(t, "k1"))
	acme, labs := newOrgID(t, d.store), newOrgID(t, d.store)
	ctx := domain.WithOrg(t.Context(), acme)
	notices := listening(t, NewNotices(d.store, nil), "") // the database is this test's: only its events

	if n := next(t, notices); !n.Resync {
		t.Fatalf("first notice = %+v; want Resync", n)
	}

	conn := seedConnections(t, c, acme, "s3cret")
	renamed := "acme-two"

	if _, err := c.Update(ctx, seededConnection, domain.ConnectionPatch{Name: &renamed}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	if err := c.RecordTest(ctx, renamed, domain.ConnectionTest{OK: true}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	forged := func(org domain.OrgID, id domain.ConnectionID) string {
		return fmt.Sprintf(`{"org_id":%q,"event":"connection","data":{"connection_id":%q,"name":"forged","change":"created"}}`, org, id)
	}

	for _, payload := range []string{forged(labs, conn.ID), forged(acme, "0b6f1c9e-0000-7000-8000-000000000999")} {
		if _, err := d.admin.Exec(t.Context(), "SELECT pg_notify('goal_org_events', $1)", payload); err != nil {
			t.Fatal(err)
		}
	}

	if err := c.Delete(ctx, renamed, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	for _, want := range []connectionEvent{
		{ConnectionID: string(conn.ID), Name: seededConnection, Change: changeCreated},
		{ConnectionID: string(conn.ID), Name: renamed, Change: changeUpdated},
		{ConnectionID: string(conn.ID), Name: renamed, Change: changeTested},
		{ConnectionID: string(conn.ID), Name: renamed, Change: changeDeleted},
	} {
		if got := nextConnectionEvent(t, notices, acme); got != want {
			t.Errorf("connection event = %+v; want %+v", got, want)
		}
	}
}

// nextConnectionEvent is the next notice, which must be org's connection event.
func nextConnectionEvent(t *testing.T, notices <-chan ports.ItemNotice, org domain.OrgID) connectionEvent {
	t.Helper()

	n := next(t, notices)
	if n.OrgID != org || n.Org == nil || n.Org.Name != "connection" || n.Org.FactoryID != "" {
		t.Fatalf("notice = %+v (org event %+v); want the organization's connection event", n, n.Org)
	}

	var got connectionEvent
	if err := json.Unmarshal(n.Org.Data, &got); err != nil {
		t.Fatal(err)
	}

	return got
}
