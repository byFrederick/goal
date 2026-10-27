package jonah_test

import (
	"context"
	"errors"
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

var (
	evalContent = domain.BlobHash(strings.Repeat("c", 64))
	caseHash    = domain.BlobHash(strings.Repeat("b", 64))
)

// runOne is the agent run of the tests' tries.
const runOne = domain.RunID("run-1")

// The eval loop's times in the tests.
const (
	evalTimeout = 5 * time.Minute
	evalMargin  = time.Minute
	evalPoll    = 10 * time.Second
)

// evalWorld is a world with eval runs, their suites and an eval loop over a runner a test sets.
type evalWorld struct {
	*world
	evals  *porttest.Evals
	suites *porttest.EvalSuites
	runner *porttest.EvalRunner
	loop   *jonah.Evals
}

func newEvalWorld(t *testing.T, run func(ctx context.Context, call int, t ports.EvalTask) (ports.EvalTry, error)) *evalWorld {
	t.Helper()

	w := &evalWorld{world: newWorld(t), suites: porttest.NewEvalSuites(), runner: porttest.NewEvalRunner(run)}
	w.evals = porttest.NewEvals(w.clock.Now)
	w.loop = jonah.NewEvals(jonah.EvalDeps{
		Orgs: w.tenancy, Evals: w.evals, Suites: w.suites, Runner: w.runner, Finished: porttest.NewFinishedRuns(w.store), Clock: w.clock,
	}, jonah.Config{Timeout: evalTimeout, Margin: evalMargin, Poll: evalPoll})

	return w
}

// evalCase is a case of agent engineer named name with tries tries, checked by its contract.
func evalCase(name string, tries int) domain.EvalCaseSpec {
	return domain.EvalCaseSpec{
		Name: name, SHA256: caseHash, Agent: "engineer", Description: "Fixes the lease", Tries: tries, Contract: true,
		Input: domain.EvalInput{Title: "Fix the lease", Body: "The lease expires early."},
	}
}

// queue makes cases the suite of the eval content and queues a run of them.
func (w *evalWorld) queue(cases ...ports.EvalSuiteCase) domain.EvalRun {
	w.t.Helper()

	w.suites.Set(w.org, evalContent, cases...)

	n := domain.NewEvalRun{FactoryID: factory, VersionID: version, ContentSHA256: evalContent, Trigger: domain.EvalManual}
	for _, c := range cases {
		n.Cases = append(n.Cases, domain.EvalCase{Name: c.Case.Name, SHA256: c.Case.SHA256, Tries: c.Case.Tries})
	}

	r, err := w.evals.CreateRun(w.ctx(), n, domain.LocalUser)
	if err != nil {
		w.t.Fatal(err)
	}

	return r
}

// work works the eval queue once and wants n tries recorded.
func (w *evalWorld) work(n int) {
	w.t.Helper()

	if got, err := w.loop.Work(w.t.Context()); err != nil || got != n {
		w.t.Fatalf("Work = %d, %v; want %d tries recorded", got, err, n)
	}
}

// run is eval run id with its results.
func (w *evalWorld) run(id domain.EvalRunID) domain.EvalRun {
	w.t.Helper()

	r, err := w.evals.GetRun(w.ctx(), id)
	if err != nil {
		w.t.Fatal(err)
	}

	return r
}

// wantTry wants try n of r's only case to have ended as state, checked by checkedBy, with a
// reason that says reason when it is not empty.
func wantTry(t *testing.T, r domain.EvalRun, n int, state domain.EvalResultState, checkedBy, reason string) domain.EvalResult {
	t.Helper()

	i := slices.IndexFunc(r.Results, func(res domain.EvalResult) bool { return res.Try == n })
	if i < 0 {
		t.Fatalf("run has no try %d: %+v", n, r.Results)
	}

	got := r.Results[i]
	if got.State != state || got.CheckedBy != checkedBy {
		t.Errorf("try %d = %s checked by %q, want %s checked by %q (detail %v)", n, got.State, got.CheckedBy, state, checkedBy, got.Detail)
	}

	if why, _ := got.Detail["reason"].(string); !strings.Contains(why, reason) {
		t.Errorf("try %d's reason = %q, want it to say %q", n, why, reason)
	}

	return got
}

func TestEvals_GradesContract(t *testing.T) {
	t.Parallel()

	w := newEvalWorld(t, func(_ context.Context, call int, _ ports.EvalTask) (ports.EvalTry, error) {
		if call == 1 {
			return ports.EvalTry{Output: []byte(`{"summary":"fixed"}`), CostUSD: 0.25, Turns: 4, RunID: runOne}, nil
		}

		return ports.EvalTry{Output: []byte(`{}`), ContractProblem: "summary is required", CostUSD: 0.5, Turns: 6, RunID: "run-2"}, nil
	})

	c := evalCase("lease-fix", 2)
	r := w.queue(ports.EvalSuiteCase{Case: c})
	w.work(2)

	got := w.run(r.ID)
	pass := wantTry(t, got, 1, domain.EvalPass, "contract", "")
	fail := wantTry(t, got, 2, domain.EvalFail, "contract", "breaks the agent's contract")

	if pass.CostUSD != 0.25 || pass.Turns != 4 || pass.RunID != runOne {
		t.Errorf("passing try = %+v, want its run's cost, turns and id", pass)
	}

	if fail.Detail["contract_problem"] != "summary is required" {
		t.Errorf("failing try's detail = %v, want the contract problem", fail.Detail)
	}

	if got.CostUSD != 0.75 {
		t.Errorf("run cost = %v, want 0.75", got.CostUSD)
	}

	for _, task := range w.runner.Tasks() {
		if task.Kind != domain.TaskKindEval || task.Case.Name != c.Name || task.Run.ID != r.ID {
			t.Errorf("task = %+v, want an eval task of case %s in run %s", task, c.Name, r.ID)
		}
	}

	if tries := []int{w.runner.Tasks()[0].Try, w.runner.Tasks()[1].Try}; !slices.Equal(tries, []int{1, 2}) {
		t.Errorf("tries run = %v, want [1 2]", tries)
	}
}

func TestEvals_GradesJudge(t *testing.T) {
	t.Parallel()

	answers := []string{followed, "half", "something else"}

	w := newEvalWorld(t, func(_ context.Context, call int, _ ports.EvalTask) (ports.EvalTry, error) {
		return ports.EvalTry{Output: []byte(`{}`), Outcome: answers[call-1], Reason: "It did what the spec said.", CostUSD: 0.1}, nil
	})

	j := judge("spec-followed", 1)
	j.Outcomes = append(j.Outcomes, domain.JudgeOutcome{Score: 0.5, Label: "half", Meaning: "Some of it."})

	c := evalCase("lease-fix", 3)
	c.Contract, c.Judge = false, j.Slug

	r := w.queue(ports.EvalSuiteCase{Case: c, Judge: j})
	w.work(3)

	got := w.run(r.ID)
	pass := wantTry(t, got, 1, domain.EvalPass, "judge:spec-followed", "")
	wantTry(t, got, 2, domain.EvalFail, "judge:spec-followed", "below its pass mark")
	wantTry(t, got, 3, domain.EvalError, "judge:spec-followed", "not one of its outcomes")

	if pass.Detail["outcome"] != followed || pass.Detail["score"] != 1.0 || pass.Detail["judge_reason"] != "It did what the spec said." {
		t.Errorf("passing try's detail = %v, want the judge's outcome, score and reason", pass.Detail)
	}

	if task := w.runner.Tasks()[0]; task.Judge.Slug != j.Slug {
		t.Errorf("task's judge = %q, want %q", task.Judge.Slug, j.Slug)
	}
}

func TestEvals_GradesReferencePatch(t *testing.T) {
	t.Parallel()

	const patch = "--- a/internal/lease.go\n+++ b/internal/lease.go\n@@ -1 +1 @@\n-a\n+b\n" +
		"--- /dev/null\n+++ b/internal/lease_test.go\n@@ -0,0 +1 @@\n+c\n"

	tries := []ports.EvalTry{
		{ChangedFiles: []string{"internal/lease.go", "internal/lease_test.go", "docs/lease.md"}, Checks: []ports.EvalCheck{{Command: "task check", ExitCode: 0}}},
		{ChangedFiles: []string{"internal/lease.go"}, Checks: []ports.EvalCheck{{Command: "task check", ExitCode: 0}}},
		{ChangedFiles: []string{"internal/lease.go", "internal/lease_test.go"}, Checks: []ports.EvalCheck{{Command: "task check", ExitCode: 1}}},
	}

	w := newEvalWorld(t, func(_ context.Context, call int, _ ports.EvalTask) (ports.EvalTry, error) { return tries[call-1], nil })

	c := evalCase("lease-fix", 3)
	c.Contract, c.ReferencePatch = false, "evals/patches/lease-fix.diff"

	r := w.queue(ports.EvalSuiteCase{Case: c, Patch: []byte(patch)})
	w.work(3)

	got := w.run(r.ID)
	checkedBy := "reference-patch:evals/patches/lease-fix.diff"

	wantTry(t, got, 1, domain.EvalPass, checkedBy, "")

	missing := wantTry(t, got, 2, domain.EvalFail, checkedBy, "leaves out 1 of the reference patch's files")
	if m, _ := missing.Detail["missing_files"].([]string); !slices.Equal(m, []string{"internal/lease_test.go"}) {
		t.Errorf("missing files = %v, want [internal/lease_test.go]", missing.Detail["missing_files"])
	}

	failed := wantTry(t, got, 3, domain.EvalFail, checkedBy, "1 checks failed")
	if f, _ := failed.Detail["failed_checks"].([]string); !slices.Equal(f, []string{"task check (exit 1)"}) {
		t.Errorf("failed checks = %v, want [task check (exit 1)]", failed.Detail["failed_checks"])
	}
}

func TestEvals_FinishesRunWhenAllTriesFinish(t *testing.T) {
	t.Parallel()

	w := newEvalWorld(t, func(context.Context, int, ports.EvalTask) (ports.EvalTry, error) { return ports.EvalTry{}, nil })
	r := w.queue(ports.EvalSuiteCase{Case: evalCase("lease-fix", 2)}, ports.EvalSuiteCase{Case: evalCase("merge-step", 1)})

	if got := w.run(r.ID); got.State != domain.EvalRunQueued {
		t.Fatalf("state before work = %s, want queued", got.State)
	}

	w.work(3)

	got := w.run(r.ID)
	if got.State != domain.EvalRunDone || !got.FinishedAt.Equal(t0) {
		t.Errorf("run = %s finished at %v, want done at %v", got.State, got.FinishedAt, t0)
	}

	w.work(0)
}

func TestEvals_MissingOrChangedCaseIsError(t *testing.T) {
	t.Parallel()

	w := newEvalWorld(t, func(context.Context, int, ports.EvalTask) (ports.EvalTry, error) { return ports.EvalTry{}, nil })
	r := w.queue(ports.EvalSuiteCase{Case: evalCase("lease-fix", 1)}, ports.EvalSuiteCase{Case: evalCase("merge-step", 1)})

	// The content now has a changed lease-fix and no merge-step.
	changed := evalCase("lease-fix", 1)
	changed.SHA256 = domain.BlobHash(strings.Repeat("d", 64))
	w.suites.Set(w.org, evalContent, ports.EvalSuiteCase{Case: changed})

	w.work(2)

	got := w.run(r.ID)
	for _, res := range got.Results {
		why, _ := res.Detail["reason"].(string)

		want := map[string]string{"lease-fix": "changed since the run was queued", "merge-step": "not in the eval run's content"}[res.Case]
		if res.State != domain.EvalError || !strings.Contains(why, want) {
			t.Errorf("case %s = %s (%q), want error saying %q", res.Case, res.State, why, want)
		}
	}

	if n := len(w.runner.Tasks()); n != 0 {
		t.Errorf("runner ran %d tries, want none", n)
	}

	if got.State != domain.EvalRunDone {
		t.Errorf("run = %s, want done", got.State)
	}
}

func TestEvals_RetriesInfraErrorOnceThenError(t *testing.T) {
	t.Parallel()

	errBox := errors.New("the box did not start")

	w := newEvalWorld(t, func(_ context.Context, call int, t ports.EvalTask) (ports.EvalTry, error) {
		// Try 1 fails twice; try 2 fails once and then passes.
		if t.Try == 1 || call == 3 {
			return ports.EvalTry{CostUSD: 0.1, Turns: 1}, errBox
		}

		return ports.EvalTry{CostUSD: 0.2, Turns: 3}, nil
	})

	r := w.queue(ports.EvalSuiteCase{Case: evalCase("lease-fix", 2)})
	w.work(2)

	got := w.run(r.ID)

	twice := wantTry(t, got, 1, domain.EvalError, "contract", "failed twice: the box did not start")
	if math.Abs(twice.CostUSD-0.2) > 1e-9 || twice.Turns != 2 {
		t.Errorf("error try cost %v and %d turns, want both attempts' 0.2 and 2", twice.CostUSD, twice.Turns)
	}

	retried := wantTry(t, got, 2, domain.EvalPass, "contract", "")
	if math.Abs(retried.CostUSD-0.3) > 1e-9 {
		t.Errorf("retried try cost %v, want both attempts' 0.3", retried.CostUSD)
	}

	if n := len(w.runner.Tasks()); n != 4 {
		t.Errorf("runner called %d times, want 4", n)
	}
}

func TestEvals_FailedRunIsFailNotError(t *testing.T) {
	t.Parallel()

	w := newEvalWorld(t, func(context.Context, int, ports.EvalTask) (ports.EvalTry, error) {
		return ports.EvalTry{Failure: "the agent ran out of turns", CostUSD: 0.4, RunID: runOne}, nil
	})

	r := w.queue(ports.EvalSuiteCase{Case: evalCase("lease-fix", 1)})
	w.work(1)

	got := wantTry(t, w.run(r.ID), 1, domain.EvalFail, "contract", "ran out of turns")
	if got.CostUSD != 0.4 || got.RunID != runOne {
		t.Errorf("try = %+v, want its run's cost and id", got)
	}

	if n := len(w.runner.Tasks()); n != 1 {
		t.Errorf("runner called %d times, want 1: a failed run is not retried", n)
	}
}

func TestEvals_LeaseLostLeavesTry(t *testing.T) {
	t.Parallel()

	var w *evalWorld

	w = newEvalWorld(t, func(ctx context.Context, _ int, _ ports.EvalTask) (ports.EvalTry, error) {
		// The try outlives its lease, and another instance claims it.
		w.clock.Advance(2*evalTimeout + evalMargin)

		if _, err := w.evals.ClaimResult(ctx, w.clock.Now(), time.Hour); err != nil {
			return ports.EvalTry{}, err
		}

		return ports.EvalTry{CostUSD: 0.1}, nil
	})

	r := w.queue(ports.EvalSuiteCase{Case: evalCase("lease-fix", 1)})
	w.work(0)

	got := w.run(r.ID)
	if res := got.Results[0]; res.State != domain.EvalRunning || !res.LeaseUntil.Equal(w.clock.Now().Add(time.Hour)) {
		t.Errorf("try = %+v, want it running under the other claim", res)
	}

	if got.State != domain.EvalRunRunning || got.CostUSD != 0 {
		t.Errorf("run = %s costing %v, want running at no cost", got.State, got.CostUSD)
	}
}

func TestEvals_CancelStopsRemainingTries(t *testing.T) {
	t.Parallel()

	var (
		w   *evalWorld
		run domain.EvalRun
	)

	w = newEvalWorld(t, func(ctx context.Context, _ int, _ ports.EvalTask) (ports.EvalTry, error) {
		if _, err := w.loop.Cancel(w.ctx(), run.ID); err != nil {
			return ports.EvalTry{}, err
		}

		// The loop's poll reads the run while the try works.
		if err := w.clock.WaitTickers(ctx, 1); err != nil {
			return ports.EvalTry{}, err
		}

		w.clock.Advance(evalPoll)
		<-ctx.Done()

		return ports.EvalTry{CostUSD: 0.3, Turns: 2, RunID: runOne}, ctx.Err()
	})

	run = w.queue(ports.EvalSuiteCase{Case: evalCase("lease-fix", 3)})
	w.work(1)

	got := w.run(run.ID)
	if got.State != domain.EvalRunCancelled {
		t.Errorf("run = %s, want cancelled", got.State)
	}

	cancelled := wantTry(t, got, 1, domain.EvalError, "contract", jonah.EvalCancelled)
	if cancelled.CostUSD != 0.3 || cancelled.RunID != runOne {
		t.Errorf("cancelled try = %+v, want its cost so far and its run", cancelled)
	}

	for _, n := range []int{2, 3} {
		wantTry(t, got, n, domain.EvalPending, "", "")
	}

	if n := len(w.runner.Tasks()); n != 1 {
		t.Errorf("runner called %d times, want 1: a cancelled try is not retried", n)
	}

	w.work(0)

	if _, err := w.loop.Cancel(w.ctx(), run.ID); !errors.Is(err, ports.ErrConflict) {
		t.Errorf("cancelling again: err = %v, want ErrConflict", err)
	}
}

func TestEvals_Estimate(t *testing.T) {
	t.Parallel()

	costs := []float64{0.2, 0.4}

	w := newEvalWorld(t, func(_ context.Context, call int, _ ports.EvalTask) (ports.EvalTry, error) {
		return ports.EvalTry{CostUSD: costs[call-1]}, nil
	})

	// lease-fix has history: two tries at 0.2 and 0.4.
	w.queue(ports.EvalSuiteCase{Case: evalCase("lease-fix", 2)})
	w.work(2)

	// The engineer's finished runs cost 1, 3 and 2: their median is 2.
	for i, cost := range []float64{1, 3, 2} {
		w.finishedRunCosting("engineer", t0.Add(time.Duration(i)*time.Minute), cost)
	}

	reviewer := evalCase("review-step", 1)
	reviewer.Agent = "reviewer"

	got, err := w.loop.Estimate(w.ctx(), factory, []domain.EvalCaseSpec{evalCase("lease-fix", 3), evalCase("merge-step", 2), reviewer})
	if err != nil {
		t.Fatal(err)
	}

	// lease-fix: 3 tries at the mean 0.3; merge-step: 2 tries at the engineer's median 2.
	if math.Abs(got.USD-4.9) > 1e-9 {
		t.Errorf("estimate = %v, want 4.9", got.USD)
	}

	if !slices.Equal(got.Unpriced, []string{"review-step"}) {
		t.Errorf("unpriced = %v, want [review-step]", got.Unpriced)
	}
}

func TestEvals_StartQueuesEveryCaseWithItsEstimate(t *testing.T) {
	t.Parallel()

	w := newEvalWorld(t, nil)

	// The engineer's finished runs cost 1 and 3: their median is 2.
	for i, cost := range []float64{1, 3} {
		w.finishedRunCosting("engineer", t0.Add(time.Duration(i)*time.Minute), cost)
	}

	reviewer := evalCase("review-step", 1)
	reviewer.Agent = "reviewer"
	cases := []domain.EvalCaseSpec{evalCase("lease-fix", 3), reviewer}

	run, est, err := w.loop.Start(w.ctx(), domain.NewEvalRun{
		FactoryID: factory, ContentSHA256: evalContent, Trigger: domain.EvalOnPublish,
	}, cases, domain.LocalUser)
	if err != nil {
		t.Fatal(err)
	}

	// lease-fix: 3 tries at the engineer's median 2; review-step has no price.
	if math.Abs(est.USD-6) > 1e-9 || !slices.Equal(est.Unpriced, []string{"review-step"}) {
		t.Errorf("estimate = %+v, want 6 with review-step unpriced", est)
	}

	got := w.run(run.ID)
	if got.State != domain.EvalRunQueued || got.Trigger != domain.EvalOnPublish || math.Abs(got.EstimateUSD-6) > 1e-9 {
		t.Errorf("run = %s %s estimate %v, want queued publish run estimated at 6", got.State, got.Trigger, got.EstimateUSD)
	}

	if len(got.Results) != 4 {
		t.Errorf("results = %d, want 4 (3 tries of lease-fix, 1 of review-step)", len(got.Results))
	}

	one, _, err := w.loop.Start(w.ctx(), domain.NewEvalRun{
		FactoryID: factory, ContentSHA256: evalContent, Trigger: domain.EvalManual, Case: "review-step",
	}, cases, domain.LocalUser)
	if err != nil {
		t.Fatal(err)
	}

	if got := w.run(one.ID); got.Case != "review-step" || len(got.Results) != 1 {
		t.Errorf("one-case run = case %q with %d results, want review-step with 1", got.Case, len(got.Results))
	}

	if _, _, err := w.loop.Start(w.ctx(), domain.NewEvalRun{
		FactoryID: factory, ContentSHA256: evalContent, Trigger: domain.EvalManual, Case: "no-such-case",
	}, cases, domain.LocalUser); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("Start of an unknown case = %v, want ErrInvalid", err)
	}
}
