package ports

import (
	"context"

	"github.com/byFrederick/goal/internal/core/domain"
)

// ModelCheck asks a model a yes or no question about an event (ADR 0028 decision 4): a trigger's
// only.modelAgrees. conn is the organization connection that pays for the call, resolved with its
// credential references filled, so the key is the organization's, never goald's. The adapter
// puts q.Event in front of the model only as untrusted data and gives the model no tools; the
// answer is structured (Worth and a one-line Reason) with the tokens and cost of the call. An
// error is any call that gave no such answer: a provider error, a timeout, a refusal or an answer
// of another shape. It holds no value of conn.
type ModelCheck interface {
	Ask(ctx context.Context, conn domain.ResolvedConnection, q domain.ModelQuestion) (domain.ModelAnswer, error)
}
