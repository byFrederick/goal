package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/engine"
	"github.com/byFrederick/goal/internal/ports"
)

// The longest title and brief a part added through the API may have (NewPartRequest).
const (
	maxPartTitle = 200
	maxPartBrief = 10000
)

// errPartInvalid is a part the assembly refuses as malformed: no title or brief, one too long,
// or a need that names none of its parts.
var errPartInvalid = errors.New("invalid part")

// ListItemParts lists an assembly's parts in plan order (ADR 0012): each one's needs, the needs
// still open, its wave, pull request and the agent working it, with how many are done and the
// assembly's lead time. An item that is not an assembly has none.
func (s *server) ListItemParts(ctx context.Context, req gen.ListItemPartsRequestObject) (gen.ListItemPartsResponseObject, error) {
	it, err := s.item(ctx, req.Item)
	if err != nil {
		return nil, err
	}

	parts, err := s.items.Parts(ctx, it.ID)
	if err != nil {
		return nil, fmt.Errorf("item %s: parts: %w", it.ID, err)
	}

	out, err := s.assemblyParts(ctx, it, parts)
	if err != nil {
		return nil, err
	}

	return gen.ListItemParts200JSONResponse(out), nil
}

// AddItemPart adds one part to an open assembly, by the request's actor (ADR 0012 §9), and
// answers the assembly's parts as ListItemParts does, the new one last.
func (s *server) AddItemPart(ctx context.Context, req gen.AddItemPartRequestObject) (gen.AddItemPartResponseObject, error) {
	if req.Body == nil {
		return nil, fmt.Errorf("%w: a part has a title and a brief", errPartInvalid)
	}

	if utf8.RuneCountInString(req.Body.Title) > maxPartTitle || utf8.RuneCountInString(req.Body.Brief) > maxPartBrief {
		return nil, fmt.Errorf("%w: a part's title is at most %d characters and its brief at most %d", errPartInvalid, maxPartTitle, maxPartBrief)
	}

	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("add part: the request has no actor")
	}

	it, err := s.item(ctx, req.Item)
	if err != nil {
		return nil, err
	}

	_, parts, err := s.engine.AddPart(ctx, it.ID, by, domain.NewPart{
		Title: req.Body.Title, Lane: deref(req.Body.Lane), Brief: req.Body.Brief, Needs: deref(req.Body.Needs),
	})
	if err != nil {
		return nil, partRefusal(err)
	}

	out, err := s.assemblyParts(ctx, it, parts)
	if err != nil {
		return nil, err
	}

	return gen.AddItemPart201JSONResponse(out), nil
}

// partRefusal is err of an added part with the API's meaning: a cycle is edge-cycle, anything
// else malformed part-invalid (a 400, where domain.ErrInvalid is otherwise a 409).
func partRefusal(err error) error {
	switch {
	case errors.Is(err, domain.ErrCycle):
		return fmt.Errorf("%w: %w", errEdgeCycle, err)
	case errors.Is(err, domain.ErrInvalid):
		return fmt.Errorf("%w: %w", errPartInvalid, err)
	default:
		return err
	}
}

// partProblem answers the refusals of an added part with their codes; a closed assembly is
// problemOf's item-closed, and a cycle edgeProblem's edge-cycle.
func partProblem(err error) (gen.Problem, bool) {
	answers := []struct {
		err    error
		status int
		code   string
	}{
		{err: errPartInvalid, status: http.StatusBadRequest, code: "part-invalid"},
		{err: engine.ErrNotAssembly, status: http.StatusConflict, code: "not-an-assembly"},
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

// assemblyParts is the body of an assembly's parts, in plan order, shared by ListItemParts and
// AddItemPart so both answer the same.
func (s *server) assemblyParts(ctx context.Context, it domain.Item, parts domain.Parts) (gen.AssemblyParts, error) {
	out := gen.AssemblyParts{Done: parts.Done, Total: parts.Total, Parts: make([]gen.AssemblyPart, 0, len(parts.Items))}
	if parts.Total == 0 {
		return out, nil
	}

	l, err := s.lineOf(ctx, it.FactoryID)
	if err != nil {
		return gen.AssemblyParts{}, err
	}

	needs, err := s.partNeeds(ctx, parts.Items)
	if err != nil {
		return gen.AssemblyParts{}, err
	}

	agents, err := s.partAgents(ctx, it.FactoryID, parts.Items)
	if err != nil {
		return gen.AssemblyParts{}, err
	}

	byID := make(map[domain.ItemID]domain.Item, len(parts.Items))
	for _, p := range parts.Items {
		byID[p.ID] = p
	}

	waves := domain.PartWaves(needs)

	for i, p := range parts.Items {
		ap := gen.AssemblyPart{
			Id: string(p.ID), Ref: s.ref(ctx, p.FactoryID, p.Number), Number: p.Number, Index: i + 1, Key: p.PartKey,
			Title: p.Title, Paths: append([]string{}, p.PartPaths...), Station: p.Station, State: gen.ItemState(p.State),
			Needs: []string{}, WaitingOn: []string{}, Wave: waves[string(p.ID)], CostUsd: p.CostUSD,
			PrUrl: optional(p.PullRequest(l)), Agent: optional(agents[p.ID]), CreatedAt: p.CreatedAt,
		}

		for _, n := range needs[string(p.ID)] {
			need := byID[domain.ItemID(n)]
			ref := s.ref(ctx, need.FactoryID, need.Number)

			ap.Needs = append(ap.Needs, ref)
			if !closedState(need.State) {
				ap.WaitingOn = append(ap.WaitingOn, ref)
			}
		}

		if !p.DoneAt.IsZero() {
			ap.DoneAt = &p.DoneAt
		}

		out.Parts = append(out.Parts, ap)
	}

	out.LeadTimeSeconds = int64(max(assemblyLead(it, parts, s.clock.Now()), 0) / time.Second)

	return out, nil
}

// lineOf is factory's line, or no line when the organization no longer runs the factory.
func (s *server) lineOf(ctx context.Context, factory domain.FactoryID) (domain.Line, error) {
	f, err := s.factories.Lookup(ctx, string(factory))
	if errors.Is(err, ports.ErrNotFound) {
		return domain.Line{}, nil
	}

	if err != nil {
		return domain.Line{}, err
	}

	return f.Line(), nil
}

// partNeeds maps each part's id to the ids of the other parts it needs, oldest edge first. Edges
// to items that are not its assembly's parts are left out.
func (s *server) partNeeds(ctx context.Context, parts []domain.Item) (map[string][]string, error) {
	needs := make(map[string][]string, len(parts))
	for _, p := range parts {
		needs[string(p.ID)] = []string{}
	}

	for _, p := range parts {
		edges, err := s.items.Edges(ctx, p.ID)
		if err != nil {
			return nil, fmt.Errorf("part %s: edges: %w", p.ID, err)
		}

		for _, e := range edges.Needs {
			if _, part := needs[string(e.Needs)]; part {
				needs[string(p.ID)] = append(needs[string(p.ID)], string(e.Needs))
			}
		}
	}

	return needs, nil
}

// partAgents are the agents working the parts in progress, by part: the lease's agent id, never
// a numbered copy.
func (s *server) partAgents(ctx context.Context, factory domain.FactoryID, parts []domain.Item) (map[domain.ItemID]string, error) {
	out := map[domain.ItemID]string{}
	seen := map[string]bool{}

	for _, p := range parts {
		if p.State != domain.StateInProgress || seen[p.Station] {
			continue
		}

		seen[p.Station] = true

		leases, err := s.items.Leases(ctx, factory, p.Station)
		if err != nil {
			return nil, fmt.Errorf("station %s: leases: %w", p.Station, err)
		}

		for _, ls := range leases {
			out[ls.ItemID] = ls.Agent
		}
	}

	return out, nil
}

// assemblyLead is an assembly's lead time: from its creation to its last part done once it is
// closed (to when it closed when no part is done), or to now while it is open.
func assemblyLead(it domain.Item, parts domain.Parts, now time.Time) time.Duration {
	if !closedState(it.State) {
		return now.Sub(it.CreatedAt)
	}

	var end time.Time
	for _, p := range parts.Items {
		if p.DoneAt.After(end) {
			end = p.DoneAt
		}
	}

	if end.IsZero() {
		end = it.UpdatedAt
	}

	return end.Sub(it.CreatedAt)
}

// closedState reports whether an item in state st is off the line for good.
func closedState(st domain.ItemState) bool {
	return st == domain.StateDone || st == domain.StateCancelled
}

// assemblyOf is the assembly it is a part of, with its place among the parts in plan order; nil
// unless it is a part. One read of the assembly and one of its parts: getItem only.
func (s *server) assemblyOf(ctx context.Context, it domain.Item) (*gen.PartOf, error) {
	if it.PartKey == "" {
		return nil, nil //nolint:nilnil // an item that is not a part has no assembly
	}

	asm, err := s.items.Item(ctx, it.ParentID)
	if err != nil {
		return nil, fmt.Errorf("assembly of part %s: %w", it.ID, err)
	}

	parts, err := s.items.Parts(ctx, asm.ID)
	if err != nil {
		return nil, fmt.Errorf("parts of %s: %w", asm.ID, err)
	}

	return &gen.PartOf{
		Id: string(asm.ID), Ref: s.ref(ctx, asm.FactoryID, asm.Number), Title: asm.Title,
		Part:  slices.IndexFunc(parts.Items, func(p domain.Item) bool { return p.ID == it.ID }) + 1,
		Parts: parts.Total,
	}, nil
}
