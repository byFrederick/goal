package api

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/jonah"
)

// The bake-off routes (ADR 0029): a bake-off is an eval run of kind bake-off, one agent of the
// current version on two to four models. The handlers resolve the factory and its current
// version, then leave starting, running again and applying to Jonah's eval loop and reading to
// the store.

// maxBakeOffList caps the bake-off list.
const maxBakeOffList = 20

// BakeOffVersions reads the parsed definition of a published version, the line a bake-off runs;
// *definitions.Cache satisfies it.
type BakeOffVersions interface {
	Version(ctx context.Context, id domain.DefinitionVersionID) (*config.Factory, error)
}

// errInvalidBakeOff is a bake-off the loop refuses to queue or apply (422): an agent the version
// does not have, bad arms or tries, an arm the bake-off does not have.
var errInvalidBakeOff = errors.New("invalid bake-off")

// bakeOffBudgetError is a bake-off refused over the learning budget, with its estimate, which the
// 409 answer carries.
type bakeOffBudgetError struct {
	estimate domain.EvalEstimate
	err      error
}

func (e *bakeOffBudgetError) Error() string { return e.err.Error() }
func (e *bakeOffBudgetError) Unwrap() error { return e.err }

func (s *server) ListBakeOffs(ctx context.Context, req gen.ListBakeOffsRequestObject) (gen.ListBakeOffsResponseObject, error) {
	rec, err := s.evalFactory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	list, err := s.evals.Runs.ListBakeOffs(ctx, rec.ID, maxBakeOffList)
	if err != nil {
		return nil, fmt.Errorf("list bake-offs: %w", err)
	}

	out := gen.ListBakeOffs200JSONResponse{BakeOffs: make([]gen.BakeOff, 0, len(list))}
	numbers := &versionNumbers{s: s}
	titles := map[domain.DefinitionVersionID]map[string]string{}

	for _, b := range list {
		if _, ok := titles[b.VersionID]; !ok {
			if titles[b.VersionID], err = s.judgeTitles(ctx, rec.ID, b.VersionID); err != nil {
				return nil, err
			}
		}

		o, err := bakeOffOut(ctx, numbers, b, titles[b.VersionID])
		if err != nil {
			return nil, err
		}

		out.BakeOffs = append(out.BakeOffs, o)
	}

	return out, nil
}

func (s *server) StartBakeOff(ctx context.Context, req gen.StartBakeOffRequestObject) (gen.StartBakeOffResponseObject, error) {
	rec, err := s.evalFactory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("start bake-off: the request has no actor")
	}

	content, err := s.bakeOffContent(ctx, rec)
	if err != nil {
		return nil, err
	}

	b := req.Body
	r := jonah.BakeOffRequest{Agent: b.Agent, Models: deref(b.Models), Trials: deref(b.Trials)}

	if b.Trials != nil && r.Trials == 0 { // 0 would ask for the default
		return nil, fmt.Errorf("%w: trials 0 is not 1 to %d", errInvalidBakeOff, domain.MaxBakeOffTries)
	}

	started, est, err := s.evals.Loop.StartBakeOff(ctx, content, r, by)
	if err != nil {
		return nil, bakeOffRefusal(err, est)
	}

	out, err := s.bakeOffStarted(ctx, rec.ID, started.ID, est)
	if err != nil {
		return nil, err
	}

	return gen.StartBakeOff202JSONResponse(out), nil
}

func (s *server) GetBakeOff(ctx context.Context, req gen.GetBakeOffRequestObject) (gen.GetBakeOffResponseObject, error) {
	rec, err := s.evalFactory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	b, err := s.bakeOff(ctx, rec.ID, req.BakeOff)
	if err != nil {
		return nil, err
	}

	out, err := s.bakeOffWithTitles(ctx, rec.ID, b)
	if err != nil {
		return nil, err
	}

	return gen.GetBakeOff200JSONResponse(out), nil
}

func (s *server) RerunBakeOff(ctx context.Context, req gen.RerunBakeOffRequestObject) (gen.RerunBakeOffResponseObject, error) {
	rec, err := s.evalFactory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("run bake-off again: the request has no actor")
	}

	if _, err := s.bakeOff(ctx, rec.ID, req.BakeOff); err != nil {
		return nil, err
	}

	content, err := s.bakeOffContent(ctx, rec)
	if err != nil {
		return nil, err
	}

	started, est, err := s.evals.Loop.RerunBakeOff(ctx, domain.EvalRunID(req.BakeOff), content, by)
	if err != nil {
		return nil, bakeOffRefusal(err, est)
	}

	out, err := s.bakeOffStarted(ctx, rec.ID, started.ID, est)
	if err != nil {
		return nil, err
	}

	return gen.RerunBakeOff202JSONResponse(out), nil
}

func (s *server) ApplyBakeOff(ctx context.Context, req gen.ApplyBakeOffRequestObject) (gen.ApplyBakeOffResponseObject, error) {
	rec, err := s.evalFactory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	if _, err := s.bakeOff(ctx, rec.ID, req.BakeOff); err != nil {
		return nil, err
	}

	_, err = s.evals.Loop.ApplyBakeOff(ctx, domain.EvalRunID(req.BakeOff), req.Body.Arm)
	if errors.Is(err, domain.ErrInvalid) {
		return nil, fmt.Errorf("%w: %w", errInvalidBakeOff, err)
	}

	if err != nil {
		return nil, err
	}

	v, err := s.defs.Draft(ctx, rec.ID)
	if err != nil {
		return nil, fmt.Errorf("read the draft bake-off %s applied to: %w", req.BakeOff, err)
	}

	return gen.ApplyBakeOff200JSONResponse(s.draftOut(v)), nil
}

// bakeOffRefusal is the answer to the loop's refusal err of a bake-off estimated at est: 409 with
// the estimate over the learning budget, 422 for a bake-off it cannot queue.
func bakeOffRefusal(err error, est domain.EvalEstimate) error {
	switch {
	case errors.Is(err, jonah.ErrLearningBudgetExhausted):
		return &bakeOffBudgetError{estimate: est, err: err}
	case errors.Is(err, domain.ErrInvalid):
		return fmt.Errorf("%w: %w", errInvalidBakeOff, err)
	default:
		return err
	}
}

// bakeOffContent is rec's current version as a bake-off runs it: its line and its eval cases.
func (s *server) bakeOffContent(ctx context.Context, rec ports.FactoryRecord) (jonah.BakeOffContent, error) {
	content, err := s.defs.EvalContent(ctx, rec.ID, authoring.VersionSelector{Kind: authoring.SelectCurrent})
	if err != nil {
		return jonah.BakeOffContent{}, err
	}

	f, err := s.evals.Versions.Version(ctx, content.Version.ID)
	if err != nil {
		return jonah.BakeOffContent{}, fmt.Errorf("read version %s of %s: %w", content.Version.ID, rec.ID, err)
	}

	line := f.Line()
	line.OrgID, line.VersionID, line.Number = content.Version.OrgID, content.Version.ID, content.Version.Number

	return jonah.BakeOffContent{Line: line, ContentSHA256: content.SHA256, Cases: content.Cases}, nil
}

// bakeOff is bake-off id of factory: errEvalRunNotFound when it is another factory's, an eval
// run of kind eval or none.
func (s *server) bakeOff(ctx context.Context, factory domain.FactoryID, id string) (domain.BakeOff, error) {
	b, err := s.evals.Runs.GetBakeOff(ctx, domain.EvalRunID(id))
	if errors.Is(err, ports.ErrNotFound) || err == nil && b.FactoryID != factory {
		return domain.BakeOff{}, fmt.Errorf("bake-off %q: %w", id, errEvalRunNotFound)
	}

	if err != nil {
		return domain.BakeOff{}, fmt.Errorf("read bake-off %s: %w", id, err)
	}

	return b, nil
}

// bakeOffStarted is the bake-off id just queued, read back with its results, and its estimate.
func (s *server) bakeOffStarted(ctx context.Context, factory domain.FactoryID, id domain.EvalRunID, est domain.EvalEstimate) (gen.BakeOffStarted, error) {
	b, err := s.bakeOff(ctx, factory, string(id))
	if err != nil {
		return gen.BakeOffStarted{}, err
	}

	out, err := s.bakeOffWithTitles(ctx, factory, b)
	if err != nil {
		return gen.BakeOffStarted{}, err
	}

	return gen.BakeOffStarted{BakeOff: out, Estimate: estimateOut(est)}, nil
}

// bakeOffWithTitles is b as the routes answer it, its judge named by its version's title.
func (s *server) bakeOffWithTitles(ctx context.Context, factory domain.FactoryID, b domain.BakeOff) (gen.BakeOff, error) {
	titles, err := s.judgeTitles(ctx, factory, b.VersionID)
	if err != nil {
		return gen.BakeOff{}, err
	}

	return bakeOffOut(ctx, &versionNumbers{s: s}, b, titles)
}

// bakeOffOut is b as the routes answer it. The tries passed per case come from its results, or,
// for a bake-off read without them (the list), from its arms' summaries.
func bakeOffOut(ctx context.Context, numbers *versionNumbers, b domain.BakeOff, titles map[string]string) (gen.BakeOff, error) {
	n, err := numbers.number(ctx, b.VersionID)
	if err != nil {
		return gen.BakeOff{}, err
	}

	out := gen.BakeOff{
		Id: string(b.ID), State: gen.BakeOffState(b.State), Agent: b.Agent, Station: b.Station, Version: deref(n),
		Trials: b.TriesPerCase, EstimateUsd: b.EstimateUSD, CostUsd: b.CostUSD, CreatedAt: b.CreatedAt,
		Arms: make([]gen.BakeOffArm, 0, len(b.Arms)),
	}

	if !b.FinishedAt.IsZero() {
		out.FinishedAt = &b.FinishedAt
	}

	for _, a := range b.Arms {
		out.Arms = append(out.Arms, gen.BakeOffArm{Arm: a.Arm, Model: a.Model, IsCurrent: a.Current, Summary: armSummaryOut(a.Summary, titles)})
	}

	out.PerCase = bakeOffCases(b)
	out.Cases = len(out.PerCase)

	if arm, ok := domain.RecommendBakeOffArm(b); ok {
		out.RecommendedArm = &arm
	}

	return out, nil
}

// bakeOffCases are b's cases by name, each with the tries passed on every arm.
func bakeOffCases(b domain.BakeOff) []gen.BakeOffCase {
	passed := map[string]map[int]int{}

	if len(b.Results) > 0 {
		for _, r := range b.Results {
			if passed[r.Case] == nil {
				passed[r.Case] = map[int]int{}
			}

			if r.State == domain.EvalPass {
				passed[r.Case][r.Arm]++
			}
		}
	} else {
		for _, a := range b.Arms {
			if a.Summary == nil {
				continue
			}

			for c, n := range a.Summary.PerCase {
				if passed[c] == nil {
					passed[c] = map[int]int{}
				}

				passed[c][a.Arm] = n
			}
		}
	}

	out := make([]gen.BakeOffCase, 0, len(passed))

	for _, c := range slices.Sorted(maps.Keys(passed)) {
		bc := gen.BakeOffCase{Case: c, Arms: make([]gen.BakeOffCaseArm, 0, len(b.Arms))}
		for _, a := range b.Arms {
			bc.Arms = append(bc.Arms, gen.BakeOffCaseArm{Arm: a.Arm, Passed: passed[c][a.Arm]})
		}

		out = append(out, bc)
	}

	return out
}

// armSummaryOut is an arm's row of the report, nil until it is recorded.
func armSummaryOut(s *domain.ArmSummary, titles map[string]string) *gen.BakeOffArmSummary {
	if s == nil {
		return nil
	}

	out := &gen.BakeOffArmSummary{
		Tries: s.Tries, Passed: s.Passed, Errors: s.Errors, PassRate: s.PassRate(), CostPerItemUsd: s.CostPerItemUSD,
		TimePerItemSeconds: s.TimePerItem.Seconds(), Turns: s.MeanTurns,
	}

	if s.Judge != "" && s.JudgedShare != nil {
		out.Judged = &gen.BakeOffJudged{Judge: s.Judge, Share: *s.JudgedShare}
		if t, ok := titles[s.Judge]; ok {
			out.Judged.Title = &t
		}
	}

	return out
}
