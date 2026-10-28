package porttest

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.JonahStore = (*Store)(nil)

// jonahRows are the store's Jonah reviews, proposals and observations, each with its
// organization, and the kind of every run StartRun recorded with one.
type jonahRows struct {
	reviews      []jonahRow[domain.JonahReview]
	proposals    []jonahRow[domain.Proposal]
	observations []jonahRow[domain.Observation]
	runKinds     map[domain.RunID]domain.RunKind
}

// jonahRow is one Jonah row of an organization.
type jonahRow[T any] struct {
	org domain.OrgID
	row T
}

// purgeJonah removes org's Jonah rows, only factory's when factory is set, as the purges'
// cascades do in PostgreSQL. Callers hold s.mu.
func (s *Store) purgeJonah(org domain.OrgID, factory domain.FactoryID) {
	gone := func(o domain.OrgID, f domain.FactoryID) bool { return o == org && (factory == "" || f == factory) }

	s.jonah.reviews = slices.DeleteFunc(s.jonah.reviews, func(r jonahRow[domain.JonahReview]) bool { return gone(r.org, r.row.FactoryID) })
	s.jonah.proposals = slices.DeleteFunc(s.jonah.proposals, func(r jonahRow[domain.Proposal]) bool { return gone(r.org, r.row.FactoryID) })
	s.jonah.observations = slices.DeleteFunc(s.jonah.observations, func(r jonahRow[domain.Observation]) bool {
		return gone(r.org, r.row.FactoryID)
	})
}

// ownsFactory reports whether factory is org's. Callers hold s.mu.
func (s *Store) ownsFactory(org domain.OrgID, factory domain.FactoryID) bool {
	owner, ok := s.factories[factory]

	return ok && owner == org
}

// QueueJonahReview implements ports.JonahStore.
func (s *Store) QueueJonahReview(ctx context.Context, n domain.NewJonahReview) (domain.JonahReview, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.JonahReview{}, err
	}

	if err := n.Check(); err != nil {
		return domain.JonahReview{}, fmt.Errorf("queue jonah review: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.ownsFactory(org, n.FactoryID) {
		return domain.JonahReview{}, fmt.Errorf("queue jonah review: factory %s: %w", n.FactoryID, ports.ErrNotFound)
	}

	for _, r := range s.jonah.reviews {
		if r.org == org && r.row.FactoryID == n.FactoryID && inFlight(r.row.State) {
			return r.row, fmt.Errorf("queue jonah review: factory %s has review %s in flight: %w", n.FactoryID, r.row.ID, ports.ErrConflict)
		}
	}

	r := domain.JonahReview{
		ID: domain.JonahReviewID(s.newID()), FactoryID: n.FactoryID, VersionID: n.VersionID, Trigger: n.Trigger,
		State: domain.JonahPending, WindowStart: n.WindowStart, WindowEnd: n.WindowEnd, CreatedBy: n.By.Ref(), CreatedAt: n.At,
	}
	s.jonah.reviews = append(s.jonah.reviews, jonahRow[domain.JonahReview]{org, r})

	return r, nil
}

// reviewClaimable reports whether r is pending, or running under a lease that ended by now.
func reviewClaimable(r domain.JonahReview, now time.Time) bool {
	return r.State == domain.JonahPending || r.State == domain.JonahRunning && !r.LeaseUntil.After(now)
}

func inFlight(st domain.JonahReviewState) bool {
	return st == domain.JonahPending || st == domain.JonahRunning
}

// ClaimJonahReview implements ports.JonahStore.
func (s *Store) ClaimJonahReview(ctx context.Context, now time.Time, lease time.Duration) (domain.JonahReview, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.JonahReview{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var due *domain.JonahReview

	for i := range s.jonah.reviews {
		r := &s.jonah.reviews[i]
		if r.org != org || !reviewClaimable(r.row, now) {
			continue
		}

		if due == nil || r.row.CreatedAt.Before(due.CreatedAt) || r.row.CreatedAt.Equal(due.CreatedAt) && r.row.ID < due.ID {
			due = &r.row
		}
	}

	if due == nil {
		return domain.JonahReview{}, fmt.Errorf("claim jonah review: %w", ports.ErrNotFound)
	}

	due.State, due.LeaseUntil = domain.JonahRunning, now.Add(lease)
	due.Attempts++

	return *due, nil
}

// FinishJonahReview implements ports.JonahStore.
func (s *Store) FinishJonahReview(ctx context.Context, claimed domain.JonahReview, res domain.JonahReviewResult) (domain.JonahReview, []domain.Proposal, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.JonahReview{}, nil, err
	}

	if err := res.Check(); err != nil {
		return domain.JonahReview{}, nil, fmt.Errorf("finish jonah review %s: %w", claimed.ID, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	i := slices.IndexFunc(s.jonah.reviews, func(r jonahRow[domain.JonahReview]) bool { return r.org == org && r.row.ID == claimed.ID })
	if i < 0 || s.jonah.reviews[i].row.State != domain.JonahRunning || !s.jonah.reviews[i].row.LeaseUntil.Equal(claimed.LeaseUntil) {
		return domain.JonahReview{}, nil, fmt.Errorf("finish jonah review %s: %w", claimed.ID, ports.ErrLeaseLost)
	}

	r := &s.jonah.reviews[i].row
	r.State, r.Error, r.RunID, r.FinishedAt, r.LeaseUntil = res.State, res.Error, res.RunID, res.At, time.Time{}

	return *r, s.storeReviewOutput(org, *r, res), nil
}

// storeReviewOutput stores res's proposals, numbered after the factory's last, and its
// observations, and returns the proposals. Callers hold s.mu.
func (s *Store) storeReviewOutput(org domain.OrgID, r domain.JonahReview, res domain.JonahReviewResult) []domain.Proposal {
	var last int64

	for _, p := range s.jonah.proposals {
		if p.org == org && p.row.FactoryID == r.FactoryID {
			last = max(last, p.row.Number)
		}
	}

	stored := make([]domain.Proposal, 0, len(res.Proposals))

	for k, n := range res.Proposals {
		p := domain.Proposal{
			ID: domain.ProposalID(s.newID()), FactoryID: r.FactoryID, Number: last + int64(k) + 1, ReviewID: r.ID, VersionID: r.VersionID,
			State: domain.ProposalProposed, Title: n.Title, Summary: n.Summary, Files: slices.Clone(n.Files),
			Evidence: slices.Clone(n.Evidence), Estimate: n.Estimate, CreatedAt: res.At,
		}
		stored = append(stored, p)
		s.jonah.proposals = append(s.jonah.proposals, jonahRow[domain.Proposal]{org, p})
	}

	for _, n := range res.Observations {
		o := domain.Observation{
			ID: domain.ObservationID(s.newID()), FactoryID: r.FactoryID, ReviewID: r.ID, Summary: n.Summary, Detail: n.Detail,
			ItemIDs: slices.Clone(n.ItemIDs), Count: n.Count, State: domain.ObservationWatching, CreatedAt: res.At,
		}
		if o.ItemIDs == nil {
			o.ItemIDs = []domain.ItemID{}
		}

		if n.Proposal > 0 {
			o.State, o.ProposalID = domain.ObservationProposed, stored[n.Proposal-1].ID
		}

		s.jonah.observations = append(s.jonah.observations, jonahRow[domain.Observation]{org, o})
	}

	return cloneProposals(stored)
}

// JonahReviews implements ports.JonahStore.
func (s *Store) JonahReviews(ctx context.Context, factory domain.FactoryID, limit int) ([]domain.JonahReview, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	out := []domain.JonahReview{}

	for _, r := range s.jonah.reviews {
		if r.org == org && r.row.FactoryID == factory {
			out = append(out, r.row)
		}
	}

	slices.SortFunc(out, func(a, b domain.JonahReview) int {
		return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), cmp.Compare(b.ID, a.ID))
	})

	return out[:min(len(out), max(limit, 0))], nil
}

// Proposals implements ports.JonahStore.
func (s *Store) Proposals(ctx context.Context, q ports.ProposalQuery) ([]domain.Proposal, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	out := []domain.Proposal{}

	for _, p := range s.jonah.proposals {
		if p.org == org && p.row.FactoryID == q.Factory && (q.State == "" || p.row.State == q.State) &&
			(q.Before == 0 || p.row.Number < q.Before) {
			out = append(out, p.row)
		}
	}

	slices.SortFunc(out, func(a, b domain.Proposal) int { return cmp.Compare(b.Number, a.Number) })

	return cloneProposals(out[:min(len(out), max(q.Limit, 0))]), nil
}

// Proposal implements ports.JonahStore.
func (s *Store) Proposal(ctx context.Context, factory domain.FactoryID, number int64) (domain.Proposal, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.Proposal{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	p := s.findProposal(org, factory, number)
	if p == nil {
		return domain.Proposal{}, fmt.Errorf("proposal %d of factory %s: %w", number, factory, ports.ErrNotFound)
	}

	return cloneProposals([]domain.Proposal{*p})[0], nil
}

// findProposal is org's proposal of number of factory, or nil. Callers hold s.mu.
func (s *Store) findProposal(org domain.OrgID, factory domain.FactoryID, number int64) *domain.Proposal {
	for i := range s.jonah.proposals {
		if p := &s.jonah.proposals[i]; p.org == org && p.row.FactoryID == factory && p.row.Number == number {
			return &p.row
		}
	}

	return nil
}

// DecideProposal implements ports.JonahStore.
func (s *Store) DecideProposal(ctx context.Context, factory domain.FactoryID, number int64, d domain.ProposalDecision) (domain.Proposal, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.Proposal{}, err
	}

	if err := d.Check(); err != nil {
		return domain.Proposal{}, fmt.Errorf("decide proposal %d: %w", number, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	p := s.findProposal(org, factory, number)

	switch {
	case p == nil:
		return domain.Proposal{}, fmt.Errorf("decide proposal %d of factory %s: %w", number, factory, ports.ErrNotFound)
	case p.State != domain.ProposalProposed:
		return domain.Proposal{}, fmt.Errorf("decide proposal %d, %s: %w", number, p.State, ports.ErrProposalDecided)
	}

	p.State, p.PublishedVersionID, p.RejectReason, p.DecidedBy, p.DecidedAt = d.State, d.PublishedVersionID, d.RejectReason, d.By.Ref(), d.At

	return cloneProposals([]domain.Proposal{*p})[0], nil
}

// Observations implements ports.JonahStore.
func (s *Store) Observations(ctx context.Context, factory domain.FactoryID, limit int) ([]domain.Observation, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	out := []domain.Observation{}

	for _, o := range s.jonah.observations {
		if o.org == org && o.row.FactoryID == factory {
			c := o.row
			c.ItemIDs = slices.Clone(c.ItemIDs)
			out = append(out, c)
		}
	}

	slices.SortFunc(out, func(a, b domain.Observation) int {
		return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), cmp.Compare(b.ID, a.ID))
	})

	return out[:min(len(out), max(limit, 0))], nil
}

// JonahSpendSince implements ports.JonahStore.
func (s *Store) JonahSpendSince(ctx context.Context, factory domain.FactoryID, since time.Time) (float64, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return 0, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var usd float64

	for id, r := range s.runs {
		if _, ok := s.visible(r.ItemID, org); ok && r.FactoryID == factory && s.jonah.runKinds[id] == domain.RunKindJonah &&
			!r.StartedAt.Before(since) {
			usd += r.Result.CostUSD
		}
	}

	return usd, nil
}

// SentBackSince implements ports.JonahStore.
func (s *Store) SentBackSince(ctx context.Context, factory domain.FactoryID, since time.Time) (int, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return 0, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	n := 0

	for id, it := range s.items {
		if it.OrgID != org || it.FactoryID != factory {
			continue
		}

		for _, t := range s.itemTouches(id) {
			if (t.Kind == domain.EventRejected || t.Kind == domain.EventGateRejected) && !t.At.Before(since) {
				n++
			}
		}
	}

	return n, nil
}

func cloneProposals(ps []domain.Proposal) []domain.Proposal {
	out := slices.Clone(ps)
	for i := range out {
		out[i].Files, out[i].Evidence = slices.Clone(out[i].Files), slices.Clone(out[i].Evidence)
	}

	return out
}
