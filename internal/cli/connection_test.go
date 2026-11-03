package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
)

const (
	cmdConnection  = "connection"
	importedGitHub = "github"
)

// goalIn runs one CLI command line against g with stdin and returns its output.
func (g *defGoald) goalIn(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()

	var buf bytes.Buffer

	err := run(t.Context(), args, &printer{w: &buf, in: strings.NewReader(stdin)}, testEnv(t, g.srv.URL, map[string]string{envOrg: orgAcme}), nil)

	return buf.String(), err
}

// githubConnection creates the connection github whose token refers to the secret acme-token,
// as one made before ADR 0037 would.
func (g *defGoald) githubConnection(t *testing.T) {
	t.Helper()

	n := domain.NewConnection{Name: importedGitHub, Kind: importedGitHub, Secrets: map[string]string{"token": "{{acme-token}}"}} //nolint:gosec // a reference to a test secret, not a value
	if _, err := g.conns.Create(domain.WithOrg(t.Context(), g.orgID), n, domain.LocalUser); err != nil {
		t.Fatal(err)
	}
}

// deleteGitHubConnection deletes the connection github.
func (g *defGoald) deleteGitHubConnection(t *testing.T) {
	t.Helper()

	if err := g.conns.Delete(domain.WithOrg(t.Context(), g.orgID), importedGitHub, domain.LocalUser); err != nil {
		t.Fatal(err)
	}
}

// goal connection, whatever follows it, prints where connections moved and exits 2, without
// its usage and without calling goald.
func TestConnectionCommand_Moved(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)

	for _, args := range [][]string{
		{cmdConnection},
		{cmdConnection, subList},
		{cmdConnection, verbCreate, importedGitHub, "--kind", importedGitHub, "--secret", "token=-"},
		{cmdConnection, "test", importedGitHub},
		{cmdConnection, "import-env", "--org", orgAcme},
	} {
		stdout, stderr, code := g.goalRun(t, secretValue, args...)
		if code != 2 || stdout != "" || strings.Count(stderr, "\n") != 1 || strings.Contains(stderr, "usage:") ||
			!strings.Contains(stderr, "goal connection moved: see Access (goal access), Secrets (goal secret) and the factory's environment files") {
			t.Errorf("%q = exit %d, stdout %q, stderr %q; want exit 2 and one line naming where it moved", args, code, stdout, stderr)
		}
	}

	if list, err := g.conns.List(domain.WithOrg(t.Context(), g.orgID)); err != nil || len(list) != 0 {
		t.Errorf("connections after goal connection = %v, %v; want none created", list, err)
	}
}
