package api

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/engine"
	"github.com/byFrederick/goal/internal/ports"
)

// AuditLog reads the audit log of the organization in ctx; ports.Audit satisfies it.
type AuditLog interface {
	AuditLog(ctx context.Context, q ports.AuditQuery) ([]domain.AuditEntry, error)
}

// Audit pages.
const (
	defaultAuditLimit = 100
	maxAuditLimit     = 500
)

// errConfirmMismatch answers a deletion whose confirmation is not the organization's name.
var errConfirmMismatch = fmt.Errorf("the confirmation is not the organization's name: %w", domain.ErrInvalid)

// actorOf returns the request's actor; the actor middleware sets one on every request.
func actorOf(ctx context.Context, what string) (domain.Actor, error) {
	a, ok := domain.ActorFrom(ctx)
	if !ok {
		return domain.Actor{}, fmt.Errorf("%s: the request has no actor", what)
	}

	return a, nil
}

func (s *server) GetOrgSettings(ctx context.Context, _ gen.GetOrgSettingsRequestObject) (gen.GetOrgSettingsResponseObject, error) {
	out, err := s.settings(ctx, orgOf(ctx))
	if err != nil {
		return nil, err
	}

	return gen.GetOrgSettings200JSONResponse(out), nil
}

func (s *server) UpdateOrgSettings(ctx context.Context, req gen.UpdateOrgSettingsRequestObject) (gen.UpdateOrgSettingsResponseObject, error) {
	actor, err := actorOf(ctx, "update organization settings")
	if err != nil {
		return nil, err
	}

	b := req.Body
	u := ports.OrgUpdate{Slug: b.Slug, Mark: b.Mark}

	if b.Name != nil {
		u.Name = new(strings.TrimSpace(*b.Name))
	}

	if b.Slots != nil {
		maxSlots, err := capIn(b.Slots.Max)
		if err != nil {
			return nil, err
		}

		u.MaxSlots = &maxSlots
		u.FactorySlots = map[domain.FactoryID]int{}

		for _, f := range deref(b.Slots.Factories) {
			factory, err := s.factory(ctx, f.FactoryId)
			if err != nil {
				return nil, err
			}

			if u.FactorySlots[domain.FactoryID(factory.Metadata.ID)], err = capIn(f.Max); err != nil {
				return nil, fmt.Errorf("factory %s: %w", f.FactoryId, err)
			}
		}
	}

	o, err := s.orgs.UpdateOrg(ctx, actor, u)
	if err != nil {
		return nil, err
	}

	out, err := s.settings(ctx, o)
	if err != nil {
		return nil, err
	}

	return gen.UpdateOrgSettings200JSONResponse(out), nil
}

// settings is the organization's settings: its general fields and the slot caps of the
// organization and of every factory it runs, by name.
func (s *server) settings(ctx context.Context, o domain.Org) (gen.OrgSettings, error) {
	caps, err := s.orgs.FactorySlots(ctx)
	if err != nil {
		return gen.OrgSettings{}, err
	}

	factories := s.factories.Factories(ctx)
	out := gen.OrgSettings{
		Name: o.Name, Slug: o.Slug, Mark: o.Mark,
		Slots: gen.OrgSlots{Max: capOut(o.MaxSlots), Factories: make([]gen.FactorySlots, 0, len(factories))},
	}

	for _, f := range factories {
		out.Slots.Factories = append(out.Slots.Factories, gen.FactorySlots{
			FactoryId: f.Metadata.ID, Name: f.Metadata.Name, Max: capOut(caps[domain.FactoryID(f.Metadata.ID)]),
		})
	}

	slices.SortFunc(out.Slots.Factories, func(a, b gen.FactorySlots) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.FactoryId, b.FactoryId))
	})

	return out, nil
}

// capIn reads a slot cap of the contract: null clears it (0), a number must be a cap.
func capIn(n *int) (int, error) {
	if n == nil {
		return 0, nil
	}

	return *n, domain.CheckSlots(*n)
}

// capOut is a slot cap as the contract writes it: null for none.
func capOut(n int) *int {
	if n == 0 {
		return nil
	}

	return &n
}

func (s *server) DeleteOrg(ctx context.Context, req gen.DeleteOrgRequestObject) (gen.DeleteOrgResponseObject, error) {
	actor, err := actorOf(ctx, "delete organization")
	if err != nil {
		return nil, err
	}

	if o := orgOf(ctx); req.Body.Confirm != o.Name {
		return nil, errConfirmMismatch
	}

	now := s.clock.Now()
	if err := s.orgs.DeleteOrg(ctx, actor, now); err != nil {
		return nil, err
	}

	return gen.DeleteOrg202JSONResponse{PurgeAfter: now.Add(s.retention).UTC()}, nil
}

func (s *server) ListAudit(ctx context.Context, req gen.ListAuditRequestObject) (gen.ListAuditResponseObject, error) {
	p := req.Params

	limit := defaultAuditLimit
	if p.Limit != nil {
		limit = *p.Limit
	}

	if limit < 1 || limit > maxAuditLimit {
		return nil, fmt.Errorf("%w: limit %d is not 1 to %d", errInvalid, limit, maxAuditLimit)
	}

	q := ports.AuditQuery{
		Before: deref(p.Before), Limit: limit + 1, Action: deref(p.Action), TargetKind: deref(p.TargetKind),
		TargetID: deref(p.TargetId),
	}

	if p.Factory != nil {
		f, err := s.factory(ctx, *p.Factory)
		if err != nil {
			return nil, err
		}

		q.FactoryID = domain.FactoryID(f.Metadata.ID)
	}

	entries, err := s.audit.AuditLog(ctx, q)
	if err != nil {
		return nil, err
	}

	out := gen.ListAudit200JSONResponse{Entries: make([]gen.AuditEntry, 0, min(len(entries), limit))}

	if len(entries) > limit {
		entries = entries[:limit]
		out.NextBefore = &entries[limit-1].ID
	}

	for _, e := range entries {
		out.Entries = append(out.Entries, auditOf(e))
	}

	return out, nil
}

func auditOf(e domain.AuditEntry) gen.AuditEntry {
	data := e.Data
	if data == nil {
		data = map[string]any{}
	}

	return gen.AuditEntry{
		Id: e.ID, At: e.At, Action: e.Action, Data: data, FactoryId: optional(string(e.FactoryID)),
		Actor:  gen.Actor{Kind: gen.ActorKind(e.Actor.Kind), Id: e.Actor.ID, Name: e.Actor.Name},
		Target: gen.AuditTarget{Kind: e.TargetKind, Id: e.TargetID},
	}
}

// problemCode is the contract's code of a 409 the client may branch on, or "".
func problemCode(err error) string {
	switch {
	case errors.Is(err, domain.ErrSlugTaken):
		return "slug-taken"
	case errors.Is(err, errConfirmMismatch):
		return "confirmation-mismatch"
	case errors.Is(err, engine.ErrItemClosed):
		return "item-closed"
	default:
		return ""
	}
}
