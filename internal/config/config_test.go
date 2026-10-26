package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/byFrederick/goal/internal/core/domain"
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
  runtime: { modelConnection: claude }
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

// A merge action merges the item's pull requests at a human's word, so it belongs only on a human
// station. Whether a connection can merge them is the connections check's (merge-needs-code-host).
// action: merge is for a human station and for an agent station with a gate, whose approval merges.
func TestLoad_MergeActionOnAHumanOrGateStation(t *testing.T) {
	t.Parallel()

	factory := func(repository, station string) fstest.MapFS {
		return fstest.MapFS{"factory.yaml": {Data: []byte(`
apiVersion: goal/v1
kind: Factory
metadata: { id: 00000000-0000-4000-8000-000000000002, name: merger }
spec:
  runtime: { modelConnection: claude` + repository + ` }
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
		{"human station without a repository", "", "{ id: merge, kind: human, action: merge }", ""},
		{"gate station", repo, "{ id: merge, kind: agent, agent: worker, gate: { onReject: work }, action: merge }", ""},
		{"agent station without a gate", repo, "{ id: merge, kind: agent, agent: worker, action: merge }", checkSchema},
		{"queue station", repo, "{ id: merge, kind: queue, action: merge }", checkSchema},
		{"done station", repo, "{ id: merge, kind: done, action: merge }", checkSchema},
		{"unknown action on a gate station", repo, "{ id: merge, kind: agent, agent: worker, gate: { onReject: work }, action: deploy }", checkSchema},
		{"unknown action", repo, "{ id: merge, kind: human, action: deploy }", checkSchema},
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
  runtime: { modelConnection: claude }
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

// The keys the builder writes are optional additions: a definition using all of them loads.
func TestLoad_AcceptsTheBuilderKeys(t *testing.T) {
	t.Parallel()

	f, err := Load(checkTree(edited(
		"  name: acme-check\n", "  name: acme-check\n  purpose: Answers acme's support questions.\n",
		runtimeModel, runtimeModel+"    size: small\n    tools: [\"go@1.26.10\"]\n"+
			"    repository: { url: \"https://github.com/acme/app\", connection: github }\n"+
			"    budget: { weeklyUsd: 50, onExhausted: stop, perItemUsd: 2 }\n",
		specAgents, "  connections: [github, playwright]\n  knowledge: { sources: [handbook] }\n"+specAgents,
		agentPrompt, agentPrompt+"      maxTurns: 30\n      connections: [playwright]\n      builtins: { read: allowed, run: allowed, subagents: off }\n",
		"      - id: work\n", "      - id: work\n        name: Work\n        description: Where the worker answers.\n        checks: [schema, make test]\n",
	), nil))
	if err != nil {
		t.Fatal(err)
	}

	r := f.Spec.Runtime
	if r.ModelConnection != "claude" || r.Size != "small" || !slices.Equal(r.Tools, []string{"go@1.26.10"}) ||
		*r.Repository != (Repository{URL: "https://github.com/acme/app", Connection: "github"}) {
		t.Errorf("runtime = %+v, want its model connection, size, tools and repository", r)
	}

	if f.Spec.Knowledge == nil || !slices.Equal(f.Spec.Knowledge.Sources, []string{"handbook"}) {
		t.Errorf("knowledge = %+v, want the handbook source", f.Spec.Knowledge)
	}

	if b := f.Spec.Runtime.Budget; b.WeeklyUSD != 50 || b.OnExhausted != "stop" || f.Metadata.Purpose == "" {
		t.Errorf("budget = %+v, purpose %q", b, f.Metadata.Purpose)
	}
}

// Since the cutover to connections each form they replaced is an error, so no draft that has one
// publishes; a version published before still loads, so its items stay visible, and its line
// names the forms, so the engine refuses to run it saying what to fix.
func TestLoad_RemovedFormsAreErrorsAndTheirVersionsDoNotRun(t *testing.T) {
	t.Parallel()

	f, problems := Check(os.DirFS("testdata/.legacy"))
	if f == nil {
		t.Fatalf("problems = %+v, want the factory decoded", problems)
	}

	counts := map[string]int{}

	for _, p := range problems {
		removed := p.Code == codeFieldRemoved || p.Code == codeEnvReferenceRemoved || p.Code == codeInlineRemoved
		if removed != p.Blocking() {
			t.Errorf("problem %s is %s; want the removed forms errors and nothing else blocking", p.Code, p.Severity)
		}

		counts[p.Code]++
	}

	want := map[string]int{codeFieldRemoved: 5, codeEnvReferenceRemoved: 1, codeInlineRemoved: 3}
	for code, n := range want {
		if counts[code] != n {
			t.Errorf("%d %s problems, want %d (all: %v)", counts[code], code, n, counts)
		}
	}

	loaded, err := Load(os.DirFS("testdata/.legacy"))
	if err != nil {
		t.Fatalf("Load = %v; want a published version with removed forms to load", err)
	}

	line := loaded.Line()
	if len(line.Removed) != 9 || !slices.Contains(line.Removed, "spec.runtime.repository.token (env-reference-removed)") ||
		!strings.Contains(line.Refusal(), "publish a version with connections") {
		t.Errorf("line removed %q, refusal %q; want the nine forms named and the fix", line.Removed, line.Refusal())
	}
}

// A vendored skill records its skills.sh origin pinned at a full commit; anything else there is
// a schema problem.
func TestLoad_SkillOriginIsAPinnedSkillsShReference(t *testing.T) {
	t.Parallel()

	commit := strings.Repeat("0a", 20)
	skill := func(origin string) fstest.MapFS {
		return checkTree(edited(specAgents, "  skills:\n    - { name: lint, source: ./skills/lint, origin: \""+origin+"\" }\n"+specAgents),
			map[string]string{"skills/lint/SKILL.md": "# lint\n"})
	}

	f, err := Load(skill("skills.sh:acme/skills@lint#" + commit))
	if err != nil {
		t.Fatal(err)
	}

	if got := f.Spec.Skills[0]; got.Origin != "skills.sh:acme/skills@lint#"+commit || !got.Local() {
		t.Errorf("skill = %+v, want the origin kept beside the local source", got)
	}

	for _, origin := range []string{
		"skills.sh:acme/skills@lint", "skills.sh:acme/skills@lint#abc123", "skills.sh:acme/skills@lint#" + strings.ToUpper(commit),
		"github:acme/skills@lint#" + commit, "skills.sh:acme/skills/lint#" + commit,
	} {
		_, problems := Check(skill(origin))
		if !slices.ContainsFunc(problems, func(p domain.DefinitionProblem) bool { return p.Check == checkSchema && p.Blocking() }) {
			t.Errorf("origin %q: problems %+v, want a schema problem", origin, problems)
		}
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

// The organization's connections a definition names: spec.connections but its inline entries,
// the model connection and the repository's, once each.
func TestFactory_ConnectionNames(t *testing.T) {
	t.Parallel()

	f := &Factory{}
	f.Spec.Connections = []Connection{{Name: "repos"}, {Name: "docs", inline: true}, {Name: "datadog"}}
	f.Spec.Runtime.ModelConnection = "model"
	f.Spec.Runtime.Repository = &Repository{URL: "https://code.example.com/acme/goal", Connection: "repos"}

	if got := f.ConnectionNames(); !slices.Equal(got, []string{"datadog", "model", "repos"}) {
		t.Errorf("ConnectionNames = %v; want datadog, model and repos", got)
	}
}
