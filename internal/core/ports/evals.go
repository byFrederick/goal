package ports

import (
	"context"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// Evals are a factory's eval runs and their tries (ADR 0025 decision 4), for the organization in
// ctx. An eval run runs cases on one definition, named by its content hash (domain.ManifestSHA256)
// and, once published, its version; each case's tries are results that goald instances claim
// under a lease, so any instance can run them and a dead one's tries are claimed again.
type Evals interface {
	// CreateRun queues a run with one pending result per case and try, in one transaction. n must
	// pass Check (domain.ErrInvalid); ErrNotFound when the factory, or the version of that
	// factory, does not exist.
	CreateRun(ctx context.Context, n domain.NewEvalRun, by domain.Actor) (domain.EvalRun, error)
	// ClaimResult takes one pending try, or a running one whose lease ended by now, of a queued or
	// running run (oldest run first), skipping tries another transaction holds. It sets the try
	// running until now+lease and its run running, and returns it with that LeaseUntil.
	// ErrNotFound when nothing is claimable.
	ClaimResult(ctx context.Context, now time.Time, lease time.Duration) (domain.EvalResult, error)
	// RecordResult records how the claimed try ended and adds its cost to the run's. claimed names
	// the try and the LeaseUntil ClaimResult returned; ErrLeaseLost unless the try is still
	// running under that lease (it was claimed again, or recorded). o must pass Check.
	RecordResult(ctx context.Context, claimed domain.EvalResult, o domain.EvalOutcome) error
	// FinishRun sets the run done, failed or cancelled, finished at at. ErrNotFound when there is
	// no such run, ErrConflict when it is already finished, domain.ErrInvalid for a state that is
	// not final.
	FinishRun(ctx context.Context, id domain.EvalRunID, state domain.EvalRunState, at time.Time) (domain.EvalRun, error)
	// GetRun is the run with its results by case and try. ErrNotFound when there is none.
	GetRun(ctx context.Context, id domain.EvalRunID) (domain.EvalRun, error)
	// CaseHistory is, per case of the factory's runs (by name), its last domain.EvalHistoryTries
	// finished tries, newest first, and its cost in the latest run that finished a try of it.
	CaseHistory(ctx context.Context, factory domain.FactoryID) ([]domain.EvalCaseHistory, error)
	// LatestRunForContent is the newest run of the factory on the content hash sha256, without
	// its results: the publish gate's question. ErrNotFound when there is none.
	LatestRunForContent(ctx context.Context, factory domain.FactoryID, sha256 domain.BlobHash) (domain.EvalRun, error)

	// A bake-off (ADR 0029) is a run of kind bake-off: ClaimResult, RecordResult and FinishRun
	// work on its tries as on any run's, and each try carries its arm. CaseHistory and
	// LatestRunForContent see eval runs of kind eval only.

	// CreateBakeOff queues a bake-off with its arms (numbered from 1 in n's order) and one pending
	// result per arm, case and try, in one transaction. n must pass Check (domain.ErrInvalid);
	// ErrNotFound when the factory, or the version of that factory, does not exist.
	CreateBakeOff(ctx context.Context, n domain.NewBakeOff, by domain.Actor) (domain.BakeOff, error)
	// ListBakeOffs is at most limit of the factory's bake-offs, newest first, with their arms and
	// without their results.
	ListBakeOffs(ctx context.Context, factory domain.FactoryID, limit int) ([]domain.BakeOff, error)
	// GetBakeOff is the bake-off with its arms and its results by arm, case and try. ErrNotFound
	// when there is no such run or it is not a bake-off.
	GetBakeOff(ctx context.Context, id domain.EvalRunID) (domain.BakeOff, error)
	// RecordArmSummary writes the report row of the bake-off's arm, replacing an earlier one. s
	// must pass Check (domain.ErrInvalid); ErrNotFound when the run has no such arm.
	RecordArmSummary(ctx context.Context, id domain.EvalRunID, arm int, s domain.ArmSummary) error
}
