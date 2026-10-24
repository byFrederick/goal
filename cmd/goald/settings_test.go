package main

import (
	"maps"
	"strings"
	"testing"
	"time"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestSettings_Defaults(t *testing.T) {
	t.Parallel()

	s, err := settingsFrom(envOf(map[string]string{
		"GOAL_PG_PASSWORD": "superuser-pw", "GOAL_PG_OWNER_PASSWORD": "owner-pw", "GOAL_PG_APP_PASSWORD": "app-pw",
	}))
	if err != nil {
		t.Fatal(err)
	}

	if s.Listen != "127.0.0.1:8080" || s.GatewayURL != "http://127.0.0.1:8080/mcp" || s.GatewayKeyRef != "env:GOAL_GATEWAY_KEY" {
		t.Errorf("listen %s, gateway %s, key %s; want the loopback defaults", s.Listen, s.GatewayURL, s.GatewayKeyRef)
	}

	// goald never connects as compose's superuser: migrations run as goal_owner, the rest as goal_app.
	if s.MigrateDatabaseURL != "postgres://goal_owner:owner-pw@127.0.0.1:5432/goal?sslmode=disable" { //nolint:gosec // a test password
		t.Errorf("migrate database url = %s, want the compose database as goal_owner", s.MigrateDatabaseURL)
	}

	if s.DatabaseURL != "postgres://goal_app:app-pw@127.0.0.1:5432/goal?sslmode=disable" { //nolint:gosec // a test password
		t.Errorf("database url = %s, want the compose database as goal_app", s.DatabaseURL)
	}

	if s.OrgRetention != 7*24*time.Hour {
		t.Errorf("org retention = %s, want 7 days", s.OrgRetention)
	}

	want := map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "env:CLAUDE_CODE_OAUTH_TOKEN"} //nolint:gosec // a reference, not a secret
	if !maps.Equal(s.TaskEnv, want) {
		t.Errorf("task env = %v, want %v", s.TaskEnv, want)
	}
}

func TestSettings_RefusesUnsafeConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "public listen address", env: map[string]string{"GOAL_LISTEN": "0.0.0.0:8080"}, want: "loopback"},
		{name: "API key for tasks", env: map[string]string{"GOAL_TASK_ENV": "ANTHROPIC_API_KEY=env:ANTHROPIC_API_KEY"}, want: "never reach"},
		{name: "GitHub token for tasks", env: map[string]string{"GOAL_TASK_ENV": "goal_github_token=env:X"}, want: "never reach"},
		{name: "literal secret for tasks", env: map[string]string{"GOAL_TASK_ENV": "CLAUDE_CODE_OAUTH_TOKEN=sk-literal"}, want: "NAME=env:REF"},
		{name: "bad duration", env: map[string]string{"GOAL_TICK_EVERY": "-1s"}, want: "GOAL_TICK_EVERY"},
		{name: "negative run cap", env: map[string]string{"GOAL_MAX_RUNS": "-2"}, want: "GOAL_MAX_RUNS"},
		{name: "no retention", env: map[string]string{"GOAL_ORG_RETENTION": "0d"}, want: "GOAL_ORG_RETENTION"},
		{name: "GitHub API in clear text", env: map[string]string{"GOAL_GITHUB_API_URL": "http://api.example.com"}, want: "must be https"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := settingsFrom(envOf(tt.env)); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("settings error = %v, want one mentioning %q", err, tt.want)
			}
		})
	}
}
