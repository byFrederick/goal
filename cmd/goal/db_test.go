package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/adapters/postgres"
)

func TestDBSettings_DefaultsMatchGoald(t *testing.T) {
	t.Parallel()

	got := dbSettingsFrom(func(k string) string { return map[string]string{"GOAL_PG_PASSWORD": "superuser-pw"}[k] })

	// The bootstrap superuser compose.yaml creates, and the passwords goald's default URLs use.
	if got.AdminURL != "postgres://goal:superuser-pw@127.0.0.1:5432/goal?sslmode=disable" { //nolint:gosec // a test password
		t.Errorf("admin url = %s; want compose's superuser", got.AdminURL)
	}

	if got.Roles != (postgres.Roles{
		OwnerPassword: postgres.DevOwnerPassword, AppPassword: postgres.DevAppPassword, AuthPassword: postgres.DevAuthPassword,
	}) {
		t.Errorf("role passwords are not the dev defaults")
	}

	// goal db migrate connects as goald's GOAL_MIGRATE_DATABASE_URL default does (cmd/goald
	// localDatabase): goal_owner with GOAL_PG_OWNER_PASSWORD.
	if want := "postgres://goal_owner:" + postgres.DevOwnerPassword + "@127.0.0.1:5432/goal?sslmode=disable"; got.MigrateURL != want {
		t.Errorf("migrate url = %s; want goald's default", got.MigrateURL)
	}

	owner := dbSettingsFrom(func(k string) string { return map[string]string{"GOAL_PG_OWNER_PASSWORD": "owner-pw"}[k] })
	if want := "postgres://goal_owner:owner-pw@127.0.0.1:5432/goal?sslmode=disable"; owner.MigrateURL != want { //nolint:gosec // a test password
		t.Errorf("migrate url = %s; want goal_owner with GOAL_PG_OWNER_PASSWORD", owner.MigrateURL)
	}
}

func TestDBSettings_EnvironmentWins(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"GOAL_DATABASE_ADMIN_URL": "postgres://admin@db.example.com/goal", "GOAL_MIGRATE_DATABASE_URL": "postgres://goal_owner@db.example.com/goal",
		"GOAL_PG_OWNER_PASSWORD": "owner-pw", "GOAL_PG_APP_PASSWORD": "app-pw", "GOAL_PG_AUTH_PASSWORD": "auth-pw",
	}

	got := dbSettingsFrom(func(k string) string { return env[k] })

	if got.AdminURL != env["GOAL_DATABASE_ADMIN_URL"] || got.MigrateURL != env["GOAL_MIGRATE_DATABASE_URL"] ||
		got.Roles != (postgres.Roles{OwnerPassword: "owner-pw", AppPassword: "app-pw", AuthPassword: "auth-pw"}) {
		t.Errorf("db settings ignore the environment")
	}
}

func TestSetUp_OnlyInitAndMigrate(t *testing.T) {
	t.Parallel()

	for _, what := range []string{"db", "storage"} {
		for _, args := range [][]string{{}, {"upgrade"}, {"init", "now"}, {"migrate", "now"}} {
			err := setUp(t.Context(), &printer{w: &bytes.Buffer{}}, func(string) string { return "" }, what, args)
			if !errors.Is(err, errUsage) {
				t.Errorf("goal %s %v = %v; want a usage error", what, args, err)
			}
		}
	}

	if err := setUp(t.Context(), &printer{w: &bytes.Buffer{}}, func(string) string { return "" }, "storage", []string{"migrate"}); !errors.Is(err, errUsage) {
		t.Errorf("goal storage migrate = %v; want a usage error", err)
	}
}

// goal db migrate that cannot connect names the variable it connected with and goal db init,
// which creates the role.
func TestDBMigrate_NamesItsConnection(t *testing.T) {
	t.Parallel()

	getenv := func(k string) string {
		return map[string]string{"GOAL_MIGRATE_DATABASE_URL": "postgres://goal_owner:pw@127.0.0.1:1/goal?sslmode=disable&connect_timeout=1"}[k] //nolint:gosec // a test password, no database
	}

	err := dbMigrate(t.Context(), &printer{w: &bytes.Buffer{}}, getenv)
	if err == nil || !strings.Contains(err.Error(), "GOAL_MIGRATE_DATABASE_URL") || !strings.Contains(err.Error(), "goal db init") {
		t.Errorf("goal db migrate with no database = %v; want it to name GOAL_MIGRATE_DATABASE_URL and goal db init", err)
	}

	if strings.Contains(err.Error(), ":pw@") {
		t.Errorf("the error shows the password: %v", err)
	}
}
