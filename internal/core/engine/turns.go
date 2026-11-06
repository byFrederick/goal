package engine

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Reasons the turn hold removes a turn edge with, in edge_removed's data.
const (
	reasonTurnLeft    = "needed item left the station"
	reasonTurnBlocked = "needed item is blocked"
	reasonTurnTarget  = "needed item has another merge target"
	reasonTurnHolder  = "another item holds the turn"
)

// defaultTarget is the merge target of an item that lands on the default branch: one per
// factory, since a station's items are all of one factory.
const defaultTarget domain.ItemID = ""

// turnTarget keys the branch it's pull request merges into: a part of a line whose assemblies
// get an integration branch merges into its assembly's, keyed by the assembly (a part's parent is
// its root); a standalone item, a landing and a part in mode trunk merge into the default branch.
// Items in different repositories that land on their default branches share the key (ADR 0043).
func turnTarget(l domain.Line, it domain.Item) domain.ItemID {
	if it.ParentID != "" && l.Flow.Assemblies.Integration.Branches() {
		return it.ParentID
	}

	return defaultTarget
}

// turnGroup is the open items at a merge station with one merge target: the one holding its
// turn, "" when none can, and the waiting ones in pull order.
type turnGroup struct {
	holder  domain.ItemID
	waiting []domain.Item
}

// holdTurns makes a station that takes turns (domain.Station.TakesTurns) claim one item per merge
// target at a time (ADR 0043). It groups the station's open items by turnTarget. The holder of
// a group's turn is the item in progress there, or else the waiting item the group's turn edges
// already point to (so an item whose merge was deferred back to the queue keeps its turn), or
// else the first waiting item in pull order with no open need but turn edges and no later start.
// Every other waiting item of the group gets a turn edge on the holder, by the engine, so the
// claim gate skips it until the holder is done or cancelled. First it removes the turn edges
// whose needed item is not its group's holder, naming why: the item left the station, is blocked,
// has another target or another item holds the turn. An edge the store refuses as a cycle or as
// one that exists is skipped.
func (e *Engine) holdTurns(ctx context.Context, l domain.Line, st domain.Station) error {
	if !st.TakesTurns() {
		return nil
	}

	waiting, err := e.store.Waiting(ctx, l.FactoryID, st.ID, overlapWaiting)
	if err != nil {
		return fmt.Errorf("station %s: turn hold: list waiting: %w", st.ID, err)
	}

	if len(waiting) == 0 {
		return nil
	}

	open, err := e.store.ListItems(ctx, ports.ItemQuery{FactoryID: l.FactoryID, Station: st.ID, States: openStates, Limit: overlapPage})
	if err != nil {
		return fmt.Errorf("station %s: turn hold: list open items: %w", st.ID, err)
	}

	ids := make([]domain.ItemID, 0, len(waiting))
	for _, it := range waiting {
		ids = append(ids, it.ID)
	}

	needs, err := e.store.OpenNeeds(ctx, l.FactoryID, ids)
	if err != nil {
		return fmt.Errorf("station %s: turn hold: read needs: %w", st.ID, err)
	}

	groups := turnGroups(l, waiting, open, needs, e.clock.Now())

	byID := make(map[domain.ItemID]domain.Item, len(open))
	for _, o := range open {
		byID[o.ID] = o
	}

	var errs []error

	for _, it := range waiting {
		target := turnTarget(l, it)
		holder := groups[target].holder

		for _, n := range needs[it.ID] {
			if n.Kind != domain.EdgeTurn || n.Item == holder {
				continue
			}

			errs = append(errs, e.releaseTurn(ctx, it, n.Item, turnReason(l, target, byID, n.Item)))
		}
	}

	for _, g := range groups {
		if g.holder == "" {
			continue
		}

		for _, it := range g.waiting {
			if it.ID != g.holder && !slices.ContainsFunc(needs[it.ID], func(n domain.OpenNeed) bool { return n.Item == g.holder }) {
				errs = append(errs, e.addTurn(ctx, it, g.holder))
			}
		}
	}

	return errors.Join(errs...)
}

// turnGroups groups the station's waiting items by merge target and picks each group's holder:
// the open item in progress at the station with the lowest number, else the waiting item the
// group's turn edges point to first in pull order, else the first waiting item that may be
// claimed but for its turn edges.
func turnGroups(l domain.Line, waiting, open []domain.Item, needs map[domain.ItemID][]domain.OpenNeed, now time.Time) map[domain.ItemID]*turnGroup {
	groups := map[domain.ItemID]*turnGroup{}

	group := func(it domain.Item) *turnGroup {
		k := turnTarget(l, it)
		if groups[k] == nil {
			groups[k] = &turnGroup{}
		}

		return groups[k]
	}

	for _, it := range waiting {
		g := group(it)
		g.waiting = append(g.waiting, it)
	}

	running := slices.DeleteFunc(slices.Clone(open), func(o domain.Item) bool { return o.State != domain.StateInProgress })
	slices.SortFunc(running, func(a, b domain.Item) int { return cmp.Compare(a.Number, b.Number) })

	for _, it := range running {
		if g := groups[turnTarget(l, it)]; g != nil && g.holder == "" {
			g.holder = it.ID
		}
	}

	for _, g := range groups {
		if g.holder == "" {
			g.holder = pointedTo(g.waiting, needs)
		}

		if g.holder == "" {
			if i := slices.IndexFunc(g.waiting, func(it domain.Item) bool { return mayHold(it, needs[it.ID], now) }); i >= 0 {
				g.holder = g.waiting[i].ID
			}
		}
	}

	return groups
}

// pointedTo is the first waiting item, in pull order, that another waiting item of the group has
// a turn edge on, "" when there is none.
func pointedTo(waiting []domain.Item, needs map[domain.ItemID][]domain.OpenNeed) domain.ItemID {
	pointed := map[domain.ItemID]bool{}

	for _, it := range waiting {
		for _, n := range needs[it.ID] {
			if n.Kind == domain.EdgeTurn {
				pointed[n.Item] = true
			}
		}
	}

	for _, it := range waiting {
		if pointed[it.ID] {
			return it.ID
		}
	}

	return ""
}

// mayHold reports whether a waiting item may take a free turn: it needs nothing open but turns
// and may start now, so the claim takes it once its stale turn edges are gone.
func mayHold(it domain.Item, needs []domain.OpenNeed, now time.Time) bool {
	return !it.StartAt.After(now) && !slices.ContainsFunc(needs, func(n domain.OpenNeed) bool { return n.Kind != domain.EdgeTurn })
}

// turnReason is why a waiting item with target no longer waits for needs.
func turnReason(l domain.Line, target domain.ItemID, open map[domain.ItemID]domain.Item, needs domain.ItemID) string {
	o, ok := open[needs]

	switch {
	case !ok:
		return reasonTurnLeft
	case o.State == domain.StateBlocked:
		return reasonTurnBlocked
	case turnTarget(l, o) != target:
		return reasonTurnTarget
	default:
		return reasonTurnHolder
	}
}

// releaseTurn removes it's turn edge on needs for reason. An edge already gone (another instance
// removed it) is skipped.
func (e *Engine) releaseTurn(ctx context.Context, it domain.Item, needs domain.ItemID, reason string) error {
	err := e.store.RemoveEdge(ctx, it.ID, needs, domain.EngineActor, e.clock.Now(), reason)

	switch {
	case errors.Is(err, ports.ErrNotFound):
		return nil
	case err != nil:
		return fmt.Errorf("item %s no longer waits for %s's turn: %w", it.ID, needs, err)
	}

	e.log.InfoContext(ctx, "turn released", slog.String("factory", string(it.FactoryID)), slog.String("item", string(it.ID)),
		slog.String("needs", string(needs)), slog.String("reason", reason))

	return nil
}

// addTurn records that it waits for holder's turn. An edge that would close a cycle, or that
// exists already (another instance wrote it), is skipped.
func (e *Engine) addTurn(ctx context.Context, it domain.Item, holder domain.ItemID) error {
	_, err := e.store.AddEdge(ctx, domain.NewEdge{Item: it.ID, Needs: holder, Kind: domain.EdgeTurn, By: domain.EngineActor, At: e.clock.Now()})

	switch {
	case errors.Is(err, domain.ErrCycle), errors.Is(err, ports.ErrConflict):
		e.log.DebugContext(ctx, "turn edge skipped", slog.String("item", string(it.ID)), slog.String("needs", string(holder)),
			slog.Any("reason", err))

		return nil
	case err != nil:
		return fmt.Errorf("item %s waits for %s's turn: %w", it.ID, holder, err)
	}

	e.log.InfoContext(ctx, "turn held", slog.String("factory", string(it.FactoryID)), slog.String("item", string(it.ID)),
		slog.String("needs", string(holder)))

	return nil
}
