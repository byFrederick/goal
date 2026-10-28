package jonah_test

import (
	"context"
	"errors"
	"maps"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/jonah"
)

// The models of the tests' arms: model ids are definition data, never Go outside tests.
const (
	modelNow   = "claude-opus-5-5"
	modelMid   = "claude-sonnet-5-5"
	modelSmall = "claude-haiku-5-5"
	modelExtra = "claude-fable-5-1"
)

// The agents of the bake-offs' line, and the engineer's station.
const (
	agentEngineer = "engineer"
	agentReviewer = "reviewer"
	stationBuild  = "build"
)

// The cases of the engineer the bake-offs run; the reviewer's review-step is left out.
const (
	leaseFix  = "lease-fix"
	mergeStep = "merge-step"
)

// bakeOffLine is the line of the bake-offs' version: the engineer on the runtime's model at
// build, the reviewer at review.
func bakeOffLine() domain.Line {
	return domain.Line{
		FactoryID: factory, VersionID: version, Runtime: domain.Runtime{Model: modelNow},
		Stations: []domain.Station{
			{ID: "inbox", Kind: domain.KindQueue},
			{ID: stationBuild, Kind: domain.KindAgent, Agent: agentEngineer},
			{ID: "review", Kind: domain.KindAgent, Agent: agentReviewer},
			{ID: "done", Kind: domain.KindDone},
		},
		Agents: []domain.AgentSpec{{Name: agentEngineer}, {Name: agentReviewer, Model: modelSmall}},
	}
}

// bakeOffContent is the content of the bake-offs, with the suite set in w's eval suites.
func (w *evalWorld) bakeOffContent() jonah.BakeOffContent {
	w.t.Helper()

	reviewer := evalCase(reviewStep, 1)
	reviewer.Agent = agentReviewer
	cases := []domain.EvalCaseSpec{evalCase(leaseFix, 1), evalCase(mergeStep, 1), reviewer}

	suite := make([]ports.EvalSuiteCase, 0, len(cases))
	for _, c := range cases {
		suite = append(suite, ports.EvalSuiteCase{Case: c})
	}

	w.suites.Set(w.org, evalContent, suite...)

	return jonah.BakeOffContent{Line: bakeOffLine(), ContentSHA256: evalContent, Cases: cases}
}

// loopWith is an eval loop over w's fakes with deps changed by change, as a second goald would
// build it.
func (w *evalWorld) loopWith(runner ports.EvalRunner, change func(*jonah.EvalDeps)) *jonah.Evals {
	d := jonah.EvalDeps{
		Orgs: w.tenancy, Evals: w.evals, Suites: w.suites, Runner: runner, Finished: porttest.NewFinishedRuns(w.store), Clock: w.clock,
	}
	if change != nil {
		change(&d)
	}

	return jonah.NewEvals(d, jonah.Config{Timeout: evalTimeout, Margin: evalMargin, Poll: evalPoll})
}

// bakeOff is bake-off id with its arms and results.
func (w *evalWorld) bakeOff(id domain.EvalRunID) domain.BakeOff {
	w.t.Helper()

	b, err := w.evals.GetBakeOff(w.ctx(), id)
	if err != nil {
		w.t.Fatal(err)
	}

	return b
}

// budgetLeft is a learning budget with a fixed amount left.
type budgetLeft float64

func (b budgetLeft) RemainingUSD(context.Context) (float64, error) { return float64(b), nil }

// drafts records the models written into drafts and answers err.
type drafts struct {
	writes []string
	err    error
}

func (d *drafts) SetAgentModel(_ context.Context, f domain.FactoryID, base domain.DefinitionVersionID, agent, model string) error {
	d.writes = append(d.writes, strings.Join([]string{string(f), string(base), agent, model}, " "))

	return d.err
}

// armTry is how a try on each model ends: the mid model passes every case cheaply, the small one
// fails merge-step, and each takes its own time and turns.
func armTry(clock *porttest.Clock, t ports.EvalTask) ports.EvalTry {
	out := ports.EvalTry{Output: []byte(`{"summary":"fixed"}`), RunID: domain.RunID("run-" + t.Model)}

	switch t.Model {
	case modelNow:
		out.CostUSD, out.Turns = 1, 5

		clock.Advance(2 * time.Second)
	case modelMid:
		out.CostUSD, out.Turns = 0.4, 4

		clock.Advance(3 * time.Second)
	default:
		out.CostUSD, out.Turns = 0.1, 2

		clock.Advance(time.Second)

		if t.Case.Name == mergeStep {
			out.ContractProblem = "summary is required"
		}
	}

	return out
}

// wantArmTasks wants tasks to be eval tasks of the engineer in bake-off id, tries of each of its
// cases on each arm's model.
func wantArmTasks(t *testing.T, tasks []ports.EvalTask, id domain.EvalRunID, tries int) {
	t.Helper()

	perModel := map[string]int{}

	for _, task := range tasks {
		if task.Kind != domain.TaskKindEval || task.Run.ID != id || task.Case.Agent != agentEngineer {
			t.Errorf("task = %+v, want an eval task of the engineer in bake-off %s", task, id)
		}

		perModel[task.Model+" "+task.Case.Name]++
	}

	for _, m := range []string{modelNow, modelMid, modelSmall} {
		for _, c := range []string{leaseFix, mergeStep} {
			if perModel[m+" "+c] != tries {
				t.Errorf("tries of %s on %s = %d, want %d (all: %v)", c, m, perModel[m+" "+c], tries, perModel)
			}
		}
	}
}

// wantSummaries wants each arm of b to have the summary want gives its model.
func wantSummaries(t *testing.T, b domain.BakeOff, want map[string]domain.ArmSummary) {
	t.Helper()

	for _, a := range b.Arms {
		s, w := a.Summary, want[a.Model]
		if s == nil {
			t.Errorf("arm %d (%s) has no summary", a.Arm, a.Model)

			continue
		}

		if s.Tries != w.Tries || s.Passed != w.Passed || s.Errors != w.Errors || math.Abs(s.CostPerItemUSD-w.CostPerItemUSD) > 1e-9 ||
			s.TimePerItem != w.TimePerItem || s.MeanTurns != w.MeanTurns || !maps.Equal(s.PerCase, w.PerCase) {
			t.Errorf("arm %d (%s) summary = %+v, want %+v", a.Arm, a.Model, *s, w)
		}
	}
}

func TestBakeOff_RunsEveryArmCaseAndTry(t *testing.T) {
	t.Parallel()

	var w *evalWorld

	w = newEvalWorld(t, func(_ context.Context, _ int, task ports.EvalTask) (ports.EvalTry, error) {
		return armTry(w.clock, task), nil
	})

	b, _, err := w.loop.StartBakeOff(w.ctx(), w.bakeOffContent(), jonah.BakeOffRequest{
		Agent: agentEngineer, Models: []string{modelNow, modelMid, modelSmall}, Trials: 2,
	}, domain.LocalUser)
	if err != nil {
		t.Fatal(err)
	}

	if len(b.Results) != 12 || b.Station != stationBuild || b.Agent != agentEngineer || b.TriesPerCase != 2 {
		t.Fatalf("bake-off = %d tries of %s at %s, %d per case; want 12 tries of the engineer at build, 2 per case",
			len(b.Results), b.Agent, b.Station, b.TriesPerCase)
	}

	w.work(12)

	wantArmTasks(t, w.runner.Tasks(), b.ID, 2)

	got := w.bakeOff(b.ID)
	if got.State != domain.EvalRunDone {
		t.Fatalf("bake-off = %s, want done", got.State)
	}

	for _, r := range got.Results {
		if want := domain.RunID("run-" + got.Arms[r.Arm-1].Model); r.RunID != want {
			t.Errorf("arm %d's try ran as %s, want %s: the arm's model", r.Arm, r.RunID, want)
		}
	}

	want := map[string]domain.ArmSummary{
		modelNow: {Tries: 4, Passed: 4, CostPerItemUSD: 1, TimePerItem: 2 * time.Second, MeanTurns: 5, PerCase: map[string]int{leaseFix: 2, mergeStep: 2}},
		modelMid: {
			Tries: 4, Passed: 4, CostPerItemUSD: 0.4, TimePerItem: 3 * time.Second, MeanTurns: 4, PerCase: map[string]int{leaseFix: 2, mergeStep: 2},
		},
		modelSmall: {Tries: 4, Passed: 2, CostPerItemUSD: 0.1, TimePerItem: time.Second, MeanTurns: 2, PerCase: map[string]int{leaseFix: 2, mergeStep: 0}},
	}

	wantSummaries(t, got, want)

	if arm, ok := domain.RecommendBakeOffArm(got); !ok || got.Arms[arm-1].Model != modelMid {
		t.Errorf("recommended arm = %d (%v), want the mid model's: the same pass rate at less cost", arm, ok)
	}
}

// pricedWorld is an eval world whose engineer's try is priced at 1, its bake-off content, and a
// loop with the default models and a learning budget of left.
func pricedWorld(t *testing.T, left float64) (*evalWorld, jonah.BakeOffContent, *jonah.Evals) {
	t.Helper()

	w := newEvalWorld(t, nil)
	content := w.bakeOffContent()
	w.finishedRunCosting(agentEngineer, t0, 1)

	defaults := []string{modelMid, modelNow, modelSmall, modelExtra, "claude-other-1"}
	loop := w.loopWith(w.runner, func(d *jonah.EvalDeps) { d.BakeOffModels, d.Budget = defaults, budgetLeft(left) })

	return w, content, loop
}

// armModels are b's arms' models in order.
func armModels(b domain.BakeOff) []string {
	out := make([]string, 0, len(b.Arms))
	for _, a := range b.Arms {
		out = append(out, a.Model)
	}

	return out
}

func TestBakeOff_StartValidates(t *testing.T) {
	t.Parallel()

	w, content, loop := pricedWorld(t, 100)

	// No models: the current model, then the defaults without it, cut at four arms; 3 tries.
	b, est, err := loop.StartBakeOff(w.ctx(), content, jonah.BakeOffRequest{Agent: agentEngineer}, domain.LocalUser)
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{modelNow, modelMid, modelSmall, modelExtra}; !slices.Equal(armModels(b), want) || !b.Arms[0].Current || b.Arms[1].Current {
		t.Errorf("default arms = %+v, want %v with the first current", b.Arms, want)
	}

	// 2 cases × 3 tries × 4 arms at 1 a try.
	if b.TriesPerCase != domain.DefaultBakeOffTries || math.Abs(est.USD-24) > 1e-9 || math.Abs(b.EstimateUSD-24) > 1e-9 {
		t.Errorf("bake-off = %d tries per case estimated at %v (%v), want 3 at 24", b.TriesPerCase, est.USD, b.EstimateUSD)
	}

	refused := []struct {
		name string
		loop *jonah.Evals
		req  jonah.BakeOffRequest
	}{
		{"one arm", w.loop, jonah.BakeOffRequest{Agent: agentEngineer}},
		{"one model named", loop, jonah.BakeOffRequest{Agent: agentEngineer, Models: []string{modelNow}}},
		{"a model twice", loop, jonah.BakeOffRequest{Agent: agentEngineer, Models: []string{modelNow, modelMid, modelMid}}},
		{"five arms", loop, jonah.BakeOffRequest{Agent: agentEngineer, Models: []string{modelNow, modelMid, modelSmall, modelExtra, "claude-other-1"}}},
		{"no current arm", loop, jonah.BakeOffRequest{Agent: agentEngineer, Models: []string{modelMid, modelSmall}}},
		{"eleven tries", loop, jonah.BakeOffRequest{Agent: agentEngineer, Models: []string{modelNow, modelMid}, Trials: 11}},
		{"negative tries", loop, jonah.BakeOffRequest{Agent: agentEngineer, Models: []string{modelNow, modelMid}, Trials: -1}},
		{"unknown agent", loop, jonah.BakeOffRequest{Agent: "designer", Models: []string{modelNow, modelMid}}},
	}

	for _, r := range refused {
		if _, _, err := r.loop.StartBakeOff(w.ctx(), content, r.req, domain.LocalUser); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", r.name, err)
		}
	}
}

func TestBakeOff_StartRefusesOverTheLearningBudget(t *testing.T) {
	t.Parallel()

	w, content, poor := pricedWorld(t, 5)

	_, est, err := poor.StartBakeOff(w.ctx(), content, jonah.BakeOffRequest{Agent: agentEngineer}, domain.LocalUser)
	if !errors.Is(err, jonah.ErrLearningBudgetExhausted) || math.Abs(est.USD-24) > 1e-9 {
		t.Errorf("over budget: estimate %v, err = %v; want 24 and ErrLearningBudgetExhausted", est.USD, err)
	}

	if list, err := w.evals.ListBakeOffs(w.ctx(), factory, 10); err != nil || len(list) != 0 {
		t.Errorf("bake-offs = %d (%v), want none queued", len(list), err)
	}
}

func TestBakeOff_RerunKeepsArmsAndTries(t *testing.T) {
	t.Parallel()

	w, content, loop := pricedWorld(t, 100)

	b, _, err := loop.StartBakeOff(w.ctx(), content, jonah.BakeOffRequest{Agent: agentEngineer, Models: []string{modelMid, modelNow}, Trials: 2}, domain.LocalUser)
	if err != nil {
		t.Fatal(err)
	}

	// Run again: the same arms and tries on the content given, a newer version.
	newer := content
	newer.Line.VersionID = "00000000-0000-4000-a000-000000000002"

	again, _, err := loop.RerunBakeOff(w.ctx(), b.ID, newer, domain.LocalUser)
	if err != nil {
		t.Fatal(err)
	}

	if again.ID == b.ID || again.VersionID != newer.Line.VersionID || again.TriesPerCase != 2 || again.Agent != agentEngineer ||
		!slices.Equal(armModels(again), armModels(b)) || !again.Arms[1].Current {
		t.Errorf("run again = %+v, want a new bake-off of %+v on the newer version", again, b)
	}

	if _, _, err := loop.RerunBakeOff(w.ctx(), "00000000-0000-4000-b000-000000000009", newer, domain.LocalUser); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("run again an unknown bake-off = %v, want ErrNotFound", err)
	}
}

func TestBakeOff_ResumesLeasedTriesAfterRestart(t *testing.T) {
	t.Parallel()

	w := newEvalWorld(t, nil)
	content := w.bakeOffContent()

	b, _, err := w.loop.StartBakeOff(w.ctx(), content, jonah.BakeOffRequest{
		Agent: agentEngineer, Models: []string{modelNow, modelSmall}, Trials: 1,
	}, domain.LocalUser)
	if err != nil {
		t.Fatal(err)
	}

	// The first goald claims a try and stops before it records it.
	ctx, stop := context.WithCancel(t.Context())
	dying := w.loopWith(porttest.NewEvalRunner(func(ctx context.Context, _ int, _ ports.EvalTask) (ports.EvalTry, error) {
		stop()

		return ports.EvalTry{}, ctx.Err()
	}), nil)

	if n, _ := dying.Work(ctx); n != 0 {
		t.Fatalf("the dying goald recorded %d tries, want none", n)
	}

	claimed := slices.IndexFunc(w.bakeOff(b.ID).Results, func(r domain.EvalResult) bool { return r.State == domain.EvalRunning })
	if claimed < 0 {
		t.Fatal("no try was left running under the dying goald's lease")
	}

	// A second goald on the same store takes it once the lease ends.
	w.clock.Advance(2*evalTimeout + evalMargin + time.Second)

	runner := porttest.NewEvalRunner(func(_ context.Context, _ int, task ports.EvalTask) (ports.EvalTry, error) {
		return armTry(w.clock, task), nil
	})

	if n, err := w.loopWith(runner, nil).Work(w.t.Context()); err != nil || n != 4 {
		t.Fatalf("the second goald recorded %d tries (%v), want 4", n, err)
	}

	got := w.bakeOff(b.ID)
	leased := got.Results[claimed]

	if want := domain.RunID("run-" + got.Arms[leased.Arm-1].Model); leased.State != domain.EvalPass || leased.RunID != want {
		t.Errorf("the leased try = %s as %s, want a pass run with its arm's model (%s)", leased.State, leased.RunID, want)
	}

	if got.State != domain.EvalRunDone || slices.ContainsFunc(got.Arms, func(a domain.BakeOffArm) bool { return a.Summary == nil || a.Summary.Tries != 2 }) {
		t.Errorf("bake-off = %s with arms %+v, want done with a summary of 2 tries per arm", got.State, got.Arms)
	}
}

func TestBakeOff_ApplyWritesTheArmModel(t *testing.T) {
	t.Parallel()

	w := newEvalWorld(t, nil)
	d := &drafts{}
	loop := w.loopWith(w.runner, func(deps *jonah.EvalDeps) { deps.Drafts = d })

	b, _, err := loop.StartBakeOff(w.ctx(), w.bakeOffContent(), jonah.BakeOffRequest{
		Agent: agentEngineer, Models: []string{modelNow, modelMid}, Trials: 1,
	}, domain.LocalUser)
	if err != nil {
		t.Fatal(err)
	}

	arm, err := loop.ApplyBakeOff(w.ctx(), b.ID, 2)
	if err != nil || arm.Model != modelMid {
		t.Fatalf("apply arm 2 = %+v, %v; want the mid model", arm, err)
	}

	if want := []string{string(factory) + " " + string(version) + " engineer " + modelMid}; !slices.Equal(d.writes, want) {
		t.Errorf("draft writes = %q, want %q", d.writes, want)
	}

	if _, err := loop.ApplyBakeOff(w.ctx(), b.ID, 3); !errors.Is(err, domain.ErrInvalid) || len(d.writes) != 1 {
		t.Errorf("apply an arm the bake-off lacks = %v after %d writes, want ErrInvalid and no write", err, len(d.writes))
	}

	d.err = ports.ErrConflict
	if _, err := loop.ApplyBakeOff(w.ctx(), b.ID, 1); !errors.Is(err, ports.ErrConflict) {
		t.Errorf("apply on a changed agent = %v, want the draft's ErrConflict", err)
	}
}
