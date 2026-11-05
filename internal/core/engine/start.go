package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// ErrItemStarted is a change of the start of an item that has started: it left its line's first
// station, or an agent took it there.
var ErrItemStarted = errors.New("item has started; its start can no longer change")

// SetStart changes when a person's item may start (ADR 0040 decision 3), by by. A time or a wait
// replaces the item's start; only after items keep it. Each open after item becomes a linked
// edge, one it already needs is kept as it is, and the store refuses an edge that would close a
// cycle (domain.ErrCycle). A done or cancelled item is ErrItemClosed, one that has started
// ErrItemStarted, a start that cannot be domain.ErrInvalid.
func (e *Engine) SetStart(ctx context.Context, id domain.ItemID, r domain.StartRequest, by domain.Actor) (domain.Item, error) {
	it, err := e.unstarted(ctx, "set the start of", id)
	if err != nil {
		return domain.Item{}, err
	}

	now := e.clock.Now()

	s, err := r.Resolve(it.FactoryID, now)
	if err != nil {
		return domain.Item{}, fmt.Errorf("set the start of %s: %w", id, err)
	}

	for _, need := range s.After {
		_, err := e.store.AddEdge(ctx, domain.NewEdge{Item: id, Needs: need, Kind: domain.EdgeLinked, By: by, At: now})
		if err != nil && !errors.Is(err, ports.ErrConflict) {
			return domain.Item{}, fmt.Errorf("set the start of %s: after %s: %w", id, need, err)
		}
	}

	at := s.At
	if r.At == "" && r.In == "" {
		at = it.StartAt
	}

	return e.writeStart(ctx, it, at, by, now)
}

// StartNow clears a person's item's start, by by, so it is released on the next tick; its
// after items still hold it. The refusals are SetStart's.
func (e *Engine) StartNow(ctx context.Context, id domain.ItemID, by domain.Actor) (domain.Item, error) {
	it, err := e.unstarted(ctx, "start now", id)
	if err != nil {
		return domain.Item{}, err
	}

	return e.writeStart(ctx, it, time.Time{}, by, e.clock.Now())
}

// writeStart stores it's start at, guarded by its position.
func (e *Engine) writeStart(ctx context.Context, it domain.Item, at time.Time, by domain.Actor, now time.Time) (domain.Item, error) {
	out, err := e.store.SetStart(ctx, it.ID, it.Position(), at, by, now)
	if errors.Is(err, ports.ErrConflict) {
		return domain.Item{}, fmt.Errorf("set the start of %s: %w", it.ID, ErrItemChanged)
	}

	if err != nil {
		return domain.Item{}, fmt.Errorf("set the start of %s: %w", it.ID, err)
	}

	e.log.InfoContext(ctx, "item start set", slog.String("item", string(it.ID)), slog.Bool("now", at.IsZero()),
		slog.String("by", by.Ref()))

	return out, nil
}

// unstarted returns the item id of a line of the organization in ctx when it has not started:
// open, waiting at its line's first station.
func (e *Engine) unstarted(ctx context.Context, op string, id domain.ItemID) (domain.Item, error) {
	it, err := e.store.Item(ctx, id)
	if err != nil {
		return domain.Item{}, fmt.Errorf("%s %s: %w", op, id, err)
	}

	if closed(it) {
		return domain.Item{}, fmt.Errorf("%s %s: %w", op, id, ErrItemClosed)
	}

	l, err := e.lineIn(ctx, it.FactoryID)
	if err != nil {
		return domain.Item{}, fmt.Errorf("%s %s: %w", op, id, err)
	}

	first, err := l.First()
	if err != nil {
		return domain.Item{}, fmt.Errorf("%s %s: %w", op, id, err)
	}

	if it.Station != first.ID || !it.StartedAt.IsZero() || !it.State.Waiting() {
		return domain.Item{}, fmt.Errorf("%s %s: it is %s at %s: %w", op, id, it.State, it.Station, ErrItemStarted)
	}

	return it, nil
}
