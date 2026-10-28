package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// A factory's saved views of the Items page (ADR 0020 decisions 1 and 2). Every operation's scope
// is factory:read: a reader lists the factory's shared views and their own personal ones, and
// manages their own. Saving, renaming or deleting a shared view also needs factory:manage. Only a
// signed-in person owns a view, so every write from an API key or a personal token, and every
// write under GOAL_AUTH=none, is refused (403 person-required); they list the shared views only.

// permFactoryManage is the editor's permission a shared view's writes ask about.
const permFactoryManage domain.Permission = "factory:manage"

// Errors of the saved views routes, answered with their codes by viewProblem.
var (
	errViewNotFound = errors.New("saved view not found")
	errViewTaken    = errors.New("saved view name taken")
	errViewInvalid  = errors.New("saved view invalid")
	// errViewNeedsPerson refuses a write by a caller who is not a signed-in person (403
	// person-required). Its own code, not forbidden: the caller passed authorization, and only
	// the view's owner is missing.
	errViewNeedsPerson = errors.New("only a signed-in person owns a saved view, never an API key or a personal token")
)

// viewProblem answers the saved views rules with their codes.
func viewProblem(err error) (gen.Problem, bool) {
	answers := []struct {
		err    error
		status int
		code   string
	}{
		{errViewNeedsPerson, http.StatusForbidden, "person-required"},
		{errViewNotFound, http.StatusNotFound, "view-not-found"},
		{errViewTaken, http.StatusConflict, "view-name-taken"},
		{errViewInvalid, http.StatusBadRequest, "invalid-view"},
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

// viewError is err of the store about a view as the API answers it. A clash wraps
// domain.ErrInvalid, so it is tested first.
func viewError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, domain.ErrSavedViewNameTaken):
		return fmt.Errorf("%w: %w", errViewTaken, err)
	case errors.Is(err, ports.ErrNotFound):
		return fmt.Errorf("%w: %w", errViewNotFound, err)
	case errors.Is(err, domain.ErrInvalid):
		return fmt.Errorf("%w: %w", errViewInvalid, err)
	default:
		return err
	}
}

func (s *server) ListSavedViews(ctx context.Context, req gen.ListSavedViewsRequestObject) (gen.ListSavedViewsResponseObject, error) {
	f, err := s.factory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	viewer, _ := s.viewOwner(ctx)

	views, err := s.items.ListSavedViews(ctx, domain.FactoryID(f.Metadata.ID), viewer)
	if err != nil {
		return nil, err
	}

	out := gen.ListSavedViews200JSONResponse{Views: make([]gen.SavedView, 0, len(views))}
	for _, v := range views {
		out.Views = append(out.Views, viewOut(v, viewer))
	}

	return out, nil
}

func (s *server) CreateSavedView(ctx context.Context, req gen.CreateSavedViewRequestObject) (gen.CreateSavedViewResponseObject, error) {
	f, err := s.factory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	owner, err := s.viewOwner(ctx)
	if err != nil {
		return nil, err
	}

	fid := domain.FactoryID(f.Metadata.ID)

	if req.Body.Shared {
		if err := s.mayShare(ctx, fid); err != nil {
			return nil, err
		}
	}

	v, err := s.items.CreateSavedView(ctx, domain.SavedView{
		Factory: fid, Owner: owner, Name: req.Body.Name, Shared: req.Body.Shared, Filters: req.Body.Filters, CreatedAt: s.clock.Now(),
	})
	if err != nil {
		return nil, viewError(err)
	}

	return gen.CreateSavedView201JSONResponse(viewOut(v, owner)), nil
}

func (s *server) UpdateSavedView(ctx context.Context, req gen.UpdateSavedViewRequestObject) (gen.UpdateSavedViewResponseObject, error) {
	fid, v, owner, err := s.writableView(ctx, req.Factory, req.View)
	if err != nil {
		return nil, err
	}

	// Validated as the store does, so a bad change answers invalid-view whatever the store says.
	change := domain.SavedView{Name: req.Body.Name, Filters: req.Body.Filters}
	if err := change.Validate(); err != nil {
		return nil, viewError(err)
	}

	v, err = s.items.UpdateSavedView(ctx, fid, v.ID, change.Name, change.Filters, s.clock.Now())
	if err != nil {
		return nil, viewError(err)
	}

	return gen.UpdateSavedView200JSONResponse(viewOut(v, owner)), nil
}

func (s *server) DeleteSavedView(ctx context.Context, req gen.DeleteSavedViewRequestObject) (gen.DeleteSavedViewResponseObject, error) {
	fid, v, _, err := s.writableView(ctx, req.Factory, req.View)
	if err != nil {
		return nil, err
	}

	if err := s.items.DeleteSavedView(ctx, fid, v.ID); err != nil {
		return nil, viewError(err)
	}

	return gen.DeleteSavedView204Response{}, nil
}

// viewOwner is the signed-in person a view of the request belongs to, or errViewNeedsPerson for
// an API key, a personal token or GOAL_AUTH=none's local user (whose id is no person's).
func (s *server) viewOwner(ctx context.Context) (domain.UserID, error) {
	p := principalOf(ctx)
	if s.auth.Mode == AuthNone || p.Kind != domain.PrincipalUser || p.User == "" || p.Token != nil {
		return "", errViewNeedsPerson
	}

	return p.User, nil
}

// mayShare refuses a write of factory f's shared views to a caller without factory:manage.
func (s *server) mayShare(ctx context.Context, f domain.FactoryID) error {
	return refusal(s.ask(ctx, permFactoryManage, f), errFactoryNotFound)
}

// writableView resolves a path's factory and a view of it the caller may change: their own
// personal view, or a shared one when they hold factory:manage. A view the caller does not see,
// another person's personal view included, answers as unknown.
func (s *server) writableView(ctx context.Context, factoryRef, id string) (domain.FactoryID, domain.SavedView, domain.UserID, error) {
	f, err := s.factory(ctx, factoryRef)
	if err != nil {
		return "", domain.SavedView{}, "", err
	}

	owner, err := s.viewOwner(ctx)
	if err != nil {
		return "", domain.SavedView{}, "", err
	}

	fid := domain.FactoryID(f.Metadata.ID)

	// The store changes any view of the factory, so the caller's are found among those they see.
	views, err := s.items.ListSavedViews(ctx, fid, owner)
	if err != nil {
		return "", domain.SavedView{}, "", err
	}

	i := -1

	for j, v := range views {
		if string(v.ID) == id {
			i = j
			break
		}
	}

	if i < 0 {
		return "", domain.SavedView{}, "", fmt.Errorf("%w: factory %s has no view %s the caller sees", errViewNotFound, fid, id)
	}

	if views[i].Shared {
		if err := s.mayShare(ctx, fid); err != nil {
			return "", domain.SavedView{}, "", err
		}
	}

	return fid, views[i], owner, nil
}

// viewOut is v as the contract has it, mine when viewer made it.
func viewOut(v domain.SavedView, viewer domain.UserID) gen.SavedView {
	filters := gen.SavedViewFilters(v.Filters)
	if filters == nil {
		filters = gen.SavedViewFilters{}
	}

	return gen.SavedView{
		Id: string(v.ID), Name: v.Name, Shared: v.Shared, Filters: filters, Mine: viewer != "" && v.Owner == viewer,
		CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	}
}
