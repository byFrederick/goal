package main

import (
	"context"
	"net/url"
	"strings"

	"github.com/byFrederick/goal/internal/adapters/postgres"
)

// dbSettings is what goal db init needs: an admin connection and the roles' passwords.
type dbSettings struct {
	// AdminURL connects as the user that owns the database's objects today
	// (GOAL_DATABASE_ADMIN_URL, default compose.yaml's goal superuser with GOAL_PG_PASSWORD).
	AdminURL string
	// Roles are the passwords of goal_owner and goal_app (GOAL_PG_OWNER_PASSWORD and
	// GOAL_PG_APP_PASSWORD, dev-only defaults); goald's default URLs use the same.
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
		},
	}
}

// setUp runs `goal db init` and `goal storage init`, the steps goal up takes before goald.
func setUp(ctx context.Context, out *printer, getenv func(string) string, what string, args []string) error {
	if len(args) != 1 || args[0] != "init" {
		return errUsage
	}

	if what == "db" {
		return dbInit(ctx, out, getenv)
	}

	return storageInit(ctx, out, getenv)
}

// dbInit prepares the database for goald (postgres.InitRoles): goal_owner, which owns the
// schema and runs the migrations, and goal_app, which goald runs as. goal up runs it every time;
// it is idempotent.
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

	out.printf("✓ database roles %s and %s ready", postgres.OwnerRole, postgres.AppRole)

	if moved > 0 {
		out.printf(", %d objects handed to %s", moved, postgres.OwnerRole)
	}

	out.printf("\n")

	return nil
}
