package porttest

import (
	"cmp"
	"context"
	"fmt"
	"slices"
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

	f := s.runSpend(org, from, since, until)

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

// runSpend sums org's runs as the postgres spend queries do: by day and factory from `from`, by
// agent, by model and on rework in [since, until). Callers hold s.mu.
func (s *Store) runSpend(org domain.OrgID, from, since, until time.Time) domain.SpendFacts {
	type factoryDay struct {
		day     time.Time
		factory domain.FactoryID
	}

	days := map[factoryDay]float64{}
	agents := map[domain.AgentSpend]float64{}
	models := map[string]float64{}

	var f domain.SpendFacts

	for _, r := range s.runs {
		if !s.runVisible(r, org) || !inWindow(r.StartedAt, from, until) {
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

	return f
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

// EachAgentModelSpend implements ports.Spend. As the postgres store pages, it finds each next
// factory, agent and model after the one before and sums its runs under the lock, and calls
// each without it; it never holds the rows at once.
func (s *Store) EachAgentModelSpend(ctx context.Context, w domain.SpendWindow, factories []domain.FactoryID, maxRows int,
	each func(domain.AgentModelSpend) error,
) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}

	if err := w.Check(); err != nil {
		return err
	}

	since, until := w.Bounds()
	runs := func(r *domain.Run) bool {
		_, ok := s.visible(r.ItemID, org)

		return ok && slices.Contains(factories, r.FactoryID) && inWindow(r.StartedAt, since, until)
	}

	var last *spendKey

	for {
		row, n, ok := s.nextSpendRow(runs, last)
		if last == nil && n > maxRows {
			return fmt.Errorf("%d spend rows, more than %d: %w", n, maxRows, ports.ErrTooMany)
		}

		if !ok {
			return nil
		}

		if err := each(row); err != nil {
			return err
		}

		last = &spendKey{string(row.FactoryID), row.Agent, row.Model}
	}
}

// spendKey orders the spend export's rows: factory id, agent and model.
type spendKey [3]string

func (k spendKey) compare(o spendKey) int {
	return cmp.Or(cmp.Compare(k[0], o[0]), cmp.Compare(k[1], o[1]), cmp.Compare(k[2], o[2]))
}

// nextSpendRow sums the runs keep keeps of the first key after last (the first key when last is
// nil) and, when last is nil, counts the keys.
func (s *Store) nextSpendRow(keep func(*domain.Run) bool, last *spendKey) (domain.AgentModelSpend, int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var (
		row   domain.AgentModelSpend
		best  spendKey
		found bool
		keys  = map[spendKey]bool{}
	)

	for _, r := range s.runs {
		if !keep(r) {
			continue
		}

		k := spendKey{string(r.FactoryID), r.Agent, r.Model}
		if last == nil {
			keys[k] = true
		} else if k.compare(*last) <= 0 {
			continue
		}

		switch {
		case found && k == best:
			row.USD += r.Result.CostUSD
		case !found || k.compare(best) < 0:
			row, best, found = domain.AgentModelSpend{FactoryID: r.FactoryID, Agent: r.Agent, Model: r.Model, USD: r.Result.CostUSD}, k, true
		}
	}

	return row, len(keys), found
}
