package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// repo makes a git repository with one commit on main, origin/main pointing at it, and a branch
// checked out from it.
func repo(t *testing.T, files ...string) string {
	t.Helper()

	dir := t.TempDir()

	gitT(t, dir, "init", "-q", "-b", "main")

	for _, f := range files {
		write(t, dir, f)
	}

	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-q", "-m", "base")
	gitT(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD")
	gitT(t, dir, "checkout", "-q", "-b", "goal/1-item")

	return dir
}

// write writes name with content that tells the version apart.
func write(t *testing.T, dir, name string, version ...string) {
	t.Helper()

	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(p, []byte(name+strings.Join(version, "")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func gitT(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...) //nolint:gosec // the test's own repository
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com", "GIT_CONFIG_GLOBAL=/dev/null")

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func TestChangedFiles(t *testing.T) {
	t.Parallel()

	t.Run("a rename across parts selects both parts", testRenameAcrossParts)
	t.Run("staged, unstaged and untracked files count, ignored ones do not", testUncommitted)
	t.Run("nothing changed", testNothingChanged)
	t.Run("a missing base runs everything and says why", testMissingBase)
	t.Run("a git failure fails the plan", testGitFailure)
}

func testRenameAcrossParts(t *testing.T) {
	t.Parallel()

	dir := repo(t, "apps/web/src/util.ts", "docs/a.md")
	if err := os.MkdirAll(filepath.Join(dir, "apps/worker/src"), 0o750); err != nil {
		t.Fatal(err)
	}

	gitT(t, dir, "mv", "apps/web/src/util.ts", "apps/worker/src/util.ts")
	gitT(t, dir, "commit", "-q", "-m", "move")

	got, err := changedFiles(t.Context(), dir, "origin/main")
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"apps/web/src/util.ts", "apps/worker/src/util.ts"}; !slices.Equal(got, want) {
		t.Fatalf("changed = %v, want %v", got, want)
	}

	if want := []string{partEnv.tasks[0], partWorker.tasks[0], partWeb.tasks[0]}; !slices.Equal(planFor(got).tasks, want) {
		t.Fatalf("tasks = %v, want %v", planFor(got).tasks, want)
	}
}

func testUncommitted(t *testing.T) {
	t.Parallel()

	dir := repo(t, ".gitignore", "apps/auth/src/a.ts", "deploy/terraform/r2.tf")
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.log\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	gitT(t, dir, "add", ".gitignore")
	write(t, dir, "deploy/terraform/r2.tf", " v2")
	write(t, dir, "apps/web/src/new.tsx")
	write(t, dir, "debug.log")

	got, err := changedFiles(t.Context(), dir, "origin/main")
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{".gitignore", "apps/web/src/new.tsx", "deploy/terraform/r2.tf"}; !slices.Equal(got, want) {
		t.Fatalf("changed = %v, want %v", got, want)
	}
}

func testNothingChanged(t *testing.T) {
	t.Parallel()

	got, err := changedFiles(t.Context(), repo(t, "README.md"), "origin/main")
	if err != nil || len(got) != 0 {
		t.Fatalf("changedFiles = %v, %v, want none", got, err)
	}
}

func testMissingBase(t *testing.T) {
	t.Parallel()

	dir := repo(t, "apps/web/src/a.tsx")

	_, err := changedFiles(t.Context(), dir, "origin/nope")
	if !errors.Is(err, errNoBase) || !strings.Contains(err.Error(), "origin/nope not found") {
		t.Fatalf("err = %v, want errNoBase naming origin/nope", err)
	}

	var stdout, stderr bytes.Buffer
	if err := run(t.Context(), []string{"-C", dir, "-base", "origin/nope"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}

	if got := strings.Fields(stdout.String()); !slices.Equal(got, tasks) {
		t.Fatalf("tasks = %v, want every task", got)
	}

	if !strings.Contains(stderr.String(), "origin/nope not found") {
		t.Fatalf("stderr = %q, want the reason", stderr.String())
	}
}

func testGitFailure(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer

	err := run(t.Context(), []string{"-C", t.TempDir()}, &stdout, &stderr)
	if err == nil || stdout.Len() != 0 {
		t.Fatalf("outside a repository: err = %v, stdout = %q, want an error and no plan", err, stdout.String())
	}
}
