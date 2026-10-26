package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// scriptedCleanUp answers each clean-up with the next of its results and tells calls about it.
type scriptedCleanUp struct {
	ports.Platform

	results []ports.AuthCleanUp
	errs    []error
	calls   chan time.Time
	n       int // only the job's goroutine calls
}

func (s *scriptedCleanUp) CleanUpAuth(_ context.Context, now time.Time) (ports.AuthCleanUp, error) {
	n := s.n
	s.n++

	s.calls <- now

	return s.results[n], s.errs[n]
}

// syncBuffer is a log destination safe for the job's goroutine and the test's.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// The job cleans up at start and then once a day, logging its counts; a round another goald
// holds or that fails changes nothing for the next.
func TestCleanUpAuth_DailyWithItsCounts(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	clock := porttest.NewClock(t0)
	job := &scriptedCleanUp{
		results: []ports.AuthCleanUp{{Sessions: 3, AuthEvents: 2}, {Skipped: true}, {}, {Verifications: 1}},
		errs:    []error{nil, nil, errors.New("database unavailable"), nil},
		calls:   make(chan time.Time, 4),
	}

	var logs syncBuffer

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		cleanUpAuth(ctx, slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})), job, clock, authCleanUpEvery)
	}()

	if err := clock.WaitTickers(ctx, 1); err != nil {
		t.Fatal(err)
	}

	for day := range len(job.results) {
		if day > 0 {
			clock.Advance(authCleanUpEvery)
		}

		if at := <-job.calls; !at.Equal(t0.Add(time.Duration(day) * authCleanUpEvery)) {
			t.Errorf("round %d cleaned up as of %v; want day %d", day, at, day)
		}
	}

	cancel()
	<-done

	out := logs.String()
	for _, want := range []string{
		`msg="sign-in cleaned up" job=goal.auth.cleanup sessions=3 verifications=0 device_codes=0 rate_limits=0 auth_events=2`,
		`msg="sign-in clean-up skipped: another goald holds it"`,
		`msg="sign-in clean-up failed" job=goal.auth.cleanup error="database unavailable"`,
		`sessions=0 verifications=1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q:\n%s", want, out)
		}
	}
}
