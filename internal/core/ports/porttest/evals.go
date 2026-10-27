package porttest

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.Evals = (*Evals)(nil)

// Evals keeps eval runs and their tries in memory, by organization, as the PostgreSQL store does:
// claims take the oldest run's tries in case and try order, and records are fenced by the claim's
// lease. It does not check that a factory or version exists.
type Evals struct {
	mu   sync.Mutex
	now  func() time.Time
	runs []*evalRun
	n    int
}

type evalRun struct {
	org domain.OrgID
	run domain.EvalRun
}

// NewEvals returns no runs; now stamps each new run's CreatedAt.
func NewEvals(now func() time.Time) *Evals { return &Evals{now: now} }

// find is org's run id. Callers hold e.mu.
func (e *Evals) find(org domain.OrgID, id domain.EvalRunID) (*evalRun, bool) {
	for _, r := range e.runs {
		if r.org == org && r.run.ID == id {
			return r, true
		}
	}

	return nil, false
}

// CreateRun implements ports.Evals.
func (e *Evals) CreateRun(ctx context.Context, n domain.NewEvalRun, by domain.Actor) (domain.EvalRun, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.EvalRun{}, err
	}

	if err := n.Check(); err != nil {
		return domain.EvalRun{}, fmt.Errorf("create eval run: %w", err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	e.n++

	r := domain.EvalRun{
		ID: domain.EvalRunID(fmt.Sprintf("0192f1c0-0000-7000-9000-%012d", e.n)), FactoryID: n.FactoryID, VersionID: n.VersionID,
		ContentSHA256: n.ContentSHA256, Trigger: n.Trigger, Case: n.Case, State: domain.EvalRunQueued, EstimateUSD: n.EstimateUSD,
		CreatedBy: by, CreatedAt: e.now(),
	}

	for _, c := range n.Cases {
		for try := 1; try <= c.Tries; try++ {
			r.Results = append(r.Results, domain.EvalResult{EvalRunID: r.ID, Case: c.Name, CaseSHA256: c.SHA256, Try: try, State: domain.EvalPending})
		}
	}

	slices.SortFunc(r.Results, func(a, b domain.EvalResult) int {
		return cmp.Or(cmp.Compare(a.Case, b.Case), cmp.Compare(a.Try, b.Try))
	})

	e.runs = append(e.runs, &evalRun{org: org, run: r})

	return cloneEvalRun(r), nil
}

// ClaimResult implements ports.Evals.
func (e *Evals) ClaimResult(ctx context.Context, now time.Time, lease time.Duration) (domain.EvalResult, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.EvalResult{}, err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	for _, r := range e.runs {
		if r.org != org || r.run.State.Final() {
			continue
		}

		for i := range r.run.Results {
			res := &r.run.Results[i]
			if res.State != domain.EvalPending && (res.State != domain.EvalRunning || res.LeaseUntil.After(now)) {
				continue
			}

			res.State, res.LeaseUntil = domain.EvalRunning, now.Add(lease)
			r.run.State = domain.EvalRunRunning

			return cloneEvalResult(*res), nil
		}
	}

	return domain.EvalResult{}, fmt.Errorf("claim eval result: %w", ports.ErrNotFound)
}

// RecordResult implements ports.Evals.
func (e *Evals) RecordResult(ctx context.Context, claimed domain.EvalResult, o domain.EvalOutcome) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}

	key := fmt.Sprintf("eval run %s case %s try %d", claimed.EvalRunID, claimed.Case, claimed.Try)

	if err := o.Check(); err != nil {
		return fmt.Errorf("record %s: %w", key, err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	r, ok := e.find(org, claimed.EvalRunID)
	if !ok {
		return fmt.Errorf("record %s: %w", key, ports.ErrLeaseLost)
	}

	for i := range r.run.Results {
		res := &r.run.Results[i]
		if res.Case != claimed.Case || res.Try != claimed.Try {
			continue
		}

		if res.State != domain.EvalRunning || !res.LeaseUntil.Equal(claimed.LeaseUntil) {
			return fmt.Errorf("record %s: %w", key, ports.ErrLeaseLost)
		}

		res.State, res.LeaseUntil, res.RunID, res.CheckedBy = o.State, time.Time{}, o.RunID, o.CheckedBy
		res.Detail, res.CostUSD, res.Turns = maps.Clone(o.Detail), o.CostUSD, o.Turns
		r.run.CostUSD += o.CostUSD

		return nil
	}

	return fmt.Errorf("record %s: %w", key, ports.ErrLeaseLost)
}

// FinishRun implements ports.Evals.
func (e *Evals) FinishRun(ctx context.Context, id domain.EvalRunID, state domain.EvalRunState, at time.Time) (domain.EvalRun, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.EvalRun{}, err
	}

	if !state.Final() {
		return domain.EvalRun{}, fmt.Errorf("finish eval run %s: state %q is not done, failed or cancelled: %w", id, state, domain.ErrInvalid)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	r, ok := e.find(org, id)
	if !ok {
		return domain.EvalRun{}, fmt.Errorf("finish eval run %s: %w", id, ports.ErrNotFound)
	}

	if r.run.State.Final() {
		return domain.EvalRun{}, fmt.Errorf("finish eval run %s: already %s: %w", id, r.run.State, ports.ErrConflict)
	}

	r.run.State, r.run.FinishedAt = state, at

	out := cloneEvalRun(r.run)
	out.Results = nil

	return out, nil
}

// GetRun implements ports.Evals.
func (e *Evals) GetRun(ctx context.Context, id domain.EvalRunID) (domain.EvalRun, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.EvalRun{}, err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	r, ok := e.find(org, id)
	if !ok {
		return domain.EvalRun{}, fmt.Errorf("eval run %s: %w", id, ports.ErrNotFound)
	}

	return cloneEvalRun(r.run), nil
}

// CaseHistory implements ports.Evals.
func (e *Evals) CaseHistory(ctx context.Context, factory domain.FactoryID) ([]domain.EvalCaseHistory, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	byCase := map[string]*domain.EvalCaseHistory{}

	// Newest run first, and a run's tries from the last.
	for _, r := range slices.Backward(e.runs) {
		if r.org != org || r.run.FactoryID != factory {
			continue
		}

		for _, res := range slices.Backward(r.run.Results) {
			if !res.State.Finished() {
				continue
			}

			h, ok := byCase[res.Case]
			if !ok {
				h = &domain.EvalCaseHistory{Case: res.Case, LatestRunID: r.run.ID}
				byCase[res.Case] = h
			}

			if h.LatestRunID == r.run.ID {
				h.LatestCostUSD += res.CostUSD
			}

			if len(h.Tries) < domain.EvalHistoryTries {
				h.Tries = append(h.Tries, cloneEvalResult(res))
			}
		}
	}

	out := make([]domain.EvalCaseHistory, 0, len(byCase))
	for _, name := range slices.Sorted(maps.Keys(byCase)) {
		out = append(out, *byCase[name])
	}

	return out, nil
}

// LatestRunForContent implements ports.Evals.
func (e *Evals) LatestRunForContent(ctx context.Context, factory domain.FactoryID, sha256 domain.BlobHash) (domain.EvalRun, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.EvalRun{}, err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	for _, r := range slices.Backward(e.runs) {
		if r.org == org && r.run.FactoryID == factory && r.run.ContentSHA256 == sha256 {
			out := cloneEvalRun(r.run)
			out.Results = nil

			return out, nil
		}
	}

	return domain.EvalRun{}, fmt.Errorf("latest eval run of content %s: %w", sha256, ports.ErrNotFound)
}

func cloneEvalRun(r domain.EvalRun) domain.EvalRun {
	r.Results = slices.Clone(r.Results)
	for i := range r.Results {
		r.Results[i] = cloneEvalResult(r.Results[i])
	}

	return r
}

func cloneEvalResult(r domain.EvalResult) domain.EvalResult {
	r.Detail = maps.Clone(r.Detail)

	return r
}
