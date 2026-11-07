package api

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// maxTeardownRuns bounds the runs a teardown reads of one item.
const maxTeardownRuns = 10_000

// GetItemTeardown answers an item's teardown (domain.TeardownOf), worked out on each request from
// its spans, runs, events and touches and stored nowhere.
func (s *server) GetItemTeardown(ctx context.Context, req gen.GetItemTeardownRequestObject) (gen.GetItemTeardownResponseObject, error) {
	it, err := s.item(ctx, req.Item)
	if err != nil {
		return nil, err
	}

	spans, err := s.items.ItemSpans(ctx, it.ID)
	if err != nil {
		return nil, fmt.Errorf("item %s: spans: %w", it.ID, err)
	}

	runs, err := s.items.Runs(ctx, it.ID, maxTeardownRuns)
	if err != nil {
		return nil, fmt.Errorf("item %s: runs: %w", it.ID, err)
	}

	events, err := s.allItemEvents(ctx, it.ID)
	if err != nil {
		return nil, err
	}

	touches, err := s.items.ItemTouches(ctx, it.ID)
	if err != nil {
		return nil, fmt.Errorf("item %s: touches: %w", it.ID, err)
	}

	target, err := s.teardownTarget(ctx, it, runs)
	if err != nil {
		return nil, err
	}

	td := domain.TeardownOf(spans, runs, events, touches, target, s.clock.Now().UTC())

	return gen.GetItemTeardown200JSONResponse(s.teardownOf(ctx, td)), nil
}

// allItemEvents reads every event of an item, a page of the most the store answers at a time.
func (s *server) allItemEvents(ctx context.Context, id domain.ItemID) ([]domain.Event, error) {
	var (
		all   []domain.Event
		after int64
	)

	for {
		page, err := s.items.ItemEvents(ctx, id, after, maxEventLimit)
		if err != nil {
			return nil, fmt.Errorf("item %s: events after %d: %w", id, after, err)
		}

		all = append(all, page...)
		if len(page) < maxEventLimit {
			return all, nil
		}

		after = page[len(page)-1].ID
	}
}

// teardownTarget is the lead-time target of the item's class, the per-item budget of the agent
// at its station (the running run's), as the item page reads them, and the line's station ids in
// order; none when its factory is not loaded.
func (s *server) teardownTarget(ctx context.Context, it domain.Item, runs []domain.Run) (domain.TeardownTarget, error) {
	f, err := s.factories.Lookup(ctx, string(it.FactoryID))
	if errors.Is(err, ports.ErrNotFound) {
		return domain.TeardownTarget{}, nil
	}

	if err != nil {
		return domain.TeardownTarget{}, err
	}

	l := f.Line()

	agent := ""
	if st, ok := l.Station(it.Station); ok {
		agent, _ = st.AgentFor(it.Lane) // a lane without a route has no agent limits
	}

	for _, r := range runs {
		if r.Result.Status == domain.RunRunning {
			agent = r.Agent
		}
	}

	stations := make([]string, 0, len(l.Stations))
	for _, st := range l.Stations {
		stations = append(stations, st.ID)
	}

	return domain.TeardownTarget{LeadTime: l.ItemTarget(it.Class), BudgetUSD: deref(budgetOf(l, agent).PerItemUsd), Stations: stations}, nil
}

// teardownOf is td as the API answers it, every person among its touches named with one lookup.
func (s *server) teardownOf(ctx context.Context, td domain.Teardown) gen.Teardown {
	out := gen.Teardown{
		Open: td.Open, LeadTimeSeconds: wholeSeconds(td.LeadTime), CostUsd: td.CostUSD,
		Rework:     gen.TeardownRework{Loops: td.Rework.Loops, Seconds: wholeSeconds(td.Rework.Time), CostUsd: td.Rework.CostUSD},
		TouchCount: len(td.Touches), TouchWaitSeconds: wholeSeconds(td.TouchWait),
		Stations: make([]gen.TeardownStation, 0, len(td.Stations)),
		Total: gen.TeardownTime{
			WorkingSeconds: wholeSeconds(td.Total.Working), WaitingSeconds: wholeSeconds(td.Total.Waiting), TotalSeconds: wholeSeconds(td.Total.Total()),
		},
		Runs: make([]gen.TeardownRun, 0, len(td.Runs)),
		RunTotal: gen.TeardownRunTotal{
			Runs: td.RunTotal.Runs, Turns: td.RunTotal.Turns, Seconds: wholeSeconds(td.RunTotal.Took), CostUsd: td.RunTotal.CostUSD,
		},
		Loops:   make([]gen.ReworkLoop, 0, len(td.Loops)),
		Touches: make([]gen.TeardownTouch, 0, len(td.Touches)),
	}

	if td.Target.LeadTime > 0 {
		out.TargetSeconds = new(wholeSeconds(td.Target.LeadTime))
	}

	if td.Target.BudgetUSD > 0 {
		out.BudgetUsd = new(td.Target.BudgetUSD)
	}

	for _, st := range td.Stations {
		out.Stations = append(out.Stations, gen.TeardownStation{
			Station: st.Station, WorkingSeconds: wholeSeconds(st.Working), WaitingSeconds: wholeSeconds(st.Waiting), TotalSeconds: wholeSeconds(st.Total()),
		})
	}

	for _, r := range td.Runs {
		out.Runs = append(out.Runs, gen.TeardownRun{
			Id: string(r.ID), Station: r.Station, Agent: r.Agent, Model: r.Model, Turns: r.Turns, Seconds: wholeSeconds(r.Took),
			CostUsd: r.CostUSD, Status: gen.RunStatus(r.Status), Outcome: gen.TeardownRunOutcome(r.Outcome),
		})
	}

	for _, l := range td.Loops {
		out.Loops = append(out.Loops, gen.ReworkLoop{
			Cause: gen.ReworkLoopCause(l.Cause), Station: l.Station, To: optional(l.To), Reason: optional(l.Reason),
			At: l.At, Seconds: wholeSeconds(l.Time), CostUsd: l.CostUSD, EventId: l.EventID,
		})
	}

	for _, t := range td.Touches {
		out.Touches = append(out.Touches, gen.TeardownTouch{Touch: s.touchOf(t.Touch), WaitSeconds: wholeSeconds(t.Wait)})
	}

	s.nameActors(ctx, filers(out.Touches, func(t *gen.TeardownTouch) *gen.Actor { return &t.Touch.Actor })...)

	return out
}

// wholeSeconds is d in whole seconds.
func wholeSeconds(d time.Duration) int64 { return int64(d / time.Second) }
