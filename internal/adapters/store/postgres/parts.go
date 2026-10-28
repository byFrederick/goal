package postgres

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// partKeyIndex is the unique index of an assembly's part keys (parent_id, part_key).
const partKeyIndex = "work_items_parent_part_key_idx"

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

	after, err := afterItems(ctx, q, cur, a.After)
	if err != nil {
		return db.WorkItem{}, err
	}

	row, err := transition(ctx, q, cur, t)
	if err != nil {
		return db.WorkItem{}, err
	}

	ids := make(map[string]uuid.UUID, len(a.Parts))
	roots := make([]db.WorkItem, 0, len(a.Parts))
	events := make([]domain.Event, 0, len(a.Parts)+1)

	for _, p := range a.Parts {
		part, err := insertPart(ctx, q, row, p, t.From.Station, t.At)
		if err != nil {
			return db.WorkItem{}, err
		}

		ids[p.Key] = part.ID
		if len(p.Needs) == 0 {
			roots = append(roots, part)
		}

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

	linked, err := linkAfter(ctx, q, roots, after, a.AfterBy, t.At)
	if err != nil {
		return db.WorkItem{}, err
	}

	events = append(events, linked...)
	events = append(events, domain.Event{
		FactoryID: domain.FactoryID(row.FactoryID.String()), ItemID: t.ItemID, At: t.At, Kind: domain.EventAssembled,
		Station: row.Station, Actor: domain.ActorEngine, Data: map[string]any{"parts": len(a.Parts)},
	})

	if err := appendEvents(ctx, q, events); err != nil {
		return db.WorkItem{}, err
	}

	return row, nil
}

// afterItems parses and reads the items an assembly's root parts follow: ErrNotFound for one that
// is missing (or of another organization), domain.ErrInvalid for one of another factory than cur.
func afterItems(ctx context.Context, q *db.Queries, cur db.WorkItem, ids []domain.ItemID) ([]uuid.UUID, error) {
	after := make([]uuid.UUID, 0, len(ids))

	for _, id := range ids {
		need, err := parseID("needed item", string(id))
		if err != nil {
			return nil, err
		}

		it, err := q.GetItem(ctx, need)
		if notFound(err) {
			return nil, fmt.Errorf("after %s: %w", id, ports.ErrNotFound)
		}

		if err != nil {
			return nil, fmt.Errorf("after %s: %w", id, err)
		}

		if it.FactoryID != cur.FactoryID {
			return nil, fmt.Errorf("after %s of another factory: %w", id, domain.ErrInvalid)
		}

		after = append(after, need)
	}

	return after, nil
}

// linkAfter inserts a linked edge, created by the given actor, from each of roots, new parts without needs, to each of
// after, and returns their edge_added events. New parts have no dependents, so no edge closes a
// cycle.
func linkAfter(ctx context.Context, q *db.Queries, roots []db.WorkItem, after []uuid.UUID, by domain.Actor, at time.Time) ([]domain.Event, error) {
	events := make([]domain.Event, 0, len(roots)*len(after))

	for _, part := range roots {
		for _, need := range after {
			row, err := q.InsertEdge(ctx, db.InsertEdgeParams{
				ItemID: part.ID, NeedsID: need, Kind: string(domain.EdgeLinked), CreatedBy: by.Ref(), CreatedAt: at,
			})
			if err != nil {
				return nil, fmt.Errorf("part %s after %s: %w", part.ID, need, err)
			}

			events = append(events, edgeEvent(domain.EventEdgeAdded, part, edgeFrom(row), by, at))
		}
	}

	return events, nil
}

// insertPart creates one part of the assembly parent at at: the parent's type, priority and class
// of service, queued at station, the one that planned it.
func insertPart(ctx context.Context, q *db.Queries, parent db.WorkItem, p domain.NewPart, station string, at time.Time) (db.WorkItem, error) {
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
		Type: parent.Type, Lane: optional(p.Lane), Title: p.Title, Body: p.Brief, Station: station,
		ClassOfService: parent.ClassOfService, Priority: int64(parent.Priority), BaseCommit: parent.BaseCommit, CreatedAt: at,
	})
	if violates(err, partKeyIndex) {
		return db.WorkItem{}, fmt.Errorf("part key %q is taken: %w", p.Key, ports.ErrConflict)
	}

	if err != nil {
		return db.WorkItem{}, fmt.Errorf("insert part %q: %w", p.Key, err)
	}

	return row, nil
}

// AddPart implements ports.Store. The factory's row is locked before the assembly's, AddEdge's
// order, so the planned edges' cycle check sees every edge committed before it; the assembly's lock
// serializes it with Assemble, a closing part and another AddPart (work_items_parent_part_key_idx
// refuses a key taken meanwhile).
func (s *Store) AddPart(ctx context.Context, n ports.NewAssemblyPart) (domain.Item, domain.Parts, error) {
	id, err := parseID("item", string(n.Assembly))
	if err != nil {
		return domain.Item{}, domain.Parts{}, fmt.Errorf("add part to %s: %w", n.Assembly, err)
	}

	var (
		part  domain.Item
		parts domain.Parts
	)

	err = s.inTx(ctx, func(q *db.Queries) error {
		cur, existing, err := lockAssembly(ctx, q, id, n.Assembly)
		if err != nil {
			return err
		}

		row, err := addPart(ctx, q, cur, existing, n)
		if err != nil {
			return err
		}

		if part, err = itemFrom(row); err != nil {
			return err
		}

		rows, err := q.ItemParts(ctx, id)
		if err != nil {
			return fmt.Errorf("parts: %w", err)
		}

		parts, err = partsFrom(rows)

		return err
	})
	if err != nil {
		return domain.Item{}, domain.Parts{}, fmt.Errorf("add part to %s: %w", n.Assembly, err)
	}

	return part, parts, nil
}

// lockAssembly locks the factory of the item id, then the item, which must be an open assembly,
// and returns it and its parts.
func lockAssembly(ctx context.Context, q *db.Queries, id uuid.UUID, item domain.ItemID) (db.WorkItem, []db.WorkItem, error) {
	it, err := q.GetItem(ctx, id)
	if notFound(err) {
		return db.WorkItem{}, nil, fmt.Errorf("item %s: %w", item, ports.ErrNotFound)
	}

	if err != nil {
		return db.WorkItem{}, nil, fmt.Errorf("item %s: %w", item, err)
	}

	if _, err := q.LockFactory(ctx, it.FactoryID); err != nil {
		return db.WorkItem{}, nil, fmt.Errorf("lock factory %s: %w", it.FactoryID, err)
	}

	cur, err := lockItem(ctx, q, id, item)
	if err != nil {
		return db.WorkItem{}, nil, err
	}

	if cur.PartKey != nil {
		return db.WorkItem{}, nil, fmt.Errorf("item %s is a part: %w", item, domain.ErrInvalid)
	}

	existing, err := q.ItemParts(ctx, id)
	if err != nil {
		return db.WorkItem{}, nil, fmt.Errorf("parts: %w", err)
	}

	switch {
	case domain.ItemState(cur.State) != domain.StateAssembly && len(existing) == 0:
		return db.WorkItem{}, nil, fmt.Errorf("item %s is no assembly: %w", item, domain.ErrInvalid)
	case domain.ItemState(cur.State) != domain.StateAssembly:
		return db.WorkItem{}, nil, fmt.Errorf("item %s is %s, not an open assembly: %w", item, cur.State, ports.ErrConflict)
	}

	return cur, existing, nil
}

// addPart creates n's part of the locked assembly cur, whose parts are existing, with its planned
// edges, events and audit entry.
func addPart(ctx context.Context, q *db.Queries, cur db.WorkItem, existing []db.WorkItem, n ports.NewAssemblyPart) (db.WorkItem, error) {
	p := n.Part
	ids := make(map[string]uuid.UUID, len(existing))
	keys := make(map[string]bool, len(existing))

	for _, e := range existing {
		ids[*e.PartKey], keys[*e.PartKey] = e.ID, true
	}

	if keys[p.Key] {
		return db.WorkItem{}, fmt.Errorf("part key %q is taken: %w", p.Key, ports.ErrConflict)
	}

	if err := domain.ValidatePart(p, keys); err != nil {
		return db.WorkItem{}, err
	}

	row, err := insertPart(ctx, q, cur, p, cur.Station, n.At)
	if err != nil {
		return db.WorkItem{}, err
	}

	for _, k := range p.Needs {
		cycle, err := q.EdgeReaches(ctx, db.EdgeReachesParams{FromID: ids[k], ToID: row.ID})
		if err != nil {
			return db.WorkItem{}, fmt.Errorf("check cycle: %w", err)
		}

		if cycle {
			return db.WorkItem{}, fmt.Errorf("part %q needs %q: %w", p.Key, k, domain.ErrCycle)
		}

		if _, err := q.InsertEdge(ctx, db.InsertEdgeParams{
			ItemID: row.ID, NeedsID: ids[k], Kind: string(domain.EdgePlanned), CreatedBy: n.By.Ref(), CreatedAt: n.At,
		}); err != nil {
			return db.WorkItem{}, fmt.Errorf("part %q needs %q: %w", p.Key, k, err)
		}
	}

	factory, assembly := domain.FactoryID(cur.FactoryID.String()), domain.ItemID(cur.ID.String())
	if err := appendEvents(ctx, q, []domain.Event{{
		FactoryID: factory, ItemID: domain.ItemID(row.ID.String()), At: n.At, Kind: domain.EventPartCreated,
		Station: row.Station, Actor: n.By.Name, Data: map[string]any{"parent": string(assembly), "key": p.Key},
	}, {
		FactoryID: factory, ItemID: assembly, At: n.At, Kind: domain.EventPartAdded, Station: cur.Station, Actor: n.By.Name,
		Data: map[string]any{"part": row.ID.String(), "key": p.Key, "actor": n.By.Ref()},
	}}); err != nil {
		return db.WorkItem{}, err
	}

	if n.Audit != nil {
		if err := appendAudit(ctx, q, *n.Audit); err != nil {
			return db.WorkItem{}, err
		}
	}

	return row, nil
}

// CascadeParts implements ports.Store. Parts are locked by id, then the assembly: the order
// closeAssembly takes them in after a part's own lock, so a cascade and a closing part never
// wait on each other in a cycle.
func (s *Store) CascadeParts(ctx context.Context, c ports.Cascade) (domain.Parts, error) {
	assembly, steps, err := cascadeSteps(c)
	if err != nil {
		return domain.Parts{}, fmt.Errorf("cascade to the parts of %s: %w", c.Assembly, err)
	}

	var parts domain.Parts

	err = s.inTx(ctx, func(q *db.Queries) error {
		if err := lockCascade(ctx, q, assembly, c.Assembly, steps); err != nil {
			return err
		}

		for _, st := range steps {
			row, err := transition(ctx, q, st.cur, st.t)
			if err != nil {
				return err
			}

			if err := closeAssembly(ctx, q, row, st.t.At, c.Cancel); err != nil {
				return err
			}
		}

		if err := appendEvents(ctx, q, c.Events); err != nil {
			return err
		}

		rows, err := q.ItemParts(ctx, assembly)
		if err != nil {
			return fmt.Errorf("parts: %w", err)
		}

		parts, err = partsFrom(rows)

		return err
	})
	if err != nil {
		return domain.Parts{}, fmt.Errorf("cascade to the parts of %s: %w", c.Assembly, err)
	}

	return parts, nil
}

// cascadeStep is one part's transition of a cascade and, once locked, the part's row.
type cascadeStep struct {
	id  uuid.UUID
	t   ports.Transition
	cur db.WorkItem
}

// cascadeSteps checks c and returns the assembly's id and the parts' transitions in lock order,
// by id.
func cascadeSteps(c ports.Cascade) (uuid.UUID, []cascadeStep, error) {
	if len(c.Parts) == 0 {
		return uuid.UUID{}, nil, fmt.Errorf("a cascade names no part: %w", domain.ErrInvalid)
	}

	assembly, err := parseID("item", string(c.Assembly))
	if err != nil {
		return uuid.UUID{}, nil, err
	}

	steps := make([]cascadeStep, len(c.Parts))

	for i, t := range c.Parts {
		if t.To.State == domain.StateAssembly {
			return uuid.UUID{}, nil, fmt.Errorf("item %s: only Assemble makes an assembly: %w", t.ItemID, domain.ErrInvalid)
		}

		id, err := parseID("item", string(t.ItemID))
		if err != nil {
			return uuid.UUID{}, nil, err
		}

		steps[i] = cascadeStep{id: id, t: t}
	}

	slices.SortFunc(steps, func(a, b cascadeStep) int { return bytes.Compare(a.id[:], b.id[:]) })

	for i := 1; i < len(steps); i++ {
		if steps[i-1].id == steps[i].id {
			return uuid.UUID{}, nil, fmt.Errorf("item %s is named twice: %w", steps[i].t.ItemID, domain.ErrInvalid)
		}
	}

	return assembly, steps, nil
}

// lockCascade locks the rows of steps, in their order, each a part of the assembly, then the
// assembly's, which must still be in state assembly.
func lockCascade(ctx context.Context, q *db.Queries, assembly uuid.UUID, id domain.ItemID, steps []cascadeStep) error {
	for i := range steps {
		st := &steps[i]

		cur, err := lockItem(ctx, q, st.id, st.t.ItemID)
		if err != nil {
			return err
		}

		if cur.PartKey == nil || cur.ParentID == nil || *cur.ParentID != assembly {
			return fmt.Errorf("item %s is no part of %s: %w", st.t.ItemID, id, domain.ErrInvalid)
		}

		st.cur = cur
	}

	a, err := lockItem(ctx, q, assembly, id)
	if err != nil {
		return err
	}

	if domain.ItemState(a.State) != domain.StateAssembly {
		return fmt.Errorf("item %s is %s, not an open assembly: %w", id, a.State, ports.ErrConflict)
	}

	return nil
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
// part: cancelled when cancel is set (a person cancelled the assembly), else done when any part
// is done, cancelled otherwise. Done, it moves to the station of its most recently done part, the
// line's done station, where a finished single item lands; cancelled, it keeps its own, as a
// cancelled single item does. An assembly with an integration branch that would close done
// lands instead: it moves to its integration_land_at station, queued, with an assembly_landing
// event, for that station to merge the branch. The assembly's row is locked after the part's,
// so of two last parts closing at once the second sees the first committed and closes it.
func closeAssembly(ctx context.Context, q *db.Queries, row db.WorkItem, at time.Time, cancel bool) error {
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

	if c.Done > 0 && !cancel && parent.IntegrationBranch != nil {
		return landAssembly(ctx, q, parent, c, at)
	}

	return finishAssembly(ctx, q, parent, c, at, cancel)
}

// finishAssembly closes parent, an assembly whose last part closed with c.Done parts done:
// done at its last done part's station when a part is done and cancel is not set, else
// cancelled at its own station, with an assembly_done event.
func finishAssembly(ctx context.Context, q *db.Queries, parent db.WorkItem, c db.PartCountsRow, at time.Time, cancel bool) error {
	closed, station := domain.StateCancelled, parent.Station
	if c.Done > 0 && !cancel {
		closed = domain.StateDone

		var err error
		if station, err = q.LastDonePartStation(ctx, parent.ID); err != nil {
			return fmt.Errorf("done station of the assembly: %w", err)
		}
	}

	if _, err := q.MoveItem(ctx, db.MoveItemParams{
		ID: parent.ID, Station: station, State: string(closed), UpdatedAt: at,
		SetDoneAt: closed == domain.StateDone, DoneAt: &at,
	}); err != nil {
		return fmt.Errorf("close assembly: %w", err)
	}

	return appendEvents(ctx, q, []domain.Event{{
		FactoryID: domain.FactoryID(parent.FactoryID.String()), ItemID: domain.ItemID(parent.ID.String()), At: at,
		Kind: domain.EventAssemblyDone, Station: station, Actor: domain.ActorEngine,
		Data: map[string]any{"state": string(closed), "parts_done": c.Done, "parts": c.Total},
	}})
}

// landAssembly moves parent, an assembly with an integration branch whose last part closed with
// c.Done parts done, to its landing station, queued, with an assembly_landing event.
func landAssembly(ctx context.Context, q *db.Queries, parent db.WorkItem, c db.PartCountsRow, at time.Time) error {
	station := deref(parent.IntegrationLandAt)

	if _, err := q.MoveItem(ctx, db.MoveItemParams{
		ID: parent.ID, Station: station, State: string(domain.StateQueued), UpdatedAt: at,
	}); err != nil {
		return fmt.Errorf("land assembly: %w", err)
	}

	return appendEvents(ctx, q, []domain.Event{{
		FactoryID: domain.FactoryID(parent.FactoryID.String()), ItemID: domain.ItemID(parent.ID.String()), At: at,
		Kind: domain.EventAssemblyLanding, Station: station, Actor: domain.ActorEngine,
		Data: map[string]any{"parts_done": c.Done, "parts": c.Total, "branch": *parent.IntegrationBranch},
	}})
}

// SetIntegrationBranch implements ports.Store.
func (s *Store) SetIntegrationBranch(ctx context.Context, root domain.ItemID, branch, landAt string, at time.Time) (bool, error) {
	id, err := parseID("item", string(root))
	if err != nil {
		return false, fmt.Errorf("integration branch of %s: %w", root, err)
	}

	if branch == "" || landAt == "" {
		return false, fmt.Errorf("integration branch of %s: a branch and a landing station are required: %w", root, domain.ErrInvalid)
	}

	var set bool

	err = s.inTx(ctx, func(q *db.Queries) error {
		n, err := q.SetIntegrationBranch(ctx, db.SetIntegrationBranchParams{
			ID: id, IntegrationBranch: branch, IntegrationLandAt: landAt, UpdatedAt: at,
		})
		if err != nil {
			return err
		}

		if set = n > 0; set {
			return nil
		}

		// No row changed: the item has a branch already, or there is none.
		if _, err := q.GetItem(ctx, id); notFound(err) {
			return ports.ErrNotFound
		} else if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return false, fmt.Errorf("integration branch of %s: %w", root, err)
	}

	return set, nil
}
