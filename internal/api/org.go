package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Orgs are the install's organizations as the API reads and changes them; ports.Tenancy
// satisfies it. Org, Orgs and CreateOrg need no organization in ctx; the others act on the one
// in ctx.
type Orgs interface {
	Orgs(ctx context.Context) ([]domain.OrgSummary, error)
	Org(ctx context.Context, ref string) (domain.Org, error)
	CreateOrg(ctx context.Context, by domain.Actor, n ports.NewOrg) (domain.Org, error)
	UpdateOrg(ctx context.Context, by domain.Actor, u ports.OrgUpdate) (domain.Org, error)
	DeleteOrg(ctx context.Context, by domain.Actor, now time.Time) error
	FactorySlots(ctx context.Context) (map[domain.FactoryID]int, error)
}

// errOrgNotFound answers an unknown or deleted organization alike: 404 with one body.
var errOrgNotFound = fmt.Errorf("organization not found: %w", ports.ErrNotFound)

type orgKey struct{}

// scope is the middleware of every operation, after routing: it records the operationId, makes
// the request's principal and binds the request to the path's organization, if any
// (bindRequest), then passes the Authorizer at the organization level. An unknown credential
// stops the request with 401, then an unknown or deleted organization with 404, before anything
// else runs.
func (s *server) scope(next gen.StrictHandlerFunc, operation string) gen.StrictHandlerFunc {
	op := operationID(operation)

	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
		ctx, err := s.bindRequest(withOperation(ctx, op), op, r.PathValue("org"))
		if err != nil {
			return nil, err
		}

		if err := s.authorize(ctx, "", errOrgNotFound); err != nil {
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

// ListOrgs lists the caller's organizations: every one of the install under GOAL_AUTH=none, a
// person's memberships (Identity.OrgsFor) with their role and the factories they see, an API
// key's own organization.
func (s *server) ListOrgs(ctx context.Context, _ gen.ListOrgsRequestObject) (gen.ListOrgsResponseObject, error) {
	p := principalOf(ctx)
	out := gen.ListOrgs200JSONResponse{Orgs: []gen.OrgSummary{}}

	switch {
	case s.auth.Mode == AuthNone:
		orgs, err := s.orgs.Orgs(ctx)
		if err != nil {
			return nil, err
		}

		for _, o := range orgs {
			out.Orgs = append(out.Orgs, gen.OrgSummary{Id: string(o.ID), Slug: o.Slug, Name: o.Name, Mark: o.Mark, FactoryCount: o.FactoryCount})
		}
	case p.Kind == domain.PrincipalService:
		o, err := s.orgs.Org(ctx, string(p.Org))
		if errors.Is(err, ports.ErrNotFound) {
			return out, nil // its organization is gone; the key no longer resolves
		}

		if err != nil {
			return nil, err
		}

		n, err := s.visibleCount(domain.WithOrg(ctx, o.ID))
		if err != nil {
			return nil, err
		}

		out.Orgs = append(out.Orgs, gen.OrgSummary{Id: string(o.ID), Slug: o.Slug, Name: o.Name, Mark: o.Mark, FactoryCount: n})
	default:
		orgs, err := s.identity.OrgsFor(ctx, p.User)
		if err != nil {
			return nil, err
		}

		for _, m := range orgs {
			o := m.Org
			out.Orgs = append(out.Orgs, gen.OrgSummary{
				Id: string(o.ID), Slug: o.Slug, Name: o.Name, Mark: o.Mark, FactoryCount: m.FactoryCount, Role: s.orgRole(p, m.Role),
			})
		}
	}

	return out, nil
}

// CreateOrg creates an organization owned by the caller (a platform admin under
// GOAL_AUTH=session; under none, the install's earliest platform admin if anyone has signed in).
func (s *server) CreateOrg(ctx context.Context, req gen.CreateOrgRequestObject) (gen.CreateOrgResponseObject, error) {
	actor, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("create organization: the request has no actor")
	}

	b := req.Body
	p := principalOf(ctx)
	n := ports.NewOrg{Slug: b.Slug, Name: strings.TrimSpace(b.Name), Mark: deref(b.Mark)}

	if s.auth.Mode == AuthSession && p.Kind == domain.PrincipalUser {
		n.Owner = p.User
	}

	o, err := s.orgs.CreateOrg(ctx, actor, n)
	if err != nil {
		return nil, err
	}

	// The caller is its owner now: the answer says what they may do there.
	p.Org, p.OrgRole, p.FactoryRoles = o.ID, domain.OrgOwner, nil
	ctx = context.WithValue(domain.WithOrg(domain.WithPrincipal(ctx, p), o.ID), orgKey{}, o)

	return gen.CreateOrg201JSONResponse(s.orgOut(ctx, o, 0)), nil
}

// GetOrg is the request's organization with the caller's role, what they may do there and how
// many of its factories they see.
func (s *server) GetOrg(ctx context.Context, _ gen.GetOrgRequestObject) (gen.GetOrgResponseObject, error) {
	n, err := s.visibleCount(ctx)
	if err != nil {
		return nil, err
	}

	return gen.GetOrg200JSONResponse(s.orgOut(ctx, orgOf(ctx), n)), nil
}

// visibleCount counts the factories of the organization in ctx the caller sees.
func (s *server) visibleCount(ctx context.Context) (int, error) {
	recs, err := s.defs.Factories(ctx)
	if err != nil {
		return 0, err
	}

	n := 0

	for _, r := range recs {
		if s.visible(ctx, r.ID) {
			n++
		}
	}

	return n, nil
}

// orgOut is the organization in ctx's request as the caller sees it.
func (s *server) orgOut(ctx context.Context, o domain.Org, factories int) gen.Org {
	p := principalOf(ctx)

	return gen.Org{
		Id: string(o.ID), Slug: o.Slug, Name: o.Name, Mark: o.Mark, FactoryCount: factories, CreatedAt: o.CreatedAt,
		Role: s.orgRole(p, p.OrgRole), Can: permissionsOut(s.can(ctx, "")),
	}
}
