package porttest

import (
	"context"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// FollowUps implements ports.Store.
func (s *Store) FollowUps(ctx context.Context, item domain.ItemID) ([]domain.FollowUp, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.visible(item, org); !ok {
		return nil, nil
	}

	var out []domain.FollowUp

	for _, f := range s.followUps {
		if f.ItemID == item {
			out = append(out, cloneFollowUp(f))
		}
	}

	return out, nil
}

// keepFollowUps keeps the follow-ups of a transition of it at at, skipping a text already kept
// for it, as the store's unique (item, text key) does. Callers hold s.mu.
func (s *Store) keepFollowUps(it *domain.Item, fs []domain.FollowUp, at time.Time) {
	for _, f := range fs {
		key := domain.FollowUpKey(f.Text)
		if slices.ContainsFunc(s.followUps, func(k domain.FollowUp) bool {
			return k.ItemID == it.ID && domain.FollowUpKey(k.Text) == key
		}) {
			continue
		}

		f = cloneFollowUp(f)
		f.ID, f.FactoryID, f.ItemID, f.CreatedAt = domain.FollowUpID(s.newID()), it.FactoryID, it.ID, at
		s.followUps = append(s.followUps, f)
	}
}

func cloneFollowUp(f domain.FollowUp) domain.FollowUp {
	if f.For != nil {
		ref := *f.For
		f.For = &ref
	}

	if f.Start != nil {
		p := *f.Start
		p.After, p.Repositories = slices.Clone(p.After), slices.Clone(p.Repositories)
		f.Start = &p
	}

	return f
}
