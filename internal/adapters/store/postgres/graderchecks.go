package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// RecordGraderCheck implements ports.PastTries: the insert takes its factory from the proposal and
// inserts nothing for an item of another factory.
func (s *Store) RecordGraderCheck(ctx context.Context, n domain.NewGraderCheck) (domain.GraderCheck, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.GraderCheck{}, domain.ErrNoOrg
	}

	if err := n.Check(); err != nil {
		return domain.GraderCheck{}, fmt.Errorf("record grader check: %w", err)
	}

	p, err := parseID("proposal", string(n.ProposalID))
	if err != nil {
		return domain.GraderCheck{}, err
	}

	item, err := parseID("item", string(n.ItemID))
	if err != nil {
		return domain.GraderCheck{}, err
	}

	run, err := optionalID("run", string(n.RunID))
	if err != nil {
		return domain.GraderCheck{}, err
	}

	id, err := newID()
	if err != nil {
		return domain.GraderCheck{}, err
	}

	var out domain.GraderCheck

	err = s.inTx(ctx, func(q *db.Queries) error {
		c, err := q.InsertGraderCheck(ctx, db.InsertGraderCheckParams{
			ID: id, Result: string(n.Result), Answer: string(n.Answer), Reason: n.Reason, RunID: run, CostUsd: n.CostUSD, CheckedAt: n.CheckedAt,
			ItemID: item, ProposalID: p,
		})

		var pg *pgconn.PgError

		switch {
		case notFound(err):
			return fmt.Errorf("proposal %s or item %s: %w", n.ProposalID, n.ItemID, ports.ErrNotFound)
		case isForeignKeyViolation(err):
			return fmt.Errorf("run %s: %w", n.RunID, ports.ErrNotFound)
		case errors.As(err, &pg) && pg.Code == uniqueViolation:
			return fmt.Errorf("item %s %s result: %w", n.ItemID, n.Result, ports.ErrConflict)
		case err != nil:
			return fmt.Errorf("insert grader check: %w", err)
		}

		out = graderCheckFrom(c)

		return nil
	})
	if err != nil {
		return domain.GraderCheck{}, fmt.Errorf("record grader check: %w", err)
	}

	return out, nil
}

// ProposalGraderChecks implements ports.PastTries.
func (s *Store) ProposalGraderChecks(ctx context.Context, proposal domain.ProposalID) ([]domain.GraderCheck, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return nil, domain.ErrNoOrg
	}

	p, err := parseID("proposal", string(proposal))
	if err != nil {
		return nil, err
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.GraderCheck, error) { return q.ProposalGraderChecks(ctx, p) })
	if err != nil {
		return nil, fmt.Errorf("grader checks of proposal %s: %w", proposal, err)
	}

	out := make([]domain.GraderCheck, len(rows))
	for i, r := range rows {
		out[i] = graderCheckFrom(r)
	}

	return out, nil
}

// GraderHitRates implements ports.PastTries.
func (s *Store) GraderHitRates(ctx context.Context, proposal domain.ProposalID) (domain.GraderHitRates, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.GraderHitRates{}, domain.ErrNoOrg
	}

	p, err := parseID("proposal", string(proposal))
	if err != nil {
		return domain.GraderHitRates{}, err
	}

	r, err := fetch(ctx, s.read, func(q *db.Queries) (db.GraderHitRatesRow, error) { return q.GraderHitRates(ctx, p) })
	if err != nil {
		return domain.GraderHitRates{}, fmt.Errorf("grader hit rates of proposal %s: %w", proposal, err)
	}

	return domain.GraderHitRates{
		OriginalHits: int(r.OriginalHits), OriginalTotal: int(r.OriginalTotal), FinalHits: int(r.FinalHits), FinalTotal: int(r.FinalTotal),
	}, nil
}

func graderCheckFrom(c db.GraderCheck) domain.GraderCheck {
	out := domain.GraderCheck{
		ID: domain.GraderCheckID(c.ID.String()), FactoryID: domain.FactoryID(c.FactoryID.String()), ProposalID: domain.ProposalID(c.ProposalID.String()),
		ItemID: domain.ItemID(c.ItemID.String()), Result: domain.GraderCheckResult(c.Result), Answer: domain.PastTryVerdict(c.Answer), Reason: c.Reason,
		CostUSD: c.CostUsd, CheckedAt: c.CheckedAt.UTC(),
	}

	if c.RunID != nil {
		out.RunID = domain.RunID(c.RunID.String())
	}

	return out
}
