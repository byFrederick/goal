package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/domain"
)

// budgetGauge is the gauge a factory's budget transitions carry, 1 while its weekly budget is
// used up and 0 once it is not. Until goald exports OpenTelemetry metrics it is a log
// attribute of budget_exhausted and budget_resumed, from which a log pipeline derives it.
const budgetGauge = "goal_factory_budget_exhausted"

// reasonBudget is the reason on the agent_cancelled event of a run the weekly budget stopped.
const reasonBudget = "budget"

// errBudgetStop is the cause of a run cancelled because its factory's weekly budget is used up
// and its policy is stop.
var errBudgetStop = errors.New("the weekly budget is used up")

// activeRun is a run this instance is working on, which it can cancel.
type activeRun struct {
	factory domain.FactoryID
	lease   domain.Lease
	cancel  context.CancelCauseFunc
}

// budgets enforces the weekly budgets for one tick and returns the lines that may claim agent
// work. For each line with a budget it reads the spend of the window once. A line whose budget
// is used up claims nothing; under BudgetStop the runs this instance has in progress at it are
// cancelled too, and their items go back to their queue. Queue releases and human decisions
// are not the budget's business: approvals and merges continue. A line whose spend cannot be
// read claims nothing this tick and keeps its last budget state.
func (e *Engine) budgets(ctx context.Context, lines map[domain.FactoryID]domain.Line) (map[domain.FactoryID]domain.Line, error) {
	now := e.clock.Now()
	claimable := make(map[domain.FactoryID]domain.Line, len(lines))

	var errs []error

	for _, id := range slices.Sorted(maps.Keys(lines)) {
		l := lines[id]

		b, err := e.weeklyBudget(ctx, l, now)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		e.noteBudget(ctx, l, b)

		switch {
		case !b.Exhausted:
			claimable[id] = l
		case b.Policy == domain.BudgetStop:
			e.cancelRuns(id, errBudgetStop)
		}
	}

	e.forgetBudgets(lines)

	return claimable, errors.Join(errs...)
}

// weeklyBudget measures the line's weekly budget at now; a line without one reads nothing.
func (e *Engine) weeklyBudget(ctx context.Context, l domain.Line, now time.Time) (domain.BudgetStatus, error) {
	if !l.Runtime.HasWeeklyBudget() {
		return l.Runtime.WeeklyBudget(0, now), nil
	}

	spent, err := e.store.SpendSince(domain.WithOrg(ctx, l.OrgID), l.FactoryID, domain.BudgetSince(now))
	if err != nil {
		return domain.BudgetStatus{}, fmt.Errorf("factory %s: read the weekly spend: %w", l.FactoryID, err)
	}

	return l.Runtime.WeeklyBudget(spent, now), nil
}

// noteBudget logs a factory's budget transitions: budget_exhausted when its budget is used up
// (also the first time this instance sees it so), budget_resumed when it no longer is.
func (e *Engine) noteBudget(ctx context.Context, l domain.Line, b domain.BudgetStatus) {
	e.budgetMu.Lock()
	was := e.exhausted[l.FactoryID]

	if b.Exhausted {
		e.exhausted[l.FactoryID] = true
	} else {
		delete(e.exhausted, l.FactoryID)
	}
	e.budgetMu.Unlock()

	if was == b.Exhausted {
		return
	}

	msg, gauge := "budget_resumed", 0
	if b.Exhausted {
		msg, gauge = "budget_exhausted", 1
	}

	e.log.InfoContext(ctx, msg, slog.String("org_id", string(l.OrgID)), slog.String("factory", string(l.FactoryID)),
		slog.Float64("spend_usd", b.SpentUSD), slog.Float64("weekly_usd", b.WeeklyUSD), slog.String("policy", string(b.Policy)),
		slog.Time("since", b.Since), slog.Int(budgetGauge, gauge))
}

// forgetBudgets drops the budget state of factories that are no longer loaded.
func (e *Engine) forgetBudgets(lines map[domain.FactoryID]domain.Line) {
	e.budgetMu.Lock()
	defer e.budgetMu.Unlock()

	maps.DeleteFunc(e.exhausted, func(id domain.FactoryID, _ bool) bool {
		_, loaded := lines[id]
		return !loaded
	})
}

// track registers the run this instance is about to work on under lease and returns its
// context, which cancelRuns and stopRun cancel, and the run itself, which untrack must be given
// when the work ends. A later claim of the same item replaces the entry: the earlier run, taken
// from its item by a person, may still be winding down.
func (e *Engine) track(ctx context.Context, lease domain.Lease) (context.Context, *activeRun) {
	runCtx, cancel := context.WithCancelCause(ctx)
	r := &activeRun{factory: lease.FactoryID, lease: lease, cancel: cancel}

	e.activeMu.Lock()
	e.active[lease.ItemID] = r
	e.activeMu.Unlock()

	return runCtx, r
}

// untrack forgets r unless a later claim of its item replaced it, and releases r's context
// only, never that of the item's next run.
func (e *Engine) untrack(r *activeRun) {
	e.activeMu.Lock()
	if e.active[r.lease.ItemID] == r {
		delete(e.active, r.lease.ItemID)
	}
	e.activeMu.Unlock()

	r.cancel(nil)
}

// cancelRuns cancels, with cause, every run this instance is working on at factory. Each run's
// work returns its item to the queue (release).
func (e *Engine) cancelRuns(factory domain.FactoryID, cause error) {
	e.activeMu.Lock()
	defer e.activeMu.Unlock()

	for _, r := range e.active {
		if r.factory == factory {
			r.cancel(cause)
		}
	}
}

// errUnloaded is the cause of a run cancelled because its factory is no longer loaded.
var errUnloaded = errors.New("the factory is no longer loaded")

// releaseEvent is the event of an item returned to its queue because its run's context ended:
// agent_cancelled with reason budget when the weekly budget stopped it, released with reason
// unloaded when its factory was unloaded, else released at shutdown.
func releaseEvent(it domain.Item, cause error, at time.Time) domain.Event {
	switch {
	case errors.Is(cause, errBudgetStop):
		return event(it, domain.EventAgentCancelled, at, domain.ActorEngine, map[string]any{keyReason: reasonBudget})
	case errors.Is(cause, errUnloaded):
		return event(it, domain.EventReleased, at, domain.ActorEngine, map[string]any{keyReason: "unloaded"})
	}

	return event(it, domain.EventReleased, at, domain.ActorEngine, map[string]any{keyReason: "shutdown"})
}
