package postgres

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The database roles Goal runs as. OwnerRole owns the schema and applies migrations; AppRole
// runs goald and owns nothing, so row-level security binds it.
const (
	OwnerRole = "goal_owner"
	AppRole   = "goal_app"
)

// Dev-only passwords for the two roles on a laptop, the defaults of GOAL_PG_OWNER_PASSWORD and
// GOAL_PG_APP_PASSWORD (like compose.yaml's goal-dev-only for the bootstrap superuser).
const (
	DevOwnerPassword = "goal-owner-dev-only" //nolint:gosec // a documented dev-only default, as in .env.example
	DevAppPassword   = "goal-app-dev-only"   //nolint:gosec // a documented dev-only default, as in .env.example
)

// ErrRoleBypassesRLS is CheckAppRole's answer for a role that row-level security would not bind.
var ErrRoleBypassesRLS = errors.New("the role bypasses row-level security")

// rolesSQL creates both roles without LOGIN when they are missing. `task migrate-check` runs the
// same file with psql.
//
//go:embed roles.sql
var rolesSQL string

// Roles holds the passwords InitRoles gives the two roles.
type Roles struct {
	OwnerPassword string
	AppPassword   string
}

// InitRoles prepares the database pool connects to for goald (`goal db init`). pool must connect
// as the bootstrap user that owns the database's objects today (compose.yaml's superuser
// locally). In one transaction it creates goal_owner and goal_app when missing, lets both log in
// with the given passwords, grants goal_owner the database and the current schema and goal_app
// their use (revoking PUBLIC's CONNECT and TEMPORARY on the database), and hands every table, view, sequence and function of the current schema that the
// bootstrap user owns to goal_owner (goose_db_version included), so the next migration runs as
// goal_owner. It returns how many objects changed owner. It is idempotent; passwords are never
// logged or put into errors.
func InitRoles(ctx context.Context, pool *pgxpool.Pool, r Roles) (moved int, err error) {
	if r.OwnerPassword == "" || r.AppPassword == "" {
		return 0, errors.New("postgres: init roles: both role passwords are required")
	}

	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := createRoles(ctx, tx); err != nil {
			return err
		}

		for _, rp := range []struct{ role, password string }{{OwnerRole, r.OwnerPassword}, {AppRole, r.AppPassword}} {
			if err := setPassword(ctx, tx, rp.role, rp.password); err != nil {
				return err
			}
		}

		moved, err = grantAndTransfer(ctx, tx)

		return err
	})
	if err != nil {
		return 0, fmt.Errorf("postgres: init roles: %w", err)
	}

	return moved, nil
}

// createRoles runs roles.sql under a lock, so concurrent callers on one database take turns.
func createRoles(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('goal.roles'))"); err != nil {
		return fmt.Errorf("lock: %w", err)
	}

	if _, err := tx.Exec(ctx, rolesSQL); err != nil {
		return fmt.Errorf("create roles: %w", err)
	}

	return nil
}

// setPassword lets role log in with password. ALTER ROLE takes no parameters, so the server
// quotes the password into the statement (format's %L) and it never passes through Go strings
// built by hand.
func setPassword(ctx context.Context, tx pgx.Tx, role, password string) error {
	var stmt string
	if err := tx.QueryRow(ctx, "SELECT format('ALTER ROLE %I LOGIN PASSWORD %L', $1::text, $2::text)", role, password).
		Scan(&stmt); err != nil {
		return fmt.Errorf("password of %s: %w", role, err)
	}

	if _, err := tx.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("password of %s: %w", role, err)
	}

	return nil
}

// grants gives goal_owner the database and the current schema, and goal_app the right to use
// them. Table privileges come from the migrations, one GRANT per table. PUBLIC loses what
// PostgreSQL gives it on a new database: CONNECT, so no other role of the cluster can LISTEN to
// or NOTIFY goald's channels, and TEMPORARY, so no role can put a temporary table in front of
// one a definer function reads (review 2.2 H1, M4). Neither role needs a temporary table.
const grants = `
DO $$ BEGIN
  EXECUTE format('REVOKE TEMPORARY, CONNECT ON DATABASE %I FROM PUBLIC', current_database());
  EXECUTE format('GRANT CONNECT, CREATE ON DATABASE %I TO goal_owner', current_database());
  EXECUTE format('GRANT CONNECT ON DATABASE %I TO goal_app', current_database());
  EXECUTE format('GRANT USAGE, CREATE ON SCHEMA %I TO goal_owner', current_schema());
  EXECUTE format('GRANT USAGE ON SCHEMA %I TO goal_app', current_schema());
END $$`

// ownedObjects lists an ALTER … OWNER TO goal_owner statement for every relation and function of
// the current schema that the current user owns. Sequences that belong to a column (serial and
// identity) move with their table, and extension members stay with their extension.
const ownedObjects = `
WITH me AS (SELECT oid FROM pg_roles WHERE rolname = current_user),
here AS (SELECT oid FROM pg_namespace WHERE nspname = current_schema())
SELECT format('ALTER %s %I.%I OWNER TO goal_owner',
         CASE c.relkind WHEN 'v' THEN 'VIEW' WHEN 'm' THEN 'MATERIALIZED VIEW' WHEN 'S' THEN 'SEQUENCE'
                        WHEN 'f' THEN 'FOREIGN TABLE' ELSE 'TABLE' END,
         current_schema(), c.relname)
FROM pg_class c
WHERE c.relnamespace = (SELECT oid FROM here) AND c.relowner = (SELECT oid FROM me)
  AND c.relkind IN ('r', 'p', 'v', 'm', 'S', 'f')
  AND NOT EXISTS (SELECT 1 FROM pg_depend d
                  WHERE d.classid = 'pg_class'::regclass AND d.objid = c.oid AND d.deptype IN ('a', 'i', 'e'))
UNION ALL
SELECT format('ALTER %s %s OWNER TO goal_owner',
         CASE p.prokind WHEN 'p' THEN 'PROCEDURE' WHEN 'a' THEN 'AGGREGATE' ELSE 'FUNCTION' END,
         p.oid::regprocedure)
FROM pg_proc p
WHERE p.pronamespace = (SELECT oid FROM here) AND p.proowner = (SELECT oid FROM me)
  AND NOT EXISTS (SELECT 1 FROM pg_depend d
                  WHERE d.classid = 'pg_proc'::regclass AND d.objid = p.oid AND d.deptype = 'e')`

// grantAndTransfer runs grants and moves the current user's objects to goal_owner one at a time
// (REASSIGN OWNED cannot be used for a bootstrap superuser, which owns system objects too).
func grantAndTransfer(ctx context.Context, tx pgx.Tx) (int, error) {
	if _, err := tx.Exec(ctx, grants); err != nil {
		return 0, fmt.Errorf("grant: %w", err)
	}

	rows, err := tx.Query(ctx, ownedObjects)
	if err != nil {
		return 0, fmt.Errorf("list owned objects: %w", err)
	}

	stmts, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return 0, fmt.Errorf("list owned objects: %w", err)
	}

	for _, stmt := range stmts {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return 0, fmt.Errorf("%s: %w", stmt, err)
		}
	}

	return len(stmts), nil
}

// CheckAppRole refuses to run goald as a role that row-level security does not bind: a
// superuser, a role with BYPASSRLS, a member of goal_owner, or a role that owns a table (owners
// are not subject to their tables' policies). The error wraps ErrRoleBypassesRLS.
func CheckAppRole(ctx context.Context, pool *pgxpool.Pool) error {
	var (
		role                         string
		super, bypass, owner, tables bool
	)

	err := pool.QueryRow(ctx, `
		SELECT r.rolname, r.rolsuper, r.rolbypassrls, pg_has_role(r.oid, 'goal_owner', 'MEMBER'),
		       EXISTS (SELECT 1 FROM pg_class c WHERE c.relowner = r.oid)
		FROM pg_roles r WHERE r.rolname = current_user`).Scan(&role, &super, &bypass, &owner, &tables)
	if err != nil {
		return fmt.Errorf("postgres: check the app role: %w (goal db init creates the roles)", err)
	}

	var why []string

	if super {
		why = append(why, "is a superuser")
	}

	if bypass {
		why = append(why, "has BYPASSRLS")
	}

	if owner {
		why = append(why, "is a member of goal_owner")
	}

	if tables {
		why = append(why, "owns tables")
	}

	if len(why) > 0 {
		return fmt.Errorf("postgres: role %q %s: %w; goald connects as goal_app", role, strings.Join(why, ", "), ErrRoleBypassesRLS)
	}

	return nil
}
