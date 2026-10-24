package porttest

import (
	"context"
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
