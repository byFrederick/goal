package cli

import (
	"bytes"
	"errors"
	"maps"
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

// goalEnv runs one CLI command line against g with env added to the CLI's environment.
func (g *defGoald) goalEnv(t *testing.T, env map[string]string, args ...string) (string, error) {
	t.Helper()

	var buf bytes.Buffer

	env = maps.Clone(env)
	env[envOrg] = orgAcme

	err := run(t.Context(), args, &printer{w: &buf, in: strings.NewReader("")}, testEnv(t, g.srv.URL, env), nil)

	return buf.String(), err
}

// The connections goal connection import-env creates.
const (
	importedGitHub = "github"
	importedModel  = "claude"
)

// imported is a connection goal connection import-env should have created: its kind, its secret
// field's value and its fields.
type imported struct {
	kind, field, secret string
	fields              map[string]string
}

// checkImported fails unless the organization holds exactly want, and out says each was created.
func (g *defGoald) checkImported(t *testing.T, out string, want map[string]imported) {
	t.Helper()

	ctx := domain.WithOrg(t.Context(), g.orgID)

	for name, w := range want {
		conn, err := g.conns.Get(ctx, name)
		if err != nil || conn.Kind != w.kind || !strings.Contains(out, "created connection "+name) {
			t.Fatalf("connection %s = %+v, %v (output %q); want one of kind %s, said so", name, conn, err, out, w.kind)
		}

		if got := g.secret(t, name, w.field); got != w.secret || !maps.Equal(conn.Fields, w.fields) {
			t.Errorf("connection %s holds %q with fields %v; want the environment's and %v", name, got, conn.Fields, w.fields)
		}
	}

	if list, err := g.conns.List(ctx); err != nil || len(list) != len(want) {
		t.Errorf("connections = %+v, %v; want only the imported ones", list, err)
	}
}

// The install's environment tokens become the organization's connections (spec 4 §14): the
// GitHub token a github connection on the install's GitHub, the subscription token (or else the
// API key) the model connection claude. No value is printed, and a second run, with other values,
// changes nothing.
func TestConnection_ImportEnv(t *testing.T) {
	t.Parallel()

	const (
		githubToken = "ghp_import-env-token"  //nolint:gosec // a test value, not a credential
		oauthToken  = "sk-ant-oat-import-env" //nolint:gosec // a test value, not a credential
		apiKey      = "sk-ant-api-import-env" //nolint:gosec // a test value, not a credential
	)

	gh := func(fields map[string]string) imported {
		return imported{kind: "github", field: "token", secret: githubToken, fields: fields}
	}

	tests := []struct {
		name string
		env  map[string]string
		want map[string]imported
	}{
		{
			name: "github.com and a subscription",
			env:  map[string]string{envGitHubToken: githubToken, envOAuthToken: oauthToken, envAnthropicKey: apiKey},
			want: map[string]imported{
				importedGitHub: gh(map[string]string{}),
				importedModel:  {kind: "claude-subscription", field: "token", secret: oauthToken, fields: map[string]string{}},
			},
		},
		{
			name: "a GitHub Enterprise Server and an API key",
			env:  map[string]string{envGitHubToken: githubToken, envGitHubWeb: "https://GHE.example.com/", envAnthropicKey: apiKey},
			want: map[string]imported{
				importedGitHub: gh(map[string]string{"host": "ghe.example.com", "api_url": "https://ghe.example.com/api/v3"}),
				importedModel:  {kind: "anthropic", field: "key", secret: apiKey, fields: map[string]string{}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := newDefGoald(t)

			out, err := g.goalEnv(t, tt.env, cmdConnection, "import-env")
			if err != nil || strings.Contains(out, githubToken) || strings.Contains(out, oauthToken) || strings.Contains(out, apiKey) {
				t.Fatalf("import-env = %q, %v; want it done without a value printed", out, err)
			}

			g.checkImported(t, out, tt.want)

			other := maps.Clone(tt.env)
			for _, k := range []string{envGitHubToken, envOAuthToken, envAnthropicKey} {
				other[k] += "-other"
			}

			again, err := g.goalEnv(t, other, cmdConnection, "import-env")
			if err != nil || strings.Contains(again, "created") || !strings.Contains(again, "github exists") {
				t.Errorf("a second import-env = %q, %v; want each connection left as it is", again, err)
			}

			g.checkImported(t, out, tt.want)
		})
	}
}

// Without any of the variables there is nothing to import, and the command says which it reads.
func TestConnection_ImportEnvWithNothingToImport(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)

	out, err := g.goalEnv(t, map[string]string{}, cmdConnection, "import-env")
	if err != nil || !strings.Contains(out, envGitHubToken) || !strings.Contains(out, envOAuthToken) {
		t.Errorf("import-env = %q, %v; want it to name the variables it reads", out, err)
	}
}
