//go:build integration

package postgres

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// betterAuthTables are the tables 00041 generated in schema auth (Better Auth 1.7 with the email
// OTP, organization, device authorization and bearer plugins).
var betterAuthTables = []string{"account", "deviceCode", "invitation", "member", "organization", "rateLimit", "session", "user", "verification"}

// Schema auth belongs to Better Auth and the identity service (spec 3 §6.0): goal_owner owns it
// and its tables; goal_auth uses it with exactly SELECT, INSERT, UPDATE and DELETE on each table
// and has no table privilege in Goal's schema; goal_app cannot even use it.
func TestSchema_AuthSchemaIsolation(t *testing.T) {
	t.Parallel()

	db := newTestDB(t, OwnerRole)
	migrate(t, db.owner)
	admin := openPool(t, db.admin)

	tables := queryStrings(t, admin, `
		SELECT c.relname::text FROM pg_class c
		WHERE c.relnamespace = 'auth'::regnamespace AND c.relkind IN ('r', 'p') ORDER BY 1`)
	if !slices.Equal(tables, betterAuthTables) {
		t.Errorf("tables in schema auth = %v; want %v", tables, betterAuthTables)
	}

	if owners := queryStrings(t, admin, `
		SELECT DISTINCT pg_get_userbyid(c.relowner)::text FROM pg_class c WHERE c.relnamespace = 'auth'::regnamespace
		UNION SELECT pg_get_userbyid(nspowner)::text FROM pg_namespace WHERE nspname = 'auth'`); !slices.Equal(owners, []string{OwnerRole}) {
		t.Errorf("schema auth and its objects are owned by %v; want goal_owner only", owners)
	}

	checkAuthPrivileges(t, admin)
}

// checkAuthPrivileges checks the privileges of TestSchema_AuthSchemaIsolation.
func checkAuthPrivileges(t *testing.T, admin *pgxpool.Pool) {
	t.Helper()

	for _, c := range []struct {
		role, priv string
		want       bool
	}{
		{AuthRole, "USAGE", true},
		{AuthRole, "CREATE", false},
		{AppRole, "USAGE", false},
		{AppRole, "CREATE", false},
	} {
		var has bool
		if err := admin.QueryRow(t.Context(), "SELECT has_schema_privilege($1, 'auth', $2)", c.role, c.priv).
			Scan(&has); err != nil || has != c.want {
			t.Errorf("%s %s on schema auth = %v, %v; want %v", c.role, c.priv, has, err, c.want)
		}
	}

	// Exactly the first four, on every table.
	privileges := []string{"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"}
	for _, table := range betterAuthTables {
		for i, priv := range privileges {
			var has bool
			if err := admin.QueryRow(t.Context(), "SELECT has_table_privilege('goal_auth', format('auth.%I', $1::text), $2)",
				table, priv).Scan(&has); err != nil || has != (i < 4) {
				t.Errorf("goal_auth %s on auth.%s = %v, %v; want %v", priv, table, has, err, i < 4)
			}
		}
	}

	if granted := queryStrings(t, admin, `
		SELECT DISTINCT table_name::text FROM information_schema.role_table_grants
		WHERE grantee = 'goal_auth' AND table_schema <> 'auth'`); len(granted) != 0 {
		t.Errorf("goal_auth has privileges on %v outside schema auth; want none", granted)
	}

	if owned := queryStrings(t, admin, `SELECT relname::text FROM pg_class WHERE relowner = 'goal_auth'::regrole`); len(owned) != 0 {
		t.Errorf("goal_auth owns %v; want nothing", owned)
	}

	// Better Auth's id columns default to goal_uuidv7(), which goal_auth may run and PUBLIC may not.
	for _, c := range []struct {
		role string
		want bool
	}{{AuthRole, true}, {AppRole, true}, {publicRole, false}} {
		var has bool
		if err := admin.QueryRow(t.Context(), "SELECT has_function_privilege($1, 'goal_uuidv7()', 'EXECUTE')", c.role).
			Scan(&has); err != nil || has != c.want {
			t.Errorf("%s EXECUTE on goal_uuidv7() = %v, %v; want %v", c.role, has, err, c.want)
		}
	}
}

// goal_uuidv7 makes version 7 UUIDs (RFC 9562): the variant of RFC 4122 and the time first, so
// later ids sort after earlier ones.
func TestUUIDv7_IsVersion7AndTimeOrdered(t *testing.T) {
	t.Parallel()

	db := newTestDB(t, OwnerRole)
	migrate(t, db.owner)

	auth := openPool(t, db.auth)

	var ids []string

	for range 3 {
		var id string
		if err := auth.QueryRow(t.Context(), "SELECT goal_uuidv7()::text").Scan(&id); err != nil {
			t.Fatal(err)
		}

		ids = append(ids, id)

		time.Sleep(2 * time.Millisecond) // the next one falls in a later millisecond
	}

	for _, id := range ids {
		if id[14] != '7' || !strings.ContainsRune("89ab", rune(id[19])) {
			t.Errorf("goal_uuidv7() = %s; want version 7, variant 10", id)
		}
	}

	if !slices.IsSorted(ids) {
		t.Errorf("goal_uuidv7() in order = %v; want ascending", ids)
	}
}

// goald migrates nothing while a role the migrations grant to is missing (a database from before
// goal_auth whose goal db init has not run), and says how to fix it.
func TestMigrate_RefusesWhileARoleIsMissing(t *testing.T) {
	t.Parallel()

	db := newTestDB(t, OwnerRole)
	owner := openPool(t, db.owner)
	absent := "goal_test_absent_" + randomHex(t)

	err := migrateRequiring(t.Context(), owner, []string{AppRole, absent})
	if !errors.Is(err, ErrRoleMissing) || !strings.Contains(err.Error(), "role "+absent+" is missing: run goal db init (goal up runs it)") {
		t.Fatalf("migrate without %s = %v; want ErrRoleMissing naming it", absent, err)
	}

	if tables := queryStrings(t, owner, `SELECT relname::text FROM pg_class WHERE relnamespace = 'public'::regnamespace AND relkind = 'r'`); len(tables) != 0 {
		t.Errorf("tables after the refusal = %v; want none applied", tables)
	}
}
