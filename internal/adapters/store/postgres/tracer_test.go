package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/byFrederick/goal/internal/core/ports"
)

// The tracer counts every query and batched query run under a metered context, and adds each
// acquire's wait; a context without a meter is neither counted nor a failure.
func TestTracer_CountsQueriesAndAcquireWait(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	tr := meterTracer{now: func() time.Time { return now }}

	ctx, m := ports.WithDBMeter(t.Context())

	for _, wait := range []time.Duration{30 * time.Millisecond, 12 * time.Millisecond} {
		actx := tr.TraceAcquireStart(ctx, nil, pgxpool.TraceAcquireStartData{})
		now = now.Add(wait)
		tr.TraceAcquireEnd(actx, nil, pgxpool.TraceAcquireEndData{})
	}

	for range 3 { // BEGIN, the query, COMMIT
		qctx := tr.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
		tr.TraceQueryEnd(qctx, nil, pgx.TraceQueryEndData{})
	}

	bctx := tr.TraceBatchStart(ctx, nil, pgx.TraceBatchStartData{})
	tr.TraceBatchQuery(bctx, nil, pgx.TraceBatchQueryData{})
	tr.TraceBatchQuery(bctx, nil, pgx.TraceBatchQueryData{})
	tr.TraceBatchEnd(bctx, nil, pgx.TraceBatchEndData{})

	if m.Queries() != 5 {
		t.Errorf("queries = %d; want 5", m.Queries())
	}

	if m.Wait() != 42*time.Millisecond {
		t.Errorf("wait = %v; want 42ms", m.Wait())
	}

	bare := context.Background()
	actx := tr.TraceAcquireStart(bare, nil, pgxpool.TraceAcquireStartData{})
	tr.TraceAcquireEnd(actx, nil, pgxpool.TraceAcquireEndData{})
	tr.TraceQueryEnd(tr.TraceQueryStart(bare, nil, pgx.TraceQueryStartData{}), nil, pgx.TraceQueryEndData{})

	if m.Queries() != 5 || m.Wait() != 42*time.Millisecond {
		t.Errorf("an unmetered context changed the meter: %d, %v", m.Queries(), m.Wait())
	}
}
