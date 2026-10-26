package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_CheckFailsWhenASourceChangesWithoutRegenerating(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	perms := filepath.Join(dir, "permissions.yaml")
	openapi := filepath.Join(dir, "openapi.yaml")
	out := filepath.Join(dir, "permissions_gen.go")
	ts := filepath.Join(dir, "access.gen.ts")

	write(t, perms, testPermissions)
	write(t, openapi, testOpenAPI)

	args := []string{"-permissions", perms, "-openapi", openapi, "-go", out, "-ts", ts}
	check := append([]string{"-check"}, args...)

	if err := run(check); !errors.Is(err, errStale) {
		t.Errorf("check before generating: %v, want a stale file (it is missing)", err)
	}

	if err := run(args); err != nil {
		t.Fatalf("generate: %v", err)
	}

	if err := run(check); err != nil {
		t.Fatalf("check after generating: %v", err)
	}

	changes := []struct {
		name, path, from, to string
	}{
		{name: "a rule", path: perms, from: "factory_role: approver", to: "factory_role: editor"},
		{name: "an operation's scope", path: openapi, from: `x-goal-scope: "org:read"`, to: "x-goal-scope: public"},
		{name: "a Better Auth statement", path: perms, from: "invitation: [create]", to: "invitation: [cancel]"},
	}

	for _, c := range changes {
		raw, err := os.ReadFile(c.path)
		if err != nil {
			t.Fatal(err)
		}

		write(t, c.path, strings.Replace(string(raw), c.from, c.to, 1))

		if err := run(check); !errors.Is(err, errStale) {
			t.Errorf("check after changing %s: %v, want a stale file", c.name, err)
		}

		write(t, c.path, string(raw))
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil { //nolint:gosec // the test's own temp dir
		t.Fatal(err)
	}
}
