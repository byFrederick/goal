package porttest

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
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

	s.move(it, t)

	ids := make(map[string]domain.ItemID, len(a.Parts))

	for _, p := range a.Parts {
		s.numbers[it.FactoryID]++
		part := &domain.Item{
			ID: domain.ItemID(s.newID()), OrgID: org, FactoryID: it.FactoryID, Number: s.numbers[it.FactoryID],
			ParentID: it.ID, PartKey: p.Key, PartPaths: slices.Clone(p.Paths), Type: it.Type, Lane: p.Lane,
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

	s.append(domain.Event{
		FactoryID: it.FactoryID, ItemID: it.ID, At: t.At, Kind: domain.EventAssembled, Station: it.Station,
		Actor: domain.ActorEngine, Data: map[string]any{"parts": len(a.Parts)},
	})

	return clone(it), s.partsOf(it.ID), s.audit, true, nil
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
// done when any part is done, cancelled otherwise. Callers hold s.mu.
func (s *Store) closeAssembly(part *domain.Item, at time.Time) {
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

	if parts.Done > 0 {
		parent.State, parent.DoneAt = domain.StateDone, at
	}

	s.trackSpan(parent, &before, at)
	s.append(domain.Event{
		FactoryID: parent.FactoryID, ItemID: parent.ID, At: at, Kind: domain.EventAssemblyDone, Station: parent.Station,
		Actor: domain.ActorEngine, Data: map[string]any{"state": string(parent.State), "parts_done": parts.Done, "parts": parts.Total},
	})
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

	parts := make([]*domain.Item, len(c.Parts))

	for i, t := range c.Parts {
		it, ok := s.visible(t.ItemID, org)

		switch {
		case !ok:
			return domain.Parts{}, nil, fmt.Errorf("item %s: %w", t.ItemID, ports.ErrNotFound)
		case it.PartKey == "" || it.ParentID != c.Assembly:
			return domain.Parts{}, nil, fmt.Errorf("item %s is no part of %s: %w", t.ItemID, c.Assembly, domain.ErrInvalid)
		case t.To.State == domain.StateAssembly:
			return domain.Parts{}, nil, fmt.Errorf("item %s: only Assemble makes an assembly: %w", t.ItemID, domain.ErrInvalid)
		case slices.Contains(parts[:i], it):
			return domain.Parts{}, nil, fmt.Errorf("item %s is named twice: %w", t.ItemID, domain.ErrInvalid)
		}

		parts[i] = it
	}

	a, ok := s.visible(c.Assembly, org)
	if !ok {
		return domain.Parts{}, nil, fmt.Errorf("item %s: %w", c.Assembly, ports.ErrNotFound)
	}

	if a.State != domain.StateAssembly {
		return domain.Parts{}, nil, fmt.Errorf("item %s is %s, not an open assembly: %w", c.Assembly, a.State, ports.ErrConflict)
	}

	for i, t := range c.Parts {
		if err := s.checkTransition(parts[i], t); err != nil {
			return domain.Parts{}, nil, err
		}
	}

	for i, t := range c.Parts {
		s.move(parts[i], t)
		s.closeAssembly(parts[i], t.At)
	}

	for _, e := range c.Events {
		s.append(e)
	}

	return s.partsOf(c.Assembly), s.audit, nil
}
