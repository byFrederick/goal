package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.Evals = (*Evals)(nil)

// Evals implements ports.Evals over 00068's eval_runs and eval_results, as goal_app under
// row-level security: every statement sees the organization in ctx. A method that changes a try
// and its run locks the try first, so claims, records and finishes never deadlock.
type Evals struct{ s *Store }

// NewEvals returns the eval runs on s's pool.
func NewEvals(s *Store) *Evals { return &Evals{s: s} }

// CreateRun implements ports.Evals.
func (e *Evals) CreateRun(ctx context.Context, n domain.NewEvalRun, by domain.Actor) (domain.EvalRun, error) {
	if err := n.Check(); err != nil {
		return domain.EvalRun{}, fmt.Errorf("create eval run: %w", err)
	}

	factory, err := parseID("factory", string(n.FactoryID))
	if err != nil {
		return domain.EvalRun{}, fmt.Errorf("create eval run: %w", err)
	}

	var version *uuid.UUID

	if n.VersionID != "" {
		v, err := parseID("version", string(n.VersionID))
		if err != nil {
			return domain.EvalRun{}, fmt.Errorf("create eval run: %w", err)
		}

		version = &v
	}

	id, err := newID()
	if err != nil {
		return domain.EvalRun{}, err
	}

	cases := db.InsertEvalResultsParams{EvalRunID: id}
	for _, c := range n.Cases {
		cases.CaseNames = append(cases.CaseNames, c.Name)
		cases.CaseSha256s = append(cases.CaseSha256s, string(c.SHA256))
		cases.Tries = append(cases.Tries, int32(c.Tries)) //nolint:gosec // Check keeps tries positive; a case file's tries are small
	}

	var out domain.EvalRun

	err = e.s.inTx(ctx, func(q *db.Queries) error {
		r, err := q.InsertEvalRun(ctx, db.InsertEvalRunParams{
			ID: id, FactoryID: factory, VersionID: version, ContentSha256: string(n.ContentSHA256), Trigger: string(n.Trigger),
			Cases: optional(n.Case), EstimateUsd: n.EstimateUSD, CreatedBy: by.Ref(),
		})
		if isForeignKeyViolation(err) {
			return fmt.Errorf("factory %s or version %q of it: %w", n.FactoryID, n.VersionID, ports.ErrNotFound)
		}

		if err != nil {
			return err
		}

		if err := q.InsertEvalResults(ctx, cases); err != nil {
			return err
		}

		if out, err = evalRunFrom(r); err != nil {
			return err
		}

		rows, err := q.EvalResults(ctx, id)
		if err != nil {
			return err
		}

		out.Results, err = convertAll(rows, evalResultFrom)

		return err
	})
	if err != nil {
		return domain.EvalRun{}, fmt.Errorf("create eval run: %w", ruleError(err, ""))
	}

	return out, nil
}

// ClaimResult implements ports.Evals.
func (e *Evals) ClaimResult(ctx context.Context, now time.Time, lease time.Duration) (domain.EvalResult, error) {
	var out domain.EvalResult

	err := e.s.inTx(ctx, func(q *db.Queries) error {
		r, err := q.ClaimEvalResult(ctx, db.ClaimEvalResultParams{Now: now, LeaseUntil: now.Add(lease)})
		if notFound(err) {
			return ports.ErrNotFound
		}

		if err != nil {
			return err
		}

		if err := q.StartEvalRun(ctx, r.EvalRunID); err != nil {
			return err
		}

		out, err = evalResultFrom(r)

		return err
	})
	if err != nil {
		return domain.EvalResult{}, fmt.Errorf("claim eval result: %w", err)
	}

	return out, nil
}

// RecordResult implements ports.Evals.
func (e *Evals) RecordResult(ctx context.Context, claimed domain.EvalResult, o domain.EvalOutcome) error {
	key := fmt.Sprintf("eval run %s case %s try %d", claimed.EvalRunID, claimed.Case, claimed.Try)

	if err := o.Check(); err != nil {
		return fmt.Errorf("record %s: %w", key, err)
	}

	detail, err := evalDetail(o.Detail)
	if err != nil {
		return fmt.Errorf("record %s: %w", key, err)
	}

	run, err := parseID("eval run", string(claimed.EvalRunID))
	if err != nil {
		return fmt.Errorf("record %s: %w", key, err)
	}

	var agentRun *uuid.UUID

	if o.RunID != "" {
		r, err := parseID("run", string(o.RunID))
		if err != nil {
			return fmt.Errorf("record %s: %w", key, err)
		}

		agentRun = &r
	}

	err = e.s.inTx(ctx, func(q *db.Queries) error {
		n, err := q.RecordEvalResult(ctx, db.RecordEvalResultParams{
			State: string(o.State), RunID: agentRun, CheckedBy: o.CheckedBy, Detail: detail, CostUsd: o.CostUSD,
			Turns:     int32(o.Turns),                                                                       //nolint:gosec // Check keeps turns non-negative; a try's turns are small
			EvalRunID: run, CaseName: claimed.Case, Try: int32(claimed.Try), LeaseUntil: claimed.LeaseUntil, //nolint:gosec // a claimed try
		})
		if isForeignKeyViolation(err) {
			return fmt.Errorf("run %s: %w", o.RunID, ports.ErrNotFound)
		}

		if err != nil {
			return err
		}

		if n == 0 {
			return ports.ErrLeaseLost
		}

		return q.AddEvalRunCost(ctx, db.AddEvalRunCostParams{CostUsd: o.CostUSD, ID: run})
	})
	if err != nil {
		return fmt.Errorf("record %s: %w", key, ruleError(err, ""))
	}

	return nil
}

// FinishRun implements ports.Evals.
func (e *Evals) FinishRun(ctx context.Context, id domain.EvalRunID, state domain.EvalRunState, at time.Time) (domain.EvalRun, error) {
	if !state.Final() {
		return domain.EvalRun{}, fmt.Errorf("finish eval run %s: state %q is not done, failed or cancelled: %w", id, state, domain.ErrInvalid)
	}

	run, err := parseID("eval run", string(id))
	if err != nil {
		return domain.EvalRun{}, fmt.Errorf("finish eval run: %w", err)
	}

	var out domain.EvalRun

	err = e.s.inTx(ctx, func(q *db.Queries) error {
		cur, err := q.LockEvalRun(ctx, run)
		if notFound(err) {
			return ports.ErrNotFound
		}

		if err != nil {
			return err
		}

		if domain.EvalRunState(cur.State).Final() {
			return fmt.Errorf("already %s: %w", cur.State, ports.ErrConflict)
		}

		r, err := q.FinishEvalRun(ctx, db.FinishEvalRunParams{State: string(state), FinishedAt: &at, ID: run})
		if err != nil {
			return err
		}

		out, err = evalRunFrom(r)

		return err
	})
	if err != nil {
		return domain.EvalRun{}, fmt.Errorf("finish eval run %s: %w", id, err)
	}

	return out, nil
}

// GetRun implements ports.Evals.
func (e *Evals) GetRun(ctx context.Context, id domain.EvalRunID) (domain.EvalRun, error) {
	run, err := parseID("eval run", string(id))
	if err != nil {
		return domain.EvalRun{}, err
	}

	var out domain.EvalRun

	err = e.s.read(ctx, func(q *db.Queries) error {
		r, err := q.EvalRun(ctx, run)
		if notFound(err) {
			return ports.ErrNotFound
		}

		if err != nil {
			return err
		}

		if out, err = evalRunFrom(r); err != nil {
			return err
		}

		rows, err := q.EvalResults(ctx, run)
		if err != nil {
			return err
		}

		out.Results, err = convertAll(rows, evalResultFrom)

		return err
	})
	if err != nil {
		return domain.EvalRun{}, fmt.Errorf("eval run %s: %w", id, err)
	}

	return out, nil
}

// CaseHistory implements ports.Evals.
func (e *Evals) CaseHistory(ctx context.Context, factory domain.FactoryID) ([]domain.EvalCaseHistory, error) {
	f, err := parseID("factory", string(factory))
	if err != nil {
		return nil, err
	}

	var (
		tries []db.EvalCaseTriesRow
		costs []db.EvalCaseLatestCostsRow
	)

	err = e.s.read(ctx, func(q *db.Queries) error {
		var err error

		if tries, err = q.EvalCaseTries(ctx, db.EvalCaseTriesParams{FactoryID: f, Tries: domain.EvalHistoryTries}); err != nil {
			return err
		}

		costs, err = q.EvalCaseLatestCosts(ctx, f)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("eval case history of factory %s: %w", factory, err)
	}

	// Both queries list the same cases by name: those with a finished try.
	out := make([]domain.EvalCaseHistory, 0, len(costs))
	for _, c := range costs {
		out = append(out, domain.EvalCaseHistory{Case: c.CaseName, LatestRunID: domain.EvalRunID(c.EvalRunID.String()), LatestCostUSD: c.CostUsd})
	}

	i := 0

	for _, t := range tries {
		for i < len(out) && out[i].Case != t.CaseName {
			i++
		}

		if i == len(out) {
			return nil, fmt.Errorf("eval case history of factory %s: case %s has tries but no cost", factory, t.CaseName)
		}

		r, err := evalResultFrom(db.EvalResult{
			EvalRunID: t.EvalRunID, CaseName: t.CaseName, CaseSha256: t.CaseSha256, Try: t.Try, State: t.State,
			LeaseUntil: t.LeaseUntil, RunID: t.RunID, CheckedBy: t.CheckedBy, Detail: t.Detail, CostUsd: t.CostUsd, Turns: t.Turns,
		})
		if err != nil {
			return nil, err
		}

		out[i].Tries = append(out[i].Tries, r)
	}

	return out, nil
}

// LatestRunForContent implements ports.Evals.
func (e *Evals) LatestRunForContent(ctx context.Context, factory domain.FactoryID, sha256 domain.BlobHash) (domain.EvalRun, error) {
	f, err := parseID("factory", string(factory))
	if err != nil {
		return domain.EvalRun{}, err
	}

	r, err := fetch(ctx, e.s.read, func(q *db.Queries) (db.EvalRun, error) {
		return q.LatestEvalRunForContent(ctx, db.LatestEvalRunForContentParams{FactoryID: f, ContentSha256: string(sha256)})
	})

	switch {
	case notFound(err):
		return domain.EvalRun{}, fmt.Errorf("eval run of factory %s on %s: %w", factory, sha256, ports.ErrNotFound)
	case err != nil:
		return domain.EvalRun{}, fmt.Errorf("eval run of factory %s on %s: %w", factory, sha256, err)
	default:
		return evalRunFrom(r)
	}
}

// evalDetail encodes a try's detail as a JSON object of at most domain.MaxEvalDetail bytes.
func evalDetail(d map[string]any) ([]byte, error) {
	if d == nil {
		return []byte("{}"), nil
	}

	b, err := json.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("detail: %w", err)
	}

	if len(b) > domain.MaxEvalDetail {
		return nil, fmt.Errorf("detail is %d bytes, over %d: %w", len(b), domain.MaxEvalDetail, domain.ErrInvalid)
	}

	return b, nil
}

func evalRunFrom(r db.EvalRun) (domain.EvalRun, error) {
	by, err := domain.ActorFromRef(r.CreatedBy)
	if err != nil {
		return domain.EvalRun{}, fmt.Errorf("eval run %s: %w", r.ID, err)
	}

	out := domain.EvalRun{
		ID: domain.EvalRunID(r.ID.String()), FactoryID: domain.FactoryID(r.FactoryID.String()),
		ContentSHA256: domain.BlobHash(r.ContentSha256), Trigger: domain.EvalTrigger(r.Trigger), Case: deref(r.Cases),
		State: domain.EvalRunState(r.State), EstimateUSD: r.EstimateUsd, CostUSD: r.CostUsd, CreatedBy: by,
		CreatedAt: r.CreatedAt.UTC(),
	}

	if r.VersionID != nil {
		out.VersionID = domain.DefinitionVersionID(r.VersionID.String())
	}

	if r.FinishedAt != nil {
		out.FinishedAt = r.FinishedAt.UTC()
	}

	return out, nil
}

func evalResultFrom(r db.EvalResult) (domain.EvalResult, error) {
	out := domain.EvalResult{
		EvalRunID: domain.EvalRunID(r.EvalRunID.String()), Case: r.CaseName, CaseSHA256: domain.BlobHash(r.CaseSha256),
		Try: int(r.Try), State: domain.EvalResultState(r.State), CheckedBy: r.CheckedBy, CostUSD: r.CostUsd, Turns: int(r.Turns),
	}

	if r.LeaseUntil != nil {
		out.LeaseUntil = r.LeaseUntil.UTC()
	}

	if r.RunID != nil {
		out.RunID = domain.RunID(r.RunID.String())
	}

	if err := json.Unmarshal(r.Detail, &out.Detail); err != nil {
		return domain.EvalResult{}, fmt.Errorf("eval run %s case %s try %d detail: %w", r.EvalRunID, r.CaseName, r.Try, err)
	}

	return out, nil
}
