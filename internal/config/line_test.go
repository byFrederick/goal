package config

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// The line carries the flow policy and targets the engine and the flow metrics read.
func TestLine_CarriesFlowPolicyAndTargets(t *testing.T) {
	t.Parallel()

	f, err := LoadDir("testdata/support")
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
			Materialize: true,
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

	plain, err := LoadDir("testdata/line")
	if err != nil {
		t.Fatal(err)
	}

	if p := plain.Line().Flow; p.Release != domain.ReleasePush || p.Targets != (domain.Targets{}) {
		t.Errorf("flow without release or targets = %+v, want push and no targets", p)
	}

	set := domain.AssemblyPolicy{
		FreshBase: domain.FreshBaseMerge, OnConflict: domain.ConflictHuman, Overlap: domain.OverlapSerialize, Materialize: true,
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
		Materialize: true,
	}); got != want {
		t.Errorf("assemblies with only onConflict = %+v, want %+v", got, want)
	}
}

// flow.assemblies takes only its settings, each one of its values.
func TestLoad_RefusesUnknownAssemblyPolicies(t *testing.T) {
	t.Parallel()

	for _, assemblies := range []string{
		"{ freshBase: squash }", "{ onConflict: retry }", "{ parts: 3 }", "{ materialize: yes please }", "{ materialize: 1 }",
	} {
		doc := baseFactory + "    flow:\n      assemblies: " + assemblies + "\n"

		_, problems := Check(checkTree(doc, nil))
		if !slices.ContainsFunc(problems, func(p domain.DefinitionProblem) bool { return p.Check == checkSchema && p.Blocking() }) {
			t.Errorf("assemblies %s: problems %+v, want a schema problem", assemblies, problems)
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

	f, err := LoadDir("testdata/line")
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

	f, err := LoadDir("testdata/line")
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

	if !slices.Equal(l.Connections, []string{"code", "docs"}) || !slices.Equal(l.KnowledgeSources, []string{"notes"}) {
		t.Errorf("connections %v, knowledge sources %v; want [code docs] and [notes]", l.Connections, l.KnowledgeSources)
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
