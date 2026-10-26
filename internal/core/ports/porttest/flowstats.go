package porttest

import (
	"context"
	"maps"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.FlowStats = (*Store)(nil)

// ItemSpans implements ports.FlowStats.
func (s *Store) ItemSpans(ctx context.Context, id domain.ItemID) ([]domain.Span, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.visible(id, org); !ok {
		return []domain.Span{}, nil
	}

	return slices.Clone(s.spans[id]), nil
}

// CurrentRun implements ports.FlowStats.
func (s *Store) CurrentRun(ctx context.Context, id domain.ItemID) (*domain.CurrentRun, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	l, leased := s.leases[id]
	if _, ok := s.visible(id, org); !ok || !leased {
		return nil, nil
	}

	var run *domain.Run

	for _, r := range s.runs {
		if r.ItemID == id && r.Result.Status == domain.RunRunning && (run == nil || r.StartedAt.After(run.StartedAt)) {
			run = r
		}
	}

	if run == nil {
		return nil, nil
	}

	cur := &domain.CurrentRun{Run: *run, Slot: l.Slot}

	for _, e := range s.events {
		if e.RunID == run.ID && e.Kind == domain.EventUsage {
			cur.Turns++
		}
	}

	return cur, nil
}

// LiveRuns implements ports.FlowStats.
func (s *Store) LiveRuns(ctx context.Context, factory domain.FactoryID) (map[domain.ItemID]domain.LiveRun, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	running := s.liveRuns(org, factory)
	out := make(map[domain.ItemID]domain.LiveRun, len(running))

	for id, r := range running {
		var live domain.LiveRun

		// Events are kept in the order they were appended, so the last match is the newest.
		for _, e := range s.events {
			if e.RunID != r.ID {
				continue
			}

			if e.Kind == domain.EventUsage {
				live.Turns++
			}

			if e.Kind == domain.EventToolCall {
				live.LastToolCall = e.Data
				if tool, _ := e.Data[domain.ToolCallTool].(string); tool == domain.ToolTodoWrite {
					live.LastPlan = e.Data
				}
			}
		}

		out[id] = live
	}

	return out, nil
}

// liveRuns returns the newest running run of every leased item of factory, by item. Callers
// hold s.mu.
func (s *Store) liveRuns(org domain.OrgID, factory domain.FactoryID) map[domain.ItemID]*domain.Run {
	newest := map[domain.ItemID]*domain.Run{}

	for _, r := range s.runs {
		_, leased := s.leases[r.ItemID]
		if !leased || r.Result.Status != domain.RunRunning || !s.ofFactory(r.ItemID, org, factory) {
			continue
		}

		n := newest[r.ItemID]
		if n == nil || r.StartedAt.After(n.StartedAt) || (r.StartedAt.Equal(n.StartedAt) && r.ID > n.ID) {
			newest[r.ItemID] = r
		}
	}

	return newest
}

// ofFactory reports whether item id is one of org's at factory. Callers hold s.mu.
func (s *Store) ofFactory(id domain.ItemID, org domain.OrgID, factory domain.FactoryID) bool {
	it, ok := s.visible(id, org)

	return ok && it.FactoryID == factory
}

// StationTimes implements ports.FlowStats.
func (s *Store) StationTimes(ctx context.Context, factory domain.FactoryID, since, until time.Time) ([]domain.StationTimes, time.Time, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, time.Time{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	m := &stationMeter{since: since, until: until, stations: map[string]*domain.StationTimes{}}

	for id, spans := range s.leaseLog {
		if s.ofFactory(id, org, factory) {
			for _, l := range spans {
				m.lease(l.Station, l.AcquiredAt, l.EndedAt)
			}
		}
	}

	for id, l := range s.leases {
		if s.ofFactory(id, org, factory) {
			m.lease(l.Station, l.AcquiredAt, until)
		}
	}

	for id, spans := range s.spans {
		if s.ofFactory(id, org, factory) {
			m.item(spans)
		}
	}

	for _, e := range s.events {
		if s.ofFactory(e.ItemID, org, factory) {
			m.event(e)
		}
	}

	return m.sorted(), m.history, nil
}

// stationMeter measures a factory's stations over [since, until) as StationTimes does, from its
// leases, spans and events one at a time.
type stationMeter struct {
	since, until time.Time
	stations     map[string]*domain.StationTimes
	// history is the earliest lease acquisition seen.
	history time.Time
}

func (m *stationMeter) at(station string) *domain.StationTimes {
	st, ok := m.stations[station]
	if !ok {
		st = &domain.StationTimes{Station: station}
		m.stations[station] = st
	}

	return st
}

// lease counts a lease held from acquired to ended, clipped to the window.
func (m *stationMeter) lease(station string, acquired, ended time.Time) {
	if m.history.IsZero() || acquired.Before(m.history) {
		m.history = acquired
	}

	if !ended.After(m.since) || !acquired.Before(m.until) {
		return
	}

	from, to := acquired, ended
	if from.Before(m.since) {
		from = m.since
	}

	if to.After(m.until) {
		to = m.until
	}

	st := m.at(station)
	st.Leases++
	st.Busy += to.Sub(from)
}

// span counts a waiting span open at the window's end or a day before it.
func (m *stationMeter) span(sp domain.Span) {
	openAt := func(t time.Time) bool {
		return sp.State.Waiting() && !sp.EnteredAt.After(t) && (sp.LeftAt.IsZero() || sp.LeftAt.After(t))
	}

	if openAt(m.until) {
		st := m.at(sp.Station)
		st.WaitingNow++

		if st.OldestWaiting.IsZero() || sp.EnteredAt.Before(st.OldestWaiting) {
			st.OldestWaiting = sp.EnteredAt
		}
	}

	if openAt(m.until.Add(-24 * time.Hour)) {
		m.at(sp.Station).WaitingDayAgo++
	}
}

// item counts one item's spans, the item once as a waiter at each station it waited at in the
// window.
func (m *stationMeter) item(spans []domain.Span) {
	waited := map[string]bool{}

	for _, sp := range spans {
		m.span(sp)

		if m.wait(sp) && !waited[sp.Station] {
			waited[sp.Station] = true
			m.at(sp.Station).Waiters++
		}
	}
}

// wait adds the time a waiting span overlaps the window, clipped to it, and reports whether it
// did.
func (m *stationMeter) wait(sp domain.Span) bool {
	from, to := sp.EnteredAt, sp.LeftAt
	if to.IsZero() || to.After(m.until) {
		to = m.until
	}

	if from.Before(m.since) {
		from = m.since
	}

	if !sp.State.Waiting() || !to.After(from) {
		return false
	}

	m.at(sp.Station).Waiting += to.Sub(from)

	return true
}

// event counts a gate verdict given in the window.
func (m *stationMeter) event(e domain.Event) {
	verdict := e.Kind == domain.EventGateApproved || e.Kind == domain.EventGateRejected
	if !verdict || e.At.Before(m.since) || !e.At.Before(m.until) {
		return
	}

	st := m.at(e.Station)
	st.Verdicts++

	if e.Kind == domain.EventGateRejected {
		st.Rejected++
	}
}

func (m *stationMeter) sorted() []domain.StationTimes {
	out := make([]domain.StationTimes, 0, len(m.stations))
	for _, name := range slices.Sorted(maps.Keys(m.stations)) {
		out = append(out, *m.stations[name])
	}

	return out
}

// LeaseSpans returns the lease spans of an item of org in the order they ended.
func (s *Store) LeaseSpans(org domain.OrgID, id domain.ItemID) []domain.LeaseSpan {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.visible(id, org); !ok {
		return nil
	}

	return slices.Clone(s.leaseLog[id])
}

// recordLease records a lease that ends now as lease_spans does: with the item's rework count
// before the change that ends it, never ending before it was acquired. Callers hold s.mu.
func (s *Store) recordLease(l domain.Lease, outcome domain.LeaseOutcome, at time.Time) {
	if at.Before(l.AcquiredAt) {
		at = l.AcquiredAt
	}

	s.leaseLog[l.ItemID] = append(s.leaseLog[l.ItemID], domain.LeaseSpan{
		ItemID: l.ItemID, Station: l.Station, Agent: l.Agent, Slot: l.Slot, ReworkRound: s.items[l.ItemID].ReworkCount,
		AcquiredAt: l.AcquiredAt, EndedAt: at, Outcome: outcome,
	})
}

// trackSpan keeps an item's spans as the item_spans trigger does in PostgreSQL: called after a
// change to it with what it was before (nil for a new item) and when the change happened. A
// change of station, state or rework count closes the open span at that time (never before it
// opened) and, unless the item is now done or cancelled, opens the next one; a new rework round
// is by the station the item left, otherwise the round's station carries on. Callers hold s.mu.
func (s *Store) trackSpan(it, before *domain.Item, at time.Time) {
	spans := s.spans[it.ID]
	by := ""

	if before != nil {
		if before.Station == it.Station && before.State == it.State && before.ReworkCount == it.ReworkCount {
			return
		}

		at, by = closeSpan(spans, at)
		if it.ReworkCount > before.ReworkCount {
			by = before.Station
		}
	}

	if !closed(it.State) && it.State != domain.StateAssembly {
		spans = append(spans, domain.Span{
			Station: it.Station, State: it.State, ReworkRound: it.ReworkCount, ReworkBy: by, EnteredAt: at,
		})
	}

	s.spans[it.ID] = spans
}

// closeSpan closes the open span, the last one, at `at` or when it opened if that is later, and
// returns that time with the span's rework_by; without an open span it returns at and "".
func closeSpan(spans []domain.Span, at time.Time) (time.Time, string) {
	n := len(spans)
	if n == 0 || !spans[n-1].LeftAt.IsZero() {
		return at, ""
	}

	open := &spans[n-1]
	if at.Before(open.EnteredAt) {
		at = open.EnteredAt
	}

	open.LeftAt = at

	return at, open.ReworkBy
}

// AgentStats implements ports.FlowStats.
func (s *Store) AgentStats(ctx context.Context, factory domain.FactoryID, agent string, since, until time.Time) (domain.AgentStats, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.AgentStats{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var out domain.AgentStats

	in := func(t time.Time) bool { return !t.Before(since) && t.Before(until) }

	s.agentTasks(&out, org, factory, agent, in)
	runs := s.agentRuns(&out, org, factory, agent, in)

	denied := domain.Tallies(s.deniedTools(runs))
	for _, d := range denied {
		out.DeniedCalls += d.Count
	}

	out.Denied = denied[:min(len(denied), domain.MaxDeniedTools)]

	return out, nil
}

// agentTasks measures the agent's finished lease spans that ended in the window and the advanced
// ones sent back. Callers hold s.mu.
func (s *Store) agentTasks(out *domain.AgentStats, org domain.OrgID, factory domain.FactoryID, agent string, in func(time.Time) bool) {
	var times []float64

	var reasons []string

	for id, leases := range s.leaseLog {
		if !s.ofFactory(id, org, factory) {
			continue
		}

		for _, l := range leases {
			if l.Agent != agent || !l.Outcome.Finished() || !in(l.EndedAt) {
				continue
			}

			out.TasksFinished++

			times = append(times, l.EndedAt.Sub(l.AcquiredAt).Seconds())
			if l.Outcome != domain.LeaseAdvanced {
				continue
			}

			out.Advanced++

			if label, back := s.sentBack(l); back {
				out.SentBack++

				reasons = append(reasons, label)
			}
		}
	}

	slices.Sort(times)
	out.TaskTimeP85 = percentile(times, 0.85)
	out.Reasons = domain.Tallies(reasons)
}

// agentRuns measures the agent's runs started in the window and returns them. Callers hold s.mu.
func (s *Store) agentRuns(out *domain.AgentStats, org domain.OrgID, factory domain.FactoryID, agent string, in func(time.Time) bool) map[domain.RunID]bool {
	runs := map[domain.RunID]bool{}

	var turns []float64

	for id, r := range s.runs {
		if !s.ofFactory(r.ItemID, org, factory) || r.Agent != agent || !in(r.StartedAt) {
			continue
		}

		runs[id] = true
		out.SpendUSD += r.Result.CostUSD

		if r.Result.Status == domain.RunSucceeded {
			out.SucceededRuns++

			turns = append(turns, float64(r.Result.Turns))
		}
	}

	slices.Sort(turns)
	out.MedianTurns = percentileOf(turns, 0.5)

	return runs
}

// sentBack reports whether the item of an advanced lease later entered its next rework round from
// another station, and the label of what that station said. Callers hold s.mu.
func (s *Store) sentBack(l domain.LeaseSpan) (string, bool) {
	i := slices.IndexFunc(s.spans[l.ItemID], func(sp domain.Span) bool {
		return sp.ReworkRound == l.ReworkRound+1 && sp.ReworkBy != "" && sp.ReworkBy != l.Station
	})
	if i < 0 {
		return "", false
	}

	return s.sendBackLabel(l.ItemID, s.spans[l.ItemID][i]), true
}

// sendBackLabel labels what sent an item into the rework round of span back: the latest
// rejection the sending station gave by then, else a person's latest move back from it. Callers
// hold s.mu.
func (s *Store) sendBackLabel(item domain.ItemID, back domain.Span) string {
	var label, moved string

	for _, e := range s.events {
		if e.ItemID != item || e.Station != back.ReworkBy || e.At.After(back.EnteredAt) {
			continue
		}

		category, _ := e.Data["category"].(string)
		reason, _ := e.Data["reason"].(string)

		switch {
		case e.Kind == domain.EventGateRejected || e.Kind == domain.EventRejected:
			label = domain.VerdictLabel(category, reason)
		case e.Kind == domain.EventMoved && e.Data["manual"] == true:
			moved = domain.VerdictLabel("", reason)
		}
	}

	if label == "" {
		return moved
	}

	return label
}

// deniedTools names the tool of every tool call the runs were denied. Callers hold s.mu.
func (s *Store) deniedTools(runs map[domain.RunID]bool) []string {
	var tools []string

	for _, e := range s.events {
		if !runs[e.RunID] {
			continue
		}

		tool, _ := e.Data["tool"].(string)

		switch {
		case e.Kind == domain.EventToolDenied:
			tools = append(tools, tool)
		case e.Kind == gatewayToolCall && e.Data["outcome"] == "denied":
			connection, _ := e.Data["connection"].(string)
			tools = append(tools, connection+"__"+tool)
		}
	}

	return tools
}

// gatewayToolCall is the kind of the gateway's tool call events (gateway.EventToolCall).
const gatewayToolCall domain.EventKind = "gateway_tool_call"
