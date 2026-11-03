package domain

import (
	"slices"
	"testing"
)

// SecretNames are a value's {{secret}} references in order, each once, spaces inside the braces
// allowed; a value without one has none.
func TestEnvironment_SecretNames(t *testing.T) {
	t.Parallel()

	tests := map[string][]string{
		"{{github-token}}":                    {"github-token"},
		"postgres://app:{{ db-pass }}@db/app": {"db-pass"},
		"{{a-key}}:{{b-key}}:{{a-key}}":       {"a-key", "b-key"},
		"acme":                                nil,
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

	tests := map[string]string{
		"{{github-token}}":                    "ghp-value",
		"postgres://app:{{ db-pass }}@db/app": "postgres://app:s3cret-pass@db/app",
		"acme":                                "acme",
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
