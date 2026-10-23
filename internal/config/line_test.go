package config

import (
	"reflect"
	"testing"

	"github.com/byFrederick/factory/internal/domain"
)

func TestLine_CarriesWhatTheEngineNeeds(t *testing.T) {
	t.Parallel()

	f, err := Load("testdata/line")
	if err != nil {
		t.Fatal(err)
	}

	l := f.Line()

	if l.FactoryID != "0b6f1c9e-5d0a-4c39-9a51-6f1f3f0e2a10" {
		t.Errorf("factory id = %s, want metadata.id", l.FactoryID)
	}

	wantRuntime := domain.Runtime{Image: "ghcr.io/acme/agent:1.0.0", Model: "claude-sonnet-5-5", MaxTurnsPerTask: 50, PerItemUSD: 2.5}
	if l.Runtime != wantRuntime {
		t.Errorf("runtime = %+v, want %+v", l.Runtime, wantRuntime)
	}

	wantStations := []domain.Station{
		{ID: "inbox", Kind: domain.KindQueue},
		{ID: "spec", Kind: domain.KindAgent, Agent: "architect", Agents: 1, WIP: 2, Output: "spec"},
		{ID: "approve-spec", Kind: domain.KindHuman, WIP: 3},
		{ID: "build", Kind: domain.KindAgent, Route: map[string]string{"backend": "engineer", "frontend": "engineer"}, Agents: 2, WIP: 2},
		{ID: "review", Kind: domain.KindAgent, Agent: "reviewer", Gate: &domain.Gate{OnReject: "build", MaxRework: 2}},
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
