package postgres

import (
	"context"
	"fmt"

	"github.com/byFrederick/goal/internal/adapters/postgres/db"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

var _ ports.FlowStats = (*Store)(nil)

// ItemSpans implements ports.FlowStats. The spans are item_spans rows, which the work_items
// trigger of migration 00030 keeps.
func (s *Store) ItemSpans(ctx context.Context, id domain.ItemID) ([]domain.Span, error) {
	uid, err := parseID("item", string(id))
	if err != nil {
		return nil, err
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.ItemSpansRow, error) { return q.ItemSpans(ctx, uid) })
	if err != nil {
		return nil, fmt.Errorf("spans of item %s: %w", id, err)
	}

	out := make([]domain.Span, 0, len(rows))
	for _, r := range rows {
		sp := domain.Span{
			Station: r.Station, State: domain.ItemState(r.State), ReworkRound: int(r.ReworkRound),
			ReworkBy: deref(r.ReworkBy), EnteredAt: r.EnteredAt.UTC(),
		}
		if r.LeftAt != nil {
			sp.LeftAt = r.LeftAt.UTC()
		}

		out = append(out, sp)
	}

	return out, nil
}

// CurrentRun implements ports.FlowStats.
func (s *Store) CurrentRun(ctx context.Context, id domain.ItemID) (*domain.CurrentRun, error) {
	uid, err := parseID("item", string(id))
	if err != nil {
		return nil, err
	}

	r, err := fetch(ctx, s.read, func(q *db.Queries) (db.CurrentRunRow, error) {
		return q.CurrentRun(ctx, db.CurrentRunParams{ItemID: uid, UsageKind: string(domain.EventUsage)})
	})
	if notFound(err) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("current run of item %s: %w", id, err)
	}

	run := domain.Run{
		ID: domain.RunID(r.ID.String()), FactoryID: domain.FactoryID(r.FactoryID.String()), ItemID: id,
		Station: r.Station, Agent: r.Agent, Model: r.Model, StartedAt: r.StartedAt.UTC(),
		Result: domain.RunResult{
			Status: domain.RunStatus(r.Status), CostUSD: r.CostUsd, Turns: int(r.Turns), InputTokens: r.InputTokens,
			OutputTokens: r.OutputTokens, SessionID: deref(r.SessionID), Error: deref(r.Error),
		},
	}
	if r.DefinitionVersionID != nil {
		run.DefinitionVersionID = domain.DefinitionVersionID(r.DefinitionVersionID.String())
	}

	return &domain.CurrentRun{Run: run, Slot: int(r.LeaseSlot), Turns: int(r.UsageTurns)}, nil
}
