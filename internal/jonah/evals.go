package jonah

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// EvalDeps are the ports the eval loop works through.
type EvalDeps struct {
	Orgs     Orgs
	Evals    ports.Evals
	Suites   ports.EvalSuites
	Runner   ports.EvalRunner
	Finished ports.FinishedRuns
	Clock    ports.Clock
	// Budget is the learning budget bake-offs are refused over; nil means none is kept.
	Budget LearningBudget
	// Drafts writes an applied bake-off arm's model into the factory's draft; nil refuses Apply.
	Drafts DraftModels
	// BakeOffModels are the models a bake-off that names none tries beside the agent's own
	// (GOAL_BAKEOFF_MODELS).
	BakeOffModels []string
	// Log may be nil.
	Log *slog.Logger
}

// Evals is Jonah's eval loop (ADR 0025 decision 4): it works the tries of queued eval runs under
// leases, each try an agent run of its case that EvalRunner makes, grades each by its case's
// check and finishes a run once every try is. Any goald instance may run it: a try whose worker
// died returns to the queue when its lease ends, and the loop keeps no state of its own.
type Evals struct {
	d   EvalDeps
	c   Config
	log *slog.Logger
}

// NewEvals returns an eval loop over d with the times of c.
func NewEvals(d EvalDeps, c Config) *Evals {
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &Evals{d: d, c: c, log: log}
}

// Detail reasons an eval result's detail gives, under "reason", besides the sentences that say why
// a try failed.
const (
	// EvalCancelled is the reason of a try that was running when its run was cancelled.
	EvalCancelled = "cancelled"
)

// maxEvalReason bounds each text an eval result's detail quotes (a run's failure, a contract
// problem, a judge's reason), in bytes, well under domain.MaxEvalDetail.
const maxEvalReason = 4 << 10

// estimateRuns is how many of an agent's newest finished runs price a case with no history.
const estimateRuns = 20

// Run works the queue at once and then each time every passes, until ctx ends; failures are
// logged and retried on the next round.
func (e *Evals) Run(ctx context.Context, every time.Duration) {
	t := e.d.Clock.NewTicker(every)
	defer t.Stop()

	for {
		if _, err := e.Work(ctx); err != nil && ctx.Err() == nil {
			e.log.WarnContext(ctx, "eval work", slog.Any("error", err))
		}

		select {
		case <-ctx.Done():
			return
		case <-t.C():
		}
	}
}

// Work claims and runs every claimable try of every live organization, one at a time, and returns
// how many it recorded. A try whose lease was lost meanwhile is logged and left to the claim that
// took it. An organization that fails does not hold up the others.
func (e *Evals) Work(ctx context.Context) (int, error) {
	orgs, err := e.d.Orgs.ActiveOrgs(ctx)
	if err != nil {
		return 0, fmt.Errorf("eval work: list organizations: %w", err)
	}

	var (
		worked int
		errs   []error
	)

	for _, org := range orgs {
		octx := domain.WithOrg(ctx, org)
		cache := &workCache{suites: map[domain.EvalRunID][]ports.EvalSuiteCase{}, arms: map[domain.EvalRunID][]domain.BakeOffArm{}}

		for ctx.Err() == nil {
			claimed, recorded, err := e.workOne(octx, cache)
			if err != nil {
				errs = append(errs, fmt.Errorf("eval work of org %s: %w", org, err))

				break
			}

			if !claimed {
				break
			}

			if recorded {
				worked++
			}
		}
	}

	return worked, errors.Join(errs...)
}

// workCache keeps what one Work read of each run: its cases, and a bake-off's arms.
type workCache struct {
	suites map[domain.EvalRunID][]ports.EvalSuiteCase
	arms   map[domain.EvalRunID][]domain.BakeOffArm
}

// workOne claims the next try and runs it: whether it claimed one and whether its result was
// recorded.
func (e *Evals) workOne(ctx context.Context, cache *workCache) (claimed, recorded bool, err error) {
	c, err := e.d.Evals.ClaimResult(ctx, e.d.Clock.Now(), attempts*e.c.Timeout+e.c.Margin)
	if errors.Is(err, ports.ErrNotFound) {
		return false, false, nil
	}

	if err != nil {
		return false, false, fmt.Errorf("claim eval try: %w", err)
	}

	run, err := e.d.Evals.GetRun(ctx, c.EvalRunID)
	if err != nil {
		// Left running: the lease ends and another claim takes the try again.
		return true, false, fmt.Errorf("eval run %s: %w", c.EvalRunID, err)
	}

	o, err := e.try(ctx, run, c, cache)
	if err != nil {
		return true, false, err
	}

	err = e.d.Evals.RecordResult(ctx, c, o)
	if errors.Is(err, ports.ErrLeaseLost) {
		e.log.WarnContext(ctx, "eval try lease lost", slog.String("eval_run_id", string(c.EvalRunID)), slog.String("case", c.Case), slog.Int("try", c.Try))

		return true, false, nil
	}

	if err != nil {
		return true, false, fmt.Errorf("record eval run %s case %s try %d: %w", c.EvalRunID, c.Case, c.Try, err)
	}

	e.log.InfoContext(ctx, "eval try recorded", slog.String("eval_run_id", string(c.EvalRunID)), slog.String("case", c.Case), slog.Int("try", c.Try),
		slog.String("state", string(o.State)))

	return true, true, e.finishIfDone(ctx, c.EvalRunID)
}

// finishIfDone sets run done once every try of it is finished. A run another instance finished
// or cancelled meanwhile is left as it is.
func (e *Evals) finishIfDone(ctx context.Context, id domain.EvalRunID) error {
	run, err := e.d.Evals.GetRun(ctx, id)
	if err != nil {
		return fmt.Errorf("eval run %s: %w", id, err)
	}

	if run.State.Final() || slices.ContainsFunc(run.Results, func(r domain.EvalResult) bool { return !r.State.Finished() }) {
		return nil
	}

	// A bake-off's summaries are written before it is done, so a done bake-off always has them;
	// writing them again replaces them.
	if run.Kind == domain.EvalKindBakeOff {
		if err := e.summarize(ctx, id); err != nil {
			return err
		}
	}

	_, err = e.d.Evals.FinishRun(ctx, id, domain.EvalRunDone, e.d.Clock.Now())
	if errors.Is(err, ports.ErrConflict) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("finish eval run %s: %w", id, err)
	}

	e.log.InfoContext(ctx, "eval run done", slog.String("eval_run_id", string(id)))

	return nil
}

// evalError is an error outcome with a reason.
func evalError(checkedBy, format string, a ...any) domain.EvalOutcome {
	return domain.EvalOutcome{State: domain.EvalError, CheckedBy: checkedBy, Detail: map[string]any{"reason": truncate(fmt.Sprintf(format, a...), maxEvalReason)}}
}

// try runs claimed try c of run and returns the outcome to record. A case the run's content no
// longer has, or has changed, is an error outcome, as is a bake-off try of an arm the bake-off
// lacks; a read that failed is an error, and the try is left to its lease.
func (e *Evals) try(ctx context.Context, run domain.EvalRun, c domain.EvalResult, cache *workCache) (domain.EvalOutcome, error) {
	if run.State.Final() {
		return evalError("", EvalCancelled), nil
	}

	var model string

	if run.Kind == domain.EvalKindBakeOff {
		m, ok, err := e.armModel(ctx, run.ID, c.Arm, cache)
		if err != nil {
			return domain.EvalOutcome{}, err
		}

		if !ok {
			return evalError("", "the bake-off has no arm %d", c.Arm), nil
		}

		model = m
	}

	suites := cache.suites

	cases, ok := suites[run.ID]
	if !ok {
		var err error

		cases, err = e.d.Suites.EvalSuite(ctx, run)
		if errors.Is(err, ports.ErrNotFound) {
			return evalError("", "the eval run's content %s is gone", run.ContentSHA256), nil
		}

		if err != nil {
			return domain.EvalOutcome{}, fmt.Errorf("eval suite of run %s: %w", run.ID, err)
		}

		suites[run.ID] = cases
	}

	i := slices.IndexFunc(cases, func(s ports.EvalSuiteCase) bool { return s.Case.Name == c.Case })
	if i < 0 {
		return evalError("", "case %s is not in the eval run's content", c.Case), nil
	}

	sc := cases[i]
	if sc.Case.SHA256 != c.CaseSHA256 {
		return evalError(sc.Case.CheckedBy(), "case %s changed since the run was queued: its file's hash is %s, not %s", c.Case, sc.Case.SHA256, c.CaseSHA256), nil
	}

	return e.runTry(ctx, ports.EvalTask{Run: run, Case: sc.Case, Try: c.Try, Judge: sc.Judge, Patch: sc.Patch, Kind: domain.TaskKindEval, Model: model})
}

// runTry runs t, once more after an infrastructure error, and grades it. The cost, turns and time
// of every attempt count. A try whose run was cancelled meanwhile is an error with reason cancelled
// and no retry; when ctx ends (goald stops) the try is left to its lease.
func (e *Evals) runTry(ctx context.Context, t ports.EvalTask) (domain.EvalOutcome, error) {
	checkedBy := t.Case.CheckedBy()

	var (
		cost  float64
		turns int
		last  error
	)

	start := e.d.Clock.Now()

	for range attempts {
		out, cancelled, err := e.attempt(ctx, t)
		cost += out.CostUSD
		turns += out.Turns

		if cancelled {
			o := evalError(checkedBy, EvalCancelled)
			o.RunID, o.CostUSD, o.Turns, o.Duration = out.RunID, cost, turns, e.d.Clock.Now().Sub(start)

			return o, nil
		}

		if ctx.Err() != nil {
			return domain.EvalOutcome{}, fmt.Errorf("eval run %s case %s try %d: %w", t.Run.ID, t.Case.Name, t.Try, ctx.Err())
		}

		if err != nil {
			last = err

			e.log.WarnContext(ctx, "eval try failed", slog.String("eval_run_id", string(t.Run.ID)), slog.String("case", t.Case.Name), slog.Int("try", t.Try),
				slog.Any("error", err))

			continue
		}

		o := grade(t, out)
		o.CostUSD, o.Turns, o.Duration = cost, turns, e.d.Clock.Now().Sub(start)

		return o, nil
	}

	o := evalError(checkedBy, "the try's run failed twice: %v", last)
	o.CostUSD, o.Turns, o.Duration = cost, turns, e.d.Clock.Now().Sub(start)

	return o, nil
}

// attempt is one try's run, bounded by the timeout. While it runs, the eval run is read every
// Poll, and the run's context is cancelled once the eval run is finished (cancelled): whether
// that happened.
func (e *Evals) attempt(ctx context.Context, t ports.EvalTask) (ports.EvalTry, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, e.c.Timeout)
	defer cancel()

	var (
		stopped atomic.Bool
		wg      sync.WaitGroup
	)

	done := make(chan struct{})
	tick := e.d.Clock.NewTicker(e.c.Poll)

	wg.Go(func() {
		defer tick.Stop()

		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-tick.C():
			}

			r, err := e.d.Evals.GetRun(ctx, t.Run.ID)
			if err != nil {
				if ctx.Err() == nil {
					e.log.WarnContext(ctx, "eval run poll", slog.String("eval_run_id", string(t.Run.ID)), slog.Any("error", err))
				}

				continue
			}

			if r.State.Final() {
				stopped.Store(true)
				cancel()

				return
			}
		}
	})

	out, err := e.d.Runner.RunEvalTry(ctx, t)

	close(done)
	wg.Wait()

	return out, stopped.Load(), err
}

// grade decides a try whose run answered by its case's check. A run that ended without a valid
// output fails whatever the check.
func grade(t ports.EvalTask, out ports.EvalTry) domain.EvalOutcome {
	g := grading{o: domain.EvalOutcome{State: domain.EvalPass, RunID: out.RunID, CheckedBy: t.Case.CheckedBy(), Detail: map[string]any{}}}

	switch {
	case out.Failure != "":
		g.fail("the agent's run ended without a valid output: %s", out.Failure)
	case t.Case.Judge != "":
		g.judge(t.Judge, out)
	case t.Case.ReferencePatch != "":
		g.referencePatch(t, out)
	case out.ContractProblem != "":
		g.fail("the output breaks the agent's contract")
		g.o.Detail["contract_problem"] = truncate(out.ContractProblem, maxEvalReason)
	}

	return g.o
}

// grading is the outcome a check builds: a pass until it fails or errs.
type grading struct{ o domain.EvalOutcome }

func (g *grading) fail(format string, a ...any) {
	g.o.State = domain.EvalFail
	g.o.Detail["reason"] = truncate(fmt.Sprintf(format, a...), maxEvalReason)
}

func (g *grading) err(format string, a ...any) {
	g.o.State = domain.EvalError
	g.o.Detail["reason"] = truncate(fmt.Sprintf(format, a...), maxEvalReason)
}

// judge passes the try when the judge's answer scores at least its pass mark; an answer that is
// not one of its outcomes is an error.
func (g *grading) judge(j domain.Judge, out ports.EvalTry) {
	jo, ok := j.Outcome(out.Outcome)
	if !ok {
		g.err("the judge answered %q, which is not one of its outcomes", truncate(out.Outcome, domain.MaxJudgeOutcome))

		return
	}

	g.o.Detail["outcome"], g.o.Detail["score"], g.o.Detail["pass_mark"] = jo.Label, jo.Score, j.PassMark
	g.o.Detail["judge_reason"] = truncate(out.Reason, maxEvalReason)

	if !j.Passes(jo.Score) {
		g.fail("the judge's score %v is below its pass mark %v", jo.Score, j.PassMark)
	}
}

// referencePatch passes the try when its change touches every file the reference patch touches
// (others too) and every check exited 0. A patch that touches no file is an error.
func (g *grading) referencePatch(t ports.EvalTask, out ports.EvalTry) {
	want := domain.PatchFiles(t.Patch)
	if len(want) == 0 {
		g.err("the reference patch %s touches no file", t.Case.ReferencePatch)

		return
	}

	var missing, failed []string

	for _, p := range want {
		if !slices.Contains(out.ChangedFiles, p) {
			missing = append(missing, p)
		}
	}

	for _, c := range out.Checks {
		if c.ExitCode != 0 {
			failed = append(failed, fmt.Sprintf("%s (exit %d)", c.Command, c.ExitCode))
		}
	}

	switch {
	case len(missing) > 0 && len(failed) > 0:
		g.fail("the change leaves out %d of the reference patch's files and %d checks failed", len(missing), len(failed))
	case len(missing) > 0:
		g.fail("the change leaves out %d of the reference patch's files", len(missing))
	case len(failed) > 0:
		g.fail("%d checks failed", len(failed))
	}

	if len(missing) > 0 {
		g.o.Detail["missing_files"] = missing
	}

	if len(failed) > 0 {
		g.o.Detail["failed_checks"] = failed
	}
}

// Cancel sets eval run id cancelled, for the organization in ctx. No further try of it is
// claimed, and Work stops a try of it that is running at its next poll. ErrNotFound when there is
// no such run, ErrConflict when it is already finished.
func (e *Evals) Cancel(ctx context.Context, id domain.EvalRunID) (domain.EvalRun, error) {
	r, err := e.d.Evals.FinishRun(ctx, id, domain.EvalRunCancelled, e.d.Clock.Now())
	if err != nil {
		return domain.EvalRun{}, fmt.Errorf("cancel eval run %s: %w", id, err)
	}

	return r, nil
}

// EvalEstimate is domain.EvalEstimate, what Estimate answers.
type EvalEstimate = domain.EvalEstimate

// Start queues an eval run of n's content for the organization in ctx, by: every case of cases,
// or only n.Case when it is set, each with its tries, estimated as Estimate prices them. It
// returns the queued run and the estimate. domain.ErrInvalid when n.Case is not one of cases or
// the run does not pass domain.NewEvalRun.Check.
func (e *Evals) Start(ctx context.Context, n domain.NewEvalRun, cases []domain.EvalCaseSpec, by domain.Actor) (domain.EvalRun, EvalEstimate, error) {
	if n.Case != "" {
		i := slices.IndexFunc(cases, func(c domain.EvalCaseSpec) bool { return c.Name == n.Case })
		if i < 0 {
			return domain.EvalRun{}, EvalEstimate{}, fmt.Errorf("start eval run: case %q is not one of the content's cases: %w", n.Case, domain.ErrInvalid)
		}

		cases = cases[i : i+1]
	}

	est, err := e.Estimate(ctx, n.FactoryID, cases)
	if err != nil {
		return domain.EvalRun{}, EvalEstimate{}, err
	}

	n.EstimateUSD, n.Cases = est.USD, make([]domain.EvalCase, 0, len(cases))
	for _, c := range cases {
		n.Cases = append(n.Cases, domain.EvalCase{Name: c.Name, SHA256: c.SHA256, Tries: c.Tries})
	}

	r, err := e.d.Evals.CreateRun(ctx, n, by)
	if err != nil {
		return domain.EvalRun{}, EvalEstimate{}, fmt.Errorf("start eval run: %w", err)
	}

	e.log.InfoContext(ctx, "eval run queued", slog.String("eval_run_id", string(r.ID)), slog.String("trigger", string(n.Trigger)),
		slog.Int("cases", len(n.Cases)))

	return r, est, nil
}

// Estimate prices running cases of factory, for the organization in ctx ("Run all, about $X"):
// each try at the mean cost of the case's last finished tries or, for a case with none, at the
// median cost of its agent's newest finished runs. A case with neither is unpriced.
func (e *Evals) Estimate(ctx context.Context, factory domain.FactoryID, cases []domain.EvalCaseSpec) (EvalEstimate, error) {
	hist, err := e.d.Evals.CaseHistory(ctx, factory)
	if err != nil {
		return EvalEstimate{}, fmt.Errorf("estimate eval run: %w", err)
	}

	perCase := make(map[string]float64, len(hist))

	for _, h := range hist {
		if len(h.Tries) == 0 {
			continue
		}

		var sum float64
		for _, t := range h.Tries {
			sum += t.CostUSD
		}

		perCase[h.Case] = sum / float64(len(h.Tries))
	}

	agents := map[string]agentCost{}

	var out EvalEstimate

	for _, c := range cases {
		if cost, ok := perCase[c.Name]; ok {
			out.USD += cost * float64(c.Tries)

			continue
		}

		a, read := agents[c.Agent]
		if !read {
			if a, err = e.agentCost(ctx, factory, c.Agent); err != nil {
				return EvalEstimate{}, err
			}

			agents[c.Agent] = a
		}

		if !a.ok {
			out.Unpriced = append(out.Unpriced, c.Name)

			continue
		}

		out.USD += a.usd * float64(c.Tries)
	}

	return out, nil
}

// agentCost is what an agent's run costs, about, and whether it has runs to say.
type agentCost struct {
	usd float64
	ok  bool
}

// agentCost is the median cost of agent's newest finished runs in factory.
func (e *Evals) agentCost(ctx context.Context, factory domain.FactoryID, agent string) (agentCost, error) {
	runs, err := e.d.Finished.Latest(ctx, ports.FinishedRunQuery{Factory: factory, Agents: []string{agent}, Limit: estimateRuns})
	if err != nil {
		return agentCost{}, fmt.Errorf("estimate eval run: finished runs of agent %s: %w", agent, err)
	}

	if len(runs) == 0 {
		return agentCost{}, nil
	}

	return agentCost{usd: medianCost(runs), ok: true}, nil
}

// medianCost is the median of runs' costs; runs is not empty.
func medianCost(runs []domain.Run) float64 {
	costs := make([]float64, len(runs))
	for i, r := range runs {
		costs[i] = r.Result.CostUSD
	}

	sort.Float64s(costs)

	n := len(costs)
	if n%2 == 1 {
		return costs[n/2]
	}

	return (costs[n/2-1] + costs[n/2]) / 2
}
