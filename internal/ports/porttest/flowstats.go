package porttest

import (
	"context"
	"maps"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
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

// LiveTurns implements ports.FlowStats.
func (s *Store) LiveTurns(ctx context.Context, factory domain.FactoryID) (map[domain.ItemID]int, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	running := s.liveRuns(org, factory)
	out := make(map[domain.ItemID]int, len(running))

	for id, r := range running {
		out[id] = 0

		for _, e := range s.events {
			if e.RunID == r.ID && e.Kind == domain.EventUsage {
				out[id]++
			}
		}
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
			for _, sp := range spans {
				m.span(sp)
			}
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

	if !closed(it.State) {
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
