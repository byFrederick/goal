package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// CreateBakeOff implements ports.Evals.
func (e *Evals) CreateBakeOff(ctx context.Context, n domain.NewBakeOff, by domain.Actor) (domain.BakeOff, error) {
	if err := n.Check(); err != nil {
		return domain.BakeOff{}, fmt.Errorf("create bake-off: %w", err)
	}

	factory, err := parseID("factory", string(n.FactoryID))
	if err != nil {
		return domain.BakeOff{}, fmt.Errorf("create bake-off: %w", err)
	}

	version, err := parseID("version", string(n.VersionID))
	if err != nil {
		return domain.BakeOff{}, fmt.Errorf("create bake-off: %w", err)
	}

	id, err := newID()
	if err != nil {
		return domain.BakeOff{}, err
	}

	arms := db.InsertEvalRunArmsParams{EvalRunID: id}
	for _, a := range n.Arms {
		arms.Models = append(arms.Models, a.Model)
		if a.Current {
			arms.CurrentModel = a.Model
		}
	}

	tries := int16(n.TriesPerCase) //nolint:gosec // Check keeps it 1 to 10
	cases := evalResultsParams(id, n.EvalCases())

	var out domain.BakeOff

	err = e.s.inTx(ctx, func(q *db.Queries) error {
		_, err := q.InsertEvalRun(ctx, db.InsertEvalRunParams{
			ID: id, Kind: string(domain.EvalKindBakeOff), FactoryID: factory, VersionID: &version, ContentSha256: string(n.ContentSHA256),
			Trigger: string(domain.EvalManual), EstimateUsd: n.EstimateUSD, CreatedBy: by.Ref(),
			Station: &n.Station, Agent: &n.Agent, TriesPerCase: &tries,
		})
		if isForeignKeyViolation(err) {
			return fmt.Errorf("factory %s or version %q of it: %w", n.FactoryID, n.VersionID, ports.ErrNotFound)
		}

		if err != nil {
			return err
		}

		if err := q.InsertEvalRunArms(ctx, arms); err != nil {
			return err
		}

		for arm := range len(n.Arms) {
			cases.Arm = int16(arm + 1)
			if err := q.InsertEvalResults(ctx, cases); err != nil {
				return err
			}
		}

		out, err = bakeOff(ctx, q, id)

		return err
	})
	if err != nil {
		return domain.BakeOff{}, fmt.Errorf("create bake-off: %w", ruleError(err, ""))
	}

	return out, nil
}

// ListBakeOffs implements ports.Evals.
func (e *Evals) ListBakeOffs(ctx context.Context, factory domain.FactoryID, limit int) ([]domain.BakeOff, error) {
	if limit < 1 {
		return nil, fmt.Errorf("bake-offs of factory %s: limit %d, want at least 1: %w", factory, limit, domain.ErrInvalid)
	}

	f, err := parseID("factory", string(factory))
	if err != nil {
		return nil, err
	}

	var out []domain.BakeOff

	err = e.s.read(ctx, func(q *db.Queries) error {
		runs, err := q.BakeOffs(ctx, db.BakeOffsParams{FactoryID: f, MaxRuns: int32(min(limit, math.MaxInt32))}) //nolint:gosec // bounded by min
		if err != nil {
			return err
		}

		ids := make([]uuid.UUID, 0, len(runs))
		for _, r := range runs {
			ids = append(ids, r.ID)
		}

		arms, err := q.EvalRunArms(ctx, ids)
		if err != nil {
			return err
		}

		byRun := make(map[uuid.UUID][]db.EvalRunArm, len(runs))
		for _, a := range arms {
			byRun[a.EvalRunID] = append(byRun[a.EvalRunID], a)
		}

		out = make([]domain.BakeOff, 0, len(runs))

		for _, r := range runs {
			b, err := bakeOffFrom(r, byRun[r.ID])
			if err != nil {
				return err
			}

			out = append(out, b)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("bake-offs of factory %s: %w", factory, err)
	}

	return out, nil
}

// GetBakeOff implements ports.Evals.
func (e *Evals) GetBakeOff(ctx context.Context, id domain.EvalRunID) (domain.BakeOff, error) {
	run, err := parseID("bake-off", string(id))
	if err != nil {
		return domain.BakeOff{}, err
	}

	var out domain.BakeOff

	err = e.s.read(ctx, func(q *db.Queries) error {
		var err error

		out, err = bakeOff(ctx, q, run)

		return err
	})
	if err != nil {
		return domain.BakeOff{}, fmt.Errorf("bake-off %s: %w", id, err)
	}

	return out, nil
}

// RecordArmSummary implements ports.Evals.
func (e *Evals) RecordArmSummary(ctx context.Context, id domain.EvalRunID, arm int, s domain.ArmSummary) error {
	key := fmt.Sprintf("bake-off %s arm %d", id, arm)

	if err := s.Check(); err != nil {
		return fmt.Errorf("record summary of %s: %w", key, err)
	}

	if arm < 1 || arm > domain.MaxBakeOffArms {
		return fmt.Errorf("record summary of %s: %w", key, ports.ErrNotFound)
	}

	run, err := parseID("bake-off", string(id))
	if err != nil {
		return fmt.Errorf("record summary of %s: %w", key, err)
	}

	perCase := s.PerCase
	if perCase == nil {
		perCase = map[string]int{}
	}

	cases, err := json.Marshal(perCase)
	if err != nil {
		return fmt.Errorf("record summary of %s: per case: %w", key, err)
	}

	//nolint:gosec // Check keeps the counts non-negative; a bake-off's tries are at most 4 × cases × 10
	p := db.RecordEvalRunArmSummaryParams{
		Tries: new(int32(s.Tries)), Passed: new(int32(s.Passed)), Errors: new(int32(s.Errors)),
		CostPerItemUsd: &s.CostPerItemUSD, MsPerItem: new(s.TimePerItem.Milliseconds()), MeanTurns: &s.MeanTurns,
		Judge: optional(s.Judge), JudgedShare: s.JudgedShare, PerCase: cases, EvalRunID: run, Arm: int16(arm),
	}

	err = e.s.inTx(ctx, func(q *db.Queries) error {
		n, err := q.RecordEvalRunArmSummary(ctx, p)
		if err != nil {
			return err
		}

		if n == 0 {
			return ports.ErrNotFound
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("record summary of %s: %w", key, ruleError(err, ""))
	}

	return nil
}

// bakeOff reads the bake-off run with its arms and results; ErrNotFound when there is no such run
// or it is not a bake-off.
func bakeOff(ctx context.Context, q *db.Queries, run uuid.UUID) (domain.BakeOff, error) {
	r, err := q.EvalRun(ctx, run)
	if notFound(err) || (err == nil && r.Kind != string(domain.EvalKindBakeOff)) {
		return domain.BakeOff{}, ports.ErrNotFound
	}

	if err != nil {
		return domain.BakeOff{}, err
	}

	arms, err := q.EvalRunArms(ctx, []uuid.UUID{run})
	if err != nil {
		return domain.BakeOff{}, err
	}

	out, err := bakeOffFrom(r, arms)
	if err != nil {
		return domain.BakeOff{}, err
	}

	rows, err := q.EvalResults(ctx, run)
	if err != nil {
		return domain.BakeOff{}, err
	}

	out.Results, err = convertAll(rows, evalResultFrom)

	return out, err
}

func bakeOffFrom(r db.EvalRun, arms []db.EvalRunArm) (domain.BakeOff, error) {
	run, err := evalRunFrom(r)
	if err != nil {
		return domain.BakeOff{}, err
	}

	out := domain.BakeOff{EvalRun: run, Station: deref(r.Station), Agent: deref(r.Agent)}
	if r.TriesPerCase != nil {
		out.TriesPerCase = int(*r.TriesPerCase)
	}

	out.Arms, err = convertAll(arms, bakeOffArmFrom)

	return out, err
}

func bakeOffArmFrom(a db.EvalRunArm) (domain.BakeOffArm, error) {
	out := domain.BakeOffArm{Arm: int(a.Arm), Model: a.Model, Current: a.IsCurrent}
	if a.SummarizedAt == nil {
		return out, nil
	}

	s := domain.ArmSummary{
		Tries: int(orZero(a.Tries)), Passed: int(orZero(a.Passed)), Errors: int(orZero(a.Errors)),
		CostPerItemUSD: orZero(a.CostPerItemUsd), TimePerItem: time.Duration(orZero(a.MsPerItem)) * time.Millisecond,
		MeanTurns: orZero(a.MeanTurns), Judge: deref(a.Judge), JudgedShare: a.JudgedShare,
	}

	if err := json.Unmarshal(a.PerCase, &s.PerCase); err != nil {
		return domain.BakeOffArm{}, fmt.Errorf("bake-off %s arm %d per case: %w", a.EvalRunID, a.Arm, err)
	}

	out.Summary = &s

	return out, nil
}

// orZero is *p, or T's zero value when p is nil.
func orZero[T any](p *T) T {
	if p == nil {
		var zero T

		return zero
	}

	return *p
}
