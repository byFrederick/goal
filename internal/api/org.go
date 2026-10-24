package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Orgs are the install's organizations as the API reads and creates them; ports.Tenancy
// satisfies it. Org, Orgs and CreateOrg need no organization in ctx.
type Orgs interface {
	Orgs(ctx context.Context) ([]domain.OrgSummary, error)
	Org(ctx context.Context, ref string) (domain.Org, error)
	CreateOrg(ctx context.Context, by domain.Actor, n ports.NewOrg) (domain.Org, error)
}

// errOrgNotFound answers an unknown or deleted organization alike: 404 with one body.
var errOrgNotFound = fmt.Errorf("organization not found: %w", ports.ErrNotFound)

type orgKey struct{}

// scope is the middleware of every operation, after routing: it records the operationId,
// resolves the path's organization, if any, and binds the request to it (domain.WithOrg), then
// passes the Authorizer at the organization level. An unknown or deleted organization stops the
// request with 404 before anything else runs.
func (s *server) scope(next gen.StrictHandlerFunc, operation string) gen.StrictHandlerFunc {
	op := operationID(operation)

	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
		ctx = withOperation(ctx, op)

		if ref := r.PathValue("org"); ref != "" {
			o, err := s.orgs.Org(ctx, ref)
			if errors.Is(err, ports.ErrNotFound) {
				return nil, errOrgNotFound
			}

			if err != nil {
				return nil, err
			}

			ctx = context.WithValue(domain.WithOrg(ctx, o.ID), orgKey{}, o)
		}

		if err := s.authorize(ctx, ""); err != nil {
			return nil, err
		}

		return next(ctx, w, r, req)
	}
}

// operationID turns the generated handler name (ApproveItem) back into the contract's
// operationId (approveItem).
func operationID(handler string) string {
	r, size := utf8.DecodeRuneInString(handler)

	return string(unicode.ToLower(r)) + handler[size:]
}

// orgOf returns the organization the request is bound to.
func orgOf(ctx context.Context) domain.Org {
	o, _ := ctx.Value(orgKey{}).(domain.Org)

	return o
}

func (s *server) ListOrgs(ctx context.Context, _ gen.ListOrgsRequestObject) (gen.ListOrgsResponseObject, error) {
	orgs, err := s.orgs.Orgs(ctx)
	if err != nil {
		return nil, err
	}

	out := gen.ListOrgs200JSONResponse{Orgs: make([]gen.OrgSummary, 0, len(orgs))}
	for _, o := range orgs {
		out.Orgs = append(out.Orgs, gen.OrgSummary{
			Id: string(o.ID), Slug: o.Slug, Name: o.Name, Mark: o.Mark, FactoryCount: o.FactoryCount,
		})
	}

	return out, nil
}

func (s *server) CreateOrg(ctx context.Context, req gen.CreateOrgRequestObject) (gen.CreateOrgResponseObject, error) {
	actor, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("create organization: the request has no actor")
	}

	b := req.Body

	o, err := s.orgs.CreateOrg(ctx, actor, ports.NewOrg{Slug: b.Slug, Name: strings.TrimSpace(b.Name), Mark: deref(b.Mark)})
	if err != nil {
		return nil, err
	}

	return gen.CreateOrg201JSONResponse(orgOut(o, 0)), nil
}

func (s *server) GetOrg(ctx context.Context, _ gen.GetOrgRequestObject) (gen.GetOrgResponseObject, error) {
	o := orgOf(ctx)

	orgs, err := s.orgs.Orgs(ctx)
	if err != nil {
		return nil, err
	}

	count := 0

	for _, sum := range orgs {
		if sum.ID == o.ID {
			count = sum.FactoryCount
		}
	}

	return gen.GetOrg200JSONResponse(orgOut(o, count)), nil
}

func orgOut(o domain.Org, factories int) gen.Org {
	return gen.Org{
		Id: string(o.ID), Slug: o.Slug, Name: o.Name, Mark: o.Mark, FactoryCount: factories, CreatedAt: o.CreatedAt,
	}
}
