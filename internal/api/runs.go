package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Versions reads definition versions, for the number a run ran on; ports.DefinitionStore
// satisfies it.
type Versions interface {
	Version(ctx context.Context, id domain.DefinitionVersionID) (domain.DefinitionVersion, error)
}

// maxItemRuns caps an item's run list; an item with more says it is truncated.
const maxItemRuns = 200

func (s *server) ListItemRuns(ctx context.Context, req gen.ListItemRunsRequestObject) (gen.ListItemRunsResponseObject, error) {
	it, err := s.item(ctx, req.Item)
	if err != nil {
		return nil, err
	}

	runs, err := s.items.Runs(ctx, it.ID, maxItemRuns+1)
	if err != nil {
		return nil, err
	}

	out := gen.ListItemRuns200JSONResponse{Runs: make([]gen.Run, 0, min(len(runs), maxItemRuns)), Truncated: len(runs) > maxItemRuns}
	numbers := versionNumbers{s: s}

	for _, r := range runs[:min(len(runs), maxItemRuns)] {
		run, err := numbers.runOf(ctx, r)
		if err != nil {
			return nil, err
		}

		out.Runs = append(out.Runs, run)
	}

	return out, nil
}

func (s *server) GetRun(ctx context.Context, req gen.GetRunRequestObject) (gen.GetRunResponseObject, error) {
	r, err := s.run(ctx, req.Run)
	if err != nil {
		return nil, err
	}

	run, err := (&versionNumbers{s: s}).runOf(ctx, r)
	if err != nil {
		return nil, err
	}

	return gen.GetRun200JSONResponse(run), nil
}

// versionNumbers looks up the numbers of the versions runs ran on, each version once.
type versionNumbers struct {
	s     *server
	known map[domain.DefinitionVersionID]*int
}

func (v *versionNumbers) runOf(ctx context.Context, r domain.Run) (gen.Run, error) {
	number, err := v.number(ctx, r.DefinitionVersionID)
	if err != nil {
		return gen.Run{}, err
	}

	run := gen.Run{
		Id: string(r.ID), ItemId: string(r.ItemID), Station: r.Station, Agent: r.Agent, Model: r.Model,
		Status: gen.RunStatus(r.Result.Status), Turns: r.Result.Turns, InputTokens: r.Result.InputTokens,
		OutputTokens: r.Result.OutputTokens, CostUsd: r.Result.CostUSD, Error: optional(r.Result.Error), StartedAt: r.StartedAt,
		DefinitionVersionId: optional(string(r.DefinitionVersionID)), DefinitionVersion: number,
	}

	if !r.EndedAt.IsZero() {
		run.EndedAt = &r.EndedAt
	}

	return run, nil
}

// number is the version's number, nil for a run without a version or one whose version is gone.
func (v *versionNumbers) number(ctx context.Context, id domain.DefinitionVersionID) (*int, error) {
	if id == "" {
		return nil, nil
	}

	if n, ok := v.known[id]; ok {
		return n, nil
	}

	var n *int

	version, err := v.s.versions.Version(ctx, id)

	switch {
	case errors.Is(err, ports.ErrNotFound):
	case err != nil:
		return nil, fmt.Errorf("version %s of a run: %w", id, err)
	default:
		n = &version.Number
	}

	if v.known == nil {
		v.known = map[domain.DefinitionVersionID]*int{}
	}

	v.known[id] = n

	return n, nil
}
