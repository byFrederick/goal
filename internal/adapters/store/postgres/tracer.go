package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/byFrederick/goal/internal/core/ports"
)

// meterTracer adds the database work run under a metered context (ports.WithDBMeter) to its
// meter: every query and batched query (BEGIN, set_config and COMMIT count, as each is a round
// trip), and how long each connection acquire waited. A context without a meter is not counted.
// pgx takes one tracer per pool, so this one type is all of them.
type meterTracer struct {
	now func() time.Time
}

var (
	_ pgx.QueryTracer       = meterTracer{}
	_ pgx.BatchTracer       = meterTracer{}
	_ pgxpool.AcquireTracer = meterTracer{}
)

type acquireStartKey struct{}

func (meterTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	ports.DBMeterFrom(ctx).AddQuery()

	return ctx
}

func (meterTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (meterTracer) TraceBatchStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceBatchStartData) context.Context {
	return ctx
}

func (meterTracer) TraceBatchQuery(ctx context.Context, _ *pgx.Conn, _ pgx.TraceBatchQueryData) {
	ports.DBMeterFrom(ctx).AddQuery()
}

func (meterTracer) TraceBatchEnd(context.Context, *pgx.Conn, pgx.TraceBatchEndData) {}

func (t meterTracer) TraceAcquireStart(ctx context.Context, _ *pgxpool.Pool, _ pgxpool.TraceAcquireStartData) context.Context {
	if ports.DBMeterFrom(ctx) == nil {
		return ctx
	}

	return context.WithValue(ctx, acquireStartKey{}, t.now())
}

func (t meterTracer) TraceAcquireEnd(ctx context.Context, _ *pgxpool.Pool, _ pgxpool.TraceAcquireEndData) {
	if start, ok := ctx.Value(acquireStartKey{}).(time.Time); ok {
		ports.DBMeterFrom(ctx).AddWait(t.now().Sub(start))
	}
}
