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

// dbSettings is what goal db init needs: an admin connection and the roles' passwords.
type dbSettings struct {
	// AdminURL connects as the user that owns the database's objects today
	// (GOAL_DATABASE_ADMIN_URL, default compose.yaml's goal superuser with GOAL_PG_PASSWORD).
	AdminURL string
	// Roles are the passwords of goal_owner, goal_app and goal_auth (GOAL_PG_OWNER_PASSWORD,
	// GOAL_PG_APP_PASSWORD and GOAL_PG_AUTH_PASSWORD, dev-only defaults); goald's and the auth
	// service's default URLs use the same.
	Roles postgres.Roles
}

func dbSettingsFrom(getenv func(string) string) dbSettings {
	or := func(name, def string) string {
		if v := strings.TrimSpace(getenv(name)); v != "" {
			return v
		}

		return def
	}

	admin := (&url.URL{
		Scheme: "postgres", User: url.UserPassword("goal", or("GOAL_PG_PASSWORD", "goal-dev-only")),
		Host: "127.0.0.1:5432", Path: "goal", RawQuery: "sslmode=disable",
	}).String()

	return dbSettings{
		AdminURL: or("GOAL_DATABASE_ADMIN_URL", admin),
		Roles: postgres.Roles{
			OwnerPassword: or("GOAL_PG_OWNER_PASSWORD", postgres.DevOwnerPassword),
			AppPassword:   or("GOAL_PG_APP_PASSWORD", postgres.DevAppPassword),
			AuthPassword:  or("GOAL_PG_AUTH_PASSWORD", postgres.DevAuthPassword),
		},
	}
}

// setUp runs `goal db init` and `goal storage init`, the steps goal up takes before goald, and
// the recoveries `goal db recover-admin <email>` and `goal db recover-owner <org> <email>`.
func setUp(ctx context.Context, out *printer, getenv func(string) string, what string, args []string) error {
	if len(args) == 0 {
		return errUsage
	}

	switch cmd, rest := args[0], args[1:]; {
	case what == "db" && cmd == "recover-admin" && len(rest) == 1:
		return recoverAdmin(ctx, out, getenv, rest[0])
	case what == "db" && cmd == "recover-owner" && len(rest) == 2:
		return recoverOwner(ctx, out, getenv, rest[0], rest[1])
	case cmd != "init" || len(rest) != 0:
		return errUsage
	case what == "db":
		return dbInit(ctx, out, getenv)
	default:
		return storageInit(ctx, out, getenv)
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
