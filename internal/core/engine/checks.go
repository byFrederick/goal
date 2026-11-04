package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// errCheckLeaseLost is the cause of a check run stopped because another instance's expiry failed
// its check: nothing it does is recorded.
var errCheckLeaseLost = errors.New("the check's lease was lost")

// queueCheck records t's firing for its schedule's instant key as a checked firing that queues a
// check (ADR 0040 decision 19), or returns the one already recorded for key. It is skipped while
// the line is paused, and while the weekly budget is spent when t pauses on it; the store skips it
// "still checking" while t's check is queued or running (ports.Intake.Fire).
func (e *Engine) queueCheck(ctx context.Context, l domain.Line, t domain.Trigger, key string, now time.Time) (domain.TriggerFiring, error) {
	ctx = domain.WithOrg(ctx, l.OrgID)

	if f, ok, err := e.intake.Firing(ctx, l.FactoryID, t.Name, key); err != nil || ok {
		return f, err
	}

	f := domain.TriggerFiring{
		FactoryID: l.FactoryID, Trigger: t.Name, Source: domain.TriggerSchedule, EventKey: domain.OnSchedule, IdempotencyKey: key,
		Outcome: domain.FiringChecked, CheckState: domain.CheckQueued, At: now,
	}

	switch {
	case l.Paused:
		f.Outcome, f.CheckState, f.Reason = domain.FiringSkipped, "", domain.CheckLinePaused
	case t.Limits.PauseOnBudget:
		b, err := e.weeklyBudget(ctx, l, now)
		if err != nil {
			return domain.TriggerFiring{}, err
		}

		if b.Exhausted {
			f.Outcome, f.CheckState = domain.FiringSkipped, ""
			f.Reason = fmt.Sprintf("%s: the weekly budget is spent ($%.2f of $%.2f)", domain.StepBudget, b.SpentUSD, b.WeeklyUSD)
		}
	}

	got, fresh, err := e.intake.Fire(ctx, f, nil, domain.FireLimits{})
	if err != nil {
		return domain.TriggerFiring{}, fmt.Errorf("queue check: %w", err)
	}

	if fresh {
		e.log.InfoContext(ctx, "trigger fired", slog.String("org_id", string(l.OrgID)), slog.String("factory", string(l.FactoryID)),
			slog.String("trigger", t.Name), slog.String("outcome", string(got.Outcome)), slog.String("reason", got.Reason))
	}

	return got, nil
}

// hasChecks reports whether l has a trigger with a check.
func hasChecks(l domain.Line) bool {
	return slices.ContainsFunc(l.Triggers, func(t domain.Trigger) bool { return t.Check != nil })
}

// workChecks fails the expired checks of every line in lines that has checks, then, for each line
// in claimable, leases its queued checks into free slots (ports.Intake.ClaimCheck) and runs each
// in the background (runTriggerCheck), outside the schedule lock. A factory at its slot cap, or this
// instance at MaxRuns, leaves the rest queued. Tick runs it on every instance.
func (e *Engine) workChecks(ctx context.Context, lines, claimable map[domain.FactoryID]domain.Line) error {
	var errs []error

	now := e.clock.Now()

	for _, id := range slices.Sorted(maps.Keys(lines)) {
		l := lines[id]
		if !hasChecks(l) {
			continue
		}

		lctx := domain.WithOrg(ctx, l.OrgID)

		n, err := e.intake.ExpireChecks(lctx, id, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("checks: factory %s: expire: %w", id, err))
		}

		if n > 0 {
			e.log.WarnContext(lctx, "checks expired", slog.String("org_id", string(l.OrgID)), slog.String("factory", string(id)), slog.Int("checks", n))
		}

		if _, ok := claimable[id]; ok {
			errs = append(errs, e.claimChecks(lctx, l, now))
		}
	}

	return errors.Join(errs...)
}

// claimChecks leases l's queued checks until none is left, a cap is reached or MaxRuns is.
func (e *Engine) claimChecks(ctx context.Context, l domain.Line, now time.Time) error {
	for ctx.Err() == nil {
		if e.cfg.MaxRuns > 0 && e.running.Load() >= int64(e.cfg.MaxRuns) {
			return nil
		}

		f, lease, err := e.intake.ClaimCheck(ctx, l.FactoryID, e.holder, now, e.cfg.LeaseTTL)

		switch {
		case errors.Is(err, ports.ErrNotFound), errors.Is(err, ports.ErrOrgFull), errors.Is(err, ports.ErrFactoryFull):
			return nil
		case err != nil:
			return fmt.Errorf("checks: factory %s: claim: %w", l.FactoryID, err)
		}

		e.log.InfoContext(ctx, "check claimed", slog.String("org_id", string(l.OrgID)), slog.String("factory", string(l.FactoryID)),
			slog.String("trigger", f.Trigger), slog.String("firing", f.ID))

		runCtx, run := e.trackCheck(ctx, lease)

		e.running.Add(1)
		e.runs.Go(func() {
			defer e.running.Add(-1)
			defer e.untrackCheck(run)

			e.runTriggerCheck(runCtx, l, lease)
		})
	}

	return nil
}

// trackCheck registers the check this instance runs under lease, so a budget stop cancels it
// (cancelRuns), and returns its context.
func (e *Engine) trackCheck(ctx context.Context, lease domain.CheckLease) (context.Context, *activeRun) {
	runCtx, cancel := context.WithCancelCause(ctx)
	r := &activeRun{factory: lease.FactoryID, cancel: cancel}

	e.activeMu.Lock()
	e.checks[lease.FiringID] = r
	e.activeMu.Unlock()

	return runCtx, r
}

// untrackCheck forgets r and releases its context.
func (e *Engine) untrackCheck(r *activeRun) {
	e.activeMu.Lock()
	for id, c := range e.checks {
		if c == r {
			delete(e.checks, id)
		}
	}
	e.activeMu.Unlock()

	r.cancel(nil)
}

// runTriggerCheck runs the check lease holds as a run of kind check (RunTask, with checkRequest) while
// heartbeating its lease, and records how it ended (FinishCheck): done with its answer's
// decisions and the state it saved when the run succeeded with an answer that passes
// domain.ParseCheckAnswer, and failed with the reason otherwise: the trigger is gone, the request
// could not be made, the run failed, timed out or was cancelled (a budget stop), or its answer
// breaks the contract. A lost lease records nothing: the expiry that took it failed the check.
func (e *Engine) runTriggerCheck(ctx context.Context, l domain.Line, lease domain.CheckLease) {
	log := e.log.With(slog.String("org_id", string(l.OrgID)), slog.String("factory", string(l.FactoryID)),
		slog.String("trigger", lease.Trigger), slog.String("firing", lease.FiringID))

	runCtx, stop := context.WithCancelCause(ctx)
	defer stop(nil)

	beatCtx, stopBeat := context.WithCancel(runCtx)
	beat := make(chan struct{})

	go func() {
		defer close(beat)
		e.beatCheck(beatCtx, lease, stop)
	}()

	res := e.triggerCheck(runCtx, l, lease)

	stopBeat()
	<-beat

	if errors.Is(context.Cause(runCtx), errCheckLeaseLost) {
		log.WarnContext(ctx, "check lease lost")
		return
	}

	res.At = e.clock.Now()

	fctx, cancel := cleanup(ctx)
	defer cancel()

	if err := e.intake.FinishCheck(fctx, lease, res); err != nil {
		log.ErrorContext(fctx, "record check", slog.Any("error", err))
		return
	}

	log.InfoContext(fctx, "check finished", slog.String("state", string(res.State)), slog.String("reason", res.Reason),
		slog.Float64("cost_usd", res.CostUSD))
}

// triggerCheck runs lease's check and returns its result, At unset.
func (e *Engine) triggerCheck(ctx context.Context, l domain.Line, lease domain.CheckLease) domain.CheckResult {
	t, ok := l.Trigger(lease.Trigger)
	if !ok || t.Check == nil {
		return checkFailed(nil, "trigger %s has no check in version %d", lease.Trigger, l.Number)
	}

	req, err := e.checkRequest(ctx, l, t)
	if err != nil {
		return checkFailed(nil, "%v", err)
	}

	run, err := e.RunTask(ctx, req)

	switch {
	case ctx.Err() != nil:
		return checkFailed(&run, "cancelled: %v", context.Cause(ctx))
	case err != nil:
		return checkFailed(&run, "the check run failed: %v", err)
	case run.Status != domain.RunSucceeded:
		return checkFailed(&run, "the check run ended %s: %s", run.Status, run.Error)
	}

	a, err := domain.ParseCheckAnswer(run.Output)
	if err != nil {
		return checkFailed(&run, "%v", err)
	}

	return domain.CheckResult{State: domain.CheckDone, RunID: run.RunID, CostUSD: run.CostUSD, Decisions: a.Decisions(), Saved: a.State}
}

// checkFailed is a failed result with the reason, and run's record and cost when it ran.
func checkFailed(run *domain.RunResult, format string, a ...any) domain.CheckResult {
	r := domain.CheckResult{State: domain.CheckFailed, Reason: fmt.Sprintf(format, a...)}
	if run != nil {
		r.RunID, r.CostUSD = run.RunID, run.CostUSD
	}

	return r
}

// checkRequest is t's check as a RunRequest of kind check: its agent, with the organization
// policy applied as to a station's run (applyPolicy), on the agent's environment, with no item,
// station, lease, repository or knowledge; check.maxUsd is its spend limit (the gateway's bundle
// says it) and check.timeout its time limit, within the policy's box time limit. An error when
// the agent or its environment is missing, the policy cannot be read or refuses its model.
func (e *Engine) checkRequest(ctx context.Context, l domain.Line, t domain.Trigger) (domain.RunRequest, error) {
	a, err := agentSpec(l, t.Check.Agent)
	if err != nil {
		return domain.RunRequest{}, err
	}

	env, err := agentEnvironment(l, a)
	if err != nil {
		return domain.RunRequest{}, err
	}

	c := claimed{line: l, agent: a, env: env}

	refusal, err := e.applyPolicy(ctx, &c)
	if err != nil {
		return domain.RunRequest{}, err
	}

	if refusal != "" {
		return domain.RunRequest{}, errors.New(refusal)
	}

	r := c.request("")
	r.Kind, r.Bundle, r.Trigger, r.Lease, r.Repository, r.Branch, r.Base, r.Knowledge = domain.RunKindCheck, domain.TaskKindCheck, t.Name, nil, domain.Repository{}, "", "", nil
	r.Timeout = t.Check.Timeout
	if c.boxLimit > 0 {
		r.Timeout = min(r.Timeout, c.boxLimit)
	}

	return r, nil
}

// beatCheck extends lease every HeartbeatEvery until ctx ends; when the lease is lost it stops
// the check with errCheckLeaseLost.
func (e *Engine) beatCheck(ctx context.Context, lease domain.CheckLease, stop context.CancelCauseFunc) {
	t := e.clock.NewTicker(e.cfg.HeartbeatEvery)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C():
		}

		err := e.intake.HeartbeatCheck(ctx, lease, e.clock.Now().Add(e.cfg.LeaseTTL))

		switch {
		case errors.Is(err, ports.ErrLeaseLost):
			stop(errCheckLeaseLost)
			return
		case err != nil && ctx.Err() == nil:
			e.log.WarnContext(ctx, "heartbeat check", slog.String("firing", lease.FiringID), slog.Any("error", err))
		}
	}
}
