package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// errAgentNotFound is an agent the factory's definition does not declare: 404 agent-not-found.
var errAgentNotFound = fmt.Errorf("agent %w", ports.ErrNotFound)

// The agent page shows the most frequent reasons work was sent back (and the
// domain.MaxDeniedTools most denied tools).
const maxReasons = 5

// GetAgentStats measures one agent of a factory over a range of UTC days (the last 7 by default):
// its tasks, how many were sent back and why, its runs' turns and spend, and the tool calls they
// were denied, with the stations it works at.
func (s *server) GetAgentStats(ctx context.Context, req gen.GetAgentStatsRequestObject) (gen.GetAgentStatsResponseObject, error) {
	now := s.clock.Now().UTC()

	r, err := rangeOf(nil, req.Params.From, req.Params.To, now)
	if err != nil {
		return nil, err
	}

	f, err := s.factory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	l := f.Line()

	agent, ok := l.Agent(req.Agent)
	if !ok {
		return nil, fmt.Errorf("factory %s has no %q: %w", f.Metadata.Name, req.Agent, errAgentNotFound)
	}

	drum, err := s.drum(ctx, l, now)
	if err != nil {
		return nil, err
	}

	st, err := s.items.AgentStats(ctx, l.FactoryID, agent.Name, r.from, r.until(now))
	if err != nil {
		return nil, err
	}

	model := agent.Model
	if model == "" {
		model = l.Runtime.Model
	}

	out := agentStatsOf(st)
	out.Agent, out.Model = agent.Name, optional(model)
	out.From, out.To = openapi_types.Date{Time: r.from}, openapi_types.Date{Time: r.to}

	for _, station := range l.Stations {
		if slices.Contains(station.LaneAgents(), agent.Name) {
			out.Stations = append(out.Stations, gen.AgentStation{Id: station.ID, Slots: station.Capacity(), Constraint: station.ID == drum})
		}
	}

	return gen.GetAgentStats200JSONResponse(out), nil
}

// agentStatsOf is the measures part of the answer: ratios and percentiles only where there is
// something to measure, the most frequent reasons and denied tools.
func agentStatsOf(st domain.AgentStats) gen.AgentStats {
	out := gen.AgentStats{
		Stations: []gen.AgentStation{}, TasksFinished: st.TasksFinished, SpendUsd: st.SpendUSD,
		SentBack: gen.SentBack{Count: st.SentBack}, Reasons: []gen.SendBackReason{},
		DeniedToolCalls: gen.DeniedToolCalls{Tools: []gen.DeniedTool{}},
	}

	if ratio, ok := st.SentBackRatio(); ok {
		out.SentBack.Ratio = &ratio
	}

	if per, ok := st.SpendPerTask(); ok {
		out.SpendPerTaskUsd = &per
	}

	if st.SucceededRuns > 0 {
		out.MedianTurns = &st.MedianTurns
	}

	if st.TasksFinished > 0 {
		out.TaskTimeP85Minutes = new(st.TaskTimeP85.Minutes())
	}

	for _, t := range st.Reasons[:min(len(st.Reasons), maxReasons)] {
		out.Reasons = append(out.Reasons, gen.SendBackReason{Label: t.Label, Count: t.Count})
	}

	out.DeniedToolCalls.Count = st.DeniedCalls
	for _, t := range st.Denied {
		out.DeniedToolCalls.Tools = append(out.DeniedToolCalls.Tools, gen.DeniedTool{Tool: t.Label, Count: t.Count})
	}

	return out
}

// agentProblem is the problem of an unknown agent, when err is one.
func agentProblem(err error) (gen.Problem, bool) {
	if !errors.Is(err, errAgentNotFound) {
		return gen.Problem{}, false
	}

	p := detailed(http.StatusNotFound, err)
	p.Code = optional("agent-not-found")

	return p, true
}
