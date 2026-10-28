package ports

import (
	"context"
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

	// Claim leases until until the oldest queued request of any live organization (ctx needs
	// none), so every organization's requests take their turn in one order: it becomes running,
	// started at now, under a new lease token. nil when none is queued.
	Claim(ctx context.Context, now, until time.Time) (*domain.DraftRequestClaim, error)
	// Heartbeat extends a lease held at now until until, under the claim's organization in ctx.
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
