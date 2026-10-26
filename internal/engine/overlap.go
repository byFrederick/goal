package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Bounds of the overlap hold's reads: the waiting items of a station it checks per turn, and
// the page size it reads the factory's open items in.
const (
	overlapWaiting = 50
	overlapPage    = 200
)

// openStates are the states of an item that is still to finish and carries its own paths: an
// assembly's parts carry the assembly's.
var openStates = []domain.ItemState{
	domain.StateQueued, domain.StateReady, domain.StateInProgress, domain.StateWaitingHuman, domain.StateBlocked,
}

// holdOverlaps makes each item waiting at st with paths wait for every open, lower-numbered item
// of the factory whose paths overlap its own (ADR 0015): an overlap edge by the engine, so the
// claim gate skips it until that item is done or cancelled. It skips parts of the item's own
// assembly, items it already needs, and an edge the store refuses as a cycle or as one that
// exists. A line whose flow.assemblies.overlap is allow holds nothing.
func (e *Engine) holdOverlaps(ctx context.Context, l domain.Line, st domain.Station) error {
	if !l.Flow.Assemblies.Overlap.Holds() {
		return nil
	}

	waiting, err := e.store.Waiting(ctx, l.FactoryID, st.ID, overlapWaiting)
	if err != nil {
		return fmt.Errorf("station %s: overlap hold: list waiting: %w", st.ID, err)
	}

	var (
		held []domain.Item
		ids  []domain.ItemID
		top  int64
	)

	for _, it := range waiting {
		if len(it.Paths) > 0 {
			held = append(held, it)
			ids = append(ids, it.ID)
			top = max(top, it.Number)
		}
	}

	if len(held) == 0 {
		return nil
	}

	open, err := e.openWithPaths(ctx, l.FactoryID, top)
	if err != nil {
		return fmt.Errorf("station %s: overlap hold: %w", st.ID, err)
	}

	if len(open) == 0 {
		return nil
	}

	needs, err := e.store.OpenNeeds(ctx, l.FactoryID, ids)
	if err != nil {
		return fmt.Errorf("station %s: overlap hold: read needs: %w", st.ID, err)
	}

	var errs []error

	for _, it := range held {
		for _, o := range open {
			if !overlaps(it, o, needs[it.ID]) {
				continue
			}

			errs = append(errs, e.addOverlap(ctx, it, o))
		}
	}

	return errors.Join(errs...)
}

// openWithPaths returns the factory's open items numbered below before that have paths,
// reading every page.
func (e *Engine) openWithPaths(ctx context.Context, factory domain.FactoryID, before int64) ([]domain.Item, error) {
	var out []domain.Item

	for {
		page, err := e.store.ListItems(ctx, ports.ItemQuery{
			FactoryID: factory, States: openStates, Before: before, Limit: overlapPage,
		})
		if err != nil {
			return nil, fmt.Errorf("list open items: %w", err)
		}

		for _, it := range page {
			if len(it.Paths) > 0 {
				out = append(out, it)
			}
		}

		if len(page) < overlapPage {
			return out, nil
		}

		before = page[len(page)-1].Number
	}
}

// overlaps reports whether it must wait for o: o is numbered lower, is not a part of its own
// assembly, is not already needed and their paths overlap.
func overlaps(it, o domain.Item, needs []domain.OpenNeed) bool {
	if o.Number >= it.Number || o.ID == it.ParentID || (it.ParentID != "" && o.ParentID == it.ParentID) {
		return false
	}

	for _, n := range needs {
		if n.Item == o.ID {
			return false
		}
	}

	return len(domain.PathsOverlap(it.Paths, o.Paths)) > 0
}

// addOverlap records that it needs o for their overlapping paths. An edge that would close a
// cycle or that exists already (another instance wrote it) is skipped.
func (e *Engine) addOverlap(ctx context.Context, it, o domain.Item) error {
	_, err := e.store.AddEdge(ctx, domain.NewEdge{
		Item: it.ID, Needs: o.ID, Kind: domain.EdgeOverlap, By: domain.EngineActor, At: e.clock.Now(),
	})

	switch {
	case errors.Is(err, domain.ErrCycle), errors.Is(err, ports.ErrConflict):
		e.log.DebugContext(ctx, "overlap edge skipped", slog.String("item", string(it.ID)), slog.String("needs", string(o.ID)),
			slog.Any("reason", err))

		return nil
	case err != nil:
		return fmt.Errorf("item %s needs %s for overlapping paths: %w", it.ID, o.ID, err)
	}

	e.log.InfoContext(ctx, "overlap held", slog.String("factory", string(it.FactoryID)), slog.String("item", string(it.ID)),
		slog.String("needs", string(o.ID)))

	return nil
}
