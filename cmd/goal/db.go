package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/byFrederick/goal/internal/adapters/postgres"
	"github.com/byFrederick/goal/internal/ports"
)

// dbSettings is what goal db init and goal db migrate need: an admin connection, the roles'
// passwords and goal_owner's connection.
type dbSettings struct {
	// AdminURL connects as the user that owns the database's objects today
	// (GOAL_DATABASE_ADMIN_URL, default compose.yaml's goal superuser with GOAL_PG_PASSWORD).
	AdminURL string
	// MigrateURL connects as goal_owner, which applies the migrations: goald's
	// GOAL_MIGRATE_DATABASE_URL, with goald's default.
	MigrateURL string
	// Roles are the passwords of goal_owner, goal_app and goal_auth (GOAL_PG_OWNER_PASSWORD,
	// GOAL_PG_APP_PASSWORD and GOAL_PG_AUTH_PASSWORD, dev-only defaults); goald's and the auth
	// service's default URLs use the same.
	Roles postgres.Roles
}

// The variables of the database's passwords and the administrator's URL.
const (
	adminURLEnv          = "GOAL_DATABASE_ADMIN_URL"
	superuserPasswordEnv = "GOAL_PG_PASSWORD"
	ownerPasswordEnv     = "GOAL_PG_OWNER_PASSWORD" //nolint:gosec // a variable name, not a credential
	appPasswordEnv       = "GOAL_PG_APP_PASSWORD"   //nolint:gosec // a variable name, not a credential
	authPasswordEnv      = "GOAL_PG_AUTH_PASSWORD"  //nolint:gosec // a variable name, not a credential
	migrateURLEnv        = "GOAL_MIGRATE_DATABASE_URL"
)

func dbSettingsFrom(getenv func(string) string) dbSettings {
	or := func(name, def string) string {
		if v := strings.TrimSpace(getenv(name)); v != "" {
			return v
		}

		return def
	}

	roles := postgres.Roles{
		OwnerPassword: or(ownerPasswordEnv, postgres.DevOwnerPassword),
		AppPassword:   or(appPasswordEnv, postgres.DevAppPassword),
		AuthPassword:  or(authPasswordEnv, postgres.DevAuthPassword),
	}

	return dbSettings{
		AdminURL:   or(adminURLEnv, localDatabase("goal", or(superuserPasswordEnv, "goal-dev-only"))),
		MigrateURL: or(migrateURLEnv, localDatabase(postgres.OwnerRole, roles.OwnerPassword)),
		Roles:      roles,
	}
}

// localDatabase is compose.yaml's database as role with password, as goald's defaults connect.
func localDatabase(role, password string) string {
	return (&url.URL{
		Scheme: "postgres", User: url.UserPassword(role, password),
		Host: "127.0.0.1:5432", Path: "goal", RawQuery: "sslmode=disable",
	}).String()
}

// setUp runs `goal db init`, `goal db migrate` and `goal storage init`, the steps goal up takes
// before the auth service and goald, and the recoveries `goal db recover-admin <email>` and
// `goal db recover-owner <org> <email>`.
func setUp(ctx context.Context, out *printer, getenv func(string) string, what string, args []string) error {
	if len(args) == 0 {
		return errUsage
	}

	cmd, rest := args[0], args[1:]
	if what != "db" {
		if cmd != "init" || len(rest) != 0 {
			return errUsage
		}

		return storageInit(ctx, out, getenv)
	}

	switch {
	case cmd == "recover-admin" && len(rest) == 1:
		return recoverAdmin(ctx, out, getenv, rest[0])
	case cmd == "recover-owner" && len(rest) == 2:
		return recoverOwner(ctx, out, getenv, rest[0], rest[1])
	case cmd == "init" && len(rest) == 0:
		return dbInit(ctx, out, getenv)
	case cmd == "migrate" && len(rest) == 0:
		return dbMigrate(ctx, out, getenv)
	default:
		return errUsage
	}
}

// dbInit prepares the database for goald (postgres.InitRoles): goal_owner, which owns the
// schema and runs the migrations, goal_app, which goald runs as, and goal_auth, which the auth
// service runs as. goal up runs it every time; it is idempotent.
func dbInit(ctx context.Context, out *printer, getenv func(string) string) error {
	s := dbSettingsFrom(getenv)

	pool, err := postgres.Open(ctx, s.AdminURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	moved, err := postgres.InitRoles(ctx, pool, s.Roles)
	if err != nil {
		return err
	}

	out.printf("✓ database roles %s, %s and %s ready", postgres.OwnerRole, postgres.AppRole, postgres.AuthRole)

	if moved > 0 {
		out.printf(", %d objects handed to %s", moved, postgres.OwnerRole)
	}

	out.printf("\n")

	return nil
}

// dbMigrate applies the pending migrations as goal_owner (GOAL_MIGRATE_DATABASE_URL, goald's
// default) with postgres.Migrate, as goald applies them at its start, which then finds nothing to
// do; like goald it applies nothing while a role the migrations grant to is missing (goal db init
// creates them). goal up runs it after goal db init and before the auth service, whose schema the
// migrations hold (00040 on): on an upgrade the auth service would otherwise start before its
// tables exist, and stop.
func dbMigrate(ctx context.Context, out *printer, getenv func(string) string) error {
	pool, err := postgres.Open(ctx, dbSettingsFrom(getenv).MigrateURL)
	if err != nil {
		return fmt.Errorf("%s: %w (goal db init creates the roles)", migrateURLEnv, err)
	}
	defer pool.Close()

	if err := postgres.Migrate(ctx, pool); err != nil {
		return err
	}

	out.printf("✓ database migrated (as %s)\n", postgres.OwnerRole)

	return nil
}

// adminPool connects as the database's administrator (GOAL_DATABASE_ADMIN_URL, as goal db init).
func adminPool(ctx context.Context, getenv func(string) string) (*pgxpool.Pool, error) {
	return postgres.Open(ctx, dbSettingsFrom(getenv).AdminURL)
}

// recoverAdmin makes the person with email a platform admin with nothing but database access
// (spec 3 §9): goald and the identity service may be stopped. Someone without a user is created,
// so they can sign in by code.
func recoverAdmin(ctx context.Context, out *printer, getenv func(string) string, email string) error {
	pool, err := adminPool(ctx, getenv)
	if err != nil {
		return err
	}
	defer pool.Close()

	r, err := postgres.RecoverAdmin(ctx, pool, email)
	if err != nil {
		return err
	}

	out.printf("✓ %s is a platform admin%s; recorded in the security log as recovery.used\n", r.User.Email, created(r))

	return nil
}

// recoverOwner makes the person with email an owner of the organization org (slug or id), as
// recoverAdmin does.
func recoverOwner(ctx context.Context, out *printer, getenv func(string) string, org, email string) error {
	pool, err := adminPool(ctx, getenv)
	if err != nil {
		return err
	}
	defer pool.Close()

	r, err := postgres.RecoverOwner(ctx, pool, org, email)
	if errors.Is(err, ports.ErrNotFound) {
		return fmt.Errorf("no live organization %q: nothing changed", org)
	}

	if err != nil {
		return err
	}

	out.printf("✓ %s is an owner of %s (%s)%s; recorded in its audit log and the security log\n", r.User.Email, r.Org.Name, r.Org.Slug, created(r))

	return nil
}

// created says what a recovery that created the person means for them.
func created(r postgres.Recovery) string {
	if !r.Created {
		return ""
	}

	return " (a new user: they sign in with a code sent to that address)"
}
