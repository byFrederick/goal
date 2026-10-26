package porttest

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// AddEdge implements ports.Store. The store's mutex serializes edges as the factory's lock does
// in PostgreSQL, so no two of them close a cycle.
func (s *Store) AddEdge(ctx context.Context, n domain.NewEdge) (domain.Edge, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.Edge{}, err
	}

	if !n.Kind.Valid() {
		return domain.Edge{}, fmt.Errorf("edge kind %q: %w", n.Kind, domain.ErrInvalid)
	}

	if n.Item == n.Needs {
		return domain.Edge{}, fmt.Errorf("item %s needs itself: %w", n.Item, domain.ErrInvalid)
	}

	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	it, ok := s.visible(n.Item, org)
	if !ok {
		return domain.Edge{}, fmt.Errorf("item %s: %w", n.Item, ports.ErrNotFound)
	}

	need, ok := s.visible(n.Needs, org)
	if !ok {
		return domain.Edge{}, fmt.Errorf("needed item %s: %w", n.Needs, ports.ErrNotFound)
	}

	if it.FactoryID != need.FactoryID {
		return domain.Edge{}, fmt.Errorf("item %s and %s are of different factories: %w", n.Item, n.Needs, domain.ErrInvalid)
	}

	if slices.ContainsFunc(s.edges, func(e domain.Edge) bool { return e.Item == n.Item && e.Needs == n.Needs }) {
		return domain.Edge{}, fmt.Errorf("item %s already needs %s: %w", n.Item, n.Needs, ports.ErrConflict)
	}

	if s.reaches(n.Needs, n.Item) {
		return domain.Edge{}, fmt.Errorf("item %s needs %s: %w", n.Needs, n.Item, domain.ErrCycle)
	}

	e := domain.Edge{Item: n.Item, Needs: n.Needs, Kind: n.Kind, By: n.By.Ref(), At: n.At}
	s.edges = append(s.edges, e)
	s.append(edgeEvent(domain.EventEdgeAdded, it, e, n.By, n.At))

	return e, nil
}

// RemoveEdge implements ports.Store.
func (s *Store) RemoveEdge(ctx context.Context, item, needs domain.ItemID, by domain.Actor, at time.Time) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	it, ok := s.visible(item, org)
	i := slices.IndexFunc(s.edges, func(e domain.Edge) bool { return e.Item == item && e.Needs == needs })

	if !ok || i < 0 {
		return fmt.Errorf("edge %s needs %s: %w", item, needs, ports.ErrNotFound)
	}

	e := s.edges[i]
	s.edges = slices.Delete(s.edges, i, i+1)
	s.append(edgeEvent(domain.EventEdgeRemoved, it, e, by, at))

	return nil
}

// Edges implements ports.Store.
func (s *Store) Edges(ctx context.Context, item domain.ItemID) (domain.ItemEdges, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.ItemEdges{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	out := domain.ItemEdges{}

	if _, ok := s.visible(item, org); !ok {
		return out, nil
	}

	for _, e := range s.edges {
		switch item {
		case e.Item:
			out.Needs = append(out.Needs, e)
		case e.Needs:
			out.NeededBy = append(out.NeededBy, e)
		}
	}

	return out, nil
}

// OpenNeeds implements ports.Store.
func (s *Store) OpenNeeds(ctx context.Context, factory domain.FactoryID, ids []domain.ItemID) (map[domain.ItemID][]domain.OpenNeed, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	out := map[domain.ItemID][]domain.OpenNeed{}

	for _, id := range ids {
		if it, ok := s.visible(id, org); !ok || it.FactoryID != factory {
			continue
		}

		var edges []domain.Edge

		for _, e := range s.edges {
			need, ok := s.items[e.Needs]
			if e.Item == id && ok && need.State != domain.StateDone && need.State != domain.StateCancelled {
				edges = append(edges, e)
			}
		}

		slices.SortStableFunc(edges, func(a, b domain.Edge) int {
			return cmp.Or(a.At.Compare(b.At), cmp.Compare(a.Needs, b.Needs))
		})

		for _, e := range edges {
			need := s.items[e.Needs]
			out[id] = append(out[id], domain.OpenNeed{Kind: e.Kind, Item: need.ID, Number: need.Number, Paths: slices.Clone(need.Paths)})
		}
	}

	return out, nil
}

// reaches reports whether from needs to, directly or through other items. Callers hold s.mu.
func (s *Store) reaches(from, to domain.ItemID) bool {
	seen := map[domain.ItemID]bool{from: true}
	next := []domain.ItemID{from}

	for len(next) > 0 {
		id := next[len(next)-1]
		next = next[:len(next)-1]

		for _, e := range s.edges {
			if e.Item != id || seen[e.Needs] {
				continue
			}

			if e.Needs == to {
				return true
			}

			seen[e.Needs] = true
			next = append(next, e.Needs)
		}
	}

	return false
}

// heldByNeeds reports whether an item needs one that is neither done nor cancelled: a claim
// skips it. Callers hold s.mu.
func (s *Store) heldByNeeds(it *domain.Item) bool {
	return slices.ContainsFunc(s.edges, func(e domain.Edge) bool {
		need, ok := s.items[e.Needs]

		return e.Item == it.ID && ok && need.State != domain.StateDone && need.State != domain.StateCancelled
	})
}

// edgeEvent is the event an edge's addition or removal appends on its dependent item.
func edgeEvent(kind domain.EventKind, it *domain.Item, e domain.Edge, by domain.Actor, at time.Time) domain.Event {
	return domain.Event{
		FactoryID: it.FactoryID, ItemID: it.ID, At: at, Kind: kind, Station: it.Station, Actor: by.Name,
		Data: map[string]any{"needs": string(e.Needs), auditKind: string(e.Kind), "actor": by.Ref()},
	}
}
