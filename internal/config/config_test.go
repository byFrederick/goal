package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExampleFactoriesAreValid(t *testing.T) {
	t.Parallel()

	dirs, _ := filepath.Glob("../../factories/*")
	if len(dirs) == 0 {
		t.Fatal("no example factories found")
	}

	for _, dir := range dirs {
		if _, err := Load(dir); err != nil {
			t.Errorf("%s: %v", dir, err)
		}
	}
}

func TestBrokenReferencesAreReported(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "factory.yaml"), []byte(`
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
`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected errors")
	}

	for _, want := range []string{"missing.md", `unknown skill "nope"`, `unknown agent "ghost"`, `"nowhere" is not a station`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s:\n%v", want, err)
		}
	}
}
