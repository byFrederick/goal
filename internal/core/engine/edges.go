package engine

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/byFrederick/goal/internal/core/domain"
)

// Link records that a person's item needs another (ADR 0012): a linked edge by by, so the item is
// not claimed while needs is open. A done or cancelled item is refused with ErrItemClosed; the
// store refuses an item that needs itself or one of another factory (domain.ErrInvalid), a
// cycle (domain.ErrCycle) and an edge that exists (ports.ErrConflict).
func (e *Engine) Link(ctx context.Context, id, needs domain.ItemID, by domain.Actor) (domain.Edge, error) {
	if err := e.openItem(ctx, "link", id); err != nil {
		return domain.Edge{}, err
	}

	edge, err := e.store.AddEdge(ctx, domain.NewEdge{Item: id, Needs: needs, Kind: domain.EdgeLinked, By: by, At: e.clock.Now()})
	if err != nil {
		return domain.Edge{}, fmt.Errorf("link %s to %s: %w", id, needs, err)
	}

	e.log.InfoContext(ctx, "item linked", slog.String("item", string(id)), slog.String("needs", string(needs)),
		slog.String("by", by.Ref()))

	return edge, nil
}

// Unlink removes the edge id → needs, whatever its kind, by by. A done or cancelled item is
// refused with ErrItemClosed; a missing edge is ports.ErrNotFound.
func (e *Engine) Unlink(ctx context.Context, id, needs domain.ItemID, by domain.Actor) error {
	if err := e.openItem(ctx, "unlink", id); err != nil {
		return err
	}

	if err := e.store.RemoveEdge(ctx, id, needs, by, e.clock.Now(), ""); err != nil {
		return fmt.Errorf("unlink %s from %s: %w", id, needs, err)
	}

	e.log.InfoContext(ctx, "item unlinked", slog.String("item", string(id)), slog.String("needs", string(needs)),
		slog.String("by", by.Ref()))

	return nil
}

// openItem refuses op on an item that is done or cancelled.
func (e *Engine) openItem(ctx context.Context, op string, id domain.ItemID) error {
	it, err := e.store.Item(ctx, id)
	if err != nil {
		return fmt.Errorf("%s %s: %w", op, id, err)
	}

	if closed(it) {
		return fmt.Errorf("%s %s: %w", op, id, ErrItemClosed)
	}

	return nil
}
