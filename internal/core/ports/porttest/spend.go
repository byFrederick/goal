package porttest

import (
	"context"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.Spend = (*Store)(nil)

// OrgSpend implements ports.Spend over the store's runs, items and spans.
func (s *Store) OrgSpend(ctx context.Context, w domain.SpendWindow) (domain.OrgSpend, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.OrgSpend{}, err
	}

	if err := w.Check(); err != nil {
		return domain.OrgSpend{}, err
	}

	since, until := w.Bounds()
	from := w.ReadSince()

	s.mu.Lock()
	defer s.mu.Unlock()

	type factoryDay struct {
		day     time.Time
		factory domain.FactoryID
	}

	days := map[factoryDay]float64{}
	agents := map[domain.AgentSpend]float64{}
	models := map[string]float64{}

	var f domain.SpendFacts

	for _, r := range s.runs {
		if _, ok := s.visible(r.ItemID, org); !ok || !inWindow(r.StartedAt, from, until) {
			continue
		}

		days[factoryDay{utcDay(r.StartedAt), r.FactoryID}] += r.Result.CostUSD

		if !inWindow(r.StartedAt, since, until) {
			continue
		}

		agents[domain.AgentSpend{FactoryID: r.FactoryID, Agent: r.Agent}] += r.Result.CostUSD
		models[r.Model] += r.Result.CostUSD

		if s.reworkAt(r.ItemID, r.StartedAt) {
			f.ReworkUSD += r.Result.CostUSD
		}
	}

	for k, usd := range days {
		f.Days = append(f.Days, domain.FactoryDaySpend{Day: k.day, FactoryID: k.factory, USD: usd})
	}

	for k, usd := range agents {
		k.USD = usd
		f.ByAgent = append(f.ByAgent, k)
	}

	for m, usd := range models {
		f.ByModel = append(f.ByModel, domain.ModelSpend{Model: m, USD: usd})
	}

	merged := map[time.Time]int{}

	for _, it := range s.items {
		if it.OrgID == org && it.State == domain.StateDone && inWindow(it.DoneAt, from, until) {
			merged[utcDay(it.DoneAt)]++
		}
	}

	for d, n := range merged {
		f.Merged = append(f.Merged, domain.DayCount{Day: d, Count: n})
	}

	return domain.NewOrgSpend(w, f), nil
}

// reworkAt reports whether the item had entered a rework round by at. Callers hold s.mu.
func (s *Store) reworkAt(id domain.ItemID, at time.Time) bool {
	for _, sp := range s.spans[id] {
		if sp.ReworkRound >= 1 && !sp.EnteredAt.After(at) {
			return true
		}
	}

	return false
}

func utcDay(t time.Time) time.Time {
	t = t.UTC()

	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
