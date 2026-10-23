package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestLoad_AcceptsEveryFixtureFactory(t *testing.T) {
	t.Parallel()

	dirs, _ := filepath.Glob("testdata/*")
	if len(dirs) == 0 {
		t.Fatal("no fixture factories found")
	}

	for _, dir := range dirs {
		if _, err := LoadDir(dir); err != nil {
			t.Errorf("%s: %v", dir, err)
		}
	}
}

func TestBrokenReferencesAreReported(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{"factory.yaml": {Data: []byte(`
apiVersion: goal/v1
kind: Factory
metadata: { id: 00000000-0000-4000-8000-000000000000, name: broken }
spec:
  runtime: { runner: docker, image: x, auth: claude-subscription }
  knowledgeBase: { collections: [notes] }
  agents:
    - { name: worker, prompt: ./missing.md, skills: [nope] }
  board:
    stations:
      - { id: inbox, kind: queue }
      - { id: work, kind: agent, agent: ghost, gate: { onReject: nowhere } }
      - { id: done, kind: done }
`)}}

	_, err := Load(files)
	if err == nil {
		t.Fatal("expected errors")
	}

	for _, want := range []string{"missing.md", `unknown skill "nope"`, `unknown agent "ghost"`, `"nowhere" is not a station`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s:\n%v", want, err)
		}
	}
}

// A factory is a self-contained tree: it may live in a bucket prefix, so a prompt path that
// climbs out of it must not resolve against whatever happens to sit next to it on disk.
func TestLoad_PathsCannotLeaveTheFactory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write(t, filepath.Join(root, "outside.md"), "# not part of the factory\n")
	write(t, filepath.Join(root, "acme", "factory.yaml"), `
apiVersion: goal/v1
kind: Factory
metadata: { id: 00000000-0000-4000-8000-000000000001, name: escape }
spec:
  runtime: { runner: docker, image: x, auth: claude-subscription }
  knowledgeBase: { collections: [notes] }
  agents:
    - { name: worker, prompt: ../outside.md }
  board:
    stations:
      - { id: work, kind: agent, agent: worker }
      - { id: done, kind: done }
`)

	_, err := LoadDir(filepath.Join(root, "acme"))
	if err == nil || !strings.Contains(err.Error(), "agent worker prompt: ../outside.md not found") {
		t.Fatalf("err = %v, want the escaping prompt reported as not found", err)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
