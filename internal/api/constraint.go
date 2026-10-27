package api

import (
	"context"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
)

// The constraint over time (2.2-E §4.8, §8.1): the line's agent stations measured over a window
// from their leases, ranked by how busy they were. The engine does not use the detected
// constraint yet (drum-buffer-rope is deferred), so it is a read model computed on request,
// never cached: any goald answers the same.

const (
	// constraintCandidates is how many candidates the report pages through ("1 of 3").
	constraintCandidates = 3
	// constraintDays is the window the constraint is detected over when none is asked for.
	constraintDays = 7
)

// measured is a line's stations measured over [since, until).
type measured struct {
	times []domain.StationTimes
	// history is the factory's first lease; zero without any.
	history time.Time
	// cands are the agent stations, busiest first.
	cands []domain.Candidate
}

// measure reads the stations of line l over [since, until) and ranks its agent stations by
// their busy share, measured from the factory's first lease when that is later than since.
func (s *server) measure(ctx context.Context, l domain.Line, since, until time.Time) (measured, error) {
	times, history, err := s.items.StationTimes(ctx, l.FactoryID, since, until)
	if err != nil {
		return measured{}, err
	}

	m := measured{times: times, history: history}
	if !history.IsZero() {
		m.cands = l.Candidates(times, until.Sub(maxTime(since, history)), len(l.Stations))
	}

	return m, nil
}

// busyOf is the busy share of station among the candidates, nil when it is not one or before
// any lease history.
func (m measured) busyOf(station string) *float64 {
	for _, c := range m.cands {
		if c.Station == station {
			return new(c.Busy)
		}
	}

	return nil
}

// drumNow is the station line l is paced by now: configured, else detected over the last week,
// else the snapshot of loads.
func (s *server) drumNow(ctx context.Context, l domain.Line, loads map[string]domain.StationLoad, now time.Time) (string, error) {
	if _, ok := l.Station(l.Flow.Constraint); ok {
		return l.Flow.Constraint, nil
	}

	m, err := s.measure(ctx, l, lastDays(constraintDays, now).from, now)
	if err != nil {
		return "", err
	}

	return drumOf(l, loads, m.cands), nil
}

func (s *server) GetConstraintReport(ctx context.Context, req gen.GetConstraintReportRequestObject) (gen.GetConstraintReportResponseObject, error) {
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

	loads, err := s.stationLoads(ctx, l.FactoryID)
	if err != nil {
		return nil, err
	}

	until := r.until(now)

	m, err := s.measure(ctx, l, r.from, until)
	if err != nil {
		return nil, err
	}

	cands := m.cands[:min(constraintCandidates, len(m.cands))]
	station, source := l.ConstraintOf(cands, loads)

	out := gen.GetConstraintReport200JSONResponse{
		From: openapi_types.Date{Time: r.from}, To: openapi_types.Date{Time: r.to},
		Constraint: optional(station), Source: gen.ConstraintSource(source), Candidates: make([]gen.ConstraintCandidate, 0, len(cands)),
	}

	if !m.history.IsZero() {
		out.CoverageSince = new(m.history)
	}

	if _, ok := l.Station(l.Flow.Constraint); ok {
		out.Configured = new(l.Flow.Constraint)
	}

	for _, c := range cands {
		st, _ := l.Station(c.Station)
		out.Candidates = append(out.Candidates, candidateOf(c, st, until.Sub(r.from)))
	}

	return out, nil
}

// candidateOf reports candidate c of station st measured over a range of length span.
func candidateOf(c domain.Candidate, st domain.Station, span time.Duration) gen.ConstraintCandidate {
	t := c.Times
	out := gen.ConstraintCandidate{
		Station: c.Station, Slots: c.Slots, Agents: st.LaneAgents(), BusyRatio: c.Busy, Leases: t.Leases,
	}

	if span > 0 {
		out.AvgWaiting = t.Waiting.Seconds() / span.Seconds()
	}

	// What one more agent there would cost a day: what each slot cost a day over the range.
	if span > 0 && c.Slots > 0 && t.SpendUSD > 0 {
		out.SpendPerSlotDayUsd = new(t.SpendUSD / float64(c.Slots) / (span.Hours() / 24))
	}

	var wait, service time.Duration

	if t.Waiters > 0 {
		wait = t.Waiting / time.Duration(t.Waiters)
		out.MeanWaitMinutes = new(wait.Minutes())
	}

	if t.Leases > 0 {
		service = t.Busy / time.Duration(t.Leases)
		out.MeanServiceMinutes = new(service.Minutes())
	}

	if saved, ok := domain.WaitSaved(c.Busy, c.Slots, service, wait, t.Leases); ok {
		out.Estimate = &struct {
			ExtraSlotSavesHours float64 `json:"extra_slot_saves_hours"`
		}{ExtraSlotSavesHours: saved.Hours()}
	}

	return out
}
