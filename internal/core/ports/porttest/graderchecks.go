package porttest

import (
	"context"
	"fmt"
	"slices"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// RecordGraderCheck implements ports.PastTries.
func (s *Store) RecordGraderCheck(ctx context.Context, n domain.NewGraderCheck) (domain.GraderCheck, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.GraderCheck{}, err
	}

	if err := n.Check(); err != nil {
		return domain.GraderCheck{}, fmt.Errorf("record grader check: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	factory, err := s.graderCheckFactory(org, n)
	if err != nil {
		return domain.GraderCheck{}, fmt.Errorf("record grader check: %w", err)
	}

	c := domain.GraderCheck{
		ID: domain.GraderCheckID(s.newID()), FactoryID: factory, ProposalID: n.ProposalID, ItemID: n.ItemID, Result: n.Result, Answer: n.Answer,
		Reason: n.Reason, RunID: n.RunID, CostUSD: n.CostUSD, CheckedAt: n.CheckedAt,
	}
	s.jonah.graderChecks = append(s.jonah.graderChecks, jonahRow[domain.GraderCheck]{org: org, row: c})

	return c, nil
}

// graderCheckFactory is the factory of n's proposal: ErrNotFound when the proposal, item or run is
// not org's or the item not the proposal's factory's, ErrConflict when n's item and result has a
// check of the proposal. Callers hold s.mu.
func (s *Store) graderCheckFactory(org domain.OrgID, n domain.NewGraderCheck) (domain.FactoryID, error) {
	i := slices.IndexFunc(s.jonah.proposals, func(r jonahRow[domain.Proposal]) bool { return r.org == org && r.row.ID == n.ProposalID })
	if i < 0 {
		return "", fmt.Errorf("proposal %s: %w", n.ProposalID, ports.ErrNotFound)
	}

	factory := s.jonah.proposals[i].row.FactoryID

	if it, ok := s.visible(n.ItemID, org); !ok || it.FactoryID != factory {
		return "", fmt.Errorf("item %s: %w", n.ItemID, ports.ErrNotFound)
	}

	if n.RunID != "" {
		if run, ok := s.runs[n.RunID]; !ok || !s.runVisible(run, org) {
			return "", fmt.Errorf("run %s: %w", n.RunID, ports.ErrNotFound)
		}
	}

	if slices.ContainsFunc(s.jonah.graderChecks, func(r jonahRow[domain.GraderCheck]) bool {
		return r.org == org && r.row.ProposalID == n.ProposalID && r.row.ItemID == n.ItemID && r.row.Result == n.Result
	}) {
		return "", fmt.Errorf("item %s %s result: %w", n.ItemID, n.Result, ports.ErrConflict)
	}

	return factory, nil
}

// ProposalGraderChecks implements ports.PastTries.
func (s *Store) ProposalGraderChecks(ctx context.Context, proposal domain.ProposalID) ([]domain.GraderCheck, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.graderChecksOf(org, proposal), nil
}

// GraderHitRates implements ports.PastTries.
func (s *Store) GraderHitRates(ctx context.Context, proposal domain.ProposalID) (domain.GraderHitRates, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.GraderHitRates{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return domain.HitRatesOf(s.graderChecksOf(org, proposal)), nil
}

// graderChecksOf is org's checks of proposal, oldest first. Callers hold s.mu.
func (s *Store) graderChecksOf(org domain.OrgID, proposal domain.ProposalID) []domain.GraderCheck {
	out := []domain.GraderCheck{}

	for _, r := range s.jonah.graderChecks {
		if r.org == org && r.row.ProposalID == proposal {
			out = append(out, r.row)
		}
	}

	slices.SortStableFunc(out, func(a, b domain.GraderCheck) int { return a.CheckedAt.Compare(b.CheckedAt) })

	return out
}

// DeleteFactory deletes a factory and every row of it, as goal_delete_factory does in PostgreSQL.
func (s *Store) DeleteFactory(id domain.FactoryID) { s.deleteFactory(id) }
