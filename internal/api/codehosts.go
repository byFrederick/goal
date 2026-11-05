package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
)

// GetCodeHosts lists the organization's code hosts (ADR 0041, decision 1): the built-in ones,
// then those its owners added.
func (s *server) GetCodeHosts(ctx context.Context, _ gen.GetCodeHostsRequestObject) (gen.GetCodeHostsResponseObject, error) {
	added, err := s.orgs.CodeHosts(ctx)
	if err != nil {
		return nil, err
	}

	return gen.GetCodeHosts200JSONResponse(codeHostsOut(added)), nil
}

// SetCodeHosts replaces the hosts the organization added, for an owner in a fresh session.
func (s *server) SetCodeHosts(ctx context.Context, req gen.SetCodeHostsRequestObject) (gen.SetCodeHostsResponseObject, error) {
	actor, err := actorOf(ctx, "set code hosts")
	if err != nil {
		return nil, err
	}

	in := make([]domain.CodeHost, 0, len(req.Body.Hosts))
	for _, h := range req.Body.Hosts {
		in = append(in, domain.CodeHost{Host: h.Host, Kind: domain.CodeHostKind(h.Kind)})
	}

	hosts, err := domain.CheckCodeHosts(in)
	if err != nil {
		return nil, err
	}

	added, err := s.orgs.SetCodeHosts(ctx, actor, hosts)
	if err != nil {
		return nil, err
	}

	return gen.SetCodeHosts200JSONResponse(codeHostsOut(added)), nil
}

// codeHostsOut is the built-in hosts, then added, each with its API base.
func codeHostsOut(added []domain.CodeHost) gen.CodeHosts {
	out := gen.CodeHosts{Hosts: []gen.CodeHost{}}
	for _, h := range append(domain.BuiltInCodeHosts(), added...) {
		out.Hosts = append(out.Hosts, gen.CodeHost{
			Host: h.Host, Kind: gen.CodeHostKind(h.Kind), ApiBase: domain.CodeHostAPIBase(h), BuiltIn: h.BuiltIn,
		})
	}

	return out
}

// codeHostProblem answers the refusals of a code host with 400: code-host-built-in for
// github.com or gitlab.com, code-host-invalid for anything else.
func codeHostProblem(err error) (gen.Problem, bool) {
	code := ""

	switch {
	case errors.Is(err, domain.ErrCodeHostBuiltIn):
		code = "code-host-built-in"
	case errors.Is(err, domain.ErrCodeHostInvalid):
		code = "code-host-invalid"
	default:
		return gen.Problem{}, false
	}

	p := detailed(http.StatusBadRequest, err)
	p.Code = optional(code)

	return p, true
}
