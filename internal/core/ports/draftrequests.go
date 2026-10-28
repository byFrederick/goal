package ports

import (
	"context"
	"errors"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// DraftRequests are the asked definition changes (ADR 0021), for the organization in ctx unless
// a method says otherwise. A factory has at most one queued or running request. Requests are
// claimed with leases fenced by a token drawn on each claim, so any goald may run any request
// and one that dies leaves it to the next once its lease ends and RequeueExpired puts it back.
type DraftRequests interface {
	// Create queues a request on n.Factory. Its rules are NewDraftRequest.Check's
	// (domain.ErrInvalid); ErrNotFound for an unknown factory, domain.ErrDraftRequestOpen while
	// the factory has a queued or running request.
	Create(ctx context.Context, n domain.NewDraftRequest) (domain.DraftRequest, error)
	// Get is the request id. ErrNotFound when there is none.
	Get(ctx context.Context, id domain.DraftRequestID) (domain.DraftRequest, error)
	// List is a factory's latest requests, newest first (ties by id descending), at most limit
	// (1 to domain.MaxDraftRequestList, else domain.ErrInvalid); none for an unknown factory.
	List(ctx context.Context, factory domain.FactoryID, limit int) ([]domain.DraftRequest, error)

	// Claim leases to until the oldest queued request of any live organization (ctx needs
	// none), so every organization's requests take their turn in one order: it becomes running,
	// started at now, under a new lease token. nil when none is queued.
	Claim(ctx context.Context, now, until time.Time) (*domain.DraftRequestClaim, error)
	// Heartbeat extends a lease held at now to until, under the claim's organization in ctx.
	// ErrLeaseLost when it ended, another claim took the request, or it is not running any more.
	Heartbeat(ctx context.Context, l domain.DraftRequestLease, now, until time.Time) error
	// Finish ends a running request as r says (domain.DraftRequestResult.Check, else
	// domain.ErrInvalid), finished at r.At, its lease released; the summary and error are cut to
	// domain.MaxDraftRequestSummary and domain.MaxDraftRequestError characters. ErrLeaseLost,
	// changing nothing, once another claim took the request or it is not running.
	Finish(ctx context.Context, l domain.DraftRequestLease, r domain.DraftRequestResult) error
	// RequeueExpired puts every running request of any organization whose lease ended by now
	// (ctx needs none) back in the queue, its start and lease cleared, and returns how many.
	RequeueExpired(ctx context.Context, now time.Time) (int, error)
}

// ErrBudgetExhausted means the factory's weekly budget is used up, so it starts no draft request
// (ADR 0021 decision 4).
var ErrBudgetExhausted = errors.New("the factory's weekly budget is used up")

// DefinitionRunner runs a claimed draft request's box (ADR 0021 decision 2), for the organization
// in ctx: a box with no repository whose /workspace holds the factory's draft at the task's
// revision, where goal-agent runs in definition mode with the factory's runtime.modelConnection
// only. It never writes into the draft: the caller applies the result.
type DefinitionRunner interface {
	// RunDefinition returns what goal-agent reported. An error wrapping ErrBudgetExhausted, when
	// the factory's weekly budget is used up, creates no box. Any other error is an
	// infrastructure failure (the factory is not loaded, its model connection, the box, prepare),
	// one wrapping ErrConflict when the draft moved past the task's revision before the box got
	// its files. A worker that finished failed is a result with status failed and its error. No
	// error and no result holds a secret value, and the box is deleted on every path.
	RunDefinition(ctx context.Context, t DefinitionTask) (DefinitionResult, error)
}

// DefinitionTask is one draft request to run on its factory's draft at Revision, the revision
// the run reads.
type DefinitionTask struct {
	Request  domain.DraftRequest
	Revision int64
}

// DefinitionResult is how goal-agent's definition mode ended: its summary, the files it changed
// (new or modified, with their content) and deleted, the paths outside the allowlist it rejected,
// and what the run cost. Status is succeeded or failed; only a succeeded result is applied.
type DefinitionResult struct {
	Status   domain.RunStatus
	Summary  string
	Changed  []DefinitionFile
	Deleted  []string
	Rejected []string
	CostUSD  float64
	Turns    int
	// Error is why a failed result failed, "" otherwise.
	Error string
}

// DefinitionFile is one file a draft request wrote: its definition path, sha256 and content.
type DefinitionFile struct {
	Path    string
	SHA256  domain.BlobHash
	Content []byte
}
