package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
)

const (
	cmdCredential = "credential"
	flagCredType  = "--type"
	credName      = "acme-token"
	credAPIKey    = "api_key"
	// credValue is the secret the credential tests store; no output may hold it.
	credValue = "sk-acme-credential-value" //nolint:gosec // a test value, not a credential
)

// goalRun runs one CLI command line against g through Run, with stdin, and returns stdout, stderr
// and the exit code.
func (g *defGoald) goalRun(t *testing.T, stdin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()

	var out, errOut bytes.Buffer

	code = Run(t.Context(), args, strings.NewReader(stdin), &out, &errOut, testEnv(t, g.srv.URL, map[string]string{envOrg: orgAcme}), nil)

	return out.String(), errOut.String(), code
}

// credentialValue is credential name's stored value, opened as a run of any factory would.
func (g *defGoald) credentialValue(t *testing.T, name string) string {
	t.Helper()

	got, err := g.creds.Resolve(domain.WithOrg(t.Context(), g.orgID), []string{name}, domain.CredentialUse{Factory: "f-any", Agent: "a"})
	if err != nil {
		t.Fatal(err)
	}

	return got[0].Value
}

// credentialMeta is credential name as goald keeps it.
func (g *defGoald) credentialMeta(t *testing.T, name string) domain.Credential {
	t.Helper()

	c, err := g.creds.Get(domain.WithOrg(t.Context(), g.orgID), name)
	if err != nil {
		t.Fatal(err)
	}

	return c
}

// goal credential create reads the value from stdin, without its newline, and sends the scope,
// expiry and reminder its flags give.
func TestCredentialCommand_CreateFromStdin(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)

	out, err := g.goalIn(t, credValue+"\n", cmdCredential, verbCreate, credName, flagCredType, credAPIKey, "--all-factories",
		"--description", "the acme API", "--expires", "2027-03-01", "--remind-days", "14")
	if err != nil || !strings.Contains(out, "created credential acme-token") {
		t.Fatalf("create = %q, %v", out, err)
	}

	if got := g.credentialValue(t, credName); got != credValue {
		t.Errorf("stored value = %q; want %q, its newline trimmed", got, credValue)
	}

	c := g.credentialMeta(t, credName)
	if !c.Scope.All || c.Description != "the acme API" || c.RemindDays != 14 ||
		c.ExpiresAt == nil || c.ExpiresAt.Format("2006-01-02T15:04Z07:00") != "2027-03-01T00:00Z" {
		t.Errorf("stored = %+v; want all factories, the description, expiry at UTC midnight and 14 days", c)
	}

	if _, err := g.goalIn(t, "other", cmdCredential, verbCreate, "scoped", flagCredType, credAPIKey, "--factory", "f-1", "--factory", "f-2"); err != nil {
		t.Fatal(err)
	}

	if c := g.credentialMeta(t, "scoped"); c.Scope.All || !slices.Equal(c.Scope.Factories, []domain.FactoryID{"f-1", "f-2"}) {
		t.Errorf("scope = %+v; want f-1 and f-2", c.Scope)
	}
}

// goal credential replace sends the new value from stdin.
func TestCredentialCommand_ReplaceFromStdin(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)

	if _, err := g.goalIn(t, "first", cmdCredential, verbCreate, credName, flagCredType, credAPIKey, "--all-factories"); err != nil {
		t.Fatal(err)
	}

	out, err := g.goalIn(t, credValue+"\r\n", cmdCredential, verbReplace, credName)
	if err != nil || !strings.Contains(out, "replaced the value of credential acme-token") {
		t.Fatalf("replace = %q, %v", out, err)
	}

	if got := g.credentialValue(t, credName); got != credValue {
		t.Errorf("stored value = %q; want %q", got, credValue)
	}

	if c := g.credentialMeta(t, credName); !c.Scope.All {
		t.Errorf("scope after replace = %+v; want it unchanged", c.Scope)
	}
}

// A value is never taken from argv, and an empty stdin is refused before goald is called.
func TestCredentialCommand_ValueNeverFromArgv(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)

	for _, line := range [][]string{
		{cmdCredential, verbCreate, credName, flagCredType, credAPIKey, "--value", credValue},
		{cmdCredential, verbCreate, credName, credValue, flagCredType, credAPIKey},
		{cmdCredential, verbReplace, credName, credValue},
		{cmdCredential, verbReplace, credName, "--value", credValue},
	} {
		if _, err := g.goalIn(t, "", line...); !errors.Is(err, errUsage) {
			t.Errorf("%q = %v; want a usage error", line, err)
		}
	}

	for _, line := range [][]string{
		{cmdCredential, verbCreate, credName, flagCredType, credAPIKey},
		{cmdCredential, verbReplace, credName},
	} {
		if _, err := g.goalIn(t, "\n", line...); !errors.Is(err, errNoCredentialValue) {
			t.Errorf("%q with an empty stdin = %v; want errNoCredentialValue", line, err)
		}
	}

	if out := g.mustGoal(t, cmdCredential, subList); !strings.Contains(out, "no credentials") {
		t.Errorf("list after the refusals = %q; want nothing created", out)
	}
}

// goal credential list prints each credential's name, type, factories, users, last use and expiry.
func TestCredentialCommand_List(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)

	if _, err := g.goalIn(t, credValue, cmdCredential, verbCreate, credName, flagCredType, credAPIKey, "--all-factories", "--expires", "2027-03-01"); err != nil {
		t.Fatal(err)
	}

	if _, err := g.goalIn(t, "other", cmdCredential, verbCreate, "deploy-key", flagCredType, "ssh_key", "--factory", "f-1"); err != nil {
		t.Fatal(err)
	}

	if _, err := g.goalIn(t, "{{acme-token}}", cmdConnection, verbCreate, importedGitHub, "--kind", importedGitHub, "--secret", "token=-"); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(g.mustGoal(t, cmdCredential, subList)), "\n")
	if len(lines) != 2 {
		t.Fatalf("list = %q; want two lines", lines)
	}

	for i, want := range [][]string{
		{credName, credAPIKey, "factories all", "used by connection github", "last used never", "expires 2027-03-01"},
		{"deploy-key", "ssh_key", "factories f-1", "used by nothing", "last used never", "expires none"},
	} {
		for _, w := range want {
			if !strings.Contains(lines[i], w) {
				t.Errorf("line %q; want %q in it", lines[i], w)
			}
		}
	}
}

// An import's dry run prints each name as new or exists and stores nothing; a real one prints
// what it did with each.
func TestCredentialCommand_ImportDryRun(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)

	if _, err := g.goalIn(t, "old", cmdCredential, verbCreate, credName, flagCredType, credAPIKey, "--all-factories"); err != nil {
		t.Fatal(err)
	}

	file := filepath.Join(t.TempDir(), "creds.env")
	if err := os.WriteFile(file, []byte("ACME_TOKEN="+credValue+"\nSENTRY_TOKEN="+credValue+"-2\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out := g.mustGoal(t, cmdCredential, verbImport, file, "--dry-run")
	for _, w := range []string{"exists acme-token (used by 0 connections)", "new    sentry-token", "dry run: nothing stored"} {
		if !strings.Contains(out, w) {
			t.Errorf("dry run = %q; want %q in it", out, w)
		}
	}

	if list := g.mustGoal(t, cmdCredential, subList); strings.Contains(list, "sentry-token") {
		t.Errorf("list after the dry run = %q; want nothing stored", list)
	}

	out = g.mustGoal(t, cmdCredential, verbImport, file, "--on-conflict", verbReplace)
	for _, w := range []string{"replaced acme-token", "created  sentry-token"} {
		if !strings.Contains(out, w) {
			t.Errorf("import = %q; want %q in it", out, w)
		}
	}

	if got := g.credentialValue(t, credName); got != credValue {
		t.Errorf("acme-token after the import = %q; want it replaced", got)
	}

	if out := g.mustGoal(t, cmdCredential, verbImport, file, "--format", "env"); !strings.Contains(out, "skipped  acme-token") {
		t.Errorf("import again = %q; want acme-token skipped", out)
	}

	for _, line := range [][]string{
		{cmdCredential, verbImport, file, "--format", "toml"},
		{cmdCredential, verbImport, file, "--on-conflict", "merge"},
		{cmdCredential, verbImport, filepath.Join(t.TempDir(), "creds.txt")},
	} {
		if _, err := g.goal(t, line...); !errors.Is(err, errUsage) {
			t.Errorf("%q = %v; want a usage error", line, err)
		}
	}
}

// goal credential rm of a credential a connection refers to fails and names the connection; once
// it is free, rm deletes it.
func TestCredentialCommand_RmConflict(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)

	if _, err := g.goalIn(t, credValue, cmdCredential, verbCreate, credName, flagCredType, credAPIKey, "--all-factories"); err != nil {
		t.Fatal(err)
	}

	if _, err := g.goalIn(t, "{{acme-token}}", cmdConnection, verbCreate, importedGitHub, "--kind", importedGitHub, "--secret", "token=-"); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := g.goalRun(t, "", cmdCredential, "rm", credName)
	if code == 0 || !strings.Contains(stderr, "connections github") {
		t.Errorf("rm in use = exit %d, stdout %q, stderr %q; want a failure naming github", code, stdout, stderr)
	}

	g.mustGoal(t, cmdConnection, verbDelete, importedGitHub)

	if out := g.mustGoal(t, cmdCredential, "rm", credName); !strings.Contains(out, "deleted credential acme-token") {
		t.Errorf("rm = %q", out)
	}

	if out := g.mustGoal(t, cmdCredential, subList); !strings.Contains(out, "no credentials") {
		t.Errorf("list after rm = %q; want none", out)
	}
}

// No command prints a value, on stdout or stderr, whether it succeeds or fails.
func TestCredentialCommand_NeverPrintsAValue(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)

	file := filepath.Join(t.TempDir(), "creds.yaml")
	if err := os.WriteFile(file, []byte("credentials:\n  acme-token: "+credValue+"\n  sentry-token: \""+credValue+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	bad := filepath.Join(t.TempDir(), "bad.env")
	if err := os.WriteFile(bad, []byte("ACME_TOKEN "+credValue+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, step := range []struct {
		stdin string
		args  []string
	}{
		{credValue + "\n", []string{cmdCredential, verbCreate, credName, flagCredType, credAPIKey, "--all-factories"}},
		{credValue, []string{cmdCredential, verbCreate, credName, flagCredType, credAPIKey}},
		{credValue, []string{cmdCredential, verbCreate, "Bad_Name", flagCredType, credAPIKey}},
		{credValue, []string{cmdCredential, verbReplace, credName}},
		{credValue, []string{cmdCredential, verbReplace, "missing"}},
		{"{{acme-token}}", []string{cmdConnection, verbCreate, importedGitHub, "--kind", importedGitHub, "--secret", "token=-"}},
		{"", []string{cmdCredential, subList}},
		{"", []string{cmdCredential, verbImport, file, "--dry-run"}},
		{"", []string{cmdCredential, verbImport, file, "--on-conflict", verbReplace}},
		{"", []string{cmdCredential, verbImport, bad}},
		{"", []string{cmdCredential, "rm", credName}},
		{"", []string{cmdCredential, verbCreate, "x-token", flagCredType, credAPIKey, "--value", credValue}},
	} {
		stdout, stderr, code := g.goalRun(t, step.stdin, step.args...)
		if strings.Contains(stdout, credValue) || strings.Contains(stderr, credValue) {
			t.Errorf("%q (exit %d) printed the value: stdout %q, stderr %q", step.args, code, stdout, stderr)
		}
	}
}
