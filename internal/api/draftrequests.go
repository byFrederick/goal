package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The draft request routes (ADR 0021): asked changes to a factory's definition, queued here and
// run by goald in a box that writes its result into the draft.

// defaultDraftRequestList is how many requests a list holds without a limit.
const defaultDraftRequestList = 20

var (
	// errDraftRequestText is text outside 1 to domain.MaxDraftRequestText characters once
	// trimmed: 422 draft-request-text-invalid.
	errDraftRequestText = fmt.Errorf("draft request text must be 1 to %d characters", domain.MaxDraftRequestText)
	// errDraftRequestNotFound is a request that is not the factory's: 404 draft-request-not-found.
	errDraftRequestNotFound = fmt.Errorf("draft request %w", ports.ErrNotFound)
)

func (s *server) CreateDraftRequest(ctx context.Context, req gen.CreateDraftRequestRequestObject) (gen.CreateDraftRequestResponseObject, error) {
	rec, err := s.definition(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	by, err := actorOf(ctx, "create draft request")
	if err != nil {
		return nil, err
	}

	if req.Body == nil {
		return nil, fmt.Errorf("%w: no body", errInvalid)
	}

	n := domain.NewDraftRequest{Factory: rec.ID, Requester: by, Text: strings.TrimSpace(req.Body.Text), Agent: deref(req.Body.Agent)}

	if c := utf8.RuneCountInString(n.Text); c < 1 || c > domain.MaxDraftRequestText {
		return nil, fmt.Errorf("%w, not %d", errDraftRequestText, c)
	}

	if n.Agent != "" {
		if err := s.draftAgent(ctx, rec.ID, n.Agent); err != nil {
			return nil, err
		}
	}

	r, err := s.draftRequests.Create(ctx, n)
	if err != nil {
		return nil, fmt.Errorf("create draft request on %s: %w", rec.ID, err)
	}

	return gen.CreateDraftRequest201JSONResponse(s.draftRequestOut(r)), nil
}

// draftAgent checks that agent is an agent of the factory's definition as a request starts from
// it (ADR 0021, decision 2): the open draft's, or the current version's when no draft is open.
// errAgentNotFound when it has none; a draft whose factory.yaml does not parse is its
// *authoring.ParseError (409 yaml-invalid).
func (s *server) draftAgent(ctx context.Context, f domain.FactoryID, agent string) error {
	var m authoring.Model

	d, err := s.defs.DraftModel(ctx, f)

	switch {
	case err == nil:
		m = d.Model
	case errors.Is(err, authoring.ErrNoDraft):
		if m, _, err = s.defs.VersionModel(ctx, f, authoring.VersionSelector{Kind: authoring.SelectCurrent}); err != nil {
			return err
		}
	default:
		return err
	}

	agents, _ := m.Agents["agents"].([]any)
	if !slices.ContainsFunc(agents, func(a any) bool {
		e, _ := a.(map[string]any)

		return e["name"] == agent
	}) {
		return fmt.Errorf("the definition of %s has no %q: %w", f, agent, errAgentNotFound)
	}

	return nil
}

func (s *server) ListDraftRequests(ctx context.Context, req gen.ListDraftRequestsRequestObject) (gen.ListDraftRequestsResponseObject, error) {
	limit := defaultDraftRequestList
	if l := req.Params.Limit; l != nil {
		if *l < 1 || *l > domain.MaxDraftRequestList {
			return nil, fmt.Errorf("%w: limit %d is not 1 to %d", errInvalid, *l, domain.MaxDraftRequestList)
		}

		limit = *l
	}

	rec, err := s.definition(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	list, err := s.draftRequests.List(ctx, rec.ID, limit)
	if err != nil {
		return nil, fmt.Errorf("list draft requests of %s: %w", rec.ID, err)
	}

	out := gen.ListDraftRequests200JSONResponse{Requests: make([]gen.DraftRequest, 0, len(list))}
	for _, r := range list {
		out.Requests = append(out.Requests, s.draftRequestOut(r))
	}

	return out, nil
}

func (s *server) GetDraftRequest(ctx context.Context, req gen.GetDraftRequestRequestObject) (gen.GetDraftRequestResponseObject, error) {
	rec, err := s.definition(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	r, err := s.draftRequests.Get(ctx, domain.DraftRequestID(req.DraftRequest))
	if errors.Is(err, ports.ErrNotFound) || err == nil && r.Factory != rec.ID {
		return nil, fmt.Errorf("%q of %s: %w", req.DraftRequest, rec.ID, errDraftRequestNotFound)
	}

	if err != nil {
		return nil, fmt.Errorf("get draft request %s: %w", req.DraftRequest, err)
	}

	return gen.GetDraftRequest200JSONResponse(s.draftRequestOut(r)), nil
}

// draftRequestOut is r as the routes answer it.
func (s *server) draftRequestOut(r domain.DraftRequest) gen.DraftRequest {
	return gen.DraftRequest{
		Id: string(r.ID), Text: r.Text, Agent: r.Agent, Status: gen.DraftRequestStatus(r.Status), Summary: r.Summary,
		ChangedPaths: append([]string{}, r.ChangedPaths...), CostUsd: r.CostUSD, Turns: r.Turns, Error: r.Error,
		Requester: s.actorOut(r.Requester), CreatedAt: r.CreatedAt, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt,
	}
}

// draftRequestProblem answers the draft request refusals with their codes.
func draftRequestProblem(err error) (gen.Problem, bool) {
	for _, c := range []struct {
		err    error
		status int
		code   string
	}{
		{errDraftRequestText, http.StatusUnprocessableEntity, "draft-request-text-invalid"},
		{errDraftRequestNotFound, http.StatusNotFound, "draft-request-not-found"},
		{domain.ErrDraftRequestOpen, http.StatusConflict, "draft-request-open"},
	} {
		if errors.Is(err, c.err) {
			p := detailed(c.status, err)
			p.Code = optional(c.code)

			return p, true
		}
	}

	return gen.Problem{}, false
}
