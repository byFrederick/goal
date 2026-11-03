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
	cmdSecret  = "secret"
	secretName = "acme-token"
	// secretValue is the value the secret tests store; no output may hold it.
	secretValue = "sk-acme-secret-value-91c4" //nolint:gosec // a test value, not a secret
)

// goalRun runs one CLI command line against g through Run, with stdin, and returns stdout, stderr
// and the exit code.
func (g *defGoald) goalRun(t *testing.T, stdin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()

	var out, errOut bytes.Buffer

	code = Run(t.Context(), args, strings.NewReader(stdin), &out, &errOut, testEnv(t, g.srv.URL, map[string]string{envOrg: orgAcme}), nil)

	return out.String(), errOut.String(), code
}

// secretValueOf is secret name's stored value, opened as a run of a factory it allows would.
func (g *defGoald) secretValueOf(t *testing.T, name string, factory domain.FactoryID) string {
	t.Helper()

	got, err := g.secrets.Resolve(domain.WithOrg(t.Context(), g.orgID), []string{name}, domain.SecretUse{Factory: factory, Agent: "a"})
	if err != nil {
		t.Fatal(err)
	}

	return got[0].Value
}

// secretMeta is secret name as goald keeps it.
func (g *defGoald) secretMeta(t *testing.T, name string) domain.Secret {
	t.Helper()

	s, err := g.secrets.Get(domain.WithOrg(t.Context(), g.orgID), name)
	if err != nil {
		t.Fatal(err)
	}

	return s
}

// goal secret create reads the value from stdin, without its newline, and sends the scope,
// expiry and reminder its flags give; without a scope flag the secret is open to no factory.
func TestSecretCommand_CreateFromStdin(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)

	out, err := g.goalIn(t, secretValue+"\n", cmdSecret, verbCreate, secretName, "--all-factories",
		"--description", "the acme API", "--expires", "2027-03-01", "--remind-days", "14")
	if err != nil || !strings.Contains(out, "created secret acme-token") {
		t.Fatalf("create = %q, %v", out, err)
	}

	if got := g.secretValueOf(t, secretName, "f-any"); got != secretValue {
		t.Errorf("stored value = %q; want %q, its newline trimmed", got, secretValue)
	}

	s := g.secretMeta(t, secretName)
	if !s.Scope.All || s.Description != "the acme API" || s.RemindDays != 14 ||
		s.ExpiresAt == nil || s.ExpiresAt.Format("2006-01-02T15:04Z07:00") != "2027-03-01T00:00Z" {
		t.Errorf("stored = %+v; want all factories, the description, expiry at UTC midnight and 14 days", s)
	}

	if _, err := g.goalIn(t, "other", cmdSecret, verbCreate, "scoped", "--factory", "f-1", "--factory", "f-2"); err != nil {
		t.Fatal(err)
	}

	if s := g.secretMeta(t, "scoped"); s.Scope.All || !slices.Equal(s.Scope.Factories, []domain.FactoryID{"f-1", "f-2"}) {
		t.Errorf("scope = %+v; want f-1 and f-2", s.Scope)
	}

	if _, err := g.goalIn(t, "other", cmdSecret, verbCreate, "unscoped"); err != nil {
		t.Fatal(err)
	}

	if s := g.secretMeta(t, "unscoped"); s.Scope.All || len(s.Scope.Factories) != 0 {
		t.Errorf("scope = %+v; want no factory", s.Scope)
	}
}

// goal secret replace sends the new value from stdin and changes nothing else.
func TestSecretCommand_ReplaceFromStdin(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)

	if _, err := g.goalIn(t, "first", cmdSecret, verbCreate, secretName, "--all-factories"); err != nil {
		t.Fatal(err)
	}

	out, err := g.goalIn(t, secretValue+"\r\n", cmdSecret, verbReplace, secretName)
	if err != nil || !strings.Contains(out, "replaced the value of secret acme-token") {
		t.Fatalf("replace = %q, %v", out, err)
	}

	if got := g.secretValueOf(t, secretName, "f-any"); got != secretValue {
		t.Errorf("stored value = %q; want %q", got, secretValue)
	}

	if s := g.secretMeta(t, secretName); !s.Scope.All {
		t.Errorf("scope after replace = %+v; want it unchanged", s.Scope)
	}
}

// A value is never taken from argv, and an empty stdin is refused before goald is called.
func TestSecretCommand_ValueNeverFromArgv(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)

	for _, line := range [][]string{
		{cmdSecret, verbCreate, secretName, "--value", secretValue},
		{cmdSecret, verbCreate, secretName, secretValue},
		{cmdSecret, verbReplace, secretName, secretValue},
		{cmdSecret, verbReplace, secretName, "--value", secretValue},
	} {
		if _, err := g.goalIn(t, "", line...); !errors.Is(err, errUsage) {
			t.Errorf("%q = %v; want a usage error", line, err)
		}
	}

	for _, line := range [][]string{{cmdSecret, verbCreate, secretName}, {cmdSecret, verbReplace, secretName}} {
		if _, err := g.goalIn(t, "\n", line...); !errors.Is(err, errNoSecretValue) {
			t.Errorf("%q with an empty stdin = %v; want errNoSecretValue", line, err)
		}
	}

	if out := g.mustGoal(t, cmdSecret, subList); !strings.Contains(out, "no secrets") {
		t.Errorf("list after the refusals = %q; want nothing created", out)
	}
}

// goal secret list prints each secret's name, factories, uses, last use and expiry; goal secret
// show prints one with its description, reminder and who changed it.
func TestSecretCommand_ListAndShow(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)

	if _, err := g.goalIn(t, secretValue, cmdSecret, verbCreate, secretName, "--all-factories", "--expires", "2027-03-01",
		"--remind-days", "7", "--description", "the acme API"); err != nil {
		t.Fatal(err)
	}

	if _, err := g.goalIn(t, "other", cmdSecret, verbCreate, "deploy-key", "--factory", "f-1"); err != nil {
		t.Fatal(err)
	}

	if _, err := g.goalIn(t, "{{acme-token}}", cmdConnection, verbCreate, importedGitHub, "--kind", importedGitHub, "--secret", "token=-"); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(g.mustGoal(t, cmdSecret, subList)), "\n")
	if len(lines) != 2 {
		t.Fatalf("list = %q; want two lines", lines)
	}

	for i, want := range [][]string{
		{secretName, "factories all", "used by connection github", "last used never", "expires 2027-03-01"},
		{"deploy-key", "factories f-1", "used by nothing", "last used never", "expires none"},
	} {
		for _, w := range want {
			if !strings.Contains(lines[i], w) {
				t.Errorf("line %q; want %q in it", lines[i], w)
			}
		}
	}

	show := g.mustGoal(t, cmdSecret, subShow, secretName)
	for _, w := range []string{
		"acme-token", "source       value", "description  the acme API", "factories    all", "used by      connection github",
		"expires      2027-03-01, reminder 7 days before", "last used    never",
	} {
		if !strings.Contains(show, w) {
			t.Errorf("show = %q; want %q in it", show, w)
		}
	}

	if _, err := g.goal(t, cmdSecret, subShow, "missing"); err == nil || !strings.Contains(err.Error(), "secret not found") {
		t.Errorf("show of no secret = %v; want secret not found", err)
	}
}

// An import's dry run prints each name as new or exists and stores nothing; a real one prints
// what it did with each.
func TestSecretCommand_ImportDryRun(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)

	if _, err := g.goalIn(t, "old", cmdSecret, verbCreate, secretName, "--all-factories"); err != nil {
		t.Fatal(err)
	}

	if _, err := g.goalIn(t, "{{acme-token}}", cmdConnection, verbCreate, importedGitHub, "--kind", importedGitHub, "--secret", "token=-"); err != nil {
		t.Fatal(err)
	}

	file := filepath.Join(t.TempDir(), "secrets.env")
	if err := os.WriteFile(file, []byte("ACME_TOKEN="+secretValue+"\nSENTRY_TOKEN="+secretValue+"-2\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out := g.mustGoal(t, cmdSecret, verbImport, file, "--dry-run")
	for _, w := range []string{"exists acme-token (used by connection github)", "new    sentry-token", "dry run: nothing stored"} {
		if !strings.Contains(out, w) {
			t.Errorf("dry run = %q; want %q in it", out, w)
		}
	}

	if list := g.mustGoal(t, cmdSecret, subList); strings.Contains(list, "sentry-token") {
		t.Errorf("list after the dry run = %q; want nothing stored", list)
	}

	out = g.mustGoal(t, cmdSecret, verbImport, file, "--on-conflict", verbReplace)
	for _, w := range []string{"replaced acme-token", "created  sentry-token"} {
		if !strings.Contains(out, w) {
			t.Errorf("import = %q; want %q in it", out, w)
		}
	}

	if got := g.secretValueOf(t, secretName, "f-any"); got != secretValue {
		t.Errorf("acme-token after the import = %q; want it replaced", got)
	}

	if s := g.secretMeta(t, "sentry-token"); s.Scope.All || len(s.Scope.Factories) != 0 {
		t.Errorf("imported scope = %+v; want no factory", s.Scope)
	}

	if out := g.mustGoal(t, cmdSecret, verbImport, file, "--format", "env"); !strings.Contains(out, "skipped  acme-token") {
		t.Errorf("import again = %q; want acme-token skipped", out)
	}

	for _, line := range [][]string{
		{cmdSecret, verbImport, file, "--format", "toml"},
		{cmdSecret, verbImport, file, "--on-conflict", "merge"},
		{cmdSecret, verbImport, filepath.Join(t.TempDir(), "secrets.txt")},
	} {
		if _, err := g.goal(t, line...); !errors.Is(err, errUsage) {
			t.Errorf("%q = %v; want a usage error", line, err)
		}
	}
}

// goal secret delete of a secret a connection refers to fails and names the connection; once it
// is free, delete deletes it.
func TestSecretCommand_DeleteInUse(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)

	if _, err := g.goalIn(t, secretValue, cmdSecret, verbCreate, secretName, "--all-factories"); err != nil {
		t.Fatal(err)
	}

	if _, err := g.goalIn(t, "{{acme-token}}", cmdConnection, verbCreate, importedGitHub, "--kind", importedGitHub, "--secret", "token=-"); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := g.goalRun(t, "", cmdSecret, verbDelete, secretName)
	if code != 1 || !strings.Contains(stderr, "secret acme-token is used by connection github") {
		t.Errorf("delete in use = exit %d, stdout %q, stderr %q; want exit 1 naming connection github", code, stdout, stderr)
	}

	g.mustGoal(t, cmdConnection, verbDelete, importedGitHub)

	if out := g.mustGoal(t, cmdSecret, verbDelete, secretName); !strings.Contains(out, "deleted secret acme-token") {
		t.Errorf("delete = %q", out)
	}

	if out := g.mustGoal(t, cmdSecret, subList); !strings.Contains(out, "no secrets") {
		t.Errorf("list after delete = %q; want none", out)
	}
}

// No command prints a value, on stdout or stderr, whether it succeeds or fails.
func TestSecretCommand_NeverPrintsAValue(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)

	file := filepath.Join(t.TempDir(), "secrets.yaml")
	if err := os.WriteFile(file, []byte("credentials:\n  acme-token: "+secretValue+"\n  sentry-token: \""+secretValue+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	bad := filepath.Join(t.TempDir(), "bad.env")
	if err := os.WriteFile(bad, []byte("ACME_TOKEN "+secretValue+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, step := range []struct {
		stdin string
		args  []string
	}{
		{secretValue + "\n", []string{cmdSecret, verbCreate, secretName, "--all-factories"}},
		{secretValue, []string{cmdSecret, verbCreate, secretName}},
		{secretValue, []string{cmdSecret, verbCreate, "Bad_Name"}},
		{secretValue, []string{cmdSecret, verbReplace, secretName}},
		{secretValue, []string{cmdSecret, verbReplace, "missing"}},
		{"{{acme-token}}", []string{cmdConnection, verbCreate, importedGitHub, "--kind", importedGitHub, "--secret", "token=-"}},
		{"", []string{cmdSecret, subList}},
		{"", []string{cmdSecret, subShow, secretName}},
		{"", []string{cmdSecret, verbImport, file, "--dry-run"}},
		{"", []string{cmdSecret, verbImport, file, "--on-conflict", verbReplace}},
		{"", []string{cmdSecret, verbImport, bad}},
		{"", []string{cmdSecret, verbDelete, secretName}},
		{"", []string{cmdSecret, verbCreate, "x-token", "--value", secretValue}},
	} {
		stdout, stderr, code := g.goalRun(t, step.stdin, step.args...)
		if strings.Contains(stdout, secretValue) || strings.Contains(stderr, secretValue) {
			t.Errorf("%q (exit %d) printed the value: stdout %q, stderr %q", step.args, code, stdout, stderr)
		}
	}
}

// goal credential, whatever follows it, prints that it moved to goal secret and exits 2, without
// calling goald or printing the whole usage.
func TestCredentialCommand_Moved(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)

	for _, args := range [][]string{
		{"credential"},
		{"credential", subList},
		{"credential", verbCreate, secretName, "--type", "api_key"},
		{"credential", "rm", secretName},
	} {
		stdout, stderr, code := g.goalRun(t, secretValue, args...)
		if code != 2 || stdout != "" || !strings.Contains(stderr, "goal credential moved to goal secret") ||
			strings.Contains(stderr, "usage:") || strings.Count(stderr, "\n") != 1 {
			t.Errorf("%q = exit %d, stdout %q, stderr %q; want exit 2 and one line naming goal secret", args, code, stdout, stderr)
		}
	}

	if out := g.mustGoal(t, cmdSecret, subList); !strings.Contains(out, "no secrets") {
		t.Errorf("list after goal credential = %q; want nothing created", out)
	}
}
