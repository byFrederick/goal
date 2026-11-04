package config

import (
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// defaultIntegration is board.flow.assemblies.integration when a definition leaves it out.
var defaultIntegration = domain.IntegrationPolicy{Mode: domain.IntegrationModeTrunk, Refresh: domain.RefreshOnLand}

// The line carries the flow policy and targets the engine and the flow metrics read.
func TestLine_CarriesFlowPolicyAndTargets(t *testing.T) {
	t.Parallel()

	f, err := LoadDir("testdata/.support")
	if err != nil {
		t.Fatal(err)
	}

	l := f.Line()
	want := domain.FlowPolicy{
		Release: domain.ReleaseDBR, Constraint: "respond", ExpediteWIP: 1,
		Andon:   &domain.AndonPolicy{ReworkRate: 0.5, Window: 10},
		Targets: domain.Targets{ThroughputPerDay: 8, LeadTime: 6 * time.Hour, ExpediteLeadTime: time.Hour},
		Assemblies: domain.AssemblyPolicy{
			FreshBase: domain.FreshBaseRebase, OnConflict: domain.ConflictRework, Overlap: domain.OverlapSerialize,
			Materialize: true, Integration: defaultIntegration,
		},
		Retry: domain.RetryPolicy{TransientAttempts: domain.DefaultTransientAttempts},
	}

	if !reflect.DeepEqual(l.Flow, want) {
		t.Errorf("flow = %+v, want %+v", l.Flow, want)
	}

	if respond, _ := l.Station("respond"); respond.Buffer != 2 {
		t.Errorf("respond buffer = %d, want 2", respond.Buffer)
	}

	if got := l.ItemTarget(domain.ClassExpedite); got != time.Hour {
		t.Errorf("expedite target = %v, want 1h", got)
	}

	plain, err := LoadDir("testdata/.line")
	if err != nil {
		t.Fatal(err)
	}

	if p := plain.Line().Flow; p.Release != domain.ReleasePush || p.Targets != (domain.Targets{}) {
		t.Errorf("flow without release or targets = %+v, want push and no targets", p)
	}

	set := domain.AssemblyPolicy{
		FreshBase: domain.FreshBaseMerge, OnConflict: domain.ConflictHuman, Overlap: domain.OverlapSerialize, Materialize: true,
		Integration: defaultIntegration,
	}
	if got := plain.Line().Flow.Assemblies; got != set {
		t.Errorf("assemblies = %+v, want %+v", got, set)
	}

	// A key left out takes its default.
	half, err := Load(checkTree(baseFactory+"    flow:\n      assemblies: { onConflict: human }\n", nil))
	if err != nil {
		t.Fatal(err)
	}

	if got, want := half.Line().Flow.Assemblies, (domain.AssemblyPolicy{
		FreshBase: domain.FreshBaseRebase, OnConflict: domain.ConflictHuman, Overlap: domain.OverlapSerialize,
		Materialize: true, Integration: defaultIntegration,
	}); got != want {
		t.Errorf("assemblies with only onConflict = %+v, want %+v", got, want)
	}
}

// flow.assemblies takes only its settings, each one of its values.
func TestLoad_RefusesUnknownAssemblyPolicies(t *testing.T) {
	t.Parallel()

	for _, assemblies := range []string{
		"{ freshBase: squash }", "{ onConflict: retry }", "{ parts: 3 }", "{ materialize: yes please }", "{ materialize: 1 }",
		"{ changedSince: yes please }",
	} {
		doc := baseFactory + "    flow:\n      assemblies: " + assemblies + "\n"

		_, problems := Check(checkTree(doc, nil))
		if !slices.ContainsFunc(problems, func(p domain.DefinitionProblem) bool { return p.Check == checkSchema && p.Blocking() }) {
			t.Errorf("assemblies %s: problems %+v, want a schema problem", assemblies, problems)
		}
	}
}

// board.flow.assemblies.integration maps field by field; left out, it is trunk, onLand, no
// minimum and no landAt.
func TestLine_CarriesAssembliesIntegration(t *testing.T) {
	t.Parallel()

	trunk := defaultIntegration
	merge := strings.Replace(baseFactory, doneStation, "      - id: land\n        kind: agent\n        agent: worker\n"+
		"        gate: { onReject: work }\n        action: merge\n"+doneStation, 1)

	for flow, want := range map[string]domain.IntegrationPolicy{
		"": trunk,
		"    flow:\n      assemblies: { overlap: allow }\n":  trunk,
		"    flow:\n      assemblies: { integration: {} }\n": trunk,
		"    flow:\n      assemblies:\n        integration: { mode: branch, refresh: eachPart, minParts: 3, landAt: land }\n": {
			Mode: domain.IntegrationModeBranch, Refresh: domain.RefreshEachPart, MinParts: 3, LandAt: "land",
		},
		"    flow:\n      assemblies:\n        integration: { minParts: 2 }\n": {
			Mode: domain.IntegrationModeTrunk, Refresh: domain.RefreshOnLand, MinParts: 2,
		},
	} {
		f, err := Load(checkTree(merge+flow, nil))
		if err != nil {
			t.Fatalf("flow %q: %v", flow, err)
		}

		if got := f.Line().Flow.Assemblies.Integration; got != want {
			t.Errorf("flow %q: integration = %+v, want %+v", flow, got, want)
		}
	}
}

// flow.assemblies.integration takes only its keys, each one of its values.
func TestLoad_RefusesUnknownIntegrationKeys(t *testing.T) {
	t.Parallel()

	for _, integration := range []string{
		"{ colour: red }", "{ mode: fork }", "{ mode: '' }", "{ refresh: always }", "{ minParts: -1 }", "{ minParts: 1.5 }",
		"{ landAt: 3 }", "branch",
	} {
		doc := baseFactory + "    flow:\n      assemblies:\n        integration: " + integration + "\n"

		_, problems := Check(checkTree(doc, nil))
		if !slices.ContainsFunc(problems, func(p domain.DefinitionProblem) bool { return p.Check == checkSchema && p.Blocking() }) {
			t.Errorf("integration %s: problems %+v, want a schema problem", integration, problems)
		}
	}
}

// board.flow.dependencies.after names the output property whose item refs the work must follow;
// unset, no property is read. dependencies takes only its keys, each a property name.
func TestLine_CarriesDependenciesAfter(t *testing.T) {
	t.Parallel()

	for flow, want := range map[string]string{
		"":                                    "",
		"    flow:\n      dependencies: {}\n": "",
		"    flow:\n      dependencies: { after: follows }\n": "follows",
	} {
		f, err := Load(checkTree(baseFactory+flow, nil))
		if err != nil {
			t.Fatal(err)
		}

		if got := f.Line().Flow.Dependencies.After; got != want {
			t.Errorf("flow %q: dependencies.after = %q, want %q", flow, got, want)
		}
	}

	for _, deps := range []string{"{ before: follows }", "{ after: 3 }", "{ after: \"\" }"} {
		doc := baseFactory + "    flow:\n      dependencies: " + deps + "\n"

		_, problems := Check(checkTree(doc, nil))
		if !slices.ContainsFunc(problems, func(p domain.DefinitionProblem) bool { return p.Check == checkSchema && p.Blocking() }) {
			t.Errorf("dependencies %s: problems %+v, want a schema problem", deps, problems)
		}
	}
}

// board.flow.assemblies.materialize turns a station's plan into an assembly with its parts; it
// is on unless a factory sets it to false.
func TestLine_CarriesAssembliesMaterialize(t *testing.T) {
	t.Parallel()

	for flow, want := range map[string]bool{
		"": true,
		"    flow:\n      assemblies: { overlap: allow }\n":     true,
		"    flow:\n      assemblies: { materialize: true }\n":  true,
		"    flow:\n      assemblies: { materialize: false }\n": false,
	} {
		f, err := Load(checkTree(baseFactory+flow, nil))
		if err != nil {
			t.Fatal(err)
		}

		if got := f.Line().Flow.Assemblies.Materialize; got != want {
			t.Errorf("flow %q: materialize = %v, want %v", flow, got, want)
		}
	}
}

// board.flow.assemblies.changedSince records base commits and gives later claims a Changed
// since your plan section; it is off unless a factory sets it to true.
func TestLine_CarriesAssembliesChangedSince(t *testing.T) {
	t.Parallel()

	for flow, want := range map[string]bool{
		"": false,
		"    flow:\n      assemblies: { overlap: allow }\n":      false,
		"    flow:\n      assemblies: { changedSince: true }\n":  true,
		"    flow:\n      assemblies: { changedSince: false }\n": false,
	} {
		f, err := Load(checkTree(baseFactory+flow, nil))
		if err != nil {
			t.Fatal(err)
		}

		if got := f.Line().Flow.Assemblies.ChangedSince; got != want {
			t.Errorf("flow %q: changedSince = %v, want %v", flow, got, want)
		}
	}
}

// The purpose agents read is metadata.purpose, or the older description when that is all a
// definition has.
func TestLine_PurposeFallsBackToDescription(t *testing.T) {
	t.Parallel()

	for doc, want := range map[string]string{
		edited("  name: acme-check\n", "  name: acme-check\n  purpose: Checks acme.\n  description: Old words.\n"): "Checks acme.",
		edited("  name: acme-check\n", "  name: acme-check\n  description: Old words.\n"):                          "Old words.",
		baseFactory: "",
	} {
		f, err := Load(checkTree(doc, nil))
		if err != nil {
			t.Fatal(err)
		}

		if got := f.Line().Purpose; got != want {
			t.Errorf("purpose = %q, want %q", got, want)
		}
	}
}

// runtime.budget's weekly cap and policy reach the line the engine enforces; drain is the
// default and dailyUsd stays display-only.
func TestLine_CarriesTheVersionsRules(t *testing.T) {
	t.Parallel()

	f, err := LoadDir("testdata/.line")
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	for _, r := range f.Line().Rules {
		names = append(names, r.Name)
	}

	if want := []string{"secrets", "go-errors"}; !slices.Equal(names, want) {
		t.Errorf("line rules = %v, want the declared %v in order", names, want)
	}

	if r := f.Line().Rules[1]; r.Description == "" || r.Body == "" || len(r.AppliesTo.Agents) == 0 || len(r.Paths) == 0 {
		t.Errorf("line rule %s = %+v, want its description, body, scope and paths", r.Name, r)
	}
}

func TestLine_WeeklyBudgetMapsToTheRuntime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		budget     string
		wantWeekly float64
		wantPolicy domain.BudgetPolicy
	}{
		{name: "stop", budget: "{ weeklyUsd: 60, onExhausted: stop }", wantWeekly: 60, wantPolicy: domain.BudgetStop},
		{name: "drain by default", budget: "{ weeklyUsd: 25.5, dailyUsd: 3 }", wantWeekly: 25.5, wantPolicy: domain.BudgetDrain},
		{name: "no weekly cap", budget: "{ dailyUsd: 10, perItemUsd: 1 }", wantPolicy: domain.BudgetDrain},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f, err := Load(checkTree(edited(runtimeModel, runtimeModel+"    budget: "+tc.budget+"\n"), nil))
			if err != nil {
				t.Fatal(err)
			}

			b := f.Line().Runtime.WeeklyBudget(0, time.Time{})
			if b.WeeklyUSD != tc.wantWeekly || b.Policy != tc.wantPolicy {
				t.Errorf("weekly budget = $%v %s, want $%v %s", b.WeeklyUSD, b.Policy, tc.wantWeekly, tc.wantPolicy)
			}
		})
	}
}

// A target is a positive number; zero or less is no target and is refused.
// board.flow.assemblies.overlap says whether goald holds items whose files overlap; it
// serializes them unless a factory allows it.
func TestLine_CarriesAssembliesOverlapPolicy(t *testing.T) {
	t.Parallel()

	for doc, want := range map[string]domain.OverlapPolicy{
		baseFactory: domain.OverlapSerialize,
		baseFactory + "    flow:\n      release: push\n":                      domain.OverlapSerialize,
		baseFactory + "    flow:\n      assemblies: {}\n":                     domain.OverlapSerialize,
		baseFactory + "    flow:\n      assemblies: { overlap: serialize }\n": domain.OverlapSerialize,
		baseFactory + "    flow:\n      assemblies: { overlap: allow }\n":     domain.OverlapAllow,
	} {
		f, err := Load(checkTree(doc, nil))
		if err != nil {
			t.Fatalf("load %q: %v", doc, err)
		}

		if got := f.Line().Flow.Assemblies.Overlap; got != want {
			t.Errorf("overlap of %q = %q, want %q", doc, got, want)
		}
	}
}

// board.flow.retry.transient.attempts caps the runs of a claim whose sandbox is lost; a
// definition without it, as every one published before the key, keeps 3.
func TestLine_CarriesTransientRetryAttempts(t *testing.T) {
	t.Parallel()

	for doc, want := range map[string]int{
		baseFactory: 3,
		baseFactory + "    flow:\n      release: push\n":                         3,
		baseFactory + "    flow:\n      retry: {}\n":                             3,
		baseFactory + "    flow:\n      retry: { transient: {} }\n":              3,
		baseFactory + "    flow:\n      retry: { transient: { attempts: 1 } }\n": 1,
		baseFactory + "    flow:\n      retry: { transient: { attempts: 5 } }\n": 5,
	} {
		f, err := Load(checkTree(doc, nil))
		if err != nil {
			t.Fatalf("load %q: %v", doc, err)
		}

		if got := f.Line().Flow.Retry.TransientAttempts; got != want {
			t.Errorf("transient attempts of %q = %d, want %d", doc, got, want)
		}
	}
}

// goald refuses a transient retry cap below one, a non-integer and unknown keys.
func TestLoad_RefusesTransientAttemptsBelowOne(t *testing.T) {
	t.Parallel()

	for _, retry := range []string{
		"{ transient: { attempts: 0 } }", "{ transient: { attempts: -1 } }", "{ transient: { attempts: 1.5 } }",
		"{ transient: { attempts: three } }", "{ transient: { backoff: 15s } }", "{ contract: 2 }", "{ transient: 3 }",
	} {
		doc := baseFactory + "    flow:\n      retry: " + retry + "\n"

		_, problems := Check(checkTree(doc, nil))
		if !slices.ContainsFunc(problems, func(p domain.DefinitionProblem) bool { return p.Check == checkSchema && p.Blocking() }) {
			t.Errorf("retry %s: problems %+v, want a schema problem", retry, problems)
		}

		if _, err := Load(checkTree(doc, nil)); err == nil {
			t.Errorf("retry %s loaded, want it refused", retry)
		}
	}
}

func TestLoad_RefusesUnknownOverlapPolicy(t *testing.T) {
	t.Parallel()

	for _, assemblies := range []string{"{ overlap: parallel }", "{ overlap: '' }", "{ overlap: 1 }", "{ wip: 2 }", "allow"} {
		doc := baseFactory + "    flow:\n      assemblies: " + assemblies + "\n"

		_, problems := Check(checkTree(doc, nil))
		if !slices.ContainsFunc(problems, func(p domain.DefinitionProblem) bool { return p.Check == checkSchema && p.Blocking() }) {
			t.Errorf("assemblies %s: problems %+v, want a schema problem", assemblies, problems)
		}
	}
}

func TestLoad_RefusesTargetsThatAreNotPositive(t *testing.T) {
	t.Parallel()

	for _, targets := range []string{
		"{ throughputPerDay: 0 }", "{ throughputPerDay: -2 }", "{ leadTimeHours: 0 }",
		"{ expediteLeadTimeMinutes: 0 }", "{ expediteLeadTimeMinutes: 1.5 }", "{ cycleTime: 3 }",
	} {
		doc := baseFactory + "    flow:\n      targets: " + targets + "\n"

		_, problems := Check(checkTree(doc, nil))
		if !slices.ContainsFunc(problems, func(p domain.DefinitionProblem) bool { return p.Check == checkSchema && p.Blocking() }) {
			t.Errorf("targets %s: problems %+v, want a schema problem", targets, problems)
		}
	}

	ok := baseFactory + "    flow:\n      targets: { throughputPerDay: 0.5, leadTimeHours: 6, expediteLeadTimeMinutes: 30 }\n"
	if _, err := Load(checkTree(ok, nil)); err != nil {
		t.Errorf("valid targets refused: %v", err)
	}
}

func TestLine_CarriesWhatTheEngineNeeds(t *testing.T) {
	t.Parallel()

	f, err := LoadDir("testdata/.line")
	if err != nil {
		t.Fatal(err)
	}

	l := f.Line()

	if l.FactoryID != "0b6f1c9e-5d0a-4c39-9a51-6f1f3f0e2a10" {
		t.Errorf("factory id = %s, want metadata.id", l.FactoryID)
	}

	wantRuntime := domain.Runtime{
		Model: "claude-sonnet-5-5", ModelConnection: "claude", Size: domain.SandboxLarge, Tools: []string{"go@1.26.10", "task@3.46.1"},
		MaxTurnsPerTask: 50, PerItemUSD: 2.5,
		Repository: domain.Repository{URL: "https://git.example.com/acme/line.git", Connection: "code"},
	}
	if !reflect.DeepEqual(l.Runtime, wantRuntime) {
		t.Errorf("runtime = %+v, want %+v", l.Runtime, wantRuntime)
	}

	if !slices.Equal(l.Connections, []string{"code", "docs"}) || !reflect.DeepEqual(l.Knowledge, []domain.KnowledgeEntry{{Name: "notes"}}) {
		t.Errorf("connections %v, knowledge %+v; want [code docs] and notes for every run", l.Connections, l.Knowledge)
	}

	wantStations := []domain.Station{
		{ID: "inbox", Kind: domain.KindQueue},
		{ID: "spec", Kind: domain.KindAgent, Agent: "architect", Agents: 1, WIP: 2, Output: "spec"},
		{ID: "approve-spec", Kind: domain.KindHuman, WIP: 3},
		{ID: "build", Kind: domain.KindAgent, Route: map[string]string{"backend": "engineer", "frontend": "engineer"}, RouteAgents: map[string]int{"backend": 1}, Agents: 2, WIP: 2, Checks: []string{"make test"}},
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
			Connections: []string{"docs"}, ToolsAllow: []string{"Read", "Grep"},
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

	if !reflect.DeepEqual(l.WorkTypes, []string{"feature", workIncident}) {
		t.Errorf("work types = %v, want board.workTypes [feature incident]", l.WorkTypes)
	}
}

// A version published before the cutover keeps loading, but its line runs nothing: it names the
// forms connections replaced, and its inline connections are not the organization's.
func TestLine_NamesTheRemovedFormsOfAPublishedVersion(t *testing.T) {
	t.Parallel()

	f, err := LoadDir("testdata/.legacy")
	if err != nil {
		t.Fatal(err)
	}

	l := f.Line()
	want := []string{
		"spec.runtime.runner (field-removed)", "spec.runtime.image (field-removed)", "spec.runtime.auth (field-removed)",
		"spec.knowledgeBase (field-removed)", "spec.secrets (field-removed)", "spec.runtime.repository.token (env-reference-removed)",
		"spec.connections[0] (connection-inline-removed)", "spec.connections[1] (connection-inline-removed)",
		"spec.connections[2] (connection-inline-removed)",
	}

	if !reflect.DeepEqual(l.Removed, want) || l.Runtime.Repository.URL != "https://github.com/acme/shop" {
		t.Errorf("removed = %q, repository %+v; want %q and the repository", l.Removed, l.Runtime.Repository, want)
	}

	if len(l.Connections) != 0 {
		t.Errorf("connections = %v, want none of the organization's", l.Connections)
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

// A route of plain agent names keeps no slots for any lane: the line is what it was before
// route entries could set a count.
func TestLine_PlainRoutesKeepNoSlots(t *testing.T) {
	t.Parallel()

	f, err := Load(checkTree(edited(workAgentLine, "        route: { backend: worker, frontend: worker }\n        agents: 2\n"), nil))
	if err != nil {
		t.Fatal(err)
	}

	want := domain.Station{ID: "work", Kind: domain.KindAgent, Route: map[string]string{"backend": "worker", "frontend": "worker"}, Agents: 2}
	if got := f.Line().Stations[1]; !reflect.DeepEqual(got, want) {
		t.Errorf("station = %+v, want %+v", got, want)
	}
}

// A station's context names what goald preloads into its runs' feedback; a station without it
// preloads nothing.
func TestLine_CarriesStationContext(t *testing.T) {
	t.Parallel()

	for doc, want := range map[string]bool{
		baseFactory: false,
		strings.Replace(baseFactory, workAgentLine, workAgentLine+"        context: []\n", 1):         false,
		strings.Replace(baseFactory, workAgentLine, workAgentLine+"        context: [inflight]\n", 1): true,
	} {
		f, err := Load(checkTree(doc, nil))
		if err != nil {
			t.Fatalf("load %q: %v", doc, err)
		}

		st, ok := f.Line().Station("work")
		if !ok {
			t.Fatalf("load %q: no station work", doc)
		}

		if got := st.Preloads(domain.ContextInFlight); got != want {
			t.Errorf("station work of %q preloads in-flight = %v, want %v", doc, got, want)
		}
	}
}

// goald refuses a station context other than inflight, and one that is not a list.
func TestLoad_RefusesUnknownStationContext(t *testing.T) {
	t.Parallel()

	for _, context := range []string{"[history]", "[inflight, inflight]", "['']", "inflight", "[1]"} {
		doc := strings.Replace(baseFactory, workAgentLine, workAgentLine+"        context: "+context+"\n", 1)

		_, problems := Check(checkTree(doc, nil))
		if !slices.ContainsFunc(problems, func(p domain.DefinitionProblem) bool { return p.Check == checkSchema && p.Blocking() }) {
			t.Errorf("context %s: problems %+v, want a schema problem", context, problems)
		}

		if _, err := Load(checkTree(doc, nil)); err == nil {
			t.Errorf("context %s loaded, want it refused", context)
		}
	}
}

// board.flow.dependencies.paths names the output field whose globs replace an item's paths; a
// definition without it maps no field.
func TestLine_CarriesDependenciesPaths(t *testing.T) {
	t.Parallel()

	for doc, want := range map[string]string{
		baseFactory: "",
		baseFactory + "    flow:\n      dependencies: {}\n":                 "",
		baseFactory + "    flow:\n      dependencies: { paths: touches }\n": "touches",
	} {
		f, err := Load(checkTree(doc, nil))
		if err != nil {
			t.Fatalf("load %q: %v", doc, err)
		}

		if got := f.Line().Flow.Dependencies.Paths; got != want {
			t.Errorf("dependencies.paths of %q = %q, want %q", doc, got, want)
		}
	}
}

// flow.dependencies takes only its fields, each one a field name.
func TestLoad_RefusesUnknownDependencyKeys(t *testing.T) {
	t.Parallel()

	for _, deps := range []string{"{ files: touches }", "{ paths: 3 }", `{ paths: "" }`, "{ paths: [touches] }", "touches"} {
		doc := baseFactory + "    flow:\n      dependencies: " + deps + "\n"

		_, problems := Check(checkTree(doc, nil))
		if !slices.ContainsFunc(problems, func(p domain.DefinitionProblem) bool { return p.Check == checkSchema && p.Blocking() }) {
			t.Errorf("dependencies %s: problems %+v, want a schema problem", deps, problems)
		}
	}
}

// branchMain is the branch the trigger mapping cases filter on.
const branchMain = "main"

// workIncident is the support factory's expedited work type.
const workIncident = "incident"

// The line carries board.triggers in order, an unset enabled as enabled.
func TestLine_Triggers(t *testing.T) {
	t.Parallel()

	f, err := LoadDir("testdata/.support")
	if err != nil {
		t.Fatal(err)
	}

	want := []domain.Trigger{
		{
			Name: "morning-sweep", Enabled: true, On: domain.OnSchedule, Cron: "0 8 * * MON-FRI", TimeZone: "Europe/Berlin",
			Create: domain.TriggerCreate{Type: "question", Station: "inbox", Title: "Inbox sweep"},
		},
		{
			Name: "shop-incident", Enabled: true, On: "shop-alerts.failed",
			Filter: domain.TriggerFilter{Repo: []string{"acme/shop"}, SkipBots: true},
			Only: domain.TriggerOnly{
				FailuresInARow: 2, Dedupe: "service", ModelAgrees: &domain.ModelAgrees{Question: "Is this a real outage rather than a blip?"},
			},
			Create: domain.TriggerCreate{
				Type: workIncident, Station: "triage", Expedite: true, Title: "{{service}} is failing", Brief: "Alert: {{alert.summary}}",
			},
			Limits: domain.TriggerLimits{PerDay: 3, PerHour: domain.DefaultPerHour, PauseOnBudget: true},
		},
	}

	if got := f.Line().Triggers; !reflect.DeepEqual(got, want) {
		t.Errorf("triggers = %+v\nwant %+v", got, want)
	}

	off := false
	if got := trigger(Trigger{Name: "off", Enabled: &off, On: domain.OnSchedule}); got.Enabled {
		t.Error("a trigger with enabled: false maps as enabled")
	}

	// The keys written before ADR 0040 decision 14 read as its forms, with their filters.
	old := []struct {
		on, want string
		filter   domain.TriggerFilter
	}{
		{domain.OnCheckRunFailed, "github.check_run", domain.TriggerFilter{Branch: branchMain, Conclusion: []string{"failure"}}},
		{domain.OnIssuesLabeled, "github.issues", domain.TriggerFilter{Branch: branchMain, Action: []string{"labeled"}}},
		{"webhook.deploys", "deploys", domain.TriggerFilter{Branch: branchMain}},
		{"github.push", "github.push", domain.TriggerFilter{Branch: branchMain}},
	}

	for _, o := range old {
		got := trigger(Trigger{Name: "t", On: o.on, Filter: &TriggerFilter{Branch: branchMain}, Limits: &TriggerLimits{PerHour: 4}})
		if got.On != o.want || !reflect.DeepEqual(got.Filter, o.filter) || got.Limits.PerHour != 4 {
			t.Errorf("on %s maps to on %s, filter %+v, perHour %d; want %s, %+v, 4", o.on, got.On, got.Filter, got.Limits.PerHour, o.want, o.filter)
		}
	}

	if got := trigger(Trigger{Name: "s", On: domain.OnSchedule}); got.Limits.PerHour != 0 {
		t.Errorf("a schedule's perHour = %d; want none", got.Limits.PerHour)
	}
}

// jonahRuntimeModel is the runtime.model of the Jonah cases.
const jonahRuntimeModel = "claude-sonnet-5-5"

// A definition without learning.jonah, or with an empty one, gets the default policy and the
// runtime's model.
func TestLine_JonahPolicyDefaults(t *testing.T) {
	t.Parallel()

	withModel := edited(runtimeModel, runtimeModel+"    model: "+jonahRuntimeModel+"\n")
	want := domain.JonahPolicy{Enabled: true, ProposeAfter: 3, Share: 0.05, Model: jonahRuntimeModel}

	for name, doc := range map[string]string{
		"no learning":  withModel,
		"empty jonah":  withModel + "  learning:\n    jonah: {}\n",
		"no jonah key": withModel + "  learning:\n    kaizen: { schedule: weekly }\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f, err := Load(checkTree(doc, nil))
			if err != nil {
				t.Fatal(err)
			}

			if got := f.Line().Jonah; got != want {
				t.Errorf("jonah = %+v, want %+v", got, want)
			}
		})
	}

	f, err := Load(checkTree(baseFactory, nil))
	if err != nil {
		t.Fatal(err)
	}

	if got := f.Line().Jonah; got != domain.DefaultJonahPolicy() {
		t.Errorf("jonah without runtime.model = %+v, want %+v", got, domain.DefaultJonahPolicy())
	}
}

// Set keys are carried, false and zero included.
func TestLine_CarriesJonahPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		jonah string
		want  domain.JonahPolicy
	}{
		{
			name:  "all keys",
			jonah: "{ enabled: true, proposeAfter: 4, share: 2.5%, model: claude-haiku-5-5 }",
			want:  domain.JonahPolicy{Enabled: true, ProposeAfter: 4, Share: 0.025, Model: "claude-haiku-5-5"},
		},
		{
			name:  "off",
			jonah: "{ enabled: false }",
			want:  domain.JonahPolicy{ProposeAfter: 3, Share: 0.05, Model: jonahRuntimeModel},
		},
		{
			name:  "no spend",
			jonah: "{ share: 0%, proposeAfter: 50 }",
			want:  domain.JonahPolicy{Enabled: true, ProposeAfter: 50, Model: jonahRuntimeModel},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc := edited(runtimeModel, runtimeModel+"    model: "+jonahRuntimeModel+"\n") + "  learning:\n    jonah: " + tc.jonah + "\n"

			f, err := Load(checkTree(doc, nil))
			if err != nil {
				t.Fatal(err)
			}

			if got := f.Line().Jonah; got != tc.want {
				t.Errorf("jonah = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// A Jonah value out of range, or a key the schema does not know, is a blocking schema problem
// in factory.yaml at its line.
func TestLoad_RefusesBadJonahPolicy(t *testing.T) {
	t.Parallel()

	for _, jonah := range []string{
		"proposeAfter: 0", "proposeAfter: 51", "proposeAfter: 2.5", "share: 120%", "share: five", "share: 5", "share: -1%",
		"enabled: sometimes", "cadence: daily",
	} {
		doc := baseFactory + "  learning:\n    jonah:\n      " + jonah + " #here\n"
		line := markedLine(doc)

		_, problems := Check(checkTree(doc, nil))
		if !slices.ContainsFunc(problems, func(p domain.DefinitionProblem) bool {
			return p.Check == checkSchema && p.Blocking() && p.File == FileName && p.Line == line && p.Section == domain.SectionFiles
		}) {
			t.Errorf("jonah %s: problems %+v, want a blocking schema problem in section files at %s:%d", jonah, problems, FileName, line)
		}
	}
}

// A version on secrets carries its environment files' env, each agent's environment, the model key
// and the repository's token into the line (ADR 0037 decisions 5, 7 and 12).
func TestFactory_LineEnvironments(t *testing.T) {
	t.Parallel()

	f, err := LoadDir("testdata/build")
	if err != nil {
		t.Fatal(err)
	}

	l := f.Line()

	if l.Runtime.ModelKey != "{{claude-key}}" || l.Runtime.ModelConnection != "" {
		t.Errorf("runtime = %+v; want the model key and no model connection", l.Runtime)
	}

	if r := l.Runtime.Repository; r.Token != "{{github-token}}" || r.Forge != "" || r.Connection != "" {
		t.Errorf("repository = %+v; want its token", r)
	}

	if !l.OnSecrets() || l.Refusal() != "" {
		t.Errorf("OnSecrets = %v, Refusal = %q; want a line on secrets that runs", l.OnSecrets(), l.Refusal())
	}

	expectEnvironments(t, l)
}

// expectEnvironments fails the test unless l has testdata/build's environments and agents'.
func expectEnvironments(t *testing.T, l domain.Line) {
	t.Helper()

	def, ok := l.Environment("")
	if want := map[string]string{"GH_TOKEN": "{{github-token}}", "SENTRY_ORG": "acme"}; !ok || !maps.Equal(def.Env, want) { //nolint:gosec // a reference, not a credential
		t.Errorf("default environment = %+v, %v; want env %v", def, ok, want)
	}

	if b, ok := l.Environment("browser"); !ok || len(b.Env) != 0 {
		t.Errorf("browser environment = %+v, %v; want one without env", b, ok)
	}

	wantDef := domain.Environment{
		Name: "default", Size: domain.SandboxMedium, Image: "ghcr.io/acme/box:1", Tools: []string{"go@1.26", "gh@2"},
		Setup: []string{"go mod download"}, Env: def.Env, Files: map[string]string{"~/.npmrc": "{{npm-config}}"},
	}
	if !reflect.DeepEqual(def, wantDef) {
		t.Errorf("default environment =\n%+v\nwant\n%+v", def, wantDef)
	}

	b, _ := l.Environment("browser")

	wantBrowser := domain.Environment{
		Name: "browser", Size: domain.SandboxLarge, Tools: []string{"node@22"},
		MCP: map[string]domain.MCPServer{
			"playwright": {
				Command: []string{"npx", "@playwright/mcp@0.0.41"},
				Tools:   map[string]string{"browser_*": "allow", "browser_install": "off"},
			},
			"tickets": {
				URL: "https://tickets.example.com/mcp", Headers: map[string]string{"Authorization": "Bearer {{tickets-key}}"},
				Tools: map[string]string{"create_comment": "ask"},
			},
		},
	}
	if !reflect.DeepEqual(b, wantBrowser) {
		t.Errorf("browser environment =\n%+v\nwant\n%+v", b, wantBrowser)
	}

	envs := map[string]string{}
	for _, a := range l.Agents {
		envs[a.Name] = a.Environment
	}

	if want := map[string]string{engineer: "", "tester": "browser"}; !maps.Equal(envs, want) {
		t.Errorf("agents' environments = %v; want %v", envs, want)
	}
}

// otherFactoryID is a factory the follow-ups tests accept follow-ups from.
const otherFactoryID = "00000000-0000-4000-8000-000000000020"

// board.flow.outputs.followUps names the follow-ups field and board.flow.followUpsFrom the
// factories, by metadata.id, that may send them; both map none when left out.
func TestLine_CarriesFollowUps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		flow  string
		field string
		from  []domain.FactoryID
	}{
		{flow: ""},
		{flow: "    flow:\n      outputs: {}\n      followUpsFrom: []\n"},
		{
			flow:  "    flow:\n      outputs: { followUps: followups }\n      followUpsFrom: [" + otherFactoryID + "]\n",
			field: "followups", from: []domain.FactoryID{otherFactoryID},
		},
	}

	for _, tt := range tests {
		f, err := Load(checkTree(baseFactory+tt.flow, nil))
		if err != nil {
			t.Fatalf("flow %q: %v", tt.flow, err)
		}

		fl := f.Line().Flow
		if fl.Outputs.FollowUps != tt.field || !slices.Equal(fl.FollowUpsFrom, tt.from) {
			t.Errorf("flow %q: outputs.followUps %q, followUpsFrom %v; want %q, %v", tt.flow, fl.Outputs.FollowUps, fl.FollowUpsFrom, tt.field, tt.from)
		}
	}

	for _, flow := range []string{
		"outputs: { followUps: \"\" }", "outputs: { close: \"\" }", "outputs: { close: [a] }", "outputs: { followUps: [a] }",
		"followUpsFrom: [acme-other]", "followUpsFrom: " + otherFactoryID, "followUpsFrom: [" + otherFactoryID + ", " + otherFactoryID + "]",
	} {
		_, problems := Check(checkTree(baseFactory+"    flow:\n      "+flow+"\n", nil))
		if !slices.ContainsFunc(problems, func(p domain.DefinitionProblem) bool { return p.Check == checkSchema && p.Blocking() }) {
			t.Errorf("flow %s: problems %+v, want a schema problem", flow, problems)
		}
	}
}

// board.flow.outputs.close maps the field that closes an item onto the line, next to followUps.
func TestLine_CarriesClose(t *testing.T) {
	t.Parallel()

	f, err := Load(checkTree(baseFactory+"    flow:\n      outputs: { followUps: followups, close: closing }\n", nil))
	if err != nil {
		t.Fatal(err)
	}

	if o := f.Line().Flow.Outputs; o.FollowUps != "followups" || o.Close != "closing" {
		t.Errorf("outputs = %+v; want followUps followups and close closing", o)
	}
}

// A station contract that declares the mapped follow-ups field declares it as a plain array, so a
// follow-up never fails its run's checks: Load refuses a $ref, an item limit or required inner
// fields, and takes strings, objects or both. Without the field mapped, the contract is not read.
func TestLoad_RefusesStrictFollowUpsField(t *testing.T) {
	t.Parallel()

	contract := func(field string) string {
		return `{"type":"object","properties":{"followups":` + field + `}}`
	}

	tests := []struct {
		name    string
		field   string
		refused bool
	}{
		{name: "strings", field: `{"type":"array","items":{"type":"string"}}`},
		{name: "strings or objects", field: `{"type":"array","items":{"anyOf":[{"type":"string"},{"type":"object","properties":{"text":{"type":"string"},"for":{"type":"string"}}}]}}`},
		{name: "no items", field: `{"type":"array"}`},
		{name: "a ref", field: `{"$ref":"#/$defs/followups"}`, refused: true},
		{name: "a ref on its items", field: `{"type":"array","items":{"$ref":"#/$defs/followup"}}`, refused: true},
		{name: "an item limit", field: `{"type":"array","maxItems":5,"items":{"type":"string"}}`, refused: true},
		{name: "required inner fields", field: `{"type":"array","items":{"type":"object","required":["text"]}}`, refused: true},
		{name: "required in one form", field: `{"type":"array","items":{"oneOf":[{"type":"string"},{"type":"object","required":["text"]}]}}`, refused: true},
		{name: "not an array", field: `{"type":"string"}`, refused: true},
	}

	mapped := edited(specAgents, "  contracts:\n    answer: ./contracts/answer.schema.json\n"+specAgents,
		workAgentLine, workAgentLine+"        output: answer\n") + "    flow:\n      outputs: { followUps: followups }\n"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := Load(checkTree(mapped, map[string]string{answerContract: contract(tt.field)}))

			switch {
			case tt.refused && (err == nil || !strings.Contains(err.Error(), "followups")):
				t.Errorf("Load = %v; want it refused, naming the field", err)
			case !tt.refused && err != nil:
				t.Errorf("Load = %v; want it loaded", err)
			}

			unmapped := strings.TrimSuffix(mapped, "    flow:\n      outputs: { followUps: followups }\n")
			if _, err := Load(checkTree(unmapped, map[string]string{answerContract: contract(tt.field)})); err != nil {
				t.Errorf("Load without the field mapped = %v; want it loaded", err)
			}
		})
	}
}
