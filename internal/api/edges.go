package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

var (
	// errEdgeInvalid is a link the store refuses as malformed: an item that needs itself or an
	// item of another factory.
	errEdgeInvalid = errors.New("invalid dependency")
	// errEdgeCycle is a link that would make an item need itself through other items.
	errEdgeCycle = errors.New("dependency cycle")
	// errEdgeExists is a link the item already has.
	errEdgeExists = errors.New("dependency exists")
	// errEdgeNotFound is an unlink of an item the item does not need.
	errEdgeNotFound = errors.New("dependency not found")
)

// LinkItem records that an item needs another, as a linked edge by the request's actor
// (ADR 0012): the item is not claimed while the needed one is open.
func (s *server) LinkItem(ctx context.Context, req gen.LinkItemRequestObject) (gen.LinkItemResponseObject, error) {
	if req.Body == nil || req.Body.Needs == "" {
		return nil, fmt.Errorf("%w: a link names the item needed", errInvalid)
	}

	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("link: the request has no actor")
	}

	it, err := s.item(ctx, req.Item)
	if err != nil {
		return nil, err
	}

	need, err := s.item(ctx, req.Body.Needs)
	if err != nil {
		return nil, err
	}

	e, err := s.engine.Link(ctx, it.ID, need.ID, by)

	switch {
	case errors.Is(err, domain.ErrInvalid):
		return nil, fmt.Errorf("%w: %w", errEdgeInvalid, err)
	case errors.Is(err, domain.ErrCycle):
		return nil, fmt.Errorf("%w: %w", errEdgeCycle, err)
	case errors.Is(err, ports.ErrConflict):
		return nil, fmt.Errorf("%w: %w", errEdgeExists, err)
	case err != nil:
		return nil, err
	}

	return gen.LinkItem201JSONResponse(s.linkOf(ctx, need, e)), nil
}

// UnlinkItem removes the edge from an item to one it needs, by the request's actor.
func (s *server) UnlinkItem(ctx context.Context, req gen.UnlinkItemRequestObject) (gen.UnlinkItemResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("unlink: the request has no actor")
	}

	it, err := s.item(ctx, req.Item)
	if err != nil {
		return nil, err
	}

	need, err := s.item(ctx, req.Need)
	if err != nil {
		return nil, err
	}

	err = s.engine.Unlink(ctx, it.ID, need.ID, by)
	if errors.Is(err, ports.ErrNotFound) {
		return nil, fmt.Errorf("%w: %w", errEdgeNotFound, err)
	}

	if err != nil {
		return nil, err
	}

	return gen.UnlinkItem204Response{}, nil
}

// itemLinks are what an item needs and what needs it, each with the other item's current
// state, oldest edge first.
func (s *server) itemLinks(ctx context.Context, id domain.ItemID) (needs, dependents []gen.ItemLink, err error) {
	edges, err := s.items.Edges(ctx, id)
	if err != nil {
		return nil, nil, fmt.Errorf("item %s: edges: %w", id, err)
	}

	needs, dependents = make([]gen.ItemLink, 0, len(edges.Needs)), make([]gen.ItemLink, 0, len(edges.NeededBy))

	for _, e := range edges.Needs {
		other, err := s.items.Item(ctx, e.Needs)
		if err != nil {
			return nil, nil, fmt.Errorf("item %s: need %s: %w", id, e.Needs, err)
		}

		needs = append(needs, s.linkOf(ctx, other, e))
	}

	for _, e := range edges.NeededBy {
		other, err := s.items.Item(ctx, e.Item)
		if err != nil {
			return nil, nil, fmt.Errorf("item %s: dependent %s: %w", id, e.Item, err)
		}

		dependents = append(dependents, s.linkOf(ctx, other, e))
	}

	return needs, dependents, nil
}

// linkOf is edge e seen from its other end, the item other.
func (s *server) linkOf(ctx context.Context, other domain.Item, e domain.Edge) gen.ItemLink {
	return gen.ItemLink{
		Id: string(other.ID), Ref: s.ref(ctx, other.FactoryID, other.Number), Kind: gen.ItemLinkKind(e.Kind),
		State: gen.ItemState(other.State), By: e.By, At: e.At,
	}
}

// edgeProblem answers the link and unlink refusals with their codes.
func edgeProblem(err error) (gen.Problem, bool) {
	answers := []struct {
		err    error
		status int
		code   string
	}{
		{err: errEdgeInvalid, status: http.StatusBadRequest, code: "edge-invalid"},
		{err: errEdgeCycle, status: http.StatusConflict, code: "edge-cycle"},
		{err: errEdgeExists, status: http.StatusConflict, code: "edge-exists"},
		{err: errEdgeNotFound, status: http.StatusNotFound, code: "edge-not-found"},
	}

	for _, a := range answers {
		if errors.Is(err, a.err) {
			p := detailed(a.status, err)
			p.Code = optional(a.code)

			return p, true
		}
	}

	return gen.Problem{}, false
}
