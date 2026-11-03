package domain

import (
	"maps"
	"slices"
	"testing"
)

// A model key reference and a model connection of the domain's tests.
const (
	testModelKey  = "{{claude-key}}"
	testModelConn = "claude"
)

// SecretNames are a value's {{secret}} references in order, each once, spaces inside the braces
// allowed; a value without one has none.
func TestEnvironment_SecretNames(t *testing.T) {
	t.Parallel()

	tests := map[string][]string{
		"{{github-token}}":                    {"github-token"},
		"postgres://app:{{ db-pass }}@db/app": {"db-pass"},
		"{{a-key}}:{{b-key}}:{{a-key}}":       {"a-key", "b-key"},
		"plain-value":                         nil,
		"":                                    nil,
	}

	for value, want := range tests {
		if got := SecretNames(value); !slices.Equal(got, want) {
			t.Errorf("SecretNames(%q) = %v; want %v", value, got, want)
		}
	}
}

// FillSecrets puts each reference's value in its place, whether the reference is the whole value
// or part of it, and leaves a plain value as it is.
func TestEnvironment_FillSecrets(t *testing.T) {
	t.Parallel()

	values := map[string]string{"db-pass": "s3cret-pass", "github-token": "ghp-value"}

	tests := map[string]string{ //nolint:gosec // test values
		"{{github-token}}":                    "ghp-value",
		"postgres://app:{{ db-pass }}@db/app": "postgres://app:s3cret-pass@db/app",
		"plain-value":                         "plain-value",
	}

	for value, want := range tests {
		if got := FillSecrets(value, values); got != want {
			t.Errorf("FillSecrets(%q) = %q; want %q", value, got, want)
		}
	}
}

// A model key that is a Claude Code OAuth token goes in CLAUDE_CODE_OAUTH_TOKEN, any other in
// ANTHROPIC_API_KEY (ADR 0037 decision 7).
func TestEnvironment_ModelKeyEnv(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"sk-ant-oat01-example": "CLAUDE_CODE_OAUTH_TOKEN",
		"sk-ant-api03-example": "ANTHROPIC_API_KEY",
		"example-key":          "ANTHROPIC_API_KEY",
	}

	for value, want := range tests {
		if got := ModelKeyEnv(value); got != want {
			t.Errorf("ModelKeyEnv(%q) = %s; want %s", value, got, want)
		}
	}
}

// An agent gets its own environment, or default when it names none; one the version has no file
// for is not found.
func TestLine_Environment(t *testing.T) {
	t.Parallel()

	l := Line{Environments: []Environment{{Name: DefaultEnvironment}, {Name: "browser"}}}

	for name, want := range map[string]string{"": DefaultEnvironment, "browser": "browser"} {
		if got, ok := l.Environment(name); !ok || got.Name != want {
			t.Errorf("Environment(%q) = %+v, %v; want %s", name, got, ok, want)
		}
	}

	if _, ok := l.Environment("missing"); ok {
		t.Error("Environment(missing) found one; want none")
	}
}

// FileEnv numbers a run's files GOAL_FILE_1, GOAL_FILE_2… by sorted path, whatever order the file
// writes them in.
func TestEnvironment_FileEnv(t *testing.T) {
	t.Parallel()

	got := FileEnv(map[string]string{"~/.npmrc": "a", "/etc/app.conf": "b", "~/.config/gh/hosts.yml": "c"})
	want := map[string]string{"/etc/app.conf": "GOAL_FILE_1", "~/.config/gh/hosts.yml": "GOAL_FILE_2", "~/.npmrc": "GOAL_FILE_3"}

	if !maps.Equal(got, want) {
		t.Errorf("FileEnv = %v; want %v", got, want)
	}

	for _, v := range got {
		if !ReservedRunEnv(v) {
			t.Errorf("%s is not platform-reserved, so an environment's env could set it", v)
		}
	}
}

// An MCP server's header or env that names a secret renders as ${GOAL_SECRET_<NAME>}, the name
// upper-cased with each other character an underscore; a plain value stays as written.
func TestEnvironment_RenderedMCPServer(t *testing.T) {
	t.Parallel()

	s := MCPServer{
		URL:     "https://tickets.example.com/mcp",
		Headers: map[string]string{"Authorization": "Bearer {{ tickets-key }}", "X-Org": "acme"},
		Env:     map[string]string{"TOKEN": "{{db-pass}}:{{tickets-key}}"},
		Tools:   map[string]string{"create_comment": "ask"},
	}

	got := s.Rendered()
	if want := map[string]string{"Authorization": "Bearer ${GOAL_SECRET_TICKETS_KEY}", "X-Org": "acme"}; !maps.Equal(got.Headers, want) {
		t.Errorf("headers = %v; want %v", got.Headers, want)
	}

	if want := "${GOAL_SECRET_DB_PASS}:${GOAL_SECRET_TICKETS_KEY}"; got.Env["TOKEN"] != want {
		t.Errorf("env TOKEN = %q; want %q", got.Env["TOKEN"], want)
	}

	if got.URL != s.URL || !maps.Equal(got.Tools, s.Tools) {
		t.Errorf("rendered %+v lost the URL or tools of %+v", got, s)
	}

	if names, want := s.SecretNames(), []string{"tickets-key", "db-pass"}; !slices.Equal(names, want) {
		t.Errorf("SecretNames = %v; want %v", names, want)
	}

	if s.Headers["Authorization"] != "Bearer {{ tickets-key }}" {
		t.Error("Rendered changed the server it was called on")
	}
}
