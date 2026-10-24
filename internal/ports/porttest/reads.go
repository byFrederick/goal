package porttest

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"math"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// ItemByNumber implements ports.Store.
func (s *Store) ItemByNumber(ctx context.Context, factory domain.FactoryID, number int64) (domain.Item, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.Item{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, it := range s.items {
		if it.OrgID == org && it.FactoryID == factory && it.Number == number {
			return clone(it), nil
		}
	}

	return domain.Item{}, fmt.Errorf("item %d of factory %s: %w", number, factory, ports.ErrNotFound)
}

// ListItems implements ports.Store.
func (s *Store) ListItems(ctx context.Context, q ports.ItemQuery) ([]domain.Item, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var match []*domain.Item

	for _, it := range s.items {
		if it.OrgID == org && it.FactoryID == q.FactoryID && (len(q.States) == 0 || slices.Contains(q.States, it.State)) &&
			(q.Station == "" || it.Station == q.Station) && (q.Before <= 0 || it.Number < q.Before) {
			match = append(match, it)
		}
	}

	slices.SortFunc(match, func(a, b *domain.Item) int { return cmp.Compare(b.Number, a.Number) })

	out := make([]domain.Item, 0, min(q.Limit, len(match)))
	for _, it := range match[:min(q.Limit, len(match))] {
		out = append(out, clone(it))
	}

	return out, nil
}

// StationLoads implements ports.Store.
func (s *Store) StationLoads(ctx context.Context, factory domain.FactoryID) ([]domain.StationLoad, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	loads := map[string]*domain.StationLoad{}

	for _, it := range s.items {
		if it.OrgID != org || it.FactoryID != factory || it.State == domain.StateDone || it.State == domain.StateCancelled {
			continue
		}

		ld, ok := loads[it.Station]
		if !ok {
			ld = &domain.StationLoad{Station: it.Station}
			loads[it.Station] = ld
		}

		switch {
		case it.State.Waiting():
			ld.Waiting++
		case it.State == domain.StateInProgress:
			ld.InProgress++
		}
	}

	out := make([]domain.StationLoad, 0, len(loads))
	for _, ld := range loads {
		out = append(out, *ld)
	}

	return out, nil
}

// FlowMetrics implements ports.Store.
func (s *Store) FlowMetrics(ctx context.Context, factory domain.FactoryID, since, until time.Time) (domain.FlowMetrics, error) {
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
		if it.OrgID != org || it.FactoryID != factory {
			continue
		}

		if it.State != domain.StateDone && it.State != domain.StateCancelled {
			m.WIP++
		}

		if inWindow(it.DoneAt, since, until) {
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

	m.SpendUSD = s.spend(org, factory, since, until)

	return m, nil
}

// spend adds the cost of an organization's factory's runs started in [since, until). Callers
// hold s.mu.
func (s *Store) spend(org domain.OrgID, factory domain.FactoryID, since, until time.Time) float64 {
	var usd float64

	for _, r := range s.runs {
		if _, ok := s.visible(r.ItemID, org); ok && r.FactoryID == factory && inWindow(r.StartedAt, since, until) {
			usd += r.Result.CostUSD
		}
	}

	return usd
}

// inWindow reports whether t, when set, is in [since, until).
func inWindow(t, since, until time.Time) bool {
	return !t.IsZero() && !t.Before(since) && t.Before(until)
}

// percentile interpolates linearly between the closest ranks of sorted seconds, as
// PostgreSQL's percentile_cont does. No values give zero.
func percentile(sorted []float64, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}

	pos := p * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := min(lo+1, len(sorted)-1)
	v := sorted[lo] + (pos-float64(lo))*(sorted[hi]-sorted[lo])

	return time.Duration(math.Round(v * float64(time.Second)))
}
