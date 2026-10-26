package porttest

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// FlowMetrics implements ports.FlowStats.
func (s *Store) FlowMetrics(ctx context.Context, q ports.FlowQuery, longWaitsAt string) (domain.FlowMetrics, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.FlowMetrics{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var (
		m        domain.FlowMetrics
		perDay   = map[time.Time]int{}
		leadTime []float64
	)

	for _, it := range s.items {
		if it.OrgID != org || it.FactoryID != q.FactoryID || !ofType(it, q.Type) {
			continue
		}

		if !closed(it.State) {
			m.WIP++
		}

		if longWaitsAt != "" && slices.ContainsFunc(s.spans[it.ID], func(sp domain.Span) bool {
			return longWait(sp, longWaitsAt, q.Since, q.Until)
		}) {
			m.LongWaits++
		}

		if inWindow(it.DoneAt, q.Since, q.Until) {
			m.ItemsDone++
			if it.ReworkCount > 0 {
				m.Reworked++
			}

			perDay[it.DoneAt.UTC().Truncate(24*time.Hour)]++
			leadTime = append(leadTime, it.DoneAt.Sub(it.CreatedAt).Seconds())
		}
	}

	for _, day := range slices.SortedFunc(maps.Keys(perDay), func(a, b time.Time) int { return a.Compare(b) }) {
		m.DonePerDay = append(m.DonePerDay, domain.DayCount{Day: day, Count: perDay[day]})
	}

	slices.Sort(leadTime)
	m.LeadTimeP50, m.LeadTimeP85 = percentile(leadTime, 0.5), percentile(leadTime, 0.85)
	m.AssembliesDone, m.AssemblyLeadTimeP50, m.AssemblyLeadTimeP85 = s.assemblyLeadTime(org, q)
	m.SpendUSD = s.spend(org, q.FactoryID, q.Since, q.Until, q.Type)

	return m, nil
}

// assemblyLeadTime is how many assemblies (items with parts) of q were done in its window and
// the percentiles of their created → last part done; cancelled ones are left out. Callers hold s.mu.
func (s *Store) assemblyLeadTime(org domain.OrgID, q ports.FlowQuery) (done int, p50, p85 time.Duration) {
	parents := map[domain.ItemID]bool{}

	for _, it := range s.items {
		if it.OrgID == org && it.ParentID != "" && it.PartKey != "" {
			parents[it.ParentID] = true
		}
	}

	var lead []float64

	for _, it := range s.items {
		if it.OrgID == org && it.FactoryID == q.FactoryID && ofType(it, q.Type) && parents[it.ID] &&
			it.State == domain.StateDone && inWindow(it.DoneAt, q.Since, q.Until) {
			lead = append(lead, it.DoneAt.Sub(it.CreatedAt).Seconds())
		}
	}

	slices.Sort(lead)

	return len(lead), percentile(lead, 0.5), percentile(lead, 0.85)
}

// longWait reports whether sp waits at station (queued or ready), overlaps [since, until) and
// had lasted over domain.LongWait by until.
func longWait(sp domain.Span, station string, since, until time.Time) bool {
	waiting := sp.Station == station && (sp.State == domain.StateQueued || sp.State == domain.StateReady)

	end := until
	if !sp.LeftAt.IsZero() && sp.LeftAt.Before(until) {
		end = sp.LeftAt
	}

	overlaps := sp.EnteredAt.Before(until) && (sp.LeftAt.IsZero() || sp.LeftAt.After(since))

	return waiting && overlaps && end.Sub(sp.EnteredAt) > domain.LongWait
}

// DoneByBucket implements ports.FlowStats.
func (s *Store) DoneByBucket(ctx context.Context, q ports.FlowQuery, d time.Duration, n int) ([]int, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	start := q.Until.Add(-time.Duration(n) * d)
	out := make([]int, n)

	for _, it := range s.items {
		if it.OrgID == org && it.FactoryID == q.FactoryID && ofType(it, q.Type) && inWindow(it.DoneAt, start, q.Until) {
			out[int(it.DoneAt.Sub(start)/d)]++
		}
	}

	return out, nil
}

// ItemTimes implements ports.FlowStats.
func (s *Store) ItemTimes(ctx context.Context, q ports.FlowQuery, limit int) ([]domain.ItemTime, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var done []*domain.Item

	for _, it := range s.items {
		if it.OrgID == org && it.FactoryID == q.FactoryID && ofType(it, q.Type) && inWindow(it.DoneAt, q.Since, q.Until) {
			done = append(done, it)
		}
	}

	slices.SortFunc(done, func(a, b *domain.Item) int { return cmp.Or(a.DoneAt.Compare(b.DoneAt), cmp.Compare(a.ID, b.ID)) })
	done = done[max(len(done)-max(limit, 0), 0):]

	out := make([]domain.ItemTime, 0, len(done))

	for _, it := range done {
		times := map[domain.Component]time.Duration{}

		for _, sp := range s.spans[it.ID] {
			if !sp.LeftAt.IsZero() {
				times[domain.ComponentOf(sp)] += sp.LeftAt.Sub(sp.EnteredAt)
			}
		}

		out = append(out, domain.ItemTime{Item: it.ID, LeadTime: it.DoneAt.Sub(it.CreatedAt), Times: times})
	}

	return out, nil
}

// ofType reports whether it is of work type typ; every item is when typ is "".
func ofType(it *domain.Item, typ string) bool { return typ == "" || it.Type == typ }
