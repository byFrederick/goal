package main

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
)

const cmdConnection = "connection"

// goalIn runs one CLI command line against g with stdin and returns its output.
func (g *defGoald) goalIn(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()

	var buf bytes.Buffer

	err := run(t.Context(), args, &printer{w: &buf, in: strings.NewReader(stdin)}, testEnv(t, g.srv.URL, map[string]string{envOrg: orgAcme}), nil)

	return buf.String(), err
}

// secret is the stored secret field of connection name.
func (g *defGoald) secret(t *testing.T, name, field string) string {
	t.Helper()

	rc, err := g.conns.Open(domain.WithOrg(t.Context(), g.orgID), name)
	if err != nil {
		t.Fatal(err)
	}

	return rc.Values[field]
}

// goal connection create reads a secret from stdin, without its newline; show and list say which
// secrets are set, never their values.
func TestConnection_CreateFromStdin(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)

	const token = "ghp_cli-secret-value" //nolint:gosec // a test value, not a credential

	out, err := g.goalIn(t, token+"\n", cmdConnection, verbCreate, "github", "--kind", "github", "--secret", "token=-", "--description", "the repositories")
	if err != nil || !strings.Contains(out, "created connection github") {
		t.Fatalf("create = %q, %v", out, err)
	}

	if got := g.secret(t, "github", "token"); got != token {
		t.Errorf("stored token = %q; want %q, its newline trimmed", got, token)
	}

	show := g.mustGoal(t, cmdConnection, "show", "github")
	list := g.mustGoal(t, cmdConnection, subList)

	for what, want := range map[string][]string{show: {"token: set", "GH_TOKEN", "the repositories", "untested"}, list: {"github", "secrets set: token"}} {
		for _, w := range want {
			if !strings.Contains(what, w) {
				t.Errorf("output %q; want %q in it", what, w)
			}
		}

		if strings.Contains(what, token) {
			t.Errorf("output %q holds the secret", what)
		}
	}
}

// A secret value on the command line is refused before anything is sent: shell history and ps
// would keep it.
func TestConnection_SecretNeverFromArgv(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)

	_, err := g.goal(t, cmdConnection, verbCreate, "github", "--kind", "github", "--secret", "token=ghp_on-the-command-line")
	if !errors.Is(err, errSecretOnCommandLine) {
		t.Errorf("create with a secret in argv = %v; want it refused", err)
	}

	if out := g.mustGoal(t, cmdConnection, subList); !strings.Contains(out, "no connections") {
		t.Errorf("list after the refusal = %q; want nothing created", out)
	}
}

// goal connection update merges --field into the connection's fields (an empty value clears one),
// renames it and replaces a secret from a file.
func TestConnection_Update(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)
	g.mustGoal(t, cmdConnection, verbCreate, "docs", "--kind", "mcp-server", "--field", "url=https://docs.example.com/mcp")
	g.mustGoal(t, cmdConnection, "update", "docs", "--field", "command=npx -y @playwright/mcp@0.0.41")

	if show := g.mustGoal(t, cmdConnection, "show", "docs"); !strings.Contains(show, "url: https://docs.example.com/mcp") || !strings.Contains(show, "command: npx") {
		t.Errorf("show after adding a field = %q; want both fields", show)
	}

	g.mustGoal(t, cmdConnection, "update", "docs", "--field", "url=", "--name", "playwright")

	if show := g.mustGoal(t, cmdConnection, "show", "playwright"); strings.Contains(show, "url:") || !strings.Contains(show, "command:") {
		t.Errorf("show after clearing url and the rename = %q; want command alone", show)
	}

	if _, err := g.goalIn(t, "ghp_first", cmdConnection, verbCreate, "github", "--kind", "github", "--secret", "token=-"); err != nil {
		t.Fatal(err)
	}

	file := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(file, []byte("ghp_second\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	g.mustGoal(t, cmdConnection, "update", "github", "--secret-file", "token="+file)

	if got := g.secret(t, "github", "token"); got != "ghp_second" {
		t.Errorf("token after --secret-file = %q; want the file's", got)
	}
}

// An organization kind is pushed from YAML; a connection of it is tested from goald, and the kind
// is deleted once nothing is of it.
func TestConnection_KindPushTestAndDelete(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)
	ping := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k-cli" {
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	t.Cleanup(ping.Close)

	file := filepath.Join(t.TempDir(), "acme-api.yaml")
	kind := `apiVersion: goal/v1
kind: ConnectionKind
metadata: {name: acme-api, title: Acme API}
spec:
  fields:
    - {name: base_url, title: Base URL, required: true}
    - {name: key, title: Key, secret: true, required: true}
  env: {ACME_URL: "{{base_url}}", ACME_KEY: "{{key}}"}
  test: {method: GET, url: "{{base_url}}/ping", headers: {Authorization: "Bearer {{key}}"}}
`

	if err := os.WriteFile(file, []byte(kind), 0o600); err != nil {
		t.Fatal(err)
	}

	if out := g.mustGoal(t, cmdConnection, "kind", "push", file); !strings.Contains(out, "pushed connection kind acme-api") {
		t.Errorf("kind push = %q", out)
	}

	if out := g.mustGoal(t, cmdConnection, "kinds"); !strings.Contains(out, "organization") || !strings.Contains(out, "key (secret)*") {
		t.Errorf("kinds = %q; want acme-api as the organization's, its key secret and required", out)
	}

	if _, err := g.goalIn(t, "k-cli", cmdConnection, verbCreate, "acme-api", "--kind", "acme-api", "--field", "base_url="+ping.URL, "--secret", "key=-"); err != nil {
		t.Fatal(err)
	}

	if out := g.mustGoal(t, cmdConnection, "test", "acme-api"); !strings.Contains(out, "works: ok (200)") {
		t.Errorf("test = %q; want it to work", out)
	}

	if _, err := g.goal(t, cmdConnection, "kind", verbDelete, "acme-api"); err == nil || !strings.Contains(err.Error(), "connections of kind") {
		t.Errorf("kind delete while a connection is of it = %v; want it refused", err)
	}

	g.mustGoal(t, cmdConnection, verbDelete, "acme-api")

	if out := g.mustGoal(t, cmdConnection, "kind", verbDelete, "acme-api"); !strings.Contains(out, "deleted connection kind acme-api") {
		t.Errorf("kind delete = %q", out)
	}
}

// Testing a connection whose kind has no test is refused, saying so.
func TestConnection_TestOfAKindWithoutOne(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)
	g.mustGoal(t, cmdConnection, verbCreate, "docs", "--kind", "mcp-server", "--field", "url=https://docs.example.com/mcp")

	if _, err := g.goal(t, cmdConnection, "test", "docs"); err == nil || !strings.Contains(err.Error(), "has no test") {
		t.Errorf("test of a kind without one = %v; want it refused", err)
	}
}
