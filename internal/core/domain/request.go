package domain

import "context"

type requestIDKey struct{}

// WithRequestID returns ctx carrying the id of the API request it serves (X-Request-Id), so the
// audit entries and logs it causes can be traced back to it.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestIDFrom returns the request id ctx carries, or "" outside a request.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)

	return id
}
