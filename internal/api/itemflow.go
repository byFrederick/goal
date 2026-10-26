package api

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// itemFlow is how an item flows (Item.flow): its spans, the lead-time target of its class, where
// a send-back from its station goes, the task limits there and the run in progress. The parts
// that come from the definition are left out when its factory is not loaded.
func (s *server) itemFlow(ctx context.Context, it domain.Item) (*gen.ItemFlow, error) {
	spans, err := s.items.ItemSpans(ctx, it.ID)
	if err != nil {
		return nil, fmt.Errorf("item %s: spans: %w", it.ID, err)
	}

	run, err := s.items.CurrentRun(ctx, it.ID)
	if err != nil {
		return nil, fmt.Errorf("item %s: current run: %w", it.ID, err)
	}

	flow := &gen.ItemFlow{Spans: make([]gen.ItemSpan, 0, len(spans))}
	for _, sp := range spans {
		flow.Spans = append(flow.Spans, spanOf(sp))
	}

	f, err := s.factories.Lookup(ctx, string(it.FactoryID))
	if errors.Is(err, ports.ErrNotFound) {
		return flow, nil
	}

	if err != nil {
		return nil, err
	}

	l := f.Line()

	if target := l.ItemTarget(it.Class); target > 0 {
		seconds := int64(target.Seconds())
		flow.TargetSeconds = &seconds
	}

	if it.State != domain.StateDone && it.State != domain.StateCancelled {
		if to, err := l.ReworkTarget(it.Station); err == nil {
			flow.ReworkTarget = &to.ID
		}
	}

	agent := ""
	if st, ok := l.Station(it.Station); ok {
		agent, _ = st.AgentFor(it.Lane) // a lane without a route has no agent limits
	}

	if run != nil {
		agent = run.Run.Agent
		flow.CurrentRun = currentRunOf(f, *run)
	}

	flow.Budget = budgetOf(l, agent)

	return flow, nil
}

func spanOf(sp domain.Span) gen.ItemSpan {
	out := gen.ItemSpan{
		Station: sp.Station, State: gen.ItemState(sp.State), ReworkRound: sp.ReworkRound,
		ReworkBy: optional(sp.ReworkBy), EnteredAt: sp.EnteredAt,
	}

	if !sp.LeftAt.IsZero() {
		out.LeftAt = &sp.LeftAt
	}

	return out
}

// budgetOf is the limits of one task of agent at the line (the runtime's without an agent).
func budgetOf(l domain.Line, agent string) gen.ItemBudget {
	turns, usd := l.Runtime.MaxTurnsPerTask, l.Runtime.PerItemUSD
	if spec, ok := l.Agent(agent); ok {
		turns, usd = spec.Limits(l.Runtime)
	}

	var b gen.ItemBudget

	if turns > 0 {
		b.MaxTurnsPerTask = &turns
	}

	if usd > 0 {
		b.PerItemUsd = &usd
	}

	return b
}

// currentRunOf describes the run in progress, with what its station checks on submit.
func currentRunOf(f *config.Factory, r domain.CurrentRun) *gen.CurrentRun {
	out := &gen.CurrentRun{
		Id: string(r.Run.ID), Agent: r.Run.Agent, Slot: r.Slot + 1, Model: r.Run.Model, Turn: r.Turns,
		StartedAt: r.Run.StartedAt, Checks: []string{},
	}

	for _, st := range f.Spec.Board.Stations {
		if st.ID != r.Run.Station {
			continue
		}

		out.OutputContract = optional(st.Output)
		for _, c := range st.Checks {
			out.Checks = append(out.Checks, checkLabel(c))
		}
	}

	return out
}

// checkLabel names a station check as the Item screen shows it: "schema", "2+ evidence links",
// or a command check's command line.
func checkLabel(c config.StationCheck) string {
	switch c.Kind {
	case config.CheckEvidenceLinks:
		if c.Min > 0 {
			return strconv.Itoa(c.Min) + "+ evidence links"
		}

		return "evidence links"
	case config.CheckCommand:
		return c.Run
	default:
		return c.Kind
	}
}
