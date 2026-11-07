package ports

import (
	"errors"
	"time"
)

// MaxFailedJobs is the most failed job names a Check carries: enough to name what failed in a
// brief or on a card, not a full CI report.
const MaxFailedJobs = 5

// CheckState is how far a check on a commit has got.
type CheckState string

// The states of a check (ADR 0044, decision 6).
const (
	// CheckPending is a check the host has registered that has not started.
	CheckPending CheckState = "pending"
	// CheckRunning is a check that has started and not finished.
	CheckRunning CheckState = "running"
	// CheckFinished is a check that has a Result.
	CheckFinished CheckState = "finished"
)

// CheckResult is what a finished check concluded.
type CheckResult string

// The results of a finished check (ADR 0044, decision 6).
const (
	// CheckPassed is a check that succeeded.
	CheckPassed CheckResult = "passed"
	// CheckFailed is a check that failed, timed out or was cancelled.
	CheckFailed CheckResult = "failed"
	// CheckNeutral is a check that finished without passing or failing, such as a skipped one.
	CheckNeutral CheckResult = "neutral"
)

// Check is one check CI runs on a commit: a GitHub check run or commit status, or a GitLab
// pipeline.
type Check struct {
	// Name is the check's name as the host shows it.
	Name string
	// State is how far it has got.
	State CheckState
	// Result is set only when State is CheckFinished.
	Result CheckResult
	// URL links to the check on the host; it may be empty.
	URL string
	// FailedJobs names at most MaxFailedJobs of the check's failed jobs, where the host says which.
	FailedJobs []string
}

// CommitChecks is what CodeHost.Checks found CI says about a commit.
type CommitChecks struct {
	// Head is the pull request's current head when Checks was given its URL, else the commit asked
	// about. The checks are those of Head.
	Head string
	// Checks are the commit's checks, in the host's order. None means no CI is configured or no
	// check has registered yet.
	Checks []Check
}

// Finished reports whether CI has finished on the commit: at least one check exists and every
// check has finished. No checks is not finished, since checks register seconds after a push.
func (c CommitChecks) Finished() bool {
	finished, total := c.Counts()

	return total > 0 && finished == total
}

// Counts returns how many of the commit's checks have finished and how many there are, as in
// "9 of 14".
func (c CommitChecks) Counts() (finished, total int) {
	for _, ch := range c.Checks {
		if ch.State == CheckFinished {
			finished++
		}
	}

	return finished, len(c.Checks)
}

// Failed returns the checks that finished with the result failed, in order.
func (c CommitChecks) Failed() []Check {
	var out []Check

	for _, ch := range c.Checks {
		if ch.State == CheckFinished && ch.Result == CheckFailed {
			out = append(out, ch)
		}
	}

	return out
}

// ErrCINotReadable means the token cannot see a repository's CI, which is not the same as no CI
// configured (ADR 0044, decision 8): hidden CI never counts as passed. CINotReadableError names
// the permission the token needs.
var ErrCINotReadable = errors.New("CI not readable")

// CINotReadableError is an error wrapping ErrCINotReadable: the token lacks Permission, such as
// read access to Checks and Commit statuses on GitHub or read_api on GitLab.
type CINotReadableError struct {
	Permission string
}

func (e *CINotReadableError) Error() string {
	return "the token cannot read CI: it needs " + e.Permission
}

// Is makes errors.Is(err, ErrCINotReadable) true.
func (e *CINotReadableError) Is(target error) bool { return target == ErrCINotReadable }

// RateLimitedError means the host refused a call for its rate limit: callers wait until RetryAt
// before calling it again, and keep the wait they were in.
type RateLimitedError struct {
	RetryAt time.Time
}

func (e *RateLimitedError) Error() string {
	return "rate limited by the code host until " + e.RetryAt.UTC().Format(time.RFC3339)
}
