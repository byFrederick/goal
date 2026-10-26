package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

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

// Reasons the overlap hold removes an edge with, in edge_removed's data.
const (
	reasonNoOverlap = "paths no longer overlap"
	reasonBehind    = "needed item is behind on the line"
)

// holdOverlaps makes each item waiting at st with paths wait for every open item of the
// factory whose paths overlap its own and that goes first on the line (ADR 0015, amended
// 2026-10-26; goesFirst): an overlap edge by the engine, so the claim gate skips it until that
// item is done or cancelled. It skips parts of the item's own assembly, items it already needs,
// and an edge the store refuses as a cycle or as one that exists. Before it adds edges it
// removes the waiting items' overlap edges whose reason no longer holds, so a stale hold clears
// itself. A line whose flow.assemblies.overlap is allow holds nothing.
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
	)

	for _, it := range waiting {
		if len(it.Paths) > 0 {
			held = append(held, it)
			ids = append(ids, it.ID)
		}
	}

	if len(held) == 0 {
		return nil
	}

	open, err := e.openWithPaths(ctx, l.FactoryID)
	if err != nil {
		return fmt.Errorf("station %s: overlap hold: %w", st.ID, err)
	}

	needs, err := e.store.OpenNeeds(ctx, l.FactoryID, ids)
	if err != nil {
		return fmt.Errorf("station %s: overlap hold: read needs: %w", st.ID, err)
	}

	byID := make(map[domain.ItemID]domain.Item, len(open))
	for _, o := range open {
		byID[o.ID] = o
	}

	p := newProgress(e.store, l)

	return errors.Join(e.releaseStale(ctx, p, held, needs, byID), e.holdBehind(ctx, p, held, open, needs))
}

// releaseStale removes the held items' overlap edges whose reason no longer holds.
func (e *Engine) releaseStale(ctx context.Context, p *progress, held []domain.Item, needs map[domain.ItemID][]domain.OpenNeed,
	open map[domain.ItemID]domain.Item,
) error {
	var errs []error

	for _, it := range held {
		for _, n := range needs[it.ID] {
			if n.Kind != domain.EdgeOverlap {
				continue
			}

			reason, err := p.stale(ctx, it, n, open)
			if err != nil {
				errs = append(errs, err)
				continue
			}

			if reason != "" {
				errs = append(errs, e.removeOverlap(ctx, it, n.Item, reason))
			}
		}
	}

	return errors.Join(errs...)
}

// holdBehind makes each held item need every open item it overlaps that goes first.
func (e *Engine) holdBehind(ctx context.Context, p *progress, held, open []domain.Item, needs map[domain.ItemID][]domain.OpenNeed) error {
	var errs []error

	for _, it := range held {
		for _, o := range open {
			if !overlaps(it, o, needs[it.ID]) {
				continue
			}

			first, err := p.goesFirst(ctx, o, it)
			if err != nil {
				errs = append(errs, err)
				continue
			}

			if first {
				errs = append(errs, e.addOverlap(ctx, it, o))
			}
		}
	}

	return errors.Join(errs...)
}

// openWithPaths returns the factory's open items that have paths, reading every page.
func (e *Engine) openWithPaths(ctx context.Context, factory domain.FactoryID) ([]domain.Item, error) {
	var (
		out    []domain.Item
		before int64
	)

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

// overlaps reports whether it may have to wait for o: o is another item, is not a part of its
// own assembly, is not already needed and their paths overlap. goesFirst decides whether it
// does.
func overlaps(it, o domain.Item, needs []domain.OpenNeed) bool {
	if o.ID == it.ID || o.ID == it.ParentID || (it.ParentID != "" && o.ParentID == it.ParentID) {
		return false
	}

	for _, n := range needs {
		if n.Item == o.ID {
			return false
		}
	}

	return len(domain.PathsOverlap(it.Paths, o.Paths)) > 0
}

// progress ranks items by how far along the line they are, reading an item's runs at most
// once per turn.
type progress struct {
	store ports.Store
	line  domain.Line
	// first is the index of the line's first agent station in line.Stations, or -1.
	first   int
	started map[domain.ItemID]bool
}

func newProgress(store ports.Store, l domain.Line) *progress {
	return &progress{
		store: store, line: l, started: map[domain.ItemID]bool{},
		first: slices.IndexFunc(l.Stations, func(s domain.Station) bool { return s.Kind == domain.KindAgent }),
	}
}

// index is the position of the item's station on the line, or -1 when it is not on it.
func (p *progress) index(it domain.Item) int {
	return slices.IndexFunc(p.line.Stations, func(s domain.Station) bool { return s.ID == it.Station })
}

// hasStarted reports whether the item is past the line's first agent station or has a run. An
// item whose station is not on the line has not started.
func (p *progress) hasStarted(ctx context.Context, it domain.Item) (bool, error) {
	i := p.index(it)
	if i < 0 {
		return false, nil
	}

	if p.first >= 0 && i > p.first {
		return true, nil
	}

	if s, ok := p.started[it.ID]; ok {
		return s, nil
	}

	runs, err := p.store.Runs(ctx, it.ID, 1)
	if err != nil {
		return false, fmt.Errorf("item %s: read runs: %w", it.ID, err)
	}

	p.started[it.ID] = len(runs) > 0

	return len(runs) > 0, nil
}

// goesFirst reports whether o goes before it: a started item before one that has not started,
// of two started items the one at the later station, and otherwise the lower number.
func (p *progress) goesFirst(ctx context.Context, o, it domain.Item) (bool, error) {
	os, err := p.hasStarted(ctx, o)
	if err != nil {
		return false, err
	}

	is, err := p.hasStarted(ctx, it)
	if err != nil {
		return false, err
	}

	switch {
	case os != is:
		return os, nil
	case os:
		if oi, ii := p.index(o), p.index(it); oi != ii {
			return oi > ii, nil
		}
	}

	return o.Number < it.Number, nil
}

// stale returns why it's overlap edge on n no longer holds, or "" when it still does. A need
// the factory's open items with paths do not include is judged by its paths alone.
func (p *progress) stale(ctx context.Context, it domain.Item, n domain.OpenNeed, open map[domain.ItemID]domain.Item) (string, error) {
	o, ok := open[n.Item]
	if !ok {
		if len(domain.PathsOverlap(it.Paths, n.Paths)) == 0 {
			return reasonNoOverlap, nil
		}

		return "", nil
	}

	if len(domain.PathsOverlap(it.Paths, o.Paths)) == 0 {
		return reasonNoOverlap, nil
	}

	first, err := p.goesFirst(ctx, o, it)
	if err != nil || first {
		return "", err
	}

	return reasonBehind, nil
}

// removeOverlap removes it's overlap edge on needs for reason. An edge already gone (another
// instance removed it) is skipped.
func (e *Engine) removeOverlap(ctx context.Context, it domain.Item, needs domain.ItemID, reason string) error {
	err := e.store.RemoveEdge(ctx, it.ID, needs, domain.EngineActor, e.clock.Now(), reason)

	switch {
	case errors.Is(err, ports.ErrNotFound):
		return nil
	case err != nil:
		return fmt.Errorf("item %s no longer needs %s: %w", it.ID, needs, err)
	}

	e.log.InfoContext(ctx, "overlap released", slog.String("factory", string(it.FactoryID)), slog.String("item", string(it.ID)),
		slog.String("needs", string(needs)), slog.String("reason", reason))

	return nil
}

// addOverlap records that it needs o for their overlapping paths. When the edge would close a
// cycle because o still has an overlap edge on it, written before it went first, that edge is
// stale: it is removed and the edge added once more. Any other edge that would close a cycle,
// and one that exists already (another instance wrote it), is skipped.
func (e *Engine) addOverlap(ctx context.Context, it, o domain.Item) error {
	err := e.addOverlapEdge(ctx, it, o)
	if errors.Is(err, domain.ErrCycle) {
		var reversed bool

		reversed, err = e.removeReverse(ctx, it, o)
		if err == nil && reversed {
			err = e.addOverlapEdge(ctx, it, o)
		} else if err == nil {
			err = domain.ErrCycle
		}
	}

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

func (e *Engine) addOverlapEdge(ctx context.Context, it, o domain.Item) error {
	_, err := e.store.AddEdge(ctx, domain.NewEdge{
		Item: it.ID, Needs: o.ID, Kind: domain.EdgeOverlap, By: domain.EngineActor, At: e.clock.Now(),
	})

	return err
}

// removeReverse removes o's overlap edge on it, reporting whether there was one. An edge of
// another kind is kept.
func (e *Engine) removeReverse(ctx context.Context, it, o domain.Item) (bool, error) {
	needs, err := e.store.OpenNeeds(ctx, o.FactoryID, []domain.ItemID{o.ID})
	if err != nil {
		return false, fmt.Errorf("read needs of %s: %w", o.ID, err)
	}

	if !slices.ContainsFunc(needs[o.ID], func(n domain.OpenNeed) bool { return n.Item == it.ID && n.Kind == domain.EdgeOverlap }) {
		return false, nil
	}

	return true, e.removeOverlap(ctx, o, it.ID, reasonBehind)
}
