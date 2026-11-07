package porttest

import (
	"context"
	"fmt"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// MarkBudgetExhausted implements ports.Store.
func (s *Store) MarkBudgetExhausted(ctx context.Context, factory domain.FactoryID, since time.Time) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.ownsFactory(org, factory) {
		return fmt.Errorf("mark the budget of factory %s used up: %w", factory, ports.ErrNotFound)
	}

	if _, marked := s.exhausted[factory]; !marked {
		s.exhausted[factory] = since.UTC()
	}

	return nil
}

// ClearBudgetExhausted implements ports.Store.
func (s *Store) ClearBudgetExhausted(ctx context.Context, factory domain.FactoryID) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ownsFactory(org, factory) {
		delete(s.exhausted, factory)
	}

	return nil
}

// BudgetExhaustedSince implements ports.Store.
func (s *Store) BudgetExhaustedSince(ctx context.Context, factory domain.FactoryID) (time.Time, bool, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return time.Time{}, false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.ownsFactory(org, factory) {
		return time.Time{}, false, nil
	}

	since, ok := s.exhausted[factory]

	return since, ok, nil
}
