package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Authorizer decides whether the request's principal may run an operation. GOAL_AUTH=none wires
// AllowAll, which allows everything (its one local user may do anything); Permissions decides
// from api/permissions.yaml (permissions.go).
//
// Every request passes it once in the /v1 middleware, after its organization is resolved
// (Factory ""), and again in the only resolvers that turn a path into a factory, an item or a
// run, server.factory, server.definition, server.item and server.run, with that factory. A
// non-nil error stops the request before the handler acts (refusal): one wrapping
// ports.ErrNotFound answers as if the organization, factory, item or run did not exist,
// domain.ErrUnauthenticated 401, domain.ErrReauthRequired 403 reauth-required, any other 403.
type Authorizer interface {
	Authorize(ctx context.Context, r AccessRequest) error
}

// AccessRequest is one authorization question.
type AccessRequest struct {
	// Principal is who asks (domain.LocalPrincipal under GOAL_AUTH=none); its Actor is how the
	// request is recorded.
	Principal domain.Principal
	// Operation is the OpenAPI operationId, such as approveItem.
	Operation string
	// Permission, when set, is asked about instead of the operation's x-goal-scope: the
	// visibility helpers (visible, can) ask about each permission on a factory.
	Permission domain.Permission
	// Org is "" for the operations outside an organization (listOrgs, createOrg, getHealth).
	Org domain.OrgID
	// Factory is "" for organization-level operations.
	Factory domain.FactoryID
}

// AllowAll allows every request: GOAL_AUTH=none's single local user may do everything. Only
// goald under GOAL_AUTH=none wires it.
type AllowAll struct{}

// Authorize implements Authorizer.
func (AllowAll) Authorize(context.Context, AccessRequest) error { return nil }

// errForbidden marks a request the Authorizer refused (403).
var errForbidden = errors.New("forbidden")

// The answers to a factory, item or run that is unknown or invisible to the principal: one fixed
// body each, without detail, so a factory's existence never leaks (problemOf).
var (
	errFactoryNotFound = fmt.Errorf("factory not found: %w", ports.ErrNotFound)
	errItemNotFound    = fmt.Errorf("item not found: %w", ports.ErrNotFound)
	errRunNotFound     = fmt.Errorf("run not found: %w", ports.ErrNotFound)
)

type operationKey struct{}

// withOperation records the request's operationId for the Authorizer.
func withOperation(ctx context.Context, operation string) context.Context {
	return context.WithValue(ctx, operationKey{}, operation)
}

// access is the question about the request in ctx and factory f.
func access(ctx context.Context, f domain.FactoryID) (AccessRequest, error) {
	p, ok := domain.PrincipalFrom(ctx)
	if !ok {
		return AccessRequest{}, errors.New("authorize: the request has no principal")
	}

	operation, _ := ctx.Value(operationKey{}).(string)
	org, _ := domain.OrgFrom(ctx)

	return AccessRequest{Principal: p, Operation: operation, Org: org, Factory: f}, nil
}

// authorize asks the Authorizer about the request in ctx and, when factory is set, that
// factory. A refusal that hides the resource answers hidden.
func (s *server) authorize(ctx context.Context, factory domain.FactoryID, hidden error) error {
	r, err := access(ctx, factory)
	if err != nil {
		return err
	}

	return refusal(s.authz.Authorize(ctx, r), hidden)
}

// refusal is the API's answer to an Authorizer error: a hidden resource answers hidden (the
// unknown resource's exact error), an unauthenticated principal or a stale session as they are,
// anything else forbidden.
func refusal(err, hidden error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, domain.ErrUnauthenticated), errors.Is(err, domain.ErrReauthRequired):
		return err
	case errors.Is(err, ports.ErrNotFound):
		return hidden
	default:
		return fmt.Errorf("%w: %w", errForbidden, err)
	}
}

// factory resolves a path's factory (its id or name) within the request's organization and
// passes the Authorizer with it; unknown and invisible answer alike. Handlers reach a factory
// only through here.
func (s *server) factory(ctx context.Context, ref string) (*config.Factory, error) {
	f, err := s.factories.Lookup(ctx, ref)
	if errors.Is(err, ports.ErrNotFound) {
		return nil, errFactoryNotFound
	}

	if err != nil {
		return nil, err
	}

	if err := s.authorize(ctx, domain.FactoryID(f.Metadata.ID), errFactoryNotFound); err != nil {
		return nil, err
	}

	return f, nil
}

// item resolves a path's item (its id or <factory>#<number>) within the request's organization
// and passes the Authorizer with its factory. Handlers reach an item only through here.
func (s *server) item(ctx context.Context, ref string) (domain.Item, error) {
	it, err := s.findItem(ctx, ref)
	if errors.Is(err, ports.ErrNotFound) {
		return domain.Item{}, errItemNotFound
	}

	if err != nil {
		return domain.Item{}, err
	}

	if err := s.authorize(ctx, it.FactoryID, errItemNotFound); err != nil {
		return domain.Item{}, err
	}

	return it, nil
}

// run resolves a path's run within the request's organization and passes the Authorizer with
// its factory. Handlers reach a run only through here.
func (s *server) run(ctx context.Context, id string) (domain.Run, error) {
	r, err := s.items.Run(ctx, domain.RunID(id))
	if errors.Is(err, ports.ErrNotFound) {
		return domain.Run{}, errRunNotFound
	}

	if err != nil {
		return domain.Run{}, err
	}

	if err := s.authorize(ctx, r.FactoryID, errRunNotFound); err != nil {
		return domain.Run{}, err
	}

	return r, nil
}

func (s *server) findItem(ctx context.Context, ref string) (domain.Item, error) {
	r, ok := domain.ParseItemRef(ref)
	if !ok {
		return s.items.Item(ctx, domain.ItemID(ref))
	}

	f, err := s.factories.Lookup(ctx, r.Factory)
	if err != nil {
		return domain.Item{}, fmt.Errorf("item %s: %w", ref, err)
	}

	return s.items.ItemByNumber(ctx, domain.FactoryID(f.Metadata.ID), r.Number)
}
