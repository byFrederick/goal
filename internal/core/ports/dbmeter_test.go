package ports_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/ports"
)

func TestDBMeter_CountsConcurrentQueriesAndWaits(t *testing.T) {
	t.Parallel()

	ctx, m := ports.WithDBMeter(t.Context())
	if ports.DBMeterFrom(ctx) != m {
		t.Fatal("DBMeterFrom does not return the context's meter")
	}

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			ports.DBMeterFrom(ctx).AddQuery()
			ports.DBMeterFrom(ctx).AddWait(time.Millisecond)
		})
	}

	wg.Wait()

	if m.Queries() != 50 || m.Wait() != 50*time.Millisecond {
		t.Errorf("meter = %d queries, %v wait; want 50, 50ms", m.Queries(), m.Wait())
	}
}

// Work outside a request has no meter: counting it is a no-op, never a panic.
func TestDBMeter_NoMeterIsANoOp(t *testing.T) {
	t.Parallel()

	m := ports.DBMeterFrom(context.Background())
	if m != nil {
		t.Fatalf("DBMeterFrom(Background) = %v; want nil", m)
	}

	m.AddQuery()
	m.AddWait(time.Second)

	if m.Queries() != 0 || m.Wait() != 0 {
		t.Errorf("nil meter = %d, %v; want zeros", m.Queries(), m.Wait())
	}
}
