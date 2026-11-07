package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// MarkBudgetExhausted implements ports.Store over 00134's factory_budget_exhaustions: the first
// since is kept (ON CONFLICT DO NOTHING).
func (s *Store) MarkBudgetExhausted(ctx context.Context, factory domain.FactoryID, since time.Time) error {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return err
	}

	err = s.inTx(ctx, func(q *db.Queries) error {
		return q.MarkBudgetExhausted(ctx, db.MarkBudgetExhaustedParams{FactoryID: fid, Since: since.UTC()})
	})

	switch {
	case isForeignKeyViolation(err):
		return fmt.Errorf("mark the budget of factory %s used up: %w", factory, ports.ErrNotFound)
	case err != nil:
		return fmt.Errorf("mark the budget of factory %s used up: %w", factory, err)
	default:
		return nil
	}
}

// ClearBudgetExhausted implements ports.Store.
func (s *Store) ClearBudgetExhausted(ctx context.Context, factory domain.FactoryID) error {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return err
	}

	if err := s.inTx(ctx, func(q *db.Queries) error { return q.ClearBudgetExhausted(ctx, fid) }); err != nil {
		return fmt.Errorf("clear the used-up budget of factory %s: %w", factory, err)
	}

	return nil
}

// BudgetExhaustedSince implements ports.Store.
func (s *Store) BudgetExhaustedSince(ctx context.Context, factory domain.FactoryID) (time.Time, bool, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return time.Time{}, false, err
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]time.Time, error) { return q.BudgetExhaustedSince(ctx, fid) })
	if err != nil {
		return time.Time{}, false, fmt.Errorf("since when the budget of factory %s is used up: %w", factory, err)
	}

	if len(rows) == 0 {
		return time.Time{}, false, nil
	}

	return rows[0].UTC(), true, nil
}
