package postgres

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// syncBuffer is a log written by LogPoolStats's goroutine and read by the test.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) lines(t *testing.T) []map[string]any {
	t.Helper()

	b.mu.Lock()
	defer b.mu.Unlock()

	var out []map[string]any

	sc := bufio.NewScanner(strings.NewReader(b.buf.String()))
	for sc.Scan() {
		var line map[string]any
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			t.Fatalf("log line %q: %v", sc.Text(), err)
		}

		out = append(out, line)
	}

	return out
}

// Each tick logs one "db pool" line with the pool's statistics at that moment; none after ctx
// ends.
func TestPoolStatsLogger_LogsOnEachTick(t *testing.T) {
	t.Parallel()

	clock := porttest.NewClock(time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC))
	logs := &syncBuffer{}
	log := slog.New(slog.NewJSONHandler(logs, nil))

	reads := make(chan int, 10)
	n := 0
	stat := func() PoolStat {
		n++
		defer func() { reads <- n }()

		return PoolStat{
			AcquireCount: int64(100 * n), EmptyAcquireCount: int64(10 * n), CanceledAcquireCount: int64(n),
			AcquireDuration: time.Duration(n) * 1500 * time.Millisecond, AcquiredConns: 8, IdleConns: 0, TotalConns: 8, MaxConns: 8,
		}
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)
		LogPoolStats(ctx, stat, clock, time.Minute, log)
	}()

	if err := clock.WaitTickers(t.Context(), 1); err != nil {
		t.Fatal(err)
	}

	for want := 1; want <= 2; want++ {
		clock.Advance(time.Minute)

		if got := <-reads; got != want {
			t.Fatalf("read %d; want %d", got, want)
		}
	}

	cancel()
	<-done
	clock.Advance(time.Minute)

	lines := logs.lines(t)
	if len(lines) != 2 {
		t.Fatalf("%d lines; want 2: %v", len(lines), lines)
	}

	for i, line := range lines {
		n := float64(i + 1)
		want := map[string]any{
			"msg": "db pool", "acquire_count": 100 * n, "empty_acquire_count": 10 * n, "canceled_acquire_count": n,
			"acquire_duration_ms": 1500 * n, "acquired_conns": 8.0, "idle_conns": 0.0, "total_conns": 8.0, "max_conns": 8.0,
		}

		for k, v := range want {
			if line[k] != v {
				t.Errorf("line %d: %s = %v; want %v (%v)", i+1, k, line[k], v, line)
			}
		}
	}
}
