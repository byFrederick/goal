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
// runs goald and owns nothing, so row-level security binds it; AuthRole runs the identity
// service (apps/auth/), with DML on Better Auth's tables in schema auth only.
const (
	OwnerRole = "goal_owner"
	AppRole   = "goal_app"
	AuthRole  = "goal_auth"
)

// Dev-only passwords for the roles on a laptop, the defaults of GOAL_PG_OWNER_PASSWORD,
// GOAL_PG_APP_PASSWORD and GOAL_PG_AUTH_PASSWORD (like compose.yaml's goal-dev-only for the
// bootstrap superuser).
const (
	DevOwnerPassword = "goal-owner-dev-only" //nolint:gosec // a documented dev-only default, as in .env.example
	DevAppPassword   = "goal-app-dev-only"   //nolint:gosec // a documented dev-only default, as in .env.example
	DevAuthPassword  = "goal-auth-dev-only"  //nolint:gosec // a documented dev-only default, as in .env.example
)

// ErrRoleMissing is Migrate's answer when a role the migrations grant to does not exist yet.
var ErrRoleMissing = errors.New("run goal db init (goal up runs it)")

// ErrRoleBypassesRLS is CheckAppRole's answer for a role that row-level security would not bind.
var ErrRoleBypassesRLS = errors.New("the role bypasses row-level security")

// rolesSQL creates the roles without LOGIN when they are missing. `task migrate-check` runs the
// same file with psql.
//
//go:embed roles.sql
var rolesSQL string

// Roles holds the passwords InitRoles gives the roles.
type Roles struct {
	OwnerPassword string
	AppPassword   string
	AuthPassword  string
}

// InitRoles prepares the database pool connects to for goald (`goal db init`). pool must connect
// as the bootstrap user that owns the database's objects today (compose.yaml's superuser
// locally; a managed PostgreSQL's administrator, which is no superuser, works too). In one
// transaction it creates goal_owner, goal_app and goal_auth when missing, grants an administrator
// that is no superuser goal_owner with INHERIT and SET (adoptOwnerRole), lets them log in with the given passwords, grants goal_owner the database and the current schema and
// goal_app and goal_auth their use (revoking PUBLIC's CONNECT and TEMPORARY on the database), and
// hands every table, view, sequence and function of the current schema that the bootstrap user
// owns to goal_owner (goose_db_version included), so the next migration runs as goal_owner, and
// creates the extensions the migrations need (pgvector's vector, 00054), which only an
// administrator can. It returns how many objects changed owner. It is idempotent; passwords are
// never logged or put into errors.
func InitRoles(ctx context.Context, pool *pgxpool.Pool, r Roles) (moved int, err error) {
	if r.OwnerPassword == "" || r.AppPassword == "" || r.AuthPassword == "" {
		return 0, errors.New("postgres: init roles: every role password is required")
	}

	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := createRoles(ctx, tx); err != nil {
			return err
		}

		if err := adoptOwnerRole(ctx, tx); err != nil {
			return err
		}

		for _, rp := range []struct{ role, password string }{
			{OwnerRole, r.OwnerPassword}, {AppRole, r.AppPassword}, {AuthRole, r.AuthPassword},
		} {
			if err := setPassword(ctx, tx, rp.role, rp.password); err != nil {
				return err
			}
		}

		if moved, err = grantAndTransfer(ctx, tx); err != nil {
			return err
		}

		return createExtensions(ctx, tx)
	})
	if err != nil {
		return 0, fmt.Errorf("postgres: init roles: %w", err)
	}

	return moved, nil
}

// ErrNoPgvector is InitRoles' answer on a PostgreSQL server without pgvector.
var ErrNoPgvector = errors.New("PostgreSQL has no pgvector extension, which the knowledge base needs: run compose.yaml's " +
	"PostgreSQL image (docker compose up -d postgres recreates its container on the same volume, data kept) or install " +
	"pgvector for PostgreSQL 18 on the server")

// createExtensions creates pgvector's extension in the current schema when it is missing.
func createExtensions(ctx context.Context, tx pgx.Tx) error {
	var available bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = 'vector')").Scan(&available); err != nil {
		return fmt.Errorf("extensions: %w", err)
	}

	if !available {
		return ErrNoPgvector
	}

	if _, err := tx.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		return fmt.Errorf("create extension vector: %w", err)
	}

	// The migrations and goal_app resolve halfvec through Goal's schema only, so an extension a
	// provider installed elsewhere (an extensions schema) is not found.
	var in, goal string
	if err := tx.QueryRow(ctx, `SELECT n.nspname::text, current_schema()::text FROM pg_extension e
		JOIN pg_namespace n ON n.oid = e.extnamespace WHERE e.extname = 'vector'`).Scan(&in, &goal); err != nil {
		return fmt.Errorf("extension vector: %w", err)
	}

	if in != goal {
		return fmt.Errorf("pgvector's extension is in schema %s, not %s with Goal's tables: run ALTER EXTENSION vector SET SCHEMA %s "+
			"as the database's administrator, then goal db init again", in, goal, goal)
	}

	return nil
}

// adoptOwnerRole lets an administrator that is no superuser act as goal_owner. With CREATEROLE,
// PostgreSQL 16 and later give it the roles it creates with ADMIN OPTION only, without INHERIT
// and SET (PlanetScale's pscale_api_<id>, spec 4 §5.12): it could neither hand the objects it
// owns to goal_owner nor run goal db recover-admin or recover-owner, which use goal_owner's
// functions. A superuser has both already and is left as it is.
func adoptOwnerRole(ctx context.Context, tx pgx.Tx) error {
	var may bool
	if err := tx.QueryRow(ctx, "SELECT pg_has_role(current_user, 'goal_owner', 'SET') "+
		"AND pg_has_role(current_user, 'goal_owner', 'USAGE')").Scan(&may); err != nil {
		return fmt.Errorf("adopt goal_owner: %w", err)
	}

	if may {
		return nil
	}

	if _, err := tx.Exec(ctx, "GRANT goal_owner TO CURRENT_USER WITH INHERIT TRUE, SET TRUE"); err != nil {
		return fmt.Errorf("adopt goal_owner: %w", err)
	}

	return nil
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

// grants gives goal_owner the database and the current schema (CREATE on the database lets its
// migrations create schema auth), and goal_app and goal_auth the right to use them. Table
// privileges come from the migrations, one GRANT per table; goal_auth gets none in the current
// schema, only the functions granted to it (spec 3 §6.0). PUBLIC loses what PostgreSQL gives it
// on a new database: CONNECT, so no other role of the cluster can LISTEN to or NOTIFY goald's
// channels, and TEMPORARY, so no role can put a temporary table in front of one a definer
// function reads (review 2.2 H1, M4). No role needs a temporary table.
const grants = `
DO $$ BEGIN
  EXECUTE format('REVOKE TEMPORARY, CONNECT ON DATABASE %I FROM PUBLIC', current_database());
  EXECUTE format('GRANT CONNECT, CREATE ON DATABASE %I TO goal_owner', current_database());
  EXECUTE format('GRANT CONNECT ON DATABASE %I TO goal_app, goal_auth', current_database());
  EXECUTE format('GRANT USAGE, CREATE ON SCHEMA %I TO goal_owner', current_schema());
  EXECUTE format('GRANT USAGE ON SCHEMA %I TO goal_app, goal_auth', current_schema());
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
