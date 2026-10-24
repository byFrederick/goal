package main

import (
	"bytes"
	"errors"
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
}

func TestDBSettings_EnvironmentWins(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"GOAL_DATABASE_ADMIN_URL": "postgres://admin@db.example.com/goal",
		"GOAL_PG_OWNER_PASSWORD":  "owner-pw", "GOAL_PG_APP_PASSWORD": "app-pw", "GOAL_PG_AUTH_PASSWORD": "auth-pw",
	}

	got := dbSettingsFrom(func(k string) string { return env[k] })

	if got.AdminURL != env["GOAL_DATABASE_ADMIN_URL"] || got.Roles != (postgres.Roles{OwnerPassword: "owner-pw", AppPassword: "app-pw", AuthPassword: "auth-pw"}) {
		t.Errorf("db settings ignore the environment")
	}
}

func TestSetUp_OnlyInit(t *testing.T) {
	t.Parallel()

	for _, what := range []string{"db", "storage"} {
		for _, args := range [][]string{{}, {"migrate"}, {"init", "now"}} {
			err := setUp(t.Context(), &printer{w: &bytes.Buffer{}}, func(string) string { return "" }, what, args)
			if !errors.Is(err, errUsage) {
				t.Errorf("goal %s %v = %v; want a usage error", what, args, err)
			}
		}
	}
}
