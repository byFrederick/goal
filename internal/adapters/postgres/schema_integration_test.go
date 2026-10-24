//go:build integration

package postgres

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// orgTables are the tables 00006 gave org_id; later phases add theirs.
var orgTables = []string{"factories", "work_items", "leases", "agent_runs", "item_events", "andon_pulls", "audit_log"}

// withoutOrgID are the tables that carry no org_id, and why. A phase that adds a table without
// org_id adds it here in the same pull request, with its reason.
var withoutOrgID = map[string]string{
	"goose_db_version": "the migration log, goal_owner's alone",
	"organizations":    "the tenant itself; its policy compares id with goal_org()",
}

// The tenancy kit, checked on the fully migrated schema: every table with org_id has row-level
// security with the org_isolation policy on goal_org() and takes the transaction's organization
// by default; every other table is allowlisted; goal_app owns nothing and cannot rewrite the
// append-only logs; views run with the caller's rights; definer functions pin their search_path.
func TestSchema_EveryOrgTableHasRLS(t *testing.T) {
	t.Parallel()

	db := newTestDB(t, OwnerRole)
	migrate(t, db.owner)
	admin := openPool(t, db.admin)

	tables := queryStrings(t, admin, `
		SELECT c.relname FROM pg_class c WHERE c.relnamespace = current_schema()::text::regnamespace AND c.relkind IN ('r', 'p')`)

	for _, table := range tables {
		hasOrgID := len(queryStrings(t, admin, `
			SELECT a.attname::text FROM pg_attribute a
			WHERE a.attrelid = (current_schema() || '.' || $1)::regclass AND a.attname = 'org_id' AND NOT a.attisdropped`, table)) == 1

		_, allowed := withoutOrgID[table]

		switch {
		case !hasOrgID && !allowed:
			t.Errorf("table %s has no org_id and is not in the allowlist", table)
		case hasOrgID && allowed:
			t.Errorf("table %s has org_id but is allowlisted", table)
		}

		if hasOrgID || table == "organizations" {
			checkPolicy(t, admin, table)
		}

		if hasOrgID {
			checkOrgDefault(t, admin, table)
		}
	}

	for _, want := range orgTables {
		if !slices.Contains(tables, want) {
			t.Errorf("table %s is missing", want)
		}
	}

	checkAppPrivileges(t, admin)

	for _, view := range queryStrings(t, admin, `
		SELECT c.relname FROM pg_class c
		WHERE c.relnamespace = current_schema()::text::regnamespace AND c.relkind = 'v'
		  AND NOT coalesce('security_invoker=true' = ANY (c.reloptions), false)`) {
		t.Errorf("view %s does not run with the caller's rights (security_invoker)", view)
	}

	for _, fn := range queryStrings(t, admin, `
		SELECT p.oid::regprocedure::text FROM pg_proc p
		WHERE p.pronamespace = current_schema()::text::regnamespace AND p.prosecdef
		  AND NOT EXISTS (SELECT 1 FROM unnest(p.proconfig) c WHERE c LIKE 'search_path=%')`) {
		t.Errorf("SECURITY DEFINER function %s does not pin its search_path", fn)
	}
}

func checkPolicy(t *testing.T, admin *pgxpool.Pool, table string) {
	t.Helper()

	var (
		rls         bool
		using, with string
	)

	err := admin.QueryRow(t.Context(), `
		SELECT c.relrowsecurity, coalesce(pg_get_expr(p.polqual, p.polrelid), ''), coalesce(pg_get_expr(p.polwithcheck, p.polrelid), '')
		FROM pg_class c LEFT JOIN pg_policy p ON p.polrelid = c.oid AND p.polname = 'org_isolation'
		WHERE c.oid = (current_schema() || '.' || $1)::regclass`, table).Scan(&rls, &using, &with)
	if err != nil {
		t.Fatal(err)
	}

	if !rls || !strings.Contains(using, "goal_org()") || !strings.Contains(with, "goal_org()") {
		t.Errorf("table %s: row-level security %v, org_isolation USING %q WITH CHECK %q; want it on, both on goal_org()",
			table, rls, using, with)
	}
}

func checkOrgDefault(t *testing.T, admin *pgxpool.Pool, table string) {
	t.Helper()

	var (
		notNull bool
		def     string
	)

	err := admin.QueryRow(t.Context(), `
		SELECT a.attnotnull, coalesce(pg_get_expr(d.adbin, d.adrelid), '') FROM pg_attribute a
		LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		WHERE a.attrelid = (current_schema() || '.' || $1)::regclass AND a.attname = 'org_id'`, table).Scan(&notNull, &def)
	if err != nil {
		t.Fatal(err)
	}

	if !notNull || def != "goal_org()" {
		t.Errorf("table %s: org_id NOT NULL %v, default %q; want NOT NULL DEFAULT goal_org()", table, notNull, def)
	}
}

func checkAppPrivileges(t *testing.T, admin *pgxpool.Pool) {
	t.Helper()

	if owned := queryStrings(t, admin, `SELECT relname::text FROM pg_class WHERE relowner = 'goal_app'::regrole`); len(owned) != 0 {
		t.Errorf("goal_app owns %v; want nothing", owned)
	}

	for _, table := range []string{"audit_log", "item_events"} {
		for _, priv := range []string{"UPDATE", "DELETE", "TRUNCATE"} {
			var has bool
			if err := admin.QueryRow(t.Context(), "SELECT has_table_privilege('goal_app', current_schema() || '.' || $1, $2)",
				table, priv).Scan(&has); err != nil || has {
				t.Errorf("goal_app %s on %s = %v, %v; want no", priv, table, has, err)
			}
		}
	}
}

func queryStrings(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) []string {
	t.Helper()

	rows, err := pool.Query(t.Context(), sql, args...)
	if err != nil {
		t.Fatal(err)
	}

	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}

	return out
}

// rowsFromBefore are one row in every table, written as goald wrote them before organizations.
type rowsFromBefore struct {
	factory domain.FactoryID
	item    domain.ItemID
}

// insertRowsFromBefore writes them on a schema at version 3 (00001-00003).
func insertRowsFromBefore(t *testing.T, pool *pgxpool.Pool) rowsFromBefore {
	t.Helper()

	factory, item, run := uuid.NewString(), uuid.NewString(), uuid.NewString()
	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)

	// Every statement names all four parameters ($1 factory, $2 item, $3 run, $4 time).
	for _, stmt := range []string{
		`INSERT INTO factories (id, name, definition, definition_sha256, last_item_number)
		 SELECT $1, 'acme-before', '{}', 'sha', 1 WHERE $2::uuid IS NOT NULL AND $3::uuid IS NOT NULL AND $4::timestamptz IS NOT NULL`,
		`INSERT INTO work_items (id, factory_id, number, type, title, station, state, created_at)
		 SELECT $2, $1, 1, 'task', 'from before', 'work', 'in_progress', $4 WHERE $3::uuid IS NOT NULL`,
		`INSERT INTO leases (item_id, factory_id, station, agent, slot, acquired_at, expires_at)
		 SELECT $2, $1, 'work', 'worker', 0, $4, $4::timestamptz + interval '1 minute' WHERE $3::uuid IS NOT NULL`,
		`INSERT INTO agent_runs (id, factory_id, item_id, station, agent, model, started_at)
		 VALUES ($3, $1, $2, 'work', 'worker', 'model-a', $4)`,
		`INSERT INTO item_events (factory_id, item_id, run_id, at, kind, station, actor)
		 VALUES ($1, $2, $3, $4, 'run_started', 'work', 'worker')`,
		`INSERT INTO andon_pulls (factory_id, reason, pulled_by, pulled_at)
		 SELECT $1, 'rework', 'engine', $4 WHERE $2::uuid IS NOT NULL AND $3::uuid IS NOT NULL`,
	} {
		if _, err := pool.Exec(t.Context(), stmt, factory, item, run, at); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	return rowsFromBefore{factory: domain.FactoryID(factory), item: domain.ItemID(item)}
}

// A database from before organizations: every row lands in the default organization, which
// goal_app then reads and writes through the store, and no other organization sees.
func TestMigrate_BackfillsDefaultOrg(t *testing.T) {
	t.Parallel()

	db := newTestDB(t, OwnerRole)
	owner := openPool(t, db.owner)
	migrateTo(t, owner, 3)
	before := insertRowsFromBefore(t, owner)
	migrate(t, db.owner)
	expectBackfilled(t, owner)

	s := New(openPool(t, db.app))
	ctx := domain.WithOrg(t.Context(), DefaultOrgID)

	if it, err := s.Item(ctx, before.item); err != nil || it.OrgID != DefaultOrgID || it.Number != 1 {
		t.Errorf("Item in the default org = %+v, %v; want the item from before as #1", it, err)
	}

	if events, err := s.ItemEvents(ctx, before.item, 0, 10); err != nil || len(events) != 1 {
		t.Errorf("ItemEvents = %+v, %v; want the event from before", events, err)
	}

	if leases, err := s.Leases(ctx, before.factory, "work"); err != nil || len(leases) != 1 {
		t.Errorf("Leases = %+v, %v; want the lease from before", leases, err)
	}

	if it, err := s.CreateItem(ctx, domain.NewItem{
		FactoryID: before.factory, Type: typeTask, Title: "after", Station: stWork,
		State: domain.StateQueued, Class: domain.ClassStandard, CreatedAt: time.Now(),
	}); err != nil || it.Number != 2 {
		t.Errorf("CreateItem after the backfill = %+v, %v; want #2", it, err)
	}

	if _, err := s.Item(domain.WithOrg(t.Context(), newOrgID(t, s)), before.item); err == nil {
		t.Error("another organization sees the item from before")
	}
}

// expectBackfilled checks each table of 00006 has its one row from before, in the default org.
func expectBackfilled(t *testing.T, owner *pgxpool.Pool) {
	t.Helper()

	for _, table := range orgTables[:6] {
		var all, inDefault int
		if err := owner.QueryRow(t.Context(), "SELECT count(*), count(*) FILTER (WHERE org_id = $1) FROM "+table,
			string(DefaultOrgID)).Scan(&all, &inDefault); err != nil {
			t.Fatal(err)
		}

		if all != 1 || inDefault != 1 {
			t.Errorf("%s: %d rows, %d in the default org; want its row in the default org", table, all, inDefault)
		}
	}
}

// Raw SQL as goal_app without an organization fails on every org-owned table that has rows,
// for reads and for writes, instead of answering with every row. (On an empty table PostgreSQL
// may never evaluate the policy, and an empty answer leaks nothing.)
func TestRLS_RawSQLWithoutOrgFails(t *testing.T) {
	t.Parallel()

	db := newTestDB(t, OwnerRole)
	owner := openPool(t, db.owner)
	migrateTo(t, owner, 3)
	insertRowsFromBefore(t, owner) // one row in every table, in the default organization
	migrate(t, db.owner)

	pool := openPool(t, db.app)
	if err := New(pool).Append(domain.WithOrg(t.Context(), DefaultOrgID), domain.AuditEntry{
		Actor: domain.LocalUser, Action: "andon.pulled", TargetKind: "factory", TargetID: "f",
	}); err != nil {
		t.Fatal(err)
	}

	for _, table := range orgTables {
		var n int
		if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&n); err == nil {
			t.Errorf("SELECT from %s without an org = %d rows; want an error", table, n)
		}
	}

	_, err := pool.Exec(t.Context(),
		"INSERT INTO factories (id, name, definition, definition_sha256) VALUES ($1, 'acme-raw', '{}', 'sha')", uuid.NewString())
	if err == nil {
		t.Error("INSERT into factories without an org succeeded; want an error")
	}
}

// Raw SQL as goal_app bound to one organization neither sees nor changes another's rows, and
// cannot write a row into another organization.
func TestRLS_OtherOrgInvisible(t *testing.T) {
	t.Parallel()

	pool := newTestPool(t)
	s := New(pool)
	a, b := newOrgID(t, s), newOrgID(t, s)
	ctxA := domain.WithOrg(t.Context(), a)
	factory := saveFactory(ctxA, t, s)

	it, err := s.CreateItem(ctxA, domain.NewItem{
		FactoryID: factory, Type: typeTask, Title: "a's", Station: stWork,
		State: domain.StateQueued, Class: domain.ClassStandard, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := s.ClaimNext(ctxA, ports.Claim{FactoryID: factory, Station: domain.Station{
		ID: stWork, Kind: domain.KindAgent,
		Agent: "worker", Agents: 1,
	}, Now: time.Now(), ExpiresAt: time.Now().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}

	for _, table := range orgTables {
		var n int
		if err := asOrg(t, pool, b, "SELECT count(*) FROM "+table+" WHERE org_id <> '"+string(b)+"'", &n); err != nil || n != 0 {
			t.Errorf("org b sees %d rows of %s that are not its own, %v; want none", n, table, err)
		}
	}

	var updated int64

	err = inOrgTx(t, pool, b, func(tx pgx.Tx) error {
		tag, err := tx.Exec(t.Context(), "UPDATE work_items SET title = 'taken' WHERE id = $1", string(it.ID))
		updated = tag.RowsAffected()

		return err
	})
	if err != nil || updated != 0 {
		t.Errorf("org b updated %d of a's items, %v; want none", updated, err)
	}

	err = inOrgTx(t, pool, b, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO factories (id, org_id, name, definition, definition_sha256)
			VALUES ($1, $2, 'acme-planted', '{}', 'sha')`, uuid.NewString(), string(a))

		return err
	})
	if err == nil {
		t.Error("org b wrote a factory into org a; want the policy to refuse it")
	}

	if got, err := s.Item(ctxA, it.ID); err != nil || got.Title != "a's" || got.State != domain.StateInProgress {
		t.Errorf("a's item = %+v, %v; want it untouched", got, err)
	}
}
