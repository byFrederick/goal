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
// claims take the oldest run's tries in case, try and arm order, and records are fenced by the
// claim's lease. It does not check that a factory or version exists.
type Evals struct {
	mu   sync.Mutex
	now  func() time.Time
	runs []*evalRun
	n    int
}

type evalRun struct {
	org domain.OrgID
	run domain.EvalRun
	// bake is the bake-off's agent and arms, nil for a run of kind eval.
	bake *domain.BakeOff
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

	r := e.newRun(domain.EvalRun{
		Kind: domain.EvalKindEval, FactoryID: n.FactoryID, VersionID: n.VersionID, ContentSHA256: n.ContentSHA256,
		Trigger: n.Trigger, Case: n.Case, EstimateUSD: n.EstimateUSD, CreatedBy: by,
	}, n.Cases, []int{0})

	e.runs = append(e.runs, &evalRun{org: org, run: r})

	return cloneEvalRun(r), nil
}

// newRun is r queued with a new id, created now, with one pending try per arm, case and try, by
// arm, case and try as GetRun lists them. Callers hold e.mu.
func (e *Evals) newRun(r domain.EvalRun, cases []domain.EvalCase, arms []int) domain.EvalRun {
	e.n++

	r.ID, r.State, r.CreatedAt = domain.EvalRunID(fmt.Sprintf("0192f1c0-0000-7000-9000-%012d", e.n)), domain.EvalRunQueued, e.now()

	for _, arm := range arms {
		for _, c := range cases {
			for try := 1; try <= c.Tries; try++ {
				r.Results = append(r.Results, domain.EvalResult{
					EvalRunID: r.ID, Arm: arm, Case: c.Name, CaseSHA256: c.SHA256, Try: try, State: domain.EvalPending,
				})
			}
		}
	}

	slices.SortFunc(r.Results, func(a, b domain.EvalResult) int {
		return cmp.Or(cmp.Compare(a.Arm, b.Arm), cmp.Compare(a.Case, b.Case), cmp.Compare(a.Try, b.Try))
	})

	return r
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

		// The first claimable try in case, try and arm order.
		var next *domain.EvalResult

		for i := range r.run.Results {
			res := &r.run.Results[i]
			if res.State != domain.EvalPending && (res.State != domain.EvalRunning || res.LeaseUntil.After(now)) {
				continue
			}

			if next == nil || cmp.Or(cmp.Compare(res.Case, next.Case), cmp.Compare(res.Try, next.Try), cmp.Compare(res.Arm, next.Arm)) < 0 {
				next = res
			}
		}

		if next != nil {
			next.State, next.LeaseUntil = domain.EvalRunning, now.Add(lease)
			r.run.State = domain.EvalRunRunning

			return cloneEvalResult(*next), nil
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

	key := fmt.Sprintf("eval run %s arm %d case %s try %d", claimed.EvalRunID, claimed.Arm, claimed.Case, claimed.Try)

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
		if res.Arm != claimed.Arm || res.Case != claimed.Case || res.Try != claimed.Try {
			continue
		}

		if res.State != domain.EvalRunning || !res.LeaseUntil.Equal(claimed.LeaseUntil) {
			return fmt.Errorf("record %s: %w", key, ports.ErrLeaseLost)
		}

		res.State, res.LeaseUntil, res.RunID, res.CheckedBy = o.State, time.Time{}, o.RunID, o.CheckedBy
		res.Detail, res.CostUSD, res.Turns, res.Duration = maps.Clone(o.Detail), o.CostUSD, o.Turns, o.Duration.Truncate(time.Millisecond)
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
		if r.org != org || r.run.FactoryID != factory || r.run.Kind != domain.EvalKindEval {
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
		if r.org == org && r.run.FactoryID == factory && r.run.ContentSHA256 == sha256 && r.run.Kind == domain.EvalKindEval {
			out := cloneEvalRun(r.run)
			out.Results = nil

			return out, nil
		}
	}

	return domain.EvalRun{}, fmt.Errorf("latest eval run of content %s: %w", sha256, ports.ErrNotFound)
}

// CreateBakeOff implements ports.Evals.
func (e *Evals) CreateBakeOff(ctx context.Context, n domain.NewBakeOff, by domain.Actor) (domain.BakeOff, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.BakeOff{}, err
	}

	if err := n.Check(); err != nil {
		return domain.BakeOff{}, fmt.Errorf("create bake-off: %w", err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	b := &domain.BakeOff{Station: n.Station, Agent: n.Agent, TriesPerCase: n.TriesPerCase}

	numbers := make([]int, 0, len(n.Arms))
	for i, a := range n.Arms {
		b.Arms = append(b.Arms, domain.BakeOffArm{Arm: i + 1, Model: a.Model, Current: a.Current})
		numbers = append(numbers, i+1)
	}

	r := e.newRun(domain.EvalRun{
		Kind: domain.EvalKindBakeOff, FactoryID: n.FactoryID, VersionID: n.VersionID, ContentSHA256: n.ContentSHA256,
		Trigger: domain.EvalManual, EstimateUSD: n.EstimateUSD, CreatedBy: by,
	}, n.EvalCases(), numbers)

	run := &evalRun{org: org, run: r, bake: b}
	e.runs = append(e.runs, run)

	return run.bakeOff(true), nil
}

// ListBakeOffs implements ports.Evals.
func (e *Evals) ListBakeOffs(ctx context.Context, factory domain.FactoryID, limit int) ([]domain.BakeOff, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	if limit < 1 {
		return nil, fmt.Errorf("bake-offs of factory %s: limit %d, want at least 1: %w", factory, limit, domain.ErrInvalid)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	out := []domain.BakeOff{}

	for _, r := range slices.Backward(e.runs) {
		if len(out) == limit {
			break
		}

		if r.org == org && r.run.FactoryID == factory && r.bake != nil {
			out = append(out, r.bakeOff(false))
		}
	}

	return out, nil
}

// GetBakeOff implements ports.Evals.
func (e *Evals) GetBakeOff(ctx context.Context, id domain.EvalRunID) (domain.BakeOff, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.BakeOff{}, err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	r, ok := e.find(org, id)
	if !ok || r.bake == nil {
		return domain.BakeOff{}, fmt.Errorf("bake-off %s: %w", id, ports.ErrNotFound)
	}

	return r.bakeOff(true), nil
}

// RecordArmSummary implements ports.Evals.
func (e *Evals) RecordArmSummary(ctx context.Context, id domain.EvalRunID, arm int, s domain.ArmSummary) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}

	key := fmt.Sprintf("bake-off %s arm %d", id, arm)

	if err := s.Check(); err != nil {
		return fmt.Errorf("record summary of %s: %w", key, err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	r, ok := e.find(org, id)
	if !ok || r.bake == nil || arm < 1 || arm > len(r.bake.Arms) {
		return fmt.Errorf("record summary of %s: %w", key, ports.ErrNotFound)
	}

	s = cloneArmSummary(s)
	s.TimePerItem = s.TimePerItem.Truncate(time.Millisecond)

	if s.PerCase == nil {
		s.PerCase = map[string]int{}
	}

	r.bake.Arms[arm-1].Summary = &s

	return nil
}

// bakeOff is a copy of r's bake-off, with its results when results is set. Callers hold e.mu.
func (r *evalRun) bakeOff(results bool) domain.BakeOff {
	out := *r.bake
	out.EvalRun = cloneEvalRun(r.run)

	if !results {
		out.Results = nil
	}

	out.Arms = slices.Clone(out.Arms)
	for i, a := range out.Arms {
		if a.Summary != nil {
			s := cloneArmSummary(*a.Summary)
			out.Arms[i].Summary = &s
		}
	}

	return out
}

func cloneArmSummary(s domain.ArmSummary) domain.ArmSummary {
	s.PerCase = maps.Clone(s.PerCase)
	if s.JudgedShare != nil {
		share := *s.JudgedShare
		s.JudgedShare = &share
	}

	return s
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
