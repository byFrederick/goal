//go:build integration

package postgres

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/byFrederick/goal/internal/core/domain"
)

// connectionsConverted is the release the refusal of 00135 names.
const connectionsConverted = "run a release that converts connections (ADR 0037)"

// acmeGit names the seeded server and secret.
const acmeGit = "acme-git"

// seedConnections is a database at 00134 with an organization that has a connection of a
// platform kind, sealed under key id old, and a factory with a published version.
type seedConnections struct {
	db         testDB
	owner      *pgxpool.Pool
	store      *Store
	org        domain.OrgID
	factory    uuid.UUID
	version    uuid.UUID
	connection uuid.UUID
}

func newSeedConnections(t *testing.T) seedConnections {
	t.Helper()

	db := newTestDB(t, OwnerRole)
	owner := openPool(t, db.owner)
	migrateTo(t, owner, 134)

	store := New(openPool(t, db.app))
	s := seedConnections{
		db: db, owner: owner, store: store, org: newOrgID(t, store),
		factory: uuid.Must(uuid.NewV7()), version: uuid.Must(uuid.NewV7()), connection: uuid.Must(uuid.NewV7()),
	}
	kind := uuid.Must(uuid.NewV7())

	s.exec(t, `INSERT INTO connection_kinds (id, org_id, name, spec, spec_sha256, created_by)
		VALUES ($1, NULL, 'acme-git', '{}', repeat('a', 64), 'system:goald')`, kind)
	s.exec(t, `INSERT INTO connections (id, org_id, name, kind_id, secret_fields, key_id, nonce, ciphertext, created_by, updated_by)
		VALUES ($1, $2, 'acme-git', $3, '{token}', 'old', $4, 'sealed', 'user:local', 'user:local')`,
		s.connection, string(s.org), kind, bytes.Repeat([]byte{1}, 12))
	s.exec(t, `INSERT INTO factories (id, org_id, name, definition, definition_sha256) VALUES ($1, $2, 'acme', '{}', 'sha')`,
		s.factory, string(s.org))
	s.exec(t, `INSERT INTO definition_versions (id, org_id, factory_id, state, number, manifest, created_by, updated_by, published_by, published_at)
		VALUES ($1, $2, $3, 'published', 1, '{}', 'user:local', 'user:local', 'user:local', now())`, s.version, string(s.org), s.factory)

	return s
}

func (s seedConnections) exec(t *testing.T, sql string, args ...any) {
	t.Helper()

	if _, err := s.owner.Exec(t.Context(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

// converted records the factory's conversion, as the release that converts connections did.
func (s seedConnections) converted(t *testing.T) {
	t.Helper()

	s.exec(t, `INSERT INTO factory_conversions (org_id, factory_id, from_version, to_version, at) VALUES ($1, $2, $3, $3, now())`,
		string(s.org), s.factory, s.version)
}

// rule inserts a standing rule keyed by the connection, revoked or not, and returns its id.
func (s seedConnections) rule(t *testing.T, revoked bool) uuid.UUID {
	t.Helper()

	id := uuid.Must(uuid.NewV7())
	s.exec(t, `INSERT INTO standing_rules (id, org_id, connection_id, tool, agent, created_by, item_id, revoked_by, revoked_at)
		VALUES ($1, $2, $3, 'create_comment', 'architect', 'user:local', $4,
		        CASE WHEN $5 THEN 'user:local' END, CASE WHEN $5 THEN now() END)`,
		id, string(s.org), s.connection, uuid.Must(uuid.NewV7()), revoked)

	return id
}

// standingRuleIDs are the ids of every standing rule, in order.
func (s seedConnections) standingRuleIDs(t *testing.T) []string {
	t.Helper()

	rows, err := s.owner.Query(t.Context(), `SELECT id::text FROM standing_rules ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}

	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}

	return ids
}

func (s seedConnections) tableExists(t *testing.T, name string) bool {
	t.Helper()

	var ok bool
	if err := s.owner.QueryRow(t.Context(), `SELECT to_regclass($1) IS NOT NULL`, name).Scan(&ok); err != nil {
		t.Fatal(err)
	}

	return ok
}

// assertConnectionsDropped checks that no connection table or connection_id column is left.
func (s seedConnections) assertConnectionsDropped(t *testing.T) {
	t.Helper()

	for _, table := range []string{"connections", "connection_kinds"} {
		if s.tableExists(t, table) {
			t.Errorf("table %s after 00135; want it dropped", table)
		}
	}

	var columns int
	if err := s.owner.QueryRow(t.Context(), `SELECT count(*) FROM information_schema.columns
		WHERE table_name IN ('standing_rules', 'knowledge_sources') AND column_name = 'connection_id'`).Scan(&columns); err != nil || columns != 0 {
		t.Errorf("%d connection_id columns after 00135, %v; want none", columns, err)
	}
}

// Migration 00135 drops connections on a converted database: the revoked rules keyed by a
// connection go, a rule keyed by a server and a synced source that reads with a secret stay, and
// goal_connection_key_ids no longer reads the connection's key id.
func TestMigrate_DropsConnections(t *testing.T) {
	t.Parallel()

	s := newSeedConnections(t)
	s.converted(t)
	revoked := s.rule(t, true)

	ctx := domain.WithOrg(t.Context(), s.org)
	if _, err := NewSecrets(s.store, testKey(t, "k1")).Create(ctx,
		domain.NewSecret{Name: acmeGit, Scope: domain.SecretScope{All: true}, Value: "test-token"}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	server, source := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	s.exec(t, `INSERT INTO standing_rules (id, org_id, factory_id, environment, server, tool, agent, created_by, item_id)
		VALUES ($1, $2, $3, 'default', 'acme-git', 'get_issue', 'architect', 'user:local', $4)`,
		server, string(s.org), s.factory, uuid.Must(uuid.NewV7()))
	s.exec(t, `INSERT INTO knowledge_sources (id, org_id, name, title, kind, connection_id, secret_id, created_by)
		SELECT $1, $2, 'handbook', 'Handbook', 'synced', $3, id, 'user:local' FROM secrets WHERE org_id = $2 AND name = 'acme-git'`,
		source, string(s.org), s.connection)

	if ids, err := ConnectionKeyIDs(t.Context(), s.store); err != nil || !slices.Equal(ids, []string{"k1", "old"}) {
		t.Fatalf("ConnectionKeyIDs at 00134 = %v, %v; want the secret's k1 and the connection's old", ids, err)
	}

	migrate(t, s.db.owner)

	s.assertConnectionsDropped(t)

	if rules := s.standingRuleIDs(t); !slices.Equal(rules, []string{server.String()}) || slices.Contains(rules, revoked.String()) {
		t.Errorf("standing rules after 00135 = %v; want only the server rule %s", rules, server)
	}

	got, err := NewKnowledge(s.store).Sources(ctx)
	if err != nil || len(got) != 1 || string(got[0].ID) != source.String() || got[0].Secret != acmeGit {
		t.Errorf("knowledge sources after 00135 = %+v, %v; want handbook, reading with secret acme-git", got, err)
	}

	if ids, err := ConnectionKeyIDs(t.Context(), s.store); err != nil || !slices.Equal(ids, []string{"k1"}) {
		t.Errorf("ConnectionKeyIDs after 00135 = %v, %v; want only the secret's k1", ids, err)
	}
}

// Migration 00135 refuses a database that was not converted, before it drops anything: it names
// the release to run first, and the connections are still there.
func TestMigrate_DropsConnectionsRefusesUnconverted(t *testing.T) {
	t.Parallel()

	for name, seed := range map[string]func(*testing.T, seedConnections){
		"a published factory without a conversion": func(*testing.T, seedConnections) {},
		"an active rule keyed by a connection": func(t *testing.T, s seedConnections) {
			t.Helper()
			s.converted(t)
			s.rule(t, false)
		},
		"a knowledge source with a connection and no secret": func(t *testing.T, s seedConnections) {
			t.Helper()
			s.converted(t)
			s.exec(t, `INSERT INTO knowledge_sources (id, org_id, name, title, kind, connection_id, next_sync_at, created_by)
				VALUES ($1, $2, 'tickets', 'Tickets', 'live', $3, NULL, 'user:local')`, uuid.Must(uuid.NewV7()), string(s.org), s.connection)
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := newSeedConnections(t)
			seed(t, s)

			err := Migrate(t.Context(), s.owner)
			if err == nil || !strings.Contains(err.Error(), connectionsConverted) {
				t.Fatalf("Migrate = %v; want a refusal that says to %s first", err, connectionsConverted)
			}

			if !s.tableExists(t, "connections") || !s.tableExists(t, "connection_kinds") {
				t.Error("a refused 00135 dropped the connection tables; want them kept")
			}
		})
	}
}
