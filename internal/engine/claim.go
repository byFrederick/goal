package engine

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// orgTurn is one organization's share of a tick: its factories in the order they take turns,
// the one to try first next time, and whether it has nothing more to claim this tick.
type orgTurn struct {
	org       domain.OrgID
	lines     []domain.Line
	next      int
	exhausted bool
}

// stationKey names an agent station of a factory.
type stationKey struct {
	factory domain.FactoryID
	station string
}

// stationTurn is what a tick has learned about one agent station: the slots it has seen held
// (nil until read), and whether it has nothing more to give this tick (no work waits, every
// slot is held, or the store failed).
type stationTurn struct {
	taken map[int]bool
	done  bool
}

// tickState is what one tick has learned so far: each agent station's turn, and the factories
// whose slot cap is reached.
type tickState struct {
	stations map[stationKey]*stationTurn
	full     map[domain.FactoryID]bool
}

// claim starts runs with two-level round-robin, so no organization starves another: in each
// pass every organization that still has work claims at most one item, trying its factories
// in turn and each factory's agent stations in line order. Passes repeat until nothing more
// can be claimed or this instance runs MaxRuns runs. The orders rotate from tick to tick. A
// factory at its slot cap is skipped for the rest of the tick, and an organization at its cap
// claims nothing more in it.
func (e *Engine) claim(ctx context.Context, lines map[domain.FactoryID]domain.Line) error {
	turns := schedule(lines, e.ticks.Add(1)-1)
	tick := &tickState{stations: map[stationKey]*stationTurn{}, full: map[domain.FactoryID]bool{}}

	var errs []error

	for {
		progress := false

		for _, o := range turns {
			if o.exhausted {
				continue
			}

			if e.cfg.MaxRuns > 0 && e.running.Load() >= int64(e.cfg.MaxRuns) {
				return errors.Join(errs...)
			}

			claimed, err := e.claimFor(ctx, o, tick)
			if err != nil {
				errs = append(errs, err)
			}

			if !claimed {
				o.exhausted = true
				continue
			}

			progress = true
		}

		if !progress {
			return errors.Join(errs...)
		}
	}
}

// schedule groups the lines by organization. Organizations are sorted by id and each one's
// factories by id, then both are rotated by the tick number.
func schedule(lines map[domain.FactoryID]domain.Line, tick uint64) []*orgTurn {
	byOrg := map[domain.OrgID][]domain.Line{}
	for _, l := range lines {
		byOrg[l.OrgID] = append(byOrg[l.OrgID], l)
	}

	orgs := rotate(slices.Sorted(maps.Keys(byOrg)), tick)

	turns := make([]*orgTurn, 0, len(orgs))

	for _, org := range orgs {
		ls := byOrg[org]
		slices.SortFunc(ls, func(a, b domain.Line) int { return cmp.Compare(a.FactoryID, b.FactoryID) })
		turns = append(turns, &orgTurn{org: org, lines: rotate(ls, tick)})
	}

	return turns
}

// rotate returns s starting at its element n mod len(s).
func rotate[T any](s []T, n uint64) []T {
	if len(s) < 2 {
		return s
	}

	k := n % uint64(len(s)) // an index below len(s)

	return append(s[k:len(s):len(s)], s[:k]...)
}

// claimFor claims one item for the organization: from its next factory in turn on, the first
// agent station with work and a free slot, skipping factories at their slot cap. It reports
// whether it started a run; an organization at its slot cap starts none.
func (e *Engine) claimFor(ctx context.Context, o *orgTurn, tick *tickState) (bool, error) {
	ctx = domain.WithOrg(ctx, o.org)

	var errs []error

	for i := range len(o.lines) {
		k := (o.next + i) % len(o.lines)
		l := o.lines[k]

		for _, st := range l.Stations {
			if st.Kind != domain.KindAgent || tick.full[l.FactoryID] {
				continue
			}

			key := stationKey{factory: l.FactoryID, station: st.ID}

			s, seen := tick.stations[key]
			if !seen {
				s = &stationTurn{}
				tick.stations[key] = s
			}

			if s.done {
				continue
			}

			claimed, err := e.claimAt(ctx, l, st, s)

			switch {
			case errors.Is(err, ports.ErrOrgFull):
				e.log.DebugContext(ctx, "claim blocked", slog.String("scope", "org"), slog.String("org_id", string(o.org)))

				return false, errors.Join(errs...)
			case errors.Is(err, ports.ErrFactoryFull):
				e.log.DebugContext(ctx, "claim blocked", slog.String("scope", "factory"), slog.String("org_id", string(o.org)),
					slog.String("factory", string(l.FactoryID)))

				tick.full[l.FactoryID] = true
			case err != nil:
				errs = append(errs, err)
			}

			if claimed {
				o.next = k + 1
				return true, errors.Join(errs...)
			}
		}
	}

	return false, errors.Join(errs...)
}

// claimAt claims the next item waiting at one agent station into a free slot and starts its
// run. On its first turn in a tick it first holds the waiting items whose paths overlap an
// open item's that goes first on the line (holdOverlaps), so the claim gate skips them. When nothing waits, every slot is held or the store fails, the station is done for the
// tick. A slot cap is returned as ports.ErrOrgFull or ports.ErrFactoryFull, leaving the station
// as it was: the factory or the organization is what is done.
func (e *Engine) claimAt(ctx context.Context, l domain.Line, st domain.Station, s *stationTurn) (bool, error) {
	if s.taken == nil {
		if err := e.holdOverlaps(ctx, l, st); err != nil {
			s.done = true
			return false, err
		}

		leases, err := e.store.Leases(ctx, l.FactoryID, st.ID)
		if err != nil {
			s.done = true
			return false, fmt.Errorf("station %s: list leases: %w", st.ID, err)
		}

		s.taken = make(map[int]bool, len(leases))
		for _, ls := range leases {
			s.taken[ls.Slot] = true
		}
	}

	for slot := range st.Capacity() {
		if s.taken[slot] {
			continue
		}

		now := e.clock.Now()

		it, lease, err := e.store.ClaimNext(ctx, ports.Claim{
			FactoryID: l.FactoryID, Station: st, Slot: slot, Now: now, ExpiresAt: now.Add(e.cfg.LeaseTTL),
		})

		switch {
		case errors.Is(err, ports.ErrNoWork):
			s.done = true
			return false, nil
		case errors.Is(err, ports.ErrSlotTaken):
			s.taken[slot] = true
			continue
		case errors.Is(err, ports.ErrOrgFull), errors.Is(err, ports.ErrFactoryFull):
			return false, err
		case err != nil:
			s.done = true
			return false, fmt.Errorf("station %s: claim slot %d: %w", st.ID, slot, err)
		}

		s.taken[slot] = true

		e.log.InfoContext(ctx, "item claimed", slog.String("org_id", string(l.OrgID)), slog.String("factory", string(l.FactoryID)),
			slog.String("station", st.ID), slog.String("item", string(it.ID)), slog.Int("slot", slot))

		runCtx, run := e.track(ctx, lease)

		e.running.Add(1)
		e.runs.Go(func() {
			defer e.running.Add(-1)
			defer e.untrack(run)

			e.work(runCtx, l, st, it, lease)
		})

		return true, nil
	}

	s.done = true // every slot is held

	return false, nil
}
