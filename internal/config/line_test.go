package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
)

func TestLine_CarriesWhatTheEngineNeeds(t *testing.T) {
	t.Parallel()

	f, err := LoadDir("testdata/line")
	if err != nil {
		t.Fatal(err)
	}

	l := f.Line()

	if l.FactoryID != "0b6f1c9e-5d0a-4c39-9a51-6f1f3f0e2a10" {
		t.Errorf("factory id = %s, want metadata.id", l.FactoryID)
	}

	wantRuntime := domain.Runtime{
		Image: "ghcr.io/acme/agent:1.0.0", Model: "claude-sonnet-5-5", MaxTurnsPerTask: 50, PerItemUSD: 2.5,
		Repository: domain.Repository{URL: "https://git.example.com/acme/line.git", AuthRef: "env:ACME_GIT_TOKEN"},
	}
	if l.Runtime != wantRuntime {
		t.Errorf("runtime = %+v, want %+v", l.Runtime, wantRuntime)
	}

	wantStations := []domain.Station{
		{ID: "inbox", Kind: domain.KindQueue},
		{ID: "spec", Kind: domain.KindAgent, Agent: "architect", Agents: 1, WIP: 2, Output: "spec"},
		{ID: "approve-spec", Kind: domain.KindHuman, WIP: 3},
		{ID: "build", Kind: domain.KindAgent, Route: map[string]string{"backend": "engineer", "frontend": "engineer"}, Agents: 2, WIP: 2, Checks: []string{"make test"}},
		{ID: "review", Kind: domain.KindAgent, Agent: "reviewer", Gate: &domain.Gate{OnReject: "build", MaxRework: 2}},
		{ID: "merge", Kind: domain.KindHuman, Action: domain.ActionMerge},
		{ID: "done", Kind: domain.KindDone},
	}
	if !reflect.DeepEqual(l.Stations, wantStations) {
		t.Errorf("stations =\n%+v\nwant\n%+v", l.Stations, wantStations)
	}

	wantAgents := []domain.AgentSpec{
		{
			Name: "architect", PromptPath: "./agents/architect.md", Model: "claude-opus-5-5",
			Skills:      []domain.SkillRef{{Name: "acme-conventions", Source: "./skills/acme-conventions"}},
			Connections: []string{"knowledge"}, ToolsAllow: []string{"Read", "Grep"},
		},
		{
			Name: "engineer", PromptPath: "./agents/engineer.md",
			Skills: []domain.SkillRef{
				{Name: "acme-conventions", Source: "./skills/acme-conventions"},
				{Name: "golang-testing", Source: "skills.sh:samber/cc-skills-golang@golang-testing"},
			},
			ToolsAllow: []string{"Read", "Edit", "Bash"}, ToolsDeny: []string{"Bash(git push*)"},
		},
		{Name: "reviewer", PromptPath: "./agents/reviewer.md"},
	}
	if !reflect.DeepEqual(l.Agents, wantAgents) {
		t.Errorf("agents =\n%+v\nwant\n%+v", l.Agents, wantAgents)
	}

	if !reflect.DeepEqual(l.ExpediteTypes, []string{"incident"}) {
		t.Errorf("expedite types = %v, want [incident]", l.ExpediteTypes)
	}
}

// The builder's keys reach the engine: built-in tool groups become the worker's rules, agent
// caps ride along, and check shorthand runs as commands.
func TestLine_MapsBuiltinsCapsAndCheckShorthand(t *testing.T) {
	t.Parallel()

	f, err := Load(checkTree(edited(
		agentPrompt, agentPrompt+`      maxTurns: 40
      perItemUsd: 1.5
      builtins:
        read: allowed
        edit: allowed
        run: { mode: allowed, commands: [pnpm, go] }
        web: ask
`,
		doneStation, "      - id: build\n        kind: agent\n        agent: worker\n        checks: [schema, go test ./..., { kind: command, run: make lint }]\n"+doneStation,
	), nil))
	if err != nil {
		t.Fatal(err)
	}

	l := f.Line()

	a := l.Agents[0]
	if a.MaxTurns != 40 || a.PerItemUSD != 1.5 {
		t.Errorf("caps = %d turns, %v USD; want 40, 1.5", a.MaxTurns, a.PerItemUSD)
	}

	const wantAllow, wantDeny = "Read Grep Glob Edit Write Bash(pnpm:*) Bash(go:*)", "WebFetch WebSearch Task"

	if strings.Join(a.ToolsAllow, " ") != wantAllow || strings.Join(a.ToolsDeny, " ") != wantDeny {
		t.Errorf("allow %q deny %q\nwant allow %q deny %q", a.ToolsAllow, a.ToolsDeny, wantAllow, wantDeny)
	}

	if got := l.Stations[2].Checks; !reflect.DeepEqual(got, []string{"go test ./...", "make lint"}) {
		t.Errorf("checks = %q, want the two command lines", got)
	}
}

// Every group off leaves the agent no built-in tool: the worker's read-only default is denied too.
func TestLine_BuiltinsAllOffDenyEveryTool(t *testing.T) {
	t.Parallel()

	f, err := Load(checkTree(edited(agentPrompt, agentPrompt+"      builtins: { read: off }\n"), nil))
	if err != nil {
		t.Fatal(err)
	}

	a := f.Line().Agents[0]

	const want = "Read Grep Glob Edit Write WebFetch WebSearch Task Bash"

	if len(a.ToolsAllow) != 0 || strings.Join(a.ToolsDeny, " ") != want {
		t.Errorf("allow %q deny %q, want nothing allowed and %q denied", a.ToolsAllow, a.ToolsDeny, want)
	}
}
