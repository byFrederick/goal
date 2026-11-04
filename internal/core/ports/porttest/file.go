package porttest

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// filedPart is what a part's row has beyond its NewItem: its key, planned paths and base commit.
type filedPart struct {
	key        string
	paths      []string
	baseCommit string
}

// file adds n as an item of org, as the PostgreSQL store's fileItem does: the next number,
// origin person when n has none, its span and its created event (part_created for a part) by
// n.FiledBy. It returns the item and its item.filed audit entry, which the caller appends once
// s.mu is released. s.mu is held.
func (s *Store) file(org domain.OrgID, n domain.NewItem, p filedPart) (*domain.Item, domain.AuditEntry) {
	s.numbers[n.FactoryID]++
	it := &domain.Item{
		ID: domain.ItemID(s.newID()), OrgID: org, FactoryID: n.FactoryID, Number: s.numbers[n.FactoryID],
		ParentID: n.ParentID, PartKey: p.key, PartPaths: slices.Clone(p.paths), Paths: slices.Clone(p.paths), BaseCommit: p.baseCommit,
		Type: n.Type, Lane: n.Lane, Title: n.Title, Body: n.Body,
		Station: n.Station, State: n.State, Class: n.Class, Priority: n.Priority,
		Outputs: map[string]json.RawMessage{}, CreatedAt: n.CreatedAt, UpdatedAt: n.CreatedAt,
		Source: n.Source, Trigger: n.Trigger, Origin: cmp.Or(n.Origin, domain.OriginPerson), Via: n.Via,
		Generation: n.Generation, Untrusted: n.Untrusted,
	}

	by := n.FiledBy()
	if n.CreatedBy != (domain.Actor{}) {
		it.CreatedBy = by.Ref()
	}

	s.items[it.ID] = it
	s.trackSpan(it, nil, n.CreatedAt)

	created, audit := domain.FiledRecords(*it, by)
	s.append(created)

	return it, audit
}

// appendAudits appends entries to a, the linked Tenancy, when there is one.
func appendAudits(ctx context.Context, a ports.Audit, entries ...domain.AuditEntry) error {
	if a == nil {
		return nil
	}

	for _, e := range entries {
		if err := a.Append(ctx, e); err != nil {
			return fmt.Errorf("audit %s: %w", e.Action, err)
		}
	}

	return nil
}
