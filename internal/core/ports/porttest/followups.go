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

// DeliverNotes implements ports.Store.
func (s *Store) DeliverNotes(ctx context.Context, factories []domain.NoteFactory, at time.Time, limit int) ([]domain.FollowUp, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	var out []domain.FollowUp

	for i := range s.followUps {
		if len(out) >= limit {
			break
		}

		f := &s.followUps[i]

		src, ok := s.visible(f.ItemID, org)
		if !ok || src.Untrusted || f.State != domain.FollowUpWaiting || f.Target != domain.FollowUpNote || f.For == nil {
			continue
		}

		s.deliverNote(org, f, src, factories, at)
		out = append(out, cloneFollowUp(*f))
	}

	return out, nil
}

// deliverNote routes one claimed note from src, as the store's transaction does. Callers hold s.mu.
func (s *Store) deliverNote(org domain.OrgID, f *domain.FollowUp, src *domain.Item, factories []domain.NoteFactory, at time.Time) {
	to, target, today := s.noteTarget(org, *f, factories, at)

	var state domain.ItemState
	if target != nil {
		state = target.State
	}

	if reason := domain.RouteNote(src.FactoryID, to, state, today); reason != "" {
		f.Reason, f.Target, f.For = domain.NoteTriageReason(*f.For, reason), domain.FollowUpTriage, nil

		return
	}

	name := string(src.FactoryID)
	if nf, ok := domain.NoteFactoryNamed(factories, name); ok && nf.Name != "" {
		name = nf.Name
	}

	s.append(domain.Event{
		FactoryID: target.FactoryID, ItemID: target.ID, At: at, Kind: domain.EventItemNote, Station: target.Station,
		Actor: domain.ActorEngine, Data: domain.NoteData(*f, domain.ItemRef{Factory: name, Number: src.Number}),
	})

	f.State = domain.FollowUpDelivered
}

// noteTarget is the factory f's for names (nil for none), its item with that number (nil for
// none) and the notes that factory took from other factories in the 24 hours before at.
// Callers hold s.mu.
func (s *Store) noteTarget(org domain.OrgID, f domain.FollowUp, factories []domain.NoteFactory, at time.Time) (*domain.NoteFactory, *domain.Item, int) {
	nf, ok := domain.NoteFactoryNamed(factories, f.For.Factory)
	if !ok {
		return nil, nil, 0
	}

	var (
		target *domain.Item
		today  int
	)

	for _, it := range s.items {
		if it.OrgID == org && it.FactoryID == nf.ID && it.Number == f.For.Number {
			target = it
		}
	}

	for _, e := range s.events {
		if e.FactoryID == nf.ID && e.Kind == domain.EventItemNote && e.At.After(at.Add(-24*time.Hour)) &&
			e.Data[domain.KeyNoteFromFactory] != string(nf.ID) {
			today++
		}
	}

	return &nf, target, today
}
