package porttest

import (
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
	m.SpendUSD = s.spend(org, q.FactoryID, q.Since, q.Until, q.Type)

	return m, nil
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

// ofType reports whether it is of work type typ; every item is when typ is "".
func ofType(it *domain.Item, typ string) bool { return typ == "" || it.Type == typ }
