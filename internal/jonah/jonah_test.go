package jonah_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/jonah"
)

const (
	factory = domain.FactoryID("00000000-0000-4000-8000-000000000001")
	version = domain.DefinitionVersionID("00000000-0000-4000-a000-000000000001")
)

// followed is the passing outcome of the judges the tests make.
const followed = "followed"

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func TestSampled_Deterministic(t *testing.T) {
	t.Parallel()

	first := map[domain.RunID]bool{}

	for i := range 100 {
		run := domain.RunID(fmt.Sprintf("run-%d", i))
		first[run] = jonah.Sampled(run, "spec-followed", 0.5)
	}

	for run, want := range first {
		if jonah.Sampled(run, "spec-followed", 0.5) != want {
			t.Fatalf("run %s: two calls disagree", run)
		}
	}

	const n, share = 10_000, 0.3

	sampled, differs := 0, 0

	for i := range n {
		run := domain.RunID(fmt.Sprintf("0192f1c0-0000-7000-8000-%012d", i))
		if jonah.Sampled(run, "spec-followed", share) {
			sampled++
		}

		if jonah.Sampled(run, "spec-followed", share) != jonah.Sampled(run, "tests-added", share) {
			differs++
		}
	}

	if got := float64(sampled) / n; math.Abs(got-share) > 0.02 {
		t.Errorf("sampled %d of %d runs (%.3f), want about %.1f", sampled, n, got, share)
	}

	if differs == 0 {
		t.Error("two judges sample exactly the same runs; the slug must be part of the hash")
	}
}

func TestSampled_ZeroAndOne(t *testing.T) {
	t.Parallel()

	for i := range 1000 {
		run := domain.RunID(fmt.Sprintf("run-%d", i))
		if jonah.Sampled(run, "spec-followed", 0) {
			t.Fatalf("run %s sampled at sample 0", run)
		}

		if !jonah.Sampled(run, "spec-followed", 1) {
			t.Fatalf("run %s not sampled at sample 1", run)
		}
	}
}

// world is one organization with a factory, the fakes the loop works through and a clock.
type world struct {
	t       *testing.T
	org     domain.OrgID
	store   *porttest.Store
	tenancy *porttest.Tenancy
	scores  *porttest.JudgeScores
	judges  *porttest.Judges
	clock   *porttest.Clock
}

// ctx is the test's context under the world's organization.
func (w *world) ctx() context.Context { return domain.WithOrg(w.t.Context(), w.org) }

func newWorld(t *testing.T) *world {
	t.Helper()

	clock := porttest.NewClock(t0)
	store := porttest.NewStore()
	tn := porttest.NewTenancy(clock.Now).WithStore(store)

	o, err := tn.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: "acme", Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}

	store.AddFactory(o.ID, factory)

	return &world{
		t: t, org: o.ID, store: store, tenancy: tn,
		scores: porttest.NewJudgeScores(store), judges: porttest.NewJudges(), clock: clock,
	}
}

// finishedRun starts and finishes a run of agent on a new item, ended at endedAt.
func (w *world) finishedRun(agent string, endedAt time.Time) domain.Run {
	w.t.Helper()

	return w.finishedRunCosting(agent, endedAt, 0)
}

// finishedRunCosting is finishedRun of a run that cost cost.
func (w *world) finishedRunCosting(agent string, endedAt time.Time, cost float64) domain.Run {
	w.t.Helper()

	it, err := w.store.CreateItem(w.ctx(), domain.NewItem{FactoryID: factory, Title: "Fix the lease", Station: "build", CreatedAt: endedAt.Add(-time.Hour)})
	if err != nil {
		w.t.Fatal(err)
	}

	id, err := w.store.StartRun(w.ctx(), ports.NewRun{
		FactoryID: factory, DefinitionVersionID: version, ItemID: it.ID, Station: "build", Agent: agent, StartedAt: endedAt.Add(-time.Minute),
	})
	if err != nil {
		w.t.Fatal(err)
	}

	if err := w.store.FinishRun(w.ctx(), id, domain.RunResult{Status: domain.RunSucceeded, CostUSD: cost}, endedAt); err != nil {
		w.t.Fatal(err)
	}

	r, err := w.store.Run(w.ctx(), id)
	if err != nil {
		w.t.Fatal(err)
	}

	return r
}

func (w *world) loop(runner ports.JudgeRunner) *jonah.Loop {
	return jonah.New(jonah.Deps{
		Orgs: w.tenancy, Runs: w.store, Finished: porttest.NewFinishedRuns(w.store), Judges: w.judges,
		Scores: w.scores, Runner: runner, Clock: w.clock,
	}, jonah.Config{Lookback: 24 * time.Hour, Timeout: 5 * time.Minute, Margin: time.Minute})
}

// recent is judge's production scores, newest first.
func (w *world) recent(judge string) []domain.JudgeScore {
	w.t.Helper()

	got, err := w.scores.Recent(w.ctx(), factory, judge, 100)
	if err != nil {
		w.t.Fatal(err)
	}

	return got
}

// sweep sweeps once with l.
func (w *world) sweep(l *jonah.Loop) {
	w.t.Helper()

	if err := l.Sweep(w.t.Context()); err != nil {
		w.t.Fatal(err)
	}
}

// work works the queue once with l and wants n scores recorded.
func (w *world) work(l *jonah.Loop, n int) {
	w.t.Helper()

	if got, err := l.Work(w.t.Context()); err != nil || got != n {
		w.t.Fatalf("Work = %d, %v; want %d scores recorded", got, err, n)
	}
}

// noProductionScores wants judge to have no production score, in its recent list or its summary.
func (w *world) noProductionScores(judge string) {
	w.t.Helper()

	if got := w.recent(judge); len(got) != 0 {
		w.t.Errorf("production scores of %s: %+v, want none", judge, got)
	}

	sum, err := w.scores.Summary(w.ctx(), factory, w.clock.Now())
	if err != nil {
		w.t.Fatal(err)
	}

	if len(sum) != 0 {
		w.t.Errorf("summary: %+v, want none", sum)
	}
}

func judge(slug string, sample float64, agents ...string) domain.Judge {
	return domain.Judge{
		Slug: slug, Title: "Spec followed", Criterion: "The change does what the spec asks.", Agents: agents,
		Outcomes: []domain.JudgeOutcome{
			{Score: 1, Label: followed, Meaning: "Every point is done."},
			{Score: 0.5, Label: "partly", Meaning: "Some points are done."},
			{Score: 0, Label: "missed", Meaning: "The spec is not followed."},
		},
		PassMark: 0.6, Sample: sample, Enabled: true, Rubric: "Compare the diff with the spec.",
	}
}

// answer is a runner that answers every call with outcome.
func answer(outcome string) *porttest.JudgeRunner {
	return porttest.NewJudgeRunner(func(_ context.Context, call int, _ ports.JudgeTask) (ports.JudgeRun, error) {
		return ports.JudgeRun{Outcome: outcome, Reason: "Nothing is missing.", CostUSD: 0.01, RunID: domain.RunID(fmt.Sprintf("judge-run-%d", call))}, nil
	})
}

func TestLoop_SweepEnqueuesOncePerJudgeAndRun(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.judges.Set(w.org, factory, version, judge("spec-followed", 1), judge("tests-added", 1, "builder"), judge("never", 0))

	builder := w.finishedRun("builder", t0.Add(-time.Hour))
	reviewer := w.finishedRun("reviewer", t0.Add(-2*time.Hour))
	w.finishedRun("builder", t0.Add(-48*time.Hour)) // outside the lookback

	l := w.loop(answer(followed))

	for range 2 {
		if err := l.Sweep(t.Context()); err != nil {
			t.Fatal(err)
		}
	}

	runsOf := func(judge string) map[domain.RunID]int {
		out := map[domain.RunID]int{}
		for _, s := range w.recent(judge) {
			out[s.RunID]++
		}

		return out
	}

	if got := runsOf("spec-followed"); len(got) != 2 || got[builder.ID] != 1 || got[reviewer.ID] != 1 {
		t.Errorf("spec-followed (every agent) scores %v, want one each of %s and %s", got, builder.ID, reviewer.ID)
	}

	if got := runsOf("tests-added"); len(got) != 1 || got[builder.ID] != 1 {
		t.Errorf("tests-added (builder only) scores %v, want one of %s", got, builder.ID)
	}

	if got := runsOf("never"); len(got) != 0 {
		t.Errorf("a judge at sample 0 scores %v, want none", got)
	}
}

func TestLoop_SweepSkipsDisabledJudge(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	off := judge("spec-followed", 1)
	off.Enabled = false
	w.judges.Set(w.org, factory, version, off)
	w.finishedRun("builder", t0.Add(-time.Hour))

	l := w.loop(answer(followed))
	if err := l.Sweep(t.Context()); err != nil {
		t.Fatal(err)
	}

	if got := w.recent("spec-followed"); len(got) != 0 {
		t.Errorf("a disabled judge queued %d scores, want none", len(got))
	}

	if n, err := l.Work(t.Context()); err != nil || n != 0 {
		t.Errorf("Work = %d, %v; want nothing to work", n, err)
	}
}

func TestLoop_WorkScoresAndPasses(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		outcome string
		score   float64
		passed  bool
	}{
		{followed, 1, true},
		{"partly", 0.5, false},
	} {
		t.Run(c.outcome, func(t *testing.T) {
			t.Parallel()

			w := newWorld(t)
			w.judges.Set(w.org, factory, version, judge("spec-followed", 1))
			run := w.finishedRun("builder", t0.Add(-time.Hour))
			runner := answer(c.outcome)
			l := w.loop(runner)

			w.sweep(l)
			w.work(l, 1)

			got := w.recent("spec-followed")[0]
			if got.State != domain.JudgeScoreScored || got.Outcome != c.outcome || got.Score != c.score || got.Passed != c.passed {
				t.Errorf("score = %s %q %v passed %v, want scored %q %v passed %v", got.State, got.Outcome, got.Score, got.Passed, c.outcome, c.score, c.passed)
			}

			if got.Reason != "Nothing is missing." || got.CostUSD != 0.01 || got.JudgeRunID != "judge-run-1" {
				t.Errorf("score keeps reason %q, cost %v, judge run %q", got.Reason, got.CostUSD, got.JudgeRunID)
			}

			if tasks := runner.Tasks(); len(tasks) != 1 || tasks[0].Run.ID != run.ID || tasks[0].Judge.Slug != "spec-followed" {
				t.Errorf("runner got %+v, want one task of run %s", tasks, run.ID)
			}
		})
	}
}

func TestLoop_WorkRecordsUnknownOutcomeAsError(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.judges.Set(w.org, factory, version, judge("spec-followed", 1))
	w.finishedRun("builder", t0.Add(-time.Hour))

	runner := answer("perfect")
	l := w.loop(runner)

	if err := l.Sweep(t.Context()); err != nil {
		t.Fatal(err)
	}

	if _, err := l.Work(t.Context()); err != nil {
		t.Fatal(err)
	}

	got := w.recent("spec-followed")[0]
	if got.State != domain.JudgeScoreError || got.Outcome != "" || got.CostUSD != 0.01 {
		t.Errorf("score = %s %q cost %v, want error with no outcome and the run's cost", got.State, got.Outcome, got.CostUSD)
	}

	if n := len(runner.Tasks()); n != 1 {
		t.Errorf("runner called %d times, want 1: an unknown outcome is not retried", n)
	}
}

func TestLoop_WorkRetriesErrorOnce(t *testing.T) {
	t.Parallel()

	errBox := errors.New("box did not start")

	for _, c := range []struct {
		name      string
		failCalls int
		want      domain.JudgeScoreState
		cost      float64
	}{
		{"recovers", 1, domain.JudgeScoreScored, 0.03},
		{"fails twice", 99, domain.JudgeScoreError, 0.04},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld(t)
			w.judges.Set(w.org, factory, version, judge("spec-followed", 1))
			w.finishedRun("builder", t0.Add(-time.Hour))

			runner := porttest.NewJudgeRunner(func(_ context.Context, call int, _ ports.JudgeTask) (ports.JudgeRun, error) {
				if call <= c.failCalls {
					return ports.JudgeRun{CostUSD: 0.02}, errBox
				}

				return ports.JudgeRun{Outcome: followed, Reason: "Nothing is missing.", CostUSD: 0.01}, nil
			})
			l := w.loop(runner)

			if err := l.Sweep(t.Context()); err != nil {
				t.Fatal(err)
			}

			if _, err := l.Work(t.Context()); err != nil {
				t.Fatal(err)
			}

			if n := len(runner.Tasks()); n != 2 {
				t.Errorf("runner called %d times, want 2: one retry after the first error", n)
			}

			got := w.recent("spec-followed")[0]
			if got.State != c.want || math.Abs(got.CostUSD-c.cost) > 1e-9 {
				t.Errorf("score = %s cost %v, want %s cost %v", got.State, got.CostUSD, c.want, c.cost)
			}

			if c.want == domain.JudgeScoreError && got.Reason == "" {
				t.Error("an error score has no reason")
			}
		})
	}
}

func TestLoop_WorkReclaimsLostLease(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.judges.Set(w.org, factory, version, judge("spec-followed", 1))
	w.finishedRun("builder", t0.Add(-time.Hour))

	started, release := make(chan struct{}), make(chan struct{})
	stalled := porttest.NewJudgeRunner(func(context.Context, int, ports.JudgeTask) (ports.JudgeRun, error) {
		close(started)
		<-release

		return ports.JudgeRun{Outcome: "missed", Reason: "Too late.", CostUSD: 0.05}, nil
	})
	first, second := w.loop(stalled), w.loop(answer(followed))

	if err := first.Sweep(t.Context()); err != nil {
		t.Fatal(err)
	}

	type result struct {
		n   int
		err error
	}

	done := make(chan result, 1)

	go func() {
		n, err := first.Work(t.Context())
		done <- result{n, err}
	}()

	<-started
	w.clock.Advance(time.Hour) // past the first worker's lease

	if n, err := second.Work(t.Context()); err != nil || n != 1 {
		t.Fatalf("second Work = %d, %v; want the stalled score reclaimed", n, err)
	}

	close(release)

	if r := <-done; r.err != nil || r.n != 0 {
		t.Errorf("first Work = %d, %v; want a lost lease logged, nothing recorded", r.n, r.err)
	}

	got := w.recent("spec-followed")
	if len(got) != 1 || got[0].State != domain.JudgeScoreScored || got[0].Outcome != followed {
		t.Errorf("scores = %+v, want one scored by the second worker", got)
	}
}

func TestLoop_TryRecordsTries(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	off := judge("spec-followed", 0, "builder")
	off.Enabled = false // a try works whether or not the judge is on
	w.judges.Set(w.org, factory, version, off)

	var builders []domain.Run
	for i := range 4 {
		builders = append(builders, w.finishedRun("builder", t0.Add(-time.Duration(i+1)*time.Hour)))
	}

	w.finishedRun("reviewer", t0.Add(-time.Minute))

	runner := answer(followed)
	l := w.loop(runner)

	tries, err := l.Try(w.ctx(), factory, version, "spec-followed", 3)
	if err != nil {
		t.Fatal(err)
	}

	if len(tries) != 3 {
		t.Fatalf("Try queued %d tries, want 3", len(tries))
	}

	for i, s := range tries {
		if s.RunID != "" || s.ItemID != builders[i].ItemID {
			t.Errorf("try %d = run %q item %s, want no run and item %s", i, s.RunID, s.ItemID, builders[i].ItemID)
		}
	}

	w.work(l, 3)

	judged := map[domain.RunID]bool{}
	for _, task := range runner.Tasks() {
		judged[task.Run.ID] = true
	}

	for _, r := range builders[:3] {
		if !judged[r.ID] {
			t.Errorf("run %s of a tried item was not judged; judged %v", r.ID, judged)
		}
	}

	w.noProductionScores("spec-followed")
}

func TestLoop_TryOfUnknownJudge(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.judges.Set(w.org, factory, version)

	if _, err := w.loop(answer(followed)).Try(w.ctx(), factory, version, "spec-followed", 10); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Try of a judge the version lacks = %v, want ErrNotFound", err)
	}
}
