package porttest

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.Watches = (*Store)(nil)

// SetStationOutcomes sets the outcomes StationOutcomes reads at station of factory. PostgreSQL
// works them out from the runs, touches and spans (00100's StationOutcomes); the fake is given
// them, as the engine's tests drive the outcomes and not the events behind them.
func (s *Store) SetStationOutcomes(factory domain.FactoryID, station string, outcomes []domain.StationOutcome) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.jonah.outcomes == nil {
		s.jonah.outcomes = map[domain.FactoryID]map[string][]domain.StationOutcome{}
	}

	if s.jonah.outcomes[factory] == nil {
		s.jonah.outcomes[factory] = map[string][]domain.StationOutcome{}
	}

	s.jonah.outcomes[factory][station] = slices.Clone(outcomes)
}

// StationOutcomes implements ports.Watches over the outcomes SetStationOutcomes gave.
func (s *Store) StationOutcomes(ctx context.Context, factory domain.FactoryID, station string, versions []domain.DefinitionVersionID) ([]domain.StationOutcome, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.ownsFactory(org, factory) {
		return []domain.StationOutcome{}, nil
	}

	out := []domain.StationOutcome{}

	for _, o := range s.jonah.outcomes[factory][station] {
		if slices.Contains(versions, o.VersionID) {
			out = append(out, o)
		}
	}

	slices.SortStableFunc(out, func(a, b domain.StationOutcome) int { return a.At.Compare(b.At) })

	return out, nil
}

// SaveWatch implements ports.Watches.
func (s *Store) SaveWatch(ctx context.Context, w domain.Watch) (bool, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.ownsFactory(org, w.FactoryID) {
		return false, fmt.Errorf("save watch: factory %s: %w", w.FactoryID, ports.ErrNotFound)
	}

	w.ProposalNumber = 0

	for i, r := range s.jonah.watches {
		row := r.row
		if r.org != org || row.FactoryID != w.FactoryID || row.VersionID != w.VersionID || row.Station != w.Station || row.Metric != w.Metric {
			continue
		}

		if !row.ClosedAt.IsZero() {
			return false, nil
		}

		w.OpenedAt = row.OpenedAt
		s.jonah.watches[i].row = cloneWatch(w)

		return true, nil
	}

	s.jonah.watches = append(s.jonah.watches, jonahRow[domain.Watch]{org, cloneWatch(w)})

	return true, nil
}

// VersionWatches implements ports.Watches.
func (s *Store) VersionWatches(ctx context.Context, factory domain.FactoryID, versions []domain.DefinitionVersionID) ([]domain.Watch, error) {
	return s.watchesWhere(ctx, func(w domain.Watch) bool { return w.FactoryID == factory && slices.Contains(versions, w.VersionID) })
}

// ProposalWatches implements ports.Watches.
func (s *Store) ProposalWatches(ctx context.Context, proposals []domain.ProposalID) ([]domain.Watch, error) {
	return s.watchesWhere(ctx, func(w domain.Watch) bool { return w.ProposalID != "" && slices.Contains(proposals, w.ProposalID) })
}

// WorseProposalWatches implements ports.Watches.
func (s *Store) WorseProposalWatches(ctx context.Context, limit int) ([]domain.Watch, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	out := []domain.Watch{}

	for _, r := range s.jonah.watches {
		if r.org != org || r.row.ProposalID == "" || r.row.Verdict != domain.WatchWorse || s.undone(org, r.row.ProposalID) {
			continue
		}

		w := cloneWatch(r.row)

		for _, p := range s.jonah.proposals {
			if p.org == org && p.row.ID == w.ProposalID {
				w.ProposalNumber = p.row.Number
			}
		}

		out = append(out, w)
	}

	slices.SortStableFunc(out, func(a, b domain.Watch) int { return b.UpdatedAt.Compare(a.UpdatedAt) })

	return out[:min(len(out), max(limit, 0))], nil
}

// undone reports whether an undo of proposal is proposed, published or applied. Callers hold s.mu.
func (s *Store) undone(org domain.OrgID, proposal domain.ProposalID) bool {
	return slices.ContainsFunc(s.jonah.proposals, func(p jonahRow[domain.Proposal]) bool {
		st := p.row.State

		return p.org == org && p.row.Undoes == proposal &&
			(st == domain.ProposalProposed || st == domain.ProposalPublished || st == domain.ProposalApplied)
	})
}

func (s *Store) watchesWhere(ctx context.Context, keep func(domain.Watch) bool) ([]domain.Watch, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	out := []domain.Watch{}

	for _, r := range s.jonah.watches {
		if r.org == org && keep(r.row) {
			out = append(out, cloneWatch(r.row))
		}
	}

	slices.SortFunc(out, func(a, b domain.Watch) int {
		return cmp.Or(cmp.Compare(a.VersionID, b.VersionID), cmp.Compare(a.Station, b.Station), cmp.Compare(a.Metric, b.Metric))
	})

	return out, nil
}

func cloneWatch(w domain.Watch) domain.Watch {
	if w.Interval != nil {
		iv := *w.Interval
		w.Interval = &iv
	}

	return w
}
