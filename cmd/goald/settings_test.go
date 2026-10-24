package main

import (
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/adapters/envsecrets"
	"github.com/byFrederick/goal/internal/adapters/s3store"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/builder"
	"github.com/byFrederick/goal/internal/config"
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

	if s.DefinitionCacheBytes != 64<<20 || !s.LegacyImport {
		t.Errorf("definition cache %d bytes, legacy import %v; want 64 MiB and on", s.DefinitionCacheBytes, s.LegacyImport)
	}

	want := map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "env:CLAUDE_CODE_OAUTH_TOKEN"} //nolint:gosec // a reference, not a secret
	if !maps.Equal(s.TaskEnv, want) {
		t.Errorf("task env = %v, want %v", s.TaskEnv, want)
	}
}

// The editor's models: the current Claude models by default, the first being the default;
// GOAL_MODELS replaces them, a label after = or one made from the id.
func TestSettings_Models(t *testing.T) {
	t.Parallel()

	s, err := settingsFrom(envOf(nil))
	if err != nil {
		t.Fatal(err)
	}

	want := []builder.Model{
		{ID: "claude-opus-5-5", Label: "Claude Opus 5.5"},
		{ID: "claude-sonnet-5-5", Label: "Claude Sonnet 5.5"},
		{ID: "claude-haiku-5-5", Label: "Claude Haiku 5.5"},
	}
	if !slices.Equal(s.Models, want) {
		t.Errorf("default models = %+v, want %+v", s.Models, want)
	}

	s, err = settingsFrom(envOf(map[string]string{modelsEnv: " claude-sonnet-5-5 , claude-fable-5-1=Fable (preview)"}))
	if err != nil {
		t.Fatal(err)
	}

	want = []builder.Model{{ID: "claude-sonnet-5-5", Label: "Claude Sonnet 5.5"}, {ID: "claude-fable-5-1", Label: "Fable (preview)"}}
	if !slices.Equal(s.Models, want) {
		t.Errorf("models = %+v, want %+v", s.Models, want)
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
		{name: "API key for tasks", env: map[string]string{taskEnvEnv: "ANTHROPIC_API_KEY=env:ANTHROPIC_API_KEY"}, want: "never reach"},
		{name: "GitHub token for tasks", env: map[string]string{taskEnvEnv: "goal_github_token=env:X"}, want: "never reach"},
		{name: "literal secret for tasks", env: map[string]string{taskEnvEnv: "CLAUDE_CODE_OAUTH_TOKEN=sk-literal"}, want: "NAME=env:REF"},
		{name: "bad duration", env: map[string]string{"GOAL_TICK_EVERY": "-1s"}, want: "GOAL_TICK_EVERY"},
		{name: "negative run cap", env: map[string]string{"GOAL_MAX_RUNS": "-2"}, want: "GOAL_MAX_RUNS"},
		{name: "no retention", env: map[string]string{"GOAL_ORG_RETENTION": "0d"}, want: "GOAL_ORG_RETENTION"},
		{name: "empty definition cache", env: map[string]string{"GOAL_DEFINITION_CACHE_BYTES": "0"}, want: "GOAL_DEFINITION_CACHE_BYTES"},
		{name: "unclear legacy import", env: map[string]string{"GOAL_LEGACY_IMPORT": "maybe"}, want: "GOAL_LEGACY_IMPORT"},
		{name: "GitHub API in clear text", env: map[string]string{"GOAL_GITHUB_API_URL": "http://api.example.com"}, want: "must be https"},
		{name: "not a model id", env: map[string]string{modelsEnv: "claude-opus-5-5,gpt-5"}, want: "not a model id"},
		{name: "a model twice", env: map[string]string{modelsEnv: "claude-opus-5-5,claude-opus-5-5"}, want: "twice"},
		{name: "an empty model", env: map[string]string{modelsEnv: "claude-opus-5-5,"}, want: "not a model id"},
		{name: "public URL in clear text", env: map[string]string{publicURLEnv: "http://goal.example.com"}, want: publicURLEnv},
		{name: "public URL without a scheme", env: map[string]string{publicURLEnv: "goal.example.com"}, want: publicURLEnv},
		{name: "public URL with a path", env: map[string]string{publicURLEnv: "https://example.com/goal"}, want: publicURLEnv},
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

// goald answers requests naming the loopback names, its listen host and its public URL's host;
// the Host check refuses every other (DNS rebinding).
func TestSettings_Hosts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  map[string]string
		want []string
	}{
		{name: "defaults", want: []string{"localhost", "127.0.0.1", "::1"}},
		{
			name: "listen host and public URL",
			env:  map[string]string{"GOAL_LISTEN": "127.0.0.2:9090", publicURLEnv: "https://goal.example.com"},
			want: []string{"localhost", "127.0.0.1", "::1", "127.0.0.2", "goal.example.com"},
		},
		{
			name: "loopback public URL in clear text", env: map[string]string{publicURLEnv: "http://localhost:8080/"},
			want: []string{"localhost", "127.0.0.1", "::1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, err := settingsFrom(envOf(tt.env))
			if err != nil {
				t.Fatal(err)
			}

			if got := s.hosts(); !slices.Equal(got, tt.want) {
				t.Errorf("hosts = %v, want %v", got, tt.want)
			}
		})
	}
}

// Every variable goald reads is one a factory definition may not reference, so no definition can
// hand goald's own settings or secrets to an agent or a connection.
func TestSettings_EveryVariableGoaldReadsIsReserved(t *testing.T) {
	t.Parallel()

	var read []string

	getenv := func(name string) string {
		read = append(read, name)

		if name == "GOAL_S3_BUCKET" {
			return "goal-test"
		}

		return ""
	}

	s, err := settingsFrom(getenv)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s3store.ConfigFromEnv(getenv); err != nil {
		t.Fatal(err)
	}

	reserved := s.reserved()
	for _, name := range append(read, "GOAL_GATEWAY_KEY") {
		if !slices.Contains(reserved, name) {
			t.Errorf("goald reads %s, but definitions may reference it", name)
		}
	}

	custom, err := settingsFrom(envOf(map[string]string{"GOAL_GATEWAY_KEY_REF": "env:ACME_SIGNING_KEY"}))
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Contains(custom.reserved(), "ACME_SIGNING_KEY") {
		t.Errorf("reserved %v, want the variable the gateway key reference names", custom.reserved())
	}
}

// The platform's secrets are reserved too: those that never reach a task and the variables the
// tasks' model access is read from (GOAL_TASK_ENV's references). The repository token is not
// reserved but kept to the repository and GitHub's MCP server.
func TestSettings_ReservesPlatformSecrets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		env      map[string]string
		reserved []string
	}{
		{name: "defaults", reserved: []string{"ANTHROPIC_API_KEY", "GOAL_GATEWAY_KEY", "CLAUDE_CODE_OAUTH_TOKEN"}},
		{
			name:     "custom task env",
			env:      map[string]string{taskEnvEnv: "CLAUDE_CODE_OAUTH_TOKEN=env:ACME_MODEL_TOKEN,ACME_FLAG=env:ACME_FLAG_SOURCE"},
			reserved: []string{"ANTHROPIC_API_KEY", "ACME_MODEL_TOKEN", "ACME_FLAG_SOURCE"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, err := settingsFrom(envOf(tt.env))
			if err != nil {
				t.Fatal(err)
			}

			got := s.credentials(envsecrets.New(func(string) (string, bool) { return "", false }))
			for _, name := range tt.reserved {
				if !slices.Contains(got.Reserved, name) {
					t.Errorf("reserved %v, want %s among them", got.Reserved, name)
				}
			}

			if slices.Contains(got.Reserved, repositoryToken) || !slices.Equal(got.RepositoryOnly, []string{repositoryToken}) ||
				!slices.Equal(got.RepositoryHosts, []string{githubMCPHost}) {
				t.Errorf("reserved %v, repository only %v to %v; want %s kept to the repository and %s",
					got.Reserved, got.RepositoryOnly, got.RepositoryHosts, repositoryToken, githubMCPHost)
			}
		})
	}
}

// The credentials check goald authors definitions with refuses a reference to one of its own
// settings with env-reserved, even though the variable is set, and passes another set variable.
func TestSettings_CredentialsRefuseGoaldsOwnVariables(t *testing.T) {
	t.Parallel()

	env := map[string]string{"GOAL_DATABASE_URL": "postgres://app@db.example.com/goal", "ACME_GIT_TOKEN": "t0ken"}

	s, err := settingsFrom(envOf(env))
	if err != nil {
		t.Fatal(err)
	}

	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }

	findings, err := s.credentials(envsecrets.New(lookup)).Check(t.Context(), "", []config.SecretUse{
		{Path: "runtime.repository.token", Ref: "env:GOAL_DATABASE_URL"},
		{Path: "spec.connections[0].headers.Authorization", Ref: "env:ACME_GIT_TOKEN"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(findings) != 1 || findings[0].Code != authoring.CodeEnvReserved || findings[0].Use.Ref != "env:GOAL_DATABASE_URL" {
		t.Errorf("findings = %+v; want only env:GOAL_DATABASE_URL refused with %s", findings, authoring.CodeEnvReserved)
	}
}
