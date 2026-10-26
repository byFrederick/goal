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

// Assemble implements ports.Store. Its audit entry goes to the linked Tenancy, if any, after the
// change, as Transition's does.
func (s *Store) Assemble(ctx context.Context, a ports.Assembly) (domain.Item, domain.Parts, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.Item{}, domain.Parts{}, err
	}

	parent, parts, audit, created, err := s.assemble(org, a)
	if err != nil || !created || audit == nil || a.Transition.Audit == nil {
		return parent, parts, err
	}

	if err := audit.Append(ctx, *a.Transition.Audit); err != nil {
		return domain.Item{}, domain.Parts{}, fmt.Errorf("audit assemble: %w", err)
	}

	return parent, parts, nil
}

// assemble reports whether it created the parts (false on a retry).
func (s *Store) assemble(org domain.OrgID, a ports.Assembly) (domain.Item, domain.Parts, ports.Audit, bool, error) {
	t := a.Transition

	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	it, ok := s.visible(t.ItemID, org)
	if !ok {
		return domain.Item{}, domain.Parts{}, nil, false, fmt.Errorf("item %s: %w", t.ItemID, ports.ErrNotFound)
	}

	if it.PartKey != "" {
		return domain.Item{}, domain.Parts{}, nil, false, fmt.Errorf("item %s is a part: %w", t.ItemID, domain.ErrInvalid)
	}

	if existing := s.partsOf(it.ID); existing.Total > 0 {
		return clone(it), existing, nil, false, nil
	}

	if t.To.State != domain.StateAssembly {
		return domain.Item{}, domain.Parts{}, nil, false, fmt.Errorf("item %s: assembling moves to state assembly, not %s: %w", t.ItemID, t.To.State, domain.ErrInvalid)
	}

	if err := domain.ValidatePlan(a.Parts); err != nil {
		return domain.Item{}, domain.Parts{}, nil, false, fmt.Errorf("assemble item %s: %w", t.ItemID, err)
	}

	if err := s.checkTransition(it, t); err != nil {
		return domain.Item{}, domain.Parts{}, nil, false, err
	}

	after, err := s.afterItems(it, a.After, org)
	if err != nil {
		return domain.Item{}, domain.Parts{}, nil, false, err
	}

	s.move(it, t)

	ids := make(map[string]domain.ItemID, len(a.Parts))

	for _, p := range a.Parts {
		s.numbers[it.FactoryID]++
		part := &domain.Item{
			ID: domain.ItemID(s.newID()), OrgID: org, FactoryID: it.FactoryID, Number: s.numbers[it.FactoryID],
			ParentID: it.ID, PartKey: p.Key, PartPaths: slices.Clone(p.Paths), Paths: slices.Clone(p.Paths), Type: it.Type, Lane: p.Lane,
			Title: p.Title, Body: p.Brief, Station: t.From.Station, State: domain.StateQueued, Class: it.Class,
			Priority: it.Priority, Outputs: map[string]json.RawMessage{}, CreatedAt: t.At, UpdatedAt: t.At,
		}
		s.items[part.ID] = part
		ids[p.Key] = part.ID
		s.trackSpan(part, nil, t.At)
		s.append(domain.Event{
			FactoryID: part.FactoryID, ItemID: part.ID, At: t.At, Kind: domain.EventPartCreated, Station: part.Station,
			Actor: domain.ActorEngine, Data: map[string]any{"parent": string(it.ID), "key": p.Key},
		})
	}

	for _, p := range a.Parts {
		for _, n := range p.Needs {
			s.edges = append(s.edges, domain.Edge{
				Item: ids[p.Key], Needs: ids[n], Kind: domain.EdgePlanned, By: domain.EngineActor.Ref(), At: t.At,
			})
		}
	}

	s.linkAfter(a, ids, after)

	s.append(domain.Event{
		FactoryID: it.FactoryID, ItemID: it.ID, At: t.At, Kind: domain.EventAssembled, Station: it.Station,
		Actor: domain.ActorEngine, Data: map[string]any{"parts": len(a.Parts)},
	})

	return clone(it), s.partsOf(it.ID), s.audit, true, nil
}

// linkAfter adds a linked edge by a.AfterBy, with its edge_added event, from each of a's parts
// without needs (ids by key) to each of after.
func (s *Store) linkAfter(a ports.Assembly, ids map[string]domain.ItemID, after []*domain.Item) {
	at := a.Transition.At

	for _, p := range a.Parts {
		if len(p.Needs) > 0 {
			continue
		}

		part := s.items[ids[p.Key]]
		for _, need := range after {
			e := domain.Edge{Item: part.ID, Needs: need.ID, Kind: domain.EdgeLinked, By: a.AfterBy.Ref(), At: at}
			s.edges = append(s.edges, e)
			s.append(edgeEvent(domain.EventEdgeAdded, part, e, a.AfterBy, at))
		}
	}
}

// afterItems are the items an assembly's root parts follow: ErrNotFound for one that is missing,
// domain.ErrInvalid for one of another factory than the assembly it.
func (s *Store) afterItems(it *domain.Item, ids []domain.ItemID, org domain.OrgID) ([]*domain.Item, error) {
	items := make([]*domain.Item, 0, len(ids))

	for _, id := range ids {
		need, ok := s.visible(id, org)
		if !ok {
			return nil, fmt.Errorf("assemble item %s after %s: %w", it.ID, id, ports.ErrNotFound)
		}

		if need.FactoryID != it.FactoryID {
			return nil, fmt.Errorf("assemble item %s after %s of another factory: %w", it.ID, id, domain.ErrInvalid)
		}

		items = append(items, need)
	}

	return items, nil
}

// AddPart implements ports.Store. The store's mutex serializes it as the factory's and the
// assembly's locks do in PostgreSQL; its audit entry goes to the linked Tenancy, if any, after
// the change, as Transition's does.
func (s *Store) AddPart(ctx context.Context, n ports.NewAssemblyPart) (domain.Item, domain.Parts, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.Item{}, domain.Parts{}, err
	}

	part, parts, audit, err := s.addPart(org, n)
	if err != nil {
		return domain.Item{}, domain.Parts{}, fmt.Errorf("add part to %s: %w", n.Assembly, err)
	}

	if audit != nil && n.Audit != nil {
		if err := audit.Append(ctx, *n.Audit); err != nil {
			return domain.Item{}, domain.Parts{}, fmt.Errorf("audit add part: %w", err)
		}
	}

	return part, parts, nil
}

func (s *Store) addPart(org domain.OrgID, n ports.NewAssemblyPart) (domain.Item, domain.Parts, ports.Audit, error) {
	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	it, ok := s.visible(n.Assembly, org)
	if !ok {
		return domain.Item{}, domain.Parts{}, nil, fmt.Errorf("item %s: %w", n.Assembly, ports.ErrNotFound)
	}

	existing := s.partsOf(it.ID)

	switch {
	case it.PartKey != "":
		return domain.Item{}, domain.Parts{}, nil, fmt.Errorf("item %s is a part: %w", n.Assembly, domain.ErrInvalid)
	case it.State != domain.StateAssembly && existing.Total == 0:
		return domain.Item{}, domain.Parts{}, nil, fmt.Errorf("item %s is no assembly: %w", n.Assembly, domain.ErrInvalid)
	case it.State != domain.StateAssembly:
		return domain.Item{}, domain.Parts{}, nil, fmt.Errorf("item %s is %s, not an open assembly: %w", n.Assembly, it.State, ports.ErrConflict)
	}

	ids := make(map[string]domain.ItemID, existing.Total)
	keys := make(map[string]bool, existing.Total)

	for _, p := range existing.Items {
		ids[p.PartKey], keys[p.PartKey] = p.ID, true
	}

	p := n.Part
	if keys[p.Key] {
		return domain.Item{}, domain.Parts{}, nil, fmt.Errorf("part key %q is taken: %w", p.Key, ports.ErrConflict)
	}

	if err := domain.ValidatePart(p, keys); err != nil {
		return domain.Item{}, domain.Parts{}, nil, err
	}

	s.numbers[it.FactoryID]++
	part := &domain.Item{
		ID: domain.ItemID(s.newID()), OrgID: org, FactoryID: it.FactoryID, Number: s.numbers[it.FactoryID],
		ParentID: it.ID, PartKey: p.Key, PartPaths: slices.Clone(p.Paths), Paths: slices.Clone(p.Paths), Type: it.Type, Lane: p.Lane,
		Title: p.Title, Body: p.Brief, Station: it.Station, State: domain.StateQueued, Class: it.Class,
		Priority: it.Priority, Outputs: map[string]json.RawMessage{}, CreatedAt: n.At, UpdatedAt: n.At,
	}

	for _, k := range p.Needs {
		if s.reaches(ids[k], part.ID) {
			return domain.Item{}, domain.Parts{}, nil, fmt.Errorf("part %q needs %q: %w", p.Key, k, domain.ErrCycle)
		}
	}

	s.items[part.ID] = part
	s.trackSpan(part, nil, n.At)

	for _, k := range p.Needs {
		s.edges = append(s.edges, domain.Edge{Item: part.ID, Needs: ids[k], Kind: domain.EdgePlanned, By: n.By.Ref(), At: n.At})
	}

	s.append(domain.Event{
		FactoryID: part.FactoryID, ItemID: part.ID, At: n.At, Kind: domain.EventPartCreated, Station: part.Station,
		Actor: n.By.Name, Data: map[string]any{"parent": string(it.ID), "key": p.Key},
	})
	s.append(domain.Event{
		FactoryID: it.FactoryID, ItemID: it.ID, At: n.At, Kind: domain.EventPartAdded, Station: it.Station,
		Actor: n.By.Name, Data: map[string]any{"part": string(part.ID), "key": p.Key, "actor": n.By.Ref()},
	})

	return clone(part), s.partsOf(it.ID), s.audit, nil
}

// Parts implements ports.Store.
func (s *Store) Parts(ctx context.Context, parent domain.ItemID) (domain.Parts, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.Parts{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.visible(parent, org); !ok {
		return domain.Parts{}, nil
	}

	return s.partsOf(parent), nil
}

// partsOf returns an item's parts by number, the plan's order. Callers hold s.mu.
func (s *Store) partsOf(parent domain.ItemID) domain.Parts {
	var out domain.Parts

	for _, it := range s.items {
		if it.ParentID == parent && it.PartKey != "" {
			out.Items = append(out.Items, clone(it))
		}
	}

	slices.SortFunc(out.Items, func(a, b domain.Item) int { return cmp.Compare(a.Number, b.Number) })

	out.Total = len(out.Items)
	for _, it := range out.Items {
		if it.State == domain.StateDone {
			out.Done++
		}
	}

	return out
}

// closeAssembly closes the assembly of a part that just closed when it was the last one open:
// cancelled when cancel is set (a person cancelled the assembly), else done when any part is
// done, cancelled otherwise. Done, it moves to the station of its most recently done part, the
// line's done station, where a finished single item lands; cancelled, it keeps its own, as a
// cancelled single item does. Callers hold s.mu.
func (s *Store) closeAssembly(part *domain.Item, at time.Time, cancel bool) {
	if part.PartKey == "" || !closed(part.State) {
		return
	}

	parent, ok := s.items[part.ParentID]
	if !ok || parent.State != domain.StateAssembly {
		return
	}

	parts := s.partsOf(parent.ID)
	if slices.ContainsFunc(parts.Items, func(it domain.Item) bool { return !closed(it.State) }) {
		return
	}

	before := *parent
	parent.State, parent.UpdatedAt = domain.StateCancelled, at

	if parts.Done > 0 && !cancel {
		parent.State, parent.DoneAt, parent.Station = domain.StateDone, at, lastDone(parts).Station
	}

	s.trackSpan(parent, &before, at)
	s.append(domain.Event{
		FactoryID: parent.FactoryID, ItemID: parent.ID, At: at, Kind: domain.EventAssemblyDone, Station: parent.Station,
		Actor: domain.ActorEngine, Data: map[string]any{"state": string(parent.State), "parts_done": parts.Done, "parts": parts.Total},
	})
}

// lastDone returns the most recently done of parts, by done time, then number; parts has one.
func lastDone(parts domain.Parts) domain.Item {
	var last domain.Item

	for _, p := range parts.Items {
		if p.State != domain.StateDone {
			continue
		}

		if last.ID == "" || p.DoneAt.After(last.DoneAt) || (p.DoneAt.Equal(last.DoneAt) && p.Number > last.Number) {
			last = p
		}
	}

	return last
}

// CascadeParts implements ports.Store. It checks every part's transition before it moves any,
// so a refused cascade writes nothing; the audit entries go to the linked Tenancy, if any, after
// the change, as Transition's do.
func (s *Store) CascadeParts(ctx context.Context, c ports.Cascade) (domain.Parts, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.Parts{}, err
	}

	parts, audit, err := s.cascade(org, c)
	if err != nil {
		return domain.Parts{}, fmt.Errorf("cascade to the parts of %s: %w", c.Assembly, err)
	}

	if audit == nil {
		return parts, nil
	}

	for _, t := range c.Parts {
		if t.Audit == nil {
			continue
		}

		if err := audit.Append(ctx, *t.Audit); err != nil {
			return domain.Parts{}, fmt.Errorf("audit cascade: %w", err)
		}
	}

	return parts, nil
}

func (s *Store) cascade(org domain.OrgID, c ports.Cascade) (domain.Parts, ports.Audit, error) {
	if len(c.Parts) == 0 {
		return domain.Parts{}, nil, fmt.Errorf("a cascade names no part: %w", domain.ErrInvalid)
	}

	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	parts, err := s.cascadeParts(org, c)
	if err != nil {
		return domain.Parts{}, nil, err
	}

	for i, t := range c.Parts {
		if err := s.checkTransition(parts[i], t); err != nil {
			return domain.Parts{}, nil, err
		}
	}

	for i, t := range c.Parts {
		s.move(parts[i], t)
		s.closeAssembly(parts[i], t.At, c.Cancel)
	}

	for _, e := range c.Events {
		s.append(e)
	}

	return s.partsOf(c.Assembly), s.audit, nil
}

// cascadeParts returns the parts c moves, each a part of c.Assembly, which must still be in
// state assembly. Callers hold s.mu.
func (s *Store) cascadeParts(org domain.OrgID, c ports.Cascade) ([]*domain.Item, error) {
	parts := make([]*domain.Item, len(c.Parts))

	for i, t := range c.Parts {
		it, ok := s.visible(t.ItemID, org)

		switch {
		case !ok:
			return nil, fmt.Errorf("item %s: %w", t.ItemID, ports.ErrNotFound)
		case it.PartKey == "" || it.ParentID != c.Assembly:
			return nil, fmt.Errorf("item %s is no part of %s: %w", t.ItemID, c.Assembly, domain.ErrInvalid)
		case t.To.State == domain.StateAssembly:
			return nil, fmt.Errorf("item %s: only Assemble makes an assembly: %w", t.ItemID, domain.ErrInvalid)
		case slices.Contains(parts[:i], it):
			return nil, fmt.Errorf("item %s is named twice: %w", t.ItemID, domain.ErrInvalid)
		}

		parts[i] = it
	}

	a, ok := s.visible(c.Assembly, org)
	if !ok {
		return nil, fmt.Errorf("item %s: %w", c.Assembly, ports.ErrNotFound)
	}

	if a.State != domain.StateAssembly {
		return nil, fmt.Errorf("item %s is %s, not an open assembly: %w", c.Assembly, a.State, ports.ErrConflict)
	}

	return parts, nil
}
