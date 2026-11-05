package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// fileFollowUps files the waiting start and triage follow-ups of every organization with loaded
// lines whose work landed or was cancelled (ADR 0040 decisions 8, 9 and 10), up to SweepLimit
// each per tick. Every loaded line takes them, a paused or stopped one too. The store routes each
// one (domain.LandFollowUp): it drops it, files it as an item, or adds it to the factory's triage
// item.
func (e *Engine) fileFollowUps(ctx context.Context, lines map[domain.FactoryID]domain.Line) error {
	byOrg := map[domain.OrgID][]domain.Line{}

	for _, id := range slices.Sorted(maps.Keys(lines)) {
		l := lines[id]
		byOrg[l.OrgID] = append(byOrg[l.OrgID], l)
	}

	var errs []error

	for _, org := range slices.Sorted(maps.Keys(byOrg)) {
		fs, err := e.store.FileFollowUps(domain.WithOrg(ctx, org), byOrg[org], e.clock.Now(), e.cfg.SweepLimit)

		for _, f := range fs {
			attrs := []any{
				slog.String("org_id", string(org)), slog.String("factory", string(f.FactoryID)),
				slog.String("item", string(f.ItemID)), slog.String("follow_up", string(f.ID)),
			}

			switch {
			case f.State == domain.FollowUpDropped:
				e.log.InfoContext(ctx, "follow-up dropped", append(attrs, slog.String("reason", f.Reason))...)
			case f.Target == domain.FollowUpTriage:
				e.log.InfoContext(ctx, "follow-up added to triage", append(attrs, slog.String("triage_item", string(f.FiledItem)))...)
			default:
				e.log.InfoContext(ctx, "follow-up filed", append(attrs, slog.String("filed_item", string(f.FiledItem)))...)
			}
		}

		if err != nil {
			errs = append(errs, fmt.Errorf("file follow-ups of organization %s: %w", org, err))
		}
	}

	return errors.Join(errs...)
}

// DropFollowUp drops follow-up id of item, which must be waiting, for by (ADR 0040 decision 10).
// One of another item, or of an item the organization in ctx cannot see, is ports.ErrNotFound;
// one that is not waiting ports.ErrConflict.
func (e *Engine) DropFollowUp(ctx context.Context, item domain.ItemID, id domain.FollowUpID, by domain.Actor) (domain.FollowUp, error) {
	f, err := e.store.DropFollowUp(ctx, item, id, by, e.clock.Now())
	if err != nil {
		return domain.FollowUp{}, fmt.Errorf("drop follow-up: %w", err)
	}

	return f, nil
}

// FileFollowUp files r, a person's item, for follow-up id of item through domain.File on the
// source's line (ADR 0040 decision 10): origin follow-up, a generation one above the source's and
// the source's untrusted. Only a dropped follow-up, or one in a triage item that is done or
// cancelled, may be filed (ports.ErrConflict otherwise); an unknown one is ports.ErrNotFound, and
// what the line does not take domain.ErrInvalid.
func (e *Engine) FileFollowUp(ctx context.Context, item domain.ItemID, id domain.FollowUpID, r domain.FileRequest) (domain.Item, error) {
	src, err := e.store.Item(ctx, item)
	if err != nil {
		return domain.Item{}, fmt.Errorf("file follow-up: %w", err)
	}

	fs, err := e.store.FollowUps(ctx, item)
	if err != nil {
		return domain.Item{}, fmt.Errorf("file follow-up: %w", err)
	}

	if !slices.ContainsFunc(fs, func(f domain.FollowUp) bool { return f.ID == id }) {
		return domain.Item{}, fmt.Errorf("file follow-up %s of item %s: %w", id, item, ports.ErrNotFound)
	}

	l, err := e.lineIn(ctx, src.FactoryID)
	if err != nil {
		return domain.Item{}, err
	}

	r.FactoryID, r.Origin, r.Generation, r.Untrusted, r.At = src.FactoryID, domain.OriginFollowUp, src.Generation+1, src.Untrusted, e.clock.Now()

	n, err := domain.File(l, r)
	if err != nil {
		return domain.Item{}, fmt.Errorf("file follow-up: %w", err)
	}

	_, it, err := e.store.FileFollowUp(ctx, item, id, n)
	if err != nil {
		return domain.Item{}, fmt.Errorf("file follow-up: %w", err)
	}

	return it, nil
}
