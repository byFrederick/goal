package ports_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/ports"
)

var (
	pending = ports.Check{Name: "lint", State: ports.CheckPending}
	running = ports.Check{Name: "build", State: ports.CheckRunning}
	passed  = ports.Check{Name: "test", State: ports.CheckFinished, Result: ports.CheckPassed}
	neutral = ports.Check{Name: "label", State: ports.CheckFinished, Result: ports.CheckNeutral}
	failed  = ports.Check{Name: "e2e", State: ports.CheckFinished, Result: ports.CheckFailed, FailedJobs: []string{"e2e (3/12)"}}
	failed2 = ports.Check{Name: "migrate", State: ports.CheckFinished, Result: ports.CheckFailed}
)

// checkSets are the commits the helpers are tested on: none registered yet, all pending, a mix and
// all finished.
var checkSets = map[string]ports.CommitChecks{
	"no checks":    {Head: "4b825dc6"},
	"all pending":  {Head: "4b825dc6", Checks: []ports.Check{pending, pending}},
	"mixed":        {Head: "4b825dc6", Checks: []ports.Check{failed, pending, passed, running, failed2}},
	"all finished": {Head: "4b825dc6", Checks: []ports.Check{passed, failed, neutral, failed2}},
}

// A commit no check has registered on yet is not finished: checks register seconds after a push.
func TestCommitChecks_Finished(t *testing.T) {
	t.Parallel()

	want := map[string]bool{"no checks": false, "all pending": false, "mixed": false, "all finished": true}

	for name, cc := range checkSets {
		if got := cc.Finished(); got != want[name] {
			t.Errorf("%s: Finished() = %v; want %v", name, got, want[name])
		}
	}
}

func TestCommitChecks_Counts(t *testing.T) {
	t.Parallel()

	want := map[string][2]int{"no checks": {0, 0}, "all pending": {0, 2}, "mixed": {3, 5}, "all finished": {4, 4}}

	for name, cc := range checkSets {
		if finished, total := cc.Counts(); finished != want[name][0] || total != want[name][1] {
			t.Errorf("%s: Counts() = %d of %d; want %d of %d", name, finished, total, want[name][0], want[name][1])
		}
	}
}

// Failed keeps the host's order and leaves out passed, neutral and unfinished checks.
func TestCommitChecks_Failed(t *testing.T) {
	t.Parallel()

	want := map[string][]string{"no checks": nil, "all pending": nil, "mixed": {"e2e", "migrate"}, "all finished": {"e2e", "migrate"}}

	for name, cc := range checkSets {
		var got []string
		for _, c := range cc.Failed() {
			got = append(got, c.Name)
		}

		if !slices.Equal(got, want[name]) {
			t.Errorf("%s: Failed() = %v; want %v", name, got, want[name])
		}
	}
}

// A failed check that has not finished is not failed yet: its result is not final.
func TestCommitChecks_FailedOnlyWhenFinished(t *testing.T) {
	t.Parallel()

	cc := ports.CommitChecks{Checks: []ports.Check{{Name: "odd", State: ports.CheckRunning, Result: ports.CheckFailed}}}
	if got := cc.Failed(); len(got) != 0 {
		t.Errorf("Failed() = %v; want none", got)
	}
}

// Callers branch on both errors through any wrapping, and keep what they carry.
func TestCommitChecks_Errors(t *testing.T) {
	t.Parallel()

	retryAt := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		err         error
		notReadable bool
		rateLimited bool
	}{
		{"not readable", &ports.CINotReadableError{Permission: "Checks: read"}, true, false},
		{"rate limited", &ports.RateLimitedError{RetryAt: retryAt}, false, true},
		{"other", errors.New("GitHub answered 500"), false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := fmt.Errorf("checks of acme/app: %w", fmt.Errorf("commit 4b825dc6: %w", tt.err))

			if got := errors.Is(err, ports.ErrCINotReadable); got != tt.notReadable {
				t.Errorf("errors.Is(ErrCINotReadable) = %v; want %v", got, tt.notReadable)
			}

			var nr *ports.CINotReadableError
			if got := errors.As(err, &nr); got != tt.notReadable {
				t.Errorf("errors.As(*CINotReadableError) = %v; want %v", got, tt.notReadable)
			} else if got && nr.Permission != "Checks: read" {
				t.Errorf("Permission = %q; want %q", nr.Permission, "Checks: read")
			}

			var rl *ports.RateLimitedError
			if got := errors.As(err, &rl); got != tt.rateLimited {
				t.Errorf("errors.As(*RateLimitedError) = %v; want %v", got, tt.rateLimited)
			} else if got && !rl.RetryAt.Equal(retryAt) {
				t.Errorf("RetryAt = %v; want %v", rl.RetryAt, retryAt)
			}
		})
	}
}
