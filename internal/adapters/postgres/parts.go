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

// Assemble implements ports.Store. The parent's row lock serializes it with a retry, so the
// second call finds the parts the first created (and work_items_parent_part_key_idx would refuse
// duplicates).
func (s *Store) Assemble(ctx context.Context, a ports.Assembly) (domain.Item, domain.Parts, error) {
	t := a.Transition

	item, err := parseID("item", string(t.ItemID))
	if err != nil {
		return domain.Item{}, domain.Parts{}, err
	}

	var (
		parent domain.Item
		parts  domain.Parts
	)

	err = s.inTx(ctx, func(q *db.Queries) error {
		cur, err := lockItem(ctx, q, item, t.ItemID)
		if err != nil {
			return err
		}

		if cur.PartKey != nil {
			return fmt.Errorf("item %s is a part: %w", t.ItemID, domain.ErrInvalid)
		}

		existing, err := q.ItemParts(ctx, item)
		if err != nil {
			return fmt.Errorf("parts: %w", err)
		}

		if len(existing) == 0 {
			if cur, err = assemble(ctx, q, cur, a); err != nil {
				return err
			}

			if existing, err = q.ItemParts(ctx, item); err != nil {
				return fmt.Errorf("parts: %w", err)
			}
		}

		if parent, err = itemFrom(cur); err != nil {
			return err
		}

		parts, err = partsFrom(existing)

		return err
	})
	if err != nil {
		return domain.Item{}, domain.Parts{}, fmt.Errorf("assemble: %w", err)
	}

	return parent, parts, nil
}

// assemble moves the locked item cur to state assembly and creates its parts, their planned
// edges and events. It returns the moved item.
func assemble(ctx context.Context, q *db.Queries, cur db.WorkItem, a ports.Assembly) (db.WorkItem, error) {
	t := a.Transition

	if t.To.State != domain.StateAssembly {
		return db.WorkItem{}, fmt.Errorf("item %s: assembling moves to state assembly, not %s: %w", t.ItemID, t.To.State, domain.ErrInvalid)
	}

	if err := domain.ValidatePlan(a.Parts); err != nil {
		return db.WorkItem{}, fmt.Errorf("item %s: %w", t.ItemID, err)
	}

	row, err := transition(ctx, q, cur, t)
	if err != nil {
		return db.WorkItem{}, err
	}

	ids := make(map[string]uuid.UUID, len(a.Parts))
	events := make([]domain.Event, 0, len(a.Parts)+1)

	for _, p := range a.Parts {
		part, err := insertPart(ctx, q, row, p, t)
		if err != nil {
			return db.WorkItem{}, err
		}

		ids[p.Key] = part.ID
		events = append(events, domain.Event{
			FactoryID: domain.FactoryID(part.FactoryID.String()), ItemID: domain.ItemID(part.ID.String()), At: t.At,
			Kind: domain.EventPartCreated, Station: part.Station, Actor: domain.ActorEngine,
			Data: map[string]any{"parent": string(t.ItemID), "key": p.Key},
		})
	}

	for _, p := range a.Parts {
		for _, n := range p.Needs {
			if _, err := q.InsertEdge(ctx, db.InsertEdgeParams{
				ItemID: ids[p.Key], NeedsID: ids[n], Kind: string(domain.EdgePlanned), CreatedBy: domain.EngineActor.Ref(), CreatedAt: t.At,
			}); err != nil {
				return db.WorkItem{}, fmt.Errorf("part %q needs %q: %w", p.Key, n, err)
			}
		}
	}

	events = append(events, domain.Event{
		FactoryID: domain.FactoryID(row.FactoryID.String()), ItemID: t.ItemID, At: t.At, Kind: domain.EventAssembled,
		Station: row.Station, Actor: domain.ActorEngine, Data: map[string]any{"parts": len(a.Parts)},
	})

	if err := appendEvents(ctx, q, events); err != nil {
		return db.WorkItem{}, err
	}

	return row, nil
}

// insertPart creates one part of the assembly parent: the parent's type, priority and class of
// service, queued at the station that planned it.
func insertPart(ctx context.Context, q *db.Queries, parent db.WorkItem, p domain.NewPart, t ports.Transition) (db.WorkItem, error) {
	id, err := newID()
	if err != nil {
		return db.WorkItem{}, err
	}

	number, err := q.NextItemNumber(ctx, parent.FactoryID)
	if err != nil {
		return db.WorkItem{}, fmt.Errorf("next item number: %w", err)
	}

	paths := p.Paths
	if paths == nil {
		paths = []string{}
	}

	row, err := q.InsertPart(ctx, db.InsertPartParams{
		ID: id, FactoryID: parent.FactoryID, Number: number, ParentID: parent.ID, PartKey: p.Key, PartPaths: paths,
		Type: parent.Type, Lane: optional(p.Lane), Title: p.Title, Body: p.Brief, Station: t.From.Station,
		ClassOfService: parent.ClassOfService, Priority: int64(parent.Priority), CreatedAt: t.At,
	})
	if err != nil {
		return db.WorkItem{}, fmt.Errorf("insert part %q: %w", p.Key, err)
	}

	return row, nil
}

// Parts implements ports.Store.
func (s *Store) Parts(ctx context.Context, parent domain.ItemID) (domain.Parts, error) {
	id, err := uuid.Parse(string(parent))
	if err != nil {
		if _, ok := domain.OrgFrom(ctx); !ok {
			return domain.Parts{}, domain.ErrNoOrg
		}

		return domain.Parts{}, nil
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.WorkItem, error) { return q.ItemParts(ctx, id) })
	if err != nil {
		return domain.Parts{}, fmt.Errorf("parts of item %s: %w", parent, err)
	}

	return partsFrom(rows)
}

func partsFrom(rows []db.WorkItem) (domain.Parts, error) {
	out := domain.Parts{Total: len(rows)}

	for _, r := range rows {
		it, err := itemFrom(r)
		if err != nil {
			return domain.Parts{}, err
		}

		if it.State == domain.StateDone {
			out.Done++
		}

		out.Items = append(out.Items, it)
	}

	return out, nil
}

// closeAssembly closes the assembly of row, a part just moved, when row closed its last open
// part: done when any part is done, cancelled otherwise. The assembly's row is locked after the
// part's, so of two last parts closing at once the second sees the first committed and closes it.
func closeAssembly(ctx context.Context, q *db.Queries, row db.WorkItem, at time.Time) error {
	state := domain.ItemState(row.State)
	if row.PartKey == nil || row.ParentID == nil || (state != domain.StateDone && state != domain.StateCancelled) {
		return nil
	}

	parent, err := q.LockItem(ctx, *row.ParentID)
	if err != nil {
		return fmt.Errorf("lock assembly: %w", err)
	}

	if domain.ItemState(parent.State) != domain.StateAssembly {
		return nil
	}

	c, err := q.PartCounts(ctx, parent.ID)
	if err != nil {
		return fmt.Errorf("count parts: %w", err)
	}

	if c.Open > 0 {
		return nil
	}

	closed := domain.StateCancelled
	if c.Done > 0 {
		closed = domain.StateDone
	}

	if _, err := q.MoveItem(ctx, db.MoveItemParams{
		ID: parent.ID, Station: parent.Station, State: string(closed), UpdatedAt: at,
		SetDoneAt: closed == domain.StateDone, DoneAt: &at,
	}); err != nil {
		return fmt.Errorf("close assembly: %w", err)
	}

	return appendEvents(ctx, q, []domain.Event{{
		FactoryID: domain.FactoryID(parent.FactoryID.String()), ItemID: domain.ItemID(parent.ID.String()), At: at,
		Kind: domain.EventAssemblyDone, Station: parent.Station, Actor: domain.ActorEngine,
		Data: map[string]any{"state": string(closed), "done": c.Done, "total": c.Total},
	}})
}
