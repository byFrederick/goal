package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"github.com/byFrederick/goal/internal/core/domain"
)

// deliverNotes delivers the waiting follow-up notes of every organization with loaded lines
// (ADR 0040 decision 7), up to SweepLimit each per tick. A note's for is resolved against all
// of the organization's loaded lines, paused or stopped ones too: they still take notes. The
// store routes each one (domain.RouteNote) and records an item_note or sends it to triage.
func (e *Engine) deliverNotes(ctx context.Context, lines map[domain.FactoryID]domain.Line) error {
	byOrg := map[domain.OrgID][]domain.NoteFactory{}

	for _, id := range slices.Sorted(maps.Keys(lines)) {
		l := lines[id]
		byOrg[l.OrgID] = append(byOrg[l.OrgID], domain.NoteFactoryOf(l))
	}

	var errs []error

	for _, org := range slices.Sorted(maps.Keys(byOrg)) {
		fs, err := e.store.DeliverNotes(domain.WithOrg(ctx, org), byOrg[org], e.clock.Now(), e.cfg.SweepLimit)

		for _, f := range fs {
			attrs := []any{
				slog.String("org_id", string(org)), slog.String("factory", string(f.FactoryID)),
				slog.String("item", string(f.ItemID)), slog.String("follow_up", string(f.ID)),
			}

			if f.State == domain.FollowUpDelivered {
				e.log.InfoContext(ctx, "follow-up note delivered", attrs...)
			} else {
				e.log.InfoContext(ctx, "follow-up note sent to triage", append(attrs, slog.String("reason", f.Reason))...)
			}
		}

		if err != nil {
			errs = append(errs, fmt.Errorf("deliver notes of organization %s: %w", org, err))
		}
	}

	return errors.Join(errs...)
}
