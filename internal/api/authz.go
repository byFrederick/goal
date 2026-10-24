package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Authorizer decides whether the request's actor may run an operation. Phase 2.2 wires
// AllowAll, which allows everything (there is no identity yet); phase 4 supplies the real
// implementation and its permission table, and nothing else in the API changes.
//
// Every request passes it once in the /v1 middleware, after its organization is resolved
// (Factory ""), and again in the only resolvers that turn a path into a factory, an item or a
// run, server.factory, server.item and server.run, with that factory. A non-nil error stops
// the request before the handler acts: one wrapping ports.ErrNotFound answers 404, any other 403.
type Authorizer interface {
	Authorize(ctx context.Context, r AccessRequest) error
}

// AccessRequest is one authorization question.
type AccessRequest struct {
	Actor domain.Actor
	// Operation is the OpenAPI operationId, such as approveItem.
	Operation string
	// Org is "" for the operations outside an organization (listOrgs, createOrg, getHealth).
	Org domain.OrgID
	// Factory is "" for organization-level operations.
	Factory domain.FactoryID
}

// AllowAll allows every request: phase 2.2's single local user may do everything.
type AllowAll struct{}

// Authorize implements Authorizer.
func (AllowAll) Authorize(context.Context, AccessRequest) error { return nil }

// errForbidden marks a request the Authorizer refused (403).
var errForbidden = errors.New("forbidden")

type operationKey struct{}

// withOperation records the request's operationId for the Authorizer.
func withOperation(ctx context.Context, operation string) context.Context {
	return context.WithValue(ctx, operationKey{}, operation)
}

// authorize asks the Authorizer about the request in ctx and, when factory is set, that factory.
func (s *server) authorize(ctx context.Context, factory domain.FactoryID) error {
	actor, ok := domain.ActorFrom(ctx)
	if !ok {
		return errors.New("authorize: the request has no actor")
	}

	operation, _ := ctx.Value(operationKey{}).(string)
	org, _ := domain.OrgFrom(ctx)

	err := s.authz.Authorize(ctx, AccessRequest{Actor: actor, Operation: operation, Org: org, Factory: factory})

	switch {
	case err == nil:
		return nil
	case errors.Is(err, ports.ErrNotFound):
		return err
	default:
		return fmt.Errorf("%w: %w", errForbidden, err)
	}
}

// factory resolves a path's factory (its id or name) within the request's organization and
// passes the Authorizer with it. Handlers reach a factory only through here.
func (s *server) factory(ctx context.Context, ref string) (*config.Factory, error) {
	f, err := s.factories.Lookup(ctx, ref)
	if err != nil {
		return nil, err
	}

	if err := s.authorize(ctx, domain.FactoryID(f.Metadata.ID)); err != nil {
		return nil, err
	}

	return f, nil
}

// item resolves a path's item (its id or <factory>#<number>) within the request's organization
// and passes the Authorizer with its factory. Handlers reach an item only through here.
func (s *server) item(ctx context.Context, ref string) (domain.Item, error) {
	it, err := s.findItem(ctx, ref)
	if err != nil {
		return domain.Item{}, err
	}

	if err := s.authorize(ctx, it.FactoryID); err != nil {
		return domain.Item{}, err
	}

	return it, nil
}

// run resolves a path's run within the request's organization and passes the Authorizer with
// its factory. Handlers reach a run only through here.
func (s *server) run(ctx context.Context, id string) (domain.Run, error) {
	r, err := s.items.Run(ctx, domain.RunID(id))
	if err != nil {
		return domain.Run{}, err
	}

	if err := s.authorize(ctx, r.FactoryID); err != nil {
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
