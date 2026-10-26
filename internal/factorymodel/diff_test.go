package factorymodel

import (
	"maps"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/byFrederick/goal/internal/domain"
)

// v8 is the version the Review and publish board compares against.
const v8 = `apiVersion: goal/v1
kind: Factory
metadata: { id: 0b6f1c9e-5d0a-4c39-9a51-6f1f3f0e2a10, name: acme-web }
spec:
  runtime: { runner: docker, image: ghcr.io/acme/agent:1.0.0, auth: anthropic-api-key, repository: { url: "https://code.example.com/acme/web.git" } }
  knowledgeBase: { collections: [notes] }
  connections:
    - connector: playwright
      name: browser
      agents: [reviewer]
  agents:
    - { name: frontend-engineer, prompt: ./agents/frontend-engineer.md }
    - { name: reviewer, prompt: ./agents/reviewer.md }
  board:
    stations:
      - { id: inbox, kind: queue }
      - id: build
        kind: agent
        agent: frontend-engineer
        wip: 3
        checks: [schema, "go test ./...", "goal validate"]
      - { id: review, kind: agent, agent: reviewer }
      - { id: merge, kind: human, wip: 4 }
      - { id: done, kind: done }
`

func snapshot(files map[string]string) Snapshot {
	m := domain.Manifest{}
	fsys := fstest.MapFS{}

	for p, body := range files {
		m[p] = domain.HashBlob([]byte(body))
		fsys[p] = &fstest.MapFile{Data: []byte(body)}
	}

	return Snapshot{Manifest: m, Files: fsys}
}

func prompts(extra map[string]string) map[string]string {
	out := map[string]string{
		"agents/frontend-engineer.md": "Build the web UI.\n",
		"agents/reviewer.md":          "Review the change.\n",
	}

	maps.Copy(out, extra)

	return out
}

func with(doc string, files map[string]string) Snapshot {
	files = prompts(files)
	files[factoryFile] = doc

	return snapshot(files)
}

func summaries(c Comparison) []string {
	out := make([]string, 0, len(c.Changes))
	for _, ch := range c.Changes {
		out = append(out, ch.Section+": "+ch.Summary)
	}

	return out
}

// The board's example: v9 adds pnpm test to Build, makes Merge a merge step and gives the browser
// connection another agent and two write tools: 3 changes in 2 sections.
func TestDiff_BoardExample(t *testing.T) {
	t.Parallel()

	v9 := strings.NewReplacer(
		`"go test ./...", "goal validate"`, `"go test ./...", "pnpm test", "goal validate"`,
		"{ id: merge, kind: human, wip: 4 }", "{ id: merge, kind: human, wip: 4, action: merge }",
		"      agents: [reviewer]\n", "      agents: [frontend-engineer, reviewer]\n      tools:\n"+
			"        browser__click: { access: write, scope: preview }\n        browser__fill_form: { access: write }\n",
	).Replace(v8)

	c := Diff(with(v8, nil), with(v9, nil))

	want := []string{
		"line: Build also runs pnpm test",
		"line: Merge becomes a merge step",
		"connections: browser gets frontend-engineer and two write tools",
	}

	if got := summaries(c); !reflect.DeepEqual(got, want) || c.Counts != (Counts{Changes: 3, Sections: 2}) {
		t.Errorf("changes = %q, counts %+v; want %q in 2 sections", got, c.Counts, want)
	}

	build := c.Changes[0]
	if build.Field != "checks" || build.Target != (Target{Kind: TargetStation, ID: "build"}) || build.ToLines != [2]int{20, 24} {
		t.Errorf("build change = %+v", build)
	}
}

// Stations compare by identity: moving one is one move, not an edit of every station after it.
func TestDiff_ReorderIsOneMove(t *testing.T) {
	t.Parallel()

	moved := strings.Replace(strings.Replace(v8, "      - { id: review, kind: agent, agent: reviewer }\n", "", 1),
		"      - { id: inbox, kind: queue }\n", "      - { id: inbox, kind: queue }\n      - { id: review, kind: agent, agent: reviewer }\n", 1)

	c := Diff(with(v8, nil), with(moved, nil))

	if got := summaries(c); !reflect.DeepEqual(got, []string{"line: Review moves after Inbox"}) || c.Changes[0].Op != Moved {
		t.Errorf("changes = %q, want one move", got)
	}
}

// New and removed items, and section fields, read as sentences.
func TestDiff_AddedRemovedAndFields(t *testing.T) {
	t.Parallel()

	newer := strings.NewReplacer(
		"name: acme-web }", "name: acme-web, purpose: Builds the acme web shop. }",
		"      - { id: review, kind: agent, agent: reviewer }\n", "      - { id: approve-build, kind: human }\n",
		"    - { name: reviewer, prompt: ./agents/reviewer.md }\n", "",
		"      agents: [reviewer]\n", "      agents: [frontend-engineer]\n",
	).Replace(v8)

	want := []string{
		"basics: Purpose set to Builds the acme web shop.",
		"line: Station Review removed",
		"line: New human station Approve build",
		"agents: Agent reviewer removed",
		"connections: browser gains agent frontend-engineer and loses agent reviewer",
	}

	if got := summaries(Diff(with(v8, nil), with(newer, nil))); !reflect.DeepEqual(got, want) {
		t.Errorf("changes =\n%q\nwant\n%q", got, want)
	}
}

// Files map to what they belong to: a prompt to its agent, a contract to the line, anything
// else, and YAML-only settings, to files.
func TestDiff_FilesMapToTheirTargets(t *testing.T) {
	t.Parallel()

	withContract := strings.Replace(v8, "  agents:\n", "  contracts: { spec: ./contracts/spec.json }\n  agents:\n", 1)
	newer := strings.Replace(withContract, "name: acme-web }", "name: acme-web, owner: web@example.com }", 1)

	from := with(withContract, map[string]string{"contracts/spec.json": `{"type": "object"}`})
	to := with(newer, map[string]string{
		"agents/reviewer.md":   "Review the change and its tests.\n",
		"contracts/spec.json":  `{"type": "object", "required": ["title"]}`,
		"evals/cases/one.yaml": "input: x\n",
	})

	want := []string{
		"agents: Prompt of reviewer changed",
		"files: Settings edited in YAML change: metadata.owner",
		"line: Contract spec changed",
		"files: evals/cases/one.yaml added",
	}

	c := Diff(from, to)
	if got := summaries(c); !reflect.DeepEqual(got, want) || c.Counts != (Counts{Changes: 4, Sections: 3}) {
		t.Errorf("changes = %q, counts %+v\nwant %q", got, c.Counts, want)
	}

	if f := c.Files; len(f) != 5 || f[0].Path != "agents/frontend-engineer.md" || f[0].Op != FileUnchanged || f[3].Op != FileAdded {
		t.Errorf("files = %+v", f)
	}
}

// The editor marks the fields a draft changed, by the draft's pointers, with the base's value.
func TestEdited_PointsAtChangedFields(t *testing.T) {
	t.Parallel()

	base, _, err := FromYAML([]byte(v8), nil)
	if err != nil {
		t.Fatal(err)
	}

	draftDoc := strings.NewReplacer(
		"        wip: 3\n", "",
		`"go test ./...", "goal validate"`, `"go test ./...", "pnpm test", "goal validate"`,
		"{ id: merge, kind: human, wip: 4 }", "{ id: merge, kind: human, wip: 4, action: merge }\n      - { id: qa, kind: human }",
	).Replace(v8)

	draft, _, err := FromYAML([]byte(draftDoc), nil)
	if err != nil {
		t.Fatal(err)
	}

	got := Edited(base, draft, Line)
	want := map[string]any{
		"/stations/1/wip":    dig(base.Line, "stations", 1, "wip"),
		"/stations/1/checks": dig(base.Line, "stations", 1, "checks"),
		"/stations/3/action": nil,
		"/stations/4":        nil,
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("edited = %v, want %v", got, want)
	}
}

// Rules compare by name like skills: each added, removed or changed rule is its own change.
func TestDiff_Rules(t *testing.T) {
	t.Parallel()

	rules := func(list string) string {
		return strings.Replace(v8, "  agents:\n", "  rules:\n"+list+"  agents:\n", 1)
	}

	older := rules("    - { name: secrets, source: ./rules/secrets.md }\n    - { name: tests, source: ./rules/tests.md }\n")
	newer := rules("    - { name: tests, source: ./rules/testing.md }\n    - { name: style, source: ./rules/style.md }\n")

	got := map[Target]ChangeOp{}

	for _, ch := range Diff(with(older, nil), with(newer, nil)).Changes {
		if ch.Section != string(Agents) {
			t.Errorf("change %+v is not in the agents section", ch)
		}

		got[ch.Target] = ch.Op
	}

	want := map[Target]ChangeOp{
		{Kind: TargetRule, ID: "secrets"}: Removed,
		{Kind: TargetRule, ID: "tests"}:   Changed,
		{Kind: TargetRule, ID: "style"}:   Added,
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("changes = %v, want %v", got, want)
	}
}
