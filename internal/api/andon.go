package api

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/engine"
)

// Limits of the andon reads.
const (
	defaultAndonPulls = 10
	maxAndonPulls     = 50
)

// GetAndon answers the factory's andon: the open pull, the recent ones, the policy and, per gate
// station in line order, the tally of its last verdicts since the last clear.
func (s *server) GetAndon(ctx context.Context, req gen.GetAndonRequestObject) (gen.GetAndonResponseObject, error) {
	limit := defaultAndonPulls
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}

	if limit < 1 || limit > maxAndonPulls {
		return nil, fmt.Errorf("%w: limit must be in [1, %d]", errInvalid, maxAndonPulls)
	}

	f, err := s.factory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	l := f.Line()

	open, err := s.items.OpenAndon(ctx, l.FactoryID)
	if err != nil {
		return nil, err
	}

	// At most one pull is open, so the newest cleared one is among the latest two.
	pulls, err := s.items.AndonPulls(ctx, l.FactoryID, max(limit, 2))
	if err != nil {
		return nil, err
	}

	out := gen.GetAndon200JSONResponse{Open: andonOf(open), Recent: make([]gen.AndonPull, 0, limit)}
	for _, p := range pulls[:min(limit, len(pulls))] {
		out.Recent = append(out.Recent, *andonOf(&p))
	}

	var lastClear time.Time

	if i := slices.IndexFunc(pulls, func(p domain.AndonPull) bool { return !p.Open() }); i >= 0 {
		lastClear = pulls[i].ClearedAt
	}

	if p := l.Flow.Andon; p != nil {
		out.Policy = &struct {
			ReworkRate float64 `json:"rework_rate"`
			Window     int     `json:"window"`
		}{ReworkRate: p.ReworkRate, Window: p.Window}
	}

	if out.Gates, err = s.gateTallies(ctx, l, lastClear); err != nil {
		return nil, err
	}

	return out, nil
}

// gateTallies tallies each gate station of the line, in line order, over its last verdicts after
// the last clear: the policy's window of them, or domain.DefaultAndonWindow without a policy.
func (s *server) gateTallies(ctx context.Context, l domain.Line, after time.Time) ([]gen.GateTally, error) {
	policy, window := l.Flow.Andon, domain.DefaultAndonWindow
	if policy != nil {
		window = policy.Window
	}

	out := []gen.GateTally{}

	for _, st := range l.Stations {
		if st.Gate == nil {
			continue
		}

		tally, err := s.items.GateDecisions(ctx, l.FactoryID, st.ID, after, window)
		if err != nil {
			return nil, err
		}

		out = append(out, gen.GateTally{
			Station: st.ID, Verdicts: tally.Verdicts, Rejected: tally.Rejected, TopReason: optional(tally.TopReason),
			Warning: policy != nil && policy.Warns(tally),
		})
	}

	return out, nil
}

// PullAndon pulls the factory's andon cord as the request's actor.
func (s *server) PullAndon(ctx context.Context, req gen.PullAndonRequestObject) (gen.PullAndonResponseObject, error) {
	if req.Body == nil {
		return nil, fmt.Errorf("%w: a pull needs its reason", errInvalid)
	}

	by, f, err := s.andonCall(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	p, err := s.engine.PullAndon(ctx, f, by, req.Body.Reason)
	if err != nil {
		return nil, andonError(err)
	}

	return gen.PullAndon201JSONResponse(*andonOf(&p)), nil
}

// ClearAndon clears the factory's andon cord as the request's actor.
func (s *server) ClearAndon(ctx context.Context, req gen.ClearAndonRequestObject) (gen.ClearAndonResponseObject, error) {
	by, f, err := s.andonCall(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	var note string
	if req.Body != nil {
		note = deref(req.Body.Note)
	}

	p, err := s.engine.ClearAndon(ctx, f, by, note)
	if err != nil {
		return nil, andonError(err)
	}

	return gen.ClearAndon200JSONResponse(*andonOf(&p)), nil
}

// andonCall resolves the factory a pull or clear is about, and who acts: the request's actor.
func (s *server) andonCall(ctx context.Context, ref string) (domain.Actor, domain.FactoryID, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return domain.Actor{}, "", errors.New("andon: the request has no actor")
	}

	f, err := s.factory(ctx, ref)
	if err != nil {
		return domain.Actor{}, "", err
	}

	return by, domain.FactoryID(f.Metadata.ID), nil
}

// andonError marks a refused reason or note as an invalid request; the engine's other refusals
// map to their problem as they are.
func andonError(err error) error {
	if errors.Is(err, engine.ErrBadAndon) {
		return fmt.Errorf("%w: %w", errInvalid, err)
	}

	return err
}

func andonOf(p *domain.AndonPull) *gen.AndonPull {
	if p == nil {
		return nil
	}

	data := p.Data
	if data == nil {
		data = map[string]any{}
	}

	out := gen.AndonPull{
		Id: p.ID, Kind: gen.AndonKind(p.Kind), Station: optional(p.Station), Reason: p.Reason, PulledBy: p.PulledBy, PulledAt: p.PulledAt,
		ClearedBy: optional(p.ClearedBy), Note: optional(p.Note), Data: data,
	}

	if !p.Open() {
		out.ClearedAt = &p.ClearedAt
	}

	return &out
}
