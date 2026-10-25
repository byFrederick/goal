package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/adapters/postgres/db"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// edgeKey is item_edges' primary key (item_id, needs_id).
const edgeKey = "item_edges_pkey"

// AddEdge implements ports.Store. Edges of a factory are added under its row lock, so the cycle
// check of each sees every edge committed before it and two concurrent edges cannot close one.
func (s *Store) AddEdge(ctx context.Context, n domain.NewEdge) (domain.Edge, error) {
	if !n.Kind.Valid() {
		return domain.Edge{}, fmt.Errorf("edge kind %q: %w", n.Kind, domain.ErrInvalid)
	}

	if n.Item == n.Needs {
		return domain.Edge{}, fmt.Errorf("item %s needs itself: %w", n.Item, domain.ErrInvalid)
	}

	item, err := parseID("item", string(n.Item))
	if err != nil {
		return domain.Edge{}, err
	}

	needs, err := parseID("needed item", string(n.Needs))
	if err != nil {
		return domain.Edge{}, err
	}

	var e domain.Edge

	err = s.inTx(ctx, func(q *db.Queries) error {
		it, err := edgeItems(ctx, q, n, item, needs)
		if err != nil {
			return err
		}

		if _, err := q.LockFactory(ctx, it.FactoryID); err != nil {
			return fmt.Errorf("lock factory %s: %w", it.FactoryID, err)
		}

		cycle, err := q.EdgeReaches(ctx, db.EdgeReachesParams{FromID: needs, ToID: item})
		if err != nil {
			return fmt.Errorf("check cycle: %w", err)
		}

		if cycle {
			return fmt.Errorf("item %s needs %s: %w", n.Needs, n.Item, domain.ErrCycle)
		}

		row, err := q.InsertEdge(ctx, db.InsertEdgeParams{
			ItemID: item, NeedsID: needs, Kind: string(n.Kind), CreatedBy: n.By.Ref(), CreatedAt: n.At,
		})
		if violates(err, edgeKey) {
			return fmt.Errorf("item %s already needs %s: %w", n.Item, n.Needs, ports.ErrConflict)
		}

		if err != nil {
			return fmt.Errorf("insert edge: %w", err)
		}

		e = edgeFrom(row)

		return appendEvents(ctx, q, []domain.Event{edgeEvent(domain.EventEdgeAdded, it, e, n.By, n.At)})
	})
	if err != nil {
		return domain.Edge{}, fmt.Errorf("add edge: %w", err)
	}

	return e, nil
}

// edgeItems reads an edge's two items and returns the dependent one: ErrNotFound when either is
// missing (or of another organization), domain.ErrInvalid when they are of different factories.
func edgeItems(ctx context.Context, q *db.Queries, n domain.NewEdge, item, needs uuid.UUID) (db.WorkItem, error) {
	it, err := q.GetItem(ctx, item)
	if notFound(err) {
		return db.WorkItem{}, fmt.Errorf("item %s: %w", n.Item, ports.ErrNotFound)
	}

	if err != nil {
		return db.WorkItem{}, fmt.Errorf("item %s: %w", n.Item, err)
	}

	need, err := q.GetItem(ctx, needs)
	if notFound(err) {
		return db.WorkItem{}, fmt.Errorf("needed item %s: %w", n.Needs, ports.ErrNotFound)
	}

	if err != nil {
		return db.WorkItem{}, fmt.Errorf("needed item %s: %w", n.Needs, err)
	}

	if it.FactoryID != need.FactoryID {
		return db.WorkItem{}, fmt.Errorf("item %s and %s are of different factories: %w", n.Item, n.Needs, domain.ErrInvalid)
	}

	return it, nil
}

// RemoveEdge implements ports.Store.
func (s *Store) RemoveEdge(ctx context.Context, item, needs domain.ItemID, by domain.Actor, at time.Time) error {
	missing := fmt.Errorf("edge %s needs %s: %w", item, needs, ports.ErrNotFound)

	itemID, err := uuid.Parse(string(item))
	if err != nil {
		return missing
	}

	needsID, err := uuid.Parse(string(needs))
	if err != nil {
		return missing
	}

	err = s.inTx(ctx, func(q *db.Queries) error {
		row, err := q.DeleteEdge(ctx, db.DeleteEdgeParams{ItemID: itemID, NeedsID: needsID})
		if notFound(err) {
			return missing
		}

		if err != nil {
			return fmt.Errorf("delete edge: %w", err)
		}

		it, err := q.GetItem(ctx, itemID)
		if err != nil {
			return fmt.Errorf("item %s: %w", item, err)
		}

		return appendEvents(ctx, q, []domain.Event{edgeEvent(domain.EventEdgeRemoved, it, edgeFrom(row), by, at)})
	})
	if err != nil {
		return fmt.Errorf("remove edge: %w", err)
	}

	return nil
}

// Edges implements ports.Store. Both directions are read in one snapshot.
func (s *Store) Edges(ctx context.Context, item domain.ItemID) (domain.ItemEdges, error) {
	out := domain.ItemEdges{}

	id, err := uuid.Parse(string(item))
	if err != nil {
		if _, ok := domain.OrgFrom(ctx); !ok {
			return out, domain.ErrNoOrg
		}

		return out, nil
	}

	err = s.read(ctx, func(q *db.Queries) error {
		needs, err := q.ItemNeeds(ctx, id)
		if err != nil {
			return fmt.Errorf("needs: %w", err)
		}

		neededBy, err := q.ItemNeededBy(ctx, id)
		if err != nil {
			return fmt.Errorf("needed by: %w", err)
		}

		out.Needs, out.NeededBy = edgesFrom(needs), edgesFrom(neededBy)

		return nil
	})
	if err != nil {
		return domain.ItemEdges{}, fmt.Errorf("edges of item %s: %w", item, err)
	}

	return out, nil
}

func edgeFrom(r db.ItemEdge) domain.Edge {
	return domain.Edge{
		Item: domain.ItemID(r.ItemID.String()), Needs: domain.ItemID(r.NeedsID.String()), Kind: domain.EdgeKind(r.Kind),
		By: r.CreatedBy, At: r.CreatedAt.UTC(),
	}
}

func edgesFrom(rows []db.ItemEdge) []domain.Edge {
	if len(rows) == 0 {
		return nil
	}

	out := make([]domain.Edge, 0, len(rows))
	for _, r := range rows {
		out = append(out, edgeFrom(r))
	}

	return out
}

// edgeEvent is the event an edge's addition or removal appends on its dependent item.
func edgeEvent(kind domain.EventKind, it db.WorkItem, e domain.Edge, by domain.Actor, at time.Time) domain.Event {
	return domain.Event{
		FactoryID: domain.FactoryID(it.FactoryID.String()), ItemID: e.Item, At: at, Kind: kind, Station: it.Station,
		Actor: by.Name, Data: map[string]any{"needs": string(e.Needs), auditKind: string(e.Kind), "actor": by.Ref()},
	}
}
