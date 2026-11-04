package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestGitT_IgnoresInheritedGitDir runs the test helpers the way the pre-push hook would, with
// GIT_DIR and GIT_WORK_TREE naming another repository, and checks that repository is untouched.
// Not parallel: t.Setenv changes the process environment.
func TestGitT_IgnoresInheritedGitDir(t *testing.T) {
	sentinel := t.TempDir()
	gitT(t, sentinel, "init", "-q", "-b", "main")
	write(t, sentinel, "keep.txt")
	gitT(t, sentinel, "add", "-A")
	gitT(t, sentinel, "commit", "-q", "-m", "sentinel")

	before := snapshot(t, sentinel)

	t.Setenv("GIT_DIR", filepath.Join(sentinel, ".git"))
	t.Setenv("GIT_WORK_TREE", sentinel)

	dir := repo(t, "a.txt")

	if _, err := changedFiles(t.Context(), dir, "origin/main"); err != nil {
		t.Fatalf("changedFiles: %v", err)
	}

	after := snapshot(t, sentinel)
	for name, b := range before {
		if !bytes.Equal(b, after[name]) {
			t.Errorf("sentinel %s changed:\nbefore: %s\nafter:  %s", name, b, after[name])
		}
	}

	if bare := after["core.bare"]; string(bare) != "false\n" {
		t.Errorf("sentinel core.bare = %q, want false", bare)
	}
}

// snapshot reads what a stray git would change in the repository at dir: HEAD, its refs, its
// config and core.bare. It runs git with no GIT_* repository variables, so it reads dir itself.
func snapshot(t *testing.T, dir string) map[string][]byte {
	t.Helper()

	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(dir, ".git", name)) //nolint:gosec // the test's own repository
		if err != nil {
			t.Fatal(err)
		}

		return b
	}

	out := func(args ...string) []byte {
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...) //nolint:gosec // the test's own repository
		cmd.Env = gitEnv("GIT_CONFIG_GLOBAL=/dev/null")

		b, err := cmd.Output()
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}

		return b
	}

	return map[string][]byte{
		"HEAD":         read("HEAD"),
		"config":       read("config"),
		"for-each-ref": out("for-each-ref"),
		"core.bare":    out("config", "core.bare"),
	}
}
