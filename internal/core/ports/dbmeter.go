package ports

import (
	"context"
	"sync/atomic"
	"time"
)

// DBMeter counts the database work done under one context: the queries run and the time spent
// waiting for a pooled connection. The API puts one in each request's context (WithDBMeter) and
// logs it; the store's tracer adds to it. It is safe for concurrent use.
type DBMeter struct {
	queries atomic.Int64
	wait    atomic.Int64 // nanoseconds
}

type dbMeterKey struct{}

// WithDBMeter returns ctx carrying a new meter, and the meter.
func WithDBMeter(ctx context.Context) (context.Context, *DBMeter) {
	m := &DBMeter{}

	return context.WithValue(ctx, dbMeterKey{}, m), m
}

// DBMeterFrom returns the meter in ctx, or nil when it carries none. Every method of a nil meter
// is a no-op, so work run outside a request is never counted and never fails.
func DBMeterFrom(ctx context.Context) *DBMeter {
	m, _ := ctx.Value(dbMeterKey{}).(*DBMeter)

	return m
}

// AddQuery counts one query.
func (m *DBMeter) AddQuery() {
	if m != nil {
		m.queries.Add(1)
	}
}

// AddWait adds the time one connection acquire waited.
func (m *DBMeter) AddWait(d time.Duration) {
	if m != nil {
		m.wait.Add(int64(d))
	}
}

// Queries is how many queries were counted; 0 for a nil meter.
func (m *DBMeter) Queries() int {
	if m == nil {
		return 0
	}

	return int(m.queries.Load())
}

// Wait is the total time acquires waited; 0 for a nil meter.
func (m *DBMeter) Wait() time.Duration {
	if m == nil {
		return 0
	}

	return time.Duration(m.wait.Load())
}
