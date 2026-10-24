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
      - { id: work, kind: agent, agent: ghost, gate: { onReject: nowhere }, checks: [{ kind: command }] }
      - { id: done, kind: done }
`)}}

	_, err := Load(files)
	if err == nil {
		t.Fatal("expected errors")
	}

	for _, want := range []string{
		"missing.md", `unknown skill "nope"`, `unknown agent "ghost"`, `"nowhere" is not a station`,
		"station work: a command check needs run",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s:\n%v", want, err)
		}
	}
}

// A merge action merges pull requests on the factory's repository at a human's word, so it
// belongs only on a human station of a factory that has a repository.
func TestLoad_MergeActionOnlyOnAHumanStationWithARepository(t *testing.T) {
	t.Parallel()

	factory := func(repository, station string) fstest.MapFS {
		return fstest.MapFS{"factory.yaml": {Data: []byte(`
apiVersion: goal/v1
kind: Factory
metadata: { id: 00000000-0000-4000-8000-000000000002, name: merger }
spec:
  runtime: { runner: docker, image: x, auth: claude-subscription` + repository + ` }
  knowledgeBase: { collections: [notes] }
  agents:
    - { name: worker, prompt: ./worker.md }
  board:
    stations:
      - { id: work, kind: agent, agent: worker }
      - ` + station + `
      - { id: done, kind: done }
`)}, "worker.md": {Data: []byte("# worker\n")}}
	}

	repo := `, repository: { url: "https://code.example.com/acme/app.git" }`

	tests := []struct {
		name, repository, station, wantErr string
	}{
		{"human station with a repository", repo, "{ id: merge, kind: human, action: merge }", ""},
		{"no repository", "", "{ id: merge, kind: human, action: merge }", "action merge needs runtime.repository"},
		{"agent station", repo, "{ id: merge, kind: agent, agent: worker, action: merge }", "schema"},
		{"unknown action", repo, "{ id: merge, kind: human, action: deploy }", "schema"},
	}

	for _, tt := range tests {
		_, err := Load(factory(tt.repository, tt.station))

		switch {
		case tt.wantErr == "" && err != nil:
			t.Errorf("%s: %v", tt.name, err)
		case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
			t.Errorf("%s: err = %v, want one mentioning %q", tt.name, err, tt.wantErr)
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

func TestPinned(t *testing.T) {
	t.Parallel()

	digest := "sha256:" + strings.Repeat("ab", 32)
	tests := map[string]bool{
		"ghcr.io/acme/db-mcp:1.2.3":            true,
		"ghcr.io/acme/db-mcp:1.2.3@" + digest:  true,
		"ghcr.io/acme/db-mcp@" + digest:        true,
		"ghcr.io/acme/db-mcp:latest@" + digest: true,
		"localhost:5000/acme/db-mcp:v0.0.83":   true,
		"ghcr.io/acme/db-mcp":                  false,
		"ghcr.io/acme/db-mcp:latest":           false,
		"localhost:5000/acme/db-mcp":           false, // the port is not a tag
		"ghcr.io/acme/db-mcp@sha256:abc":       false,
	}

	for image, want := range tests {
		if got := Pinned(image); got != want {
			t.Errorf("Pinned(%q) = %v, want %v", image, got, want)
		}
	}
}

func TestWarnings_NameUnpinnedConnectionImages(t *testing.T) {
	t.Parallel()

	f, err := Load(fstest.MapFS{
		"agents/worker.md": {Data: []byte("# worker\n")},
		"factory.yaml": {Data: []byte(`
apiVersion: goal/v1
kind: Factory
metadata: { id: 00000000-0000-4000-8000-000000000002, name: unpinned }
spec:
  runtime: { runner: docker, image: x, auth: claude-subscription }
  knowledgeBase: { collections: [notes] }
  connections:
    - { name: shop-db, type: mcp, transport: stdio, image: "ghcr.io/acme/db-mcp:latest", network: true }
    - { name: wiki, type: mcp, transport: stdio, image: "ghcr.io/acme/docs-mcp:2.0.1" }
  agents:
    - { name: worker, prompt: ./agents/worker.md, connections: [shop-db, wiki] }
  board:
    stations:
      - { id: work, kind: agent, agent: worker }
      - { id: done, kind: done }
`)},
	})
	if err != nil {
		t.Fatal(err)
	}

	if w := f.Warnings(); len(w) != 1 || !strings.Contains(w[0], "connection shop-db: image ghcr.io/acme/db-mcp:latest is not pinned") {
		t.Fatalf("warnings = %q, want one for the shop-db connection", w)
	}

	if !f.Spec.Connections[0].Network || f.Spec.Connections[1].Network {
		t.Fatalf("network = %v, %v; want true, false", f.Spec.Connections[0].Network, f.Spec.Connections[1].Network)
	}
}

// The keys the builder writes are optional additions: a definition using all of them loads,
// and the connector form keeps its connector settings and takes its name from the connector.
func TestLoad_AcceptsTheBuilderKeys(t *testing.T) {
	t.Parallel()

	f, err := Load(checkTree(edited(
		"  name: acme-check\n", "  name: acme-check\n  purpose: Answers acme's support questions.\n",
		runtimeAuth, runtimeAuth+"    budget: { weeklyUsd: 50, onExhausted: stop, perItemUsd: 2 }\n",
		specAgents, `  connections:
    - connector: playwright@2
      agents: [worker]
      origins: [https://preview.example.com]
      tools:
        playwright__click: { agents: [worker], scope: preview, approval: required, enabled: true }
`+specAgents,
		agentPrompt, agentPrompt+"      maxTurns: 30\n      builtins: { read: allowed, run: allowed, subagents: off }\n",
		"      - id: work\n", "      - id: work\n        name: Work\n        description: Where the worker answers.\n        checks: [schema, make test]\n",
	), nil))
	if err != nil {
		t.Fatal(err)
	}

	c := f.Spec.Connections[0]
	if c.Name != "playwright" || string(c.Settings["origins"]) != `["https://preview.example.com"]` || len(c.Settings) != 1 {
		t.Errorf("connection = %+v, want name playwright and one setting, origins", c)
	}

	if b := f.Spec.Runtime.Budget; b.WeeklyUSD != 50 || b.OnExhausted != "stop" || f.Metadata.Purpose == "" {
		t.Errorf("budget = %+v, purpose %q", b, f.Metadata.Purpose)
	}
}

func TestLoad_RejectsBuiltinsBesideCustomTools(t *testing.T) {
	t.Parallel()

	_, err := Load(checkTree(edited(agentPrompt, agentPrompt+"      tools: { allow: [Read] }\n      builtins: { read: allowed }\n"), nil))
	if err == nil || !strings.Contains(err.Error(), "schema") {
		t.Fatalf("err = %v, want a schema problem", err)
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
