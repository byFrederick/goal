package porttest

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.PastTries = (*Store)(nil)

// QueuePastTries implements ports.PastTries.
func (s *Store) QueuePastTries(ctx context.Context, factory domain.FactoryID, proposal domain.ProposalID, tries []domain.NewPastTry,
	at time.Time,
) ([]domain.PastTry, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	for _, n := range tries {
		if err := n.Check(); err != nil {
			return nil, fmt.Errorf("queue past tries: %w", err)
		}
	}

	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	number := s.proposalNumber(org, factory, proposal)
	if number == 0 {
		return nil, fmt.Errorf("queue past tries: proposal %s: %w", proposal, ports.ErrNotFound)
	}

	if slices.ContainsFunc(s.jonah.tries, func(r jonahRow[domain.PastTry]) bool { return r.org == org && r.row.ProposalID == proposal }) {
		return nil, fmt.Errorf("queue past tries: proposal %s has tries: %w", proposal, ports.ErrConflict)
	}

	out, err := s.newPastTries(org, factory, proposal, number, tries, at)
	if err != nil {
		return nil, err
	}

	for _, t := range out {
		s.jonah.tries = append(s.jonah.tries, jonahRow[domain.PastTry]{org: org, row: t})
	}

	return out, nil
}

// proposalNumber is the number of org's factory's proposal, 0 when there is none. Callers hold s.mu.
func (s *Store) proposalNumber(org domain.OrgID, factory domain.FactoryID, proposal domain.ProposalID) int64 {
	for _, p := range s.jonah.proposals {
		if p.org == org && p.row.FactoryID == factory && p.row.ID == proposal {
			return p.row.Number
		}
	}

	return 0
}

// newPastTries are the queued tries of tries, checked against org's items and runs. Callers hold s.mu.
func (s *Store) newPastTries(org domain.OrgID, factory domain.FactoryID, proposal domain.ProposalID, number int64,
	tries []domain.NewPastTry, at time.Time,
) ([]domain.PastTry, error) {
	out := make([]domain.PastTry, 0, len(tries))

	for _, n := range tries {
		it, ok := s.visible(n.ItemID, org)
		run, rok := s.runs[n.OriginalRunID]

		if !ok || it.FactoryID != factory || !rok || !s.runVisible(run, org) {
			return nil, fmt.Errorf("queue past tries: item %s or run %s: %w", n.ItemID, n.OriginalRunID, ports.ErrNotFound)
		}

		if slices.ContainsFunc(out, func(t domain.PastTry) bool { return t.ItemID == n.ItemID && t.Attempt == n.TryAttempt() }) {
			return nil, fmt.Errorf("queue past tries: item %s attempt %d twice: %w", n.ItemID, n.TryAttempt(), ports.ErrConflict)
		}

		out = append(out, domain.PastTry{
			ID: domain.PastTryID(s.newID()), FactoryID: factory, ProposalID: proposal, ProposalNumber: number, ItemID: n.ItemID, Attempt: n.TryAttempt(),
			Station: n.Station, OriginalRunID: n.OriginalRunID, Model: n.Model, State: domain.PastTryQueued, CreatedAt: at,
		})
	}

	return out, nil
}

// ProposalPastTries implements ports.PastTries.
func (s *Store) ProposalPastTries(ctx context.Context, proposal domain.ProposalID) ([]domain.PastTry, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	out := []domain.PastTry{}

	for _, r := range s.jonah.tries {
		if r.org == org && r.row.ProposalID == proposal {
			out = append(out, r.row)
		}
	}

	return out, nil
}

// PastTry implements ports.PastTries.
func (s *Store) PastTry(ctx context.Context, id domain.PastTryID) (domain.PastTry, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.PastTry{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, r := range s.jonah.tries {
		if r.org == org && r.row.ID == id {
			return r.row, nil
		}
	}

	return domain.PastTry{}, fmt.Errorf("past try %s: %w", id, ports.ErrNotFound)
}

// ClaimPastTry implements ports.PastTries.
func (s *Store) ClaimPastTry(ctx context.Context, factory domain.FactoryID, now time.Time, lease time.Duration, limit int) (domain.PastTry, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.PastTry{}, err
	}

	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	running, next := 0, -1

	for i, r := range s.jonah.tries {
		if r.org != org || r.row.FactoryID != factory {
			continue
		}

		live := r.row.State == domain.PastTryRunning && r.row.LeaseUntil.After(now)
		if live {
			running++
		}

		if next < 0 && (r.row.State == domain.PastTryQueued || (r.row.State == domain.PastTryRunning && !live)) {
			next = i
		}
	}

	if next < 0 || running >= limit {
		return domain.PastTry{}, fmt.Errorf("claim past try of factory %s: %w", factory, ports.ErrNotFound)
	}

	t := &s.jonah.tries[next].row
	t.State, t.LeaseUntil, t.Attempts = domain.PastTryRunning, now.Add(lease), t.Attempts+1

	return *t, nil
}

// FinishPastTry implements ports.PastTries.
func (s *Store) FinishPastTry(ctx context.Context, claimed domain.PastTry, r domain.PastTryResult) (domain.PastTry, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.PastTry{}, err
	}

	if err := r.Check(); err != nil {
		return domain.PastTry{}, fmt.Errorf("finish past try %s: %w", claimed.ID, err)
	}

	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	for i := range s.jonah.tries {
		t := &s.jonah.tries[i].row
		if s.jonah.tries[i].org != org || t.ID != claimed.ID {
			continue
		}

		if t.State != domain.PastTryRunning || !t.LeaseUntil.Equal(claimed.LeaseUntil) {
			return domain.PastTry{}, fmt.Errorf("finish past try %s: %w", claimed.ID, ports.ErrLeaseLost)
		}

		t.State, t.LeaseUntil, t.FinishedAt = domain.PastTryDone, time.Time{}, r.At
		t.Verdict, t.Error, t.Reason, t.Diff, t.CostUSD, t.TryRunID = r.Verdict, r.Error, r.Reason, r.Diff, r.CostUSD, r.TryRunID

		return *t, nil
	}

	return domain.PastTry{}, fmt.Errorf("finish past try %s: %w", claimed.ID, ports.ErrLeaseLost)
}

// PastTryTarget implements ports.PastTries.
func (s *Store) PastTryTarget(ctx context.Context, item domain.ItemID, station string) (domain.Run, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.Run{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var best *domain.Run

	for id, r := range s.runs {
		if !s.firstRunAt(org, id, r, item, station) {
			continue
		}

		if best == nil || r.StartedAt.After(best.StartedAt) || (r.StartedAt.Equal(best.StartedAt) && r.ID > best.ID) {
			best = r
		}
	}

	if best == nil {
		return domain.Run{}, fmt.Errorf("past try target of item %s at %s: %w", item, station, ports.ErrNotFound)
	}

	return *best, nil
}

// firstRunAt reports whether run id, r, is a finished station run of org's item at station that
// was a claim's first: its branch head is recorded. Callers hold s.mu.
func (s *Store) firstRunAt(org domain.OrgID, id domain.RunID, r *domain.Run, item domain.ItemID, station string) bool {
	return r.ItemID == item && r.Station == station && s.runVisible(r, org) && s.jonah.runKinds[id] == domain.RunKindStation &&
		r.Result.Status != domain.RunRunning && r.Input.BranchHead != ""
}

// AgentDoneItems implements ports.PastTries.
func (s *Store) AgentDoneItems(ctx context.Context, factory domain.FactoryID, agent string, limit int) ([]domain.AgentItem, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	out := []domain.AgentItem{}

	for id, it := range s.items {
		if it.OrgID != org || it.FactoryID != factory || it.State != domain.StateDone {
			continue
		}

		var best *domain.Run

		for rid, r := range s.runs {
			if r.Agent != agent || !s.firstRunAt(org, rid, r, id, r.Station) {
				continue
			}

			if best == nil || r.StartedAt.After(best.StartedAt) || (r.StartedAt.Equal(best.StartedAt) && r.ID > best.ID) {
				best = r
			}
		}

		if best == nil {
			continue
		}

		a := domain.AgentItem{ItemID: id, ItemNumber: it.Number, ItemTitle: it.Title, Run: *best, CostUSD: it.CostUSD, DoneAt: it.DoneAt}

		for _, e := range s.events {
			if e.ItemID == id && (e.Kind == domain.EventRejected || e.Kind == domain.EventGateRejected) {
				a.SentBackAt = append(a.SentBackAt, e.Station)
			}
		}

		out = append(out, a)
	}

	slices.SortFunc(out, func(a, b domain.AgentItem) int { return cmp.Or(b.DoneAt.Compare(a.DoneAt), cmp.Compare(a.ItemID, b.ItemID)) })

	return out[:min(len(out), max(limit, 0))], nil
}

// PastTryInput implements ports.PastTries.
func (s *Store) PastTryInput(ctx context.Context, run domain.RunID) (domain.PastTryInput, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.PastTryInput{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.runs[run]
	if !ok || !s.runVisible(r, org) || r.ItemID == "" {
		return domain.PastTryInput{}, fmt.Errorf("run %s: %w", run, ports.ErrNotFound)
	}

	return domain.PastTryInput{Run: *r, Outputs: s.outputsBefore(r.ItemID, r.StartedAt)}, nil
}

// outputsBefore is item's latest output of each station among its runs that ended before before.
// Callers hold s.mu.
func (s *Store) outputsBefore(item domain.ItemID, before time.Time) map[string]json.RawMessage {
	latest := map[string]*domain.Run{}

	for _, o := range s.runs {
		if o.ItemID != item || o.EndedAt.IsZero() || !o.EndedAt.Before(before) || len(o.Result.Output) == 0 {
			continue
		}

		if l := latest[o.Station]; l == nil || o.EndedAt.After(l.EndedAt) || (o.EndedAt.Equal(l.EndedAt) && o.ID > l.ID) {
			latest[o.Station] = o
		}
	}

	outputs := make(map[string]json.RawMessage, len(latest))
	for st, o := range latest {
		outputs[st] = slices.Clone(o.Result.Output)
	}

	return outputs
}
