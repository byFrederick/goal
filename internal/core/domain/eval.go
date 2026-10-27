package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"time"
)

// EvalRunID identifies one eval run (a UUIDv7).
type EvalRunID string

// EvalTrigger is why an eval run started. Values match eval_runs.trigger.
type EvalTrigger string

// Eval triggers (ADR 0025 decision 4): a person asked for it, or a publish runs the cases on the
// draft first.
const (
	EvalManual    EvalTrigger = "manual"
	EvalOnPublish EvalTrigger = "publish"
)

// EvalRunState is where an eval run is. Values match eval_runs.state; done, failed and cancelled
// are final, and only those have a FinishedAt.
type EvalRunState string

// Eval run states.
const (
	EvalRunQueued    EvalRunState = "queued"
	EvalRunRunning   EvalRunState = "running"
	EvalRunDone      EvalRunState = "done"
	EvalRunFailed    EvalRunState = "failed"
	EvalRunCancelled EvalRunState = "cancelled"
)

// Final reports whether s is a finished run's state.
func (s EvalRunState) Final() bool {
	return s == EvalRunDone || s == EvalRunFailed || s == EvalRunCancelled
}

// EvalResultState is where one try of a case is. Values match eval_results.state. A try that
// failed for an infrastructure reason is error, never fail (ADR 0025 decision 4).
type EvalResultState string

// Eval result states: pass, fail and error are finished.
const (
	EvalPending EvalResultState = "pending"
	EvalRunning EvalResultState = "running"
	EvalPass    EvalResultState = "pass"
	EvalFail    EvalResultState = "fail"
	EvalError   EvalResultState = "error"
)

// Finished reports whether s is a finished try's state.
func (s EvalResultState) Finished() bool { return s == EvalPass || s == EvalFail || s == EvalError }

// evalCaseName is the form of a case's name, its file name under evals/cases without .yaml, as
// internal/config checks it.
var evalCaseName = regexp.MustCompile(`^[a-z][a-z0-9-]{1,38}[a-z0-9]$`)

// MaxEvalDetail is the largest EvalOutcome.Detail, encoded, in bytes (256 KiB).
const MaxEvalDetail = 256 << 10

// EvalCase is one case an eval run runs: its name, the hash of its file and how many tries.
type EvalCase struct {
	Name   string
	SHA256 BlobHash
	Tries  int
}

// NewEvalRun is an eval run to queue: the cases of one definition, named by the hash of its
// content (ManifestSHA256) and, once published, its version. A draft's run has no VersionID.
type NewEvalRun struct {
	FactoryID     FactoryID
	VersionID     DefinitionVersionID
	ContentSHA256 BlobHash
	Trigger       EvalTrigger
	// Case is the one case a run was asked for; empty runs every case.
	Case        string
	Cases       []EvalCase
	EstimateUSD float64
}

// Check reports what makes n impossible to queue, wrapping ErrInvalid.
func (n NewEvalRun) Check() error {
	var errs []error

	bad := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("eval run: "+format+": %w", append(args, ErrInvalid)...))
	}

	if n.FactoryID == "" {
		bad("no factory")
	}

	if !n.ContentSHA256.Valid() {
		bad("content hash %q is not 64 lowercase hex", n.ContentSHA256)
	}

	if n.Trigger != EvalManual && n.Trigger != EvalOnPublish {
		bad("trigger %q is not manual or publish", n.Trigger)
	}

	if n.EstimateUSD < 0 {
		bad("estimate %v is negative", n.EstimateUSD)
	}

	if n.Case != "" && (len(n.Cases) != 1 || n.Cases[0].Name != n.Case) {
		bad("case %q was asked for, so it is the only case to run", n.Case)
	}

	return errors.Join(append(errs, checkEvalCases(n.Cases))...)
}

// checkEvalCases reports what makes an eval run's case list impossible to run, wrapping
// ErrInvalid: none, a name that is not a case file's, one listed twice, a bad hash or no tries.
func checkEvalCases(cases []EvalCase) error {
	if len(cases) == 0 {
		return fmt.Errorf("eval run: no cases: %w", ErrInvalid)
	}

	var errs []error

	seen := make(map[string]bool, len(cases))

	for _, c := range cases {
		switch {
		case !evalCaseName.MatchString(c.Name):
			errs = append(errs, fmt.Errorf("eval run: case name %q is not a case file's name: %w", c.Name, ErrInvalid))
		case seen[c.Name]:
			errs = append(errs, fmt.Errorf("eval run: case %q is listed twice: %w", c.Name, ErrInvalid))
		}

		seen[c.Name] = true

		if !c.SHA256.Valid() {
			errs = append(errs, fmt.Errorf("eval run: case %q: hash %q is not 64 lowercase hex: %w", c.Name, c.SHA256, ErrInvalid))
		}

		if c.Tries < 1 {
			errs = append(errs, fmt.Errorf("eval run: case %q: %d tries, want at least 1: %w", c.Name, c.Tries, ErrInvalid))
		}
	}

	return errors.Join(errs...)
}

// EvalRun is one eval run and, from GetRun, its results by case and try.
type EvalRun struct {
	ID            EvalRunID
	FactoryID     FactoryID
	VersionID     DefinitionVersionID
	ContentSHA256 BlobHash
	Trigger       EvalTrigger
	Case          string
	State         EvalRunState
	EstimateUSD   float64
	// CostUSD is the sum of its recorded tries' costs.
	CostUSD   float64
	CreatedBy Actor
	CreatedAt time.Time
	// FinishedAt is zero until the run is done, failed or cancelled.
	FinishedAt time.Time
	Results    []EvalResult
}

// EvalResult is one try of one case in an eval run.
type EvalResult struct {
	EvalRunID  EvalRunID
	Case       string
	CaseSHA256 BlobHash
	Try        int
	State      EvalResultState
	// LeaseUntil is when a running try's claim ends; zero otherwise. RecordResult fences on it.
	LeaseUntil time.Time
	// RunID is the agent run that ran the try, empty when none did (or it was purged).
	RunID     RunID
	CheckedBy string
	Detail    map[string]any
	CostUSD   float64
	Turns     int
}

// EvalOutcome is how a claimed try ended: pass, fail or error, with what checked it (the
// contract, a judge, a reference patch), what it found, and what the try cost.
type EvalOutcome struct {
	State     EvalResultState
	RunID     RunID
	CheckedBy string
	Detail    map[string]any
	CostUSD   float64
	Turns     int
}

// Check reports what makes o impossible to record, wrapping ErrInvalid. Detail's size is checked
// where it is encoded.
func (o EvalOutcome) Check() error {
	var errs []error

	if !o.State.Finished() {
		errs = append(errs, fmt.Errorf("eval outcome %q is not pass, fail or error: %w", o.State, ErrInvalid))
	}

	if o.CostUSD < 0 {
		errs = append(errs, fmt.Errorf("eval outcome cost %v is negative: %w", o.CostUSD, ErrInvalid))
	}

	if o.Turns < 0 {
		errs = append(errs, fmt.Errorf("eval outcome turns %d is negative: %w", o.Turns, ErrInvalid))
	}

	if len(o.CheckedBy) > 200 {
		errs = append(errs, fmt.Errorf("eval outcome checked_by is over 200 bytes: %w", ErrInvalid))
	}

	return errors.Join(errs...)
}

// EvalCaseHistory is one case's recent record in a factory: its last finished tries, newest
// first, and what the case cost in the latest run that ran it.
type EvalCaseHistory struct {
	Case  string
	Tries []EvalResult
	// LatestRunID is that latest run, and LatestCostUSD the sum of the case's tries in it.
	LatestRunID   EvalRunID
	LatestCostUSD float64
}

// EvalHistoryTries is how many finished tries EvalCaseHistory keeps per case.
const EvalHistoryTries = 3

// ManifestSHA256 is the hash of a definition's content: the SHA-256 of "path\x00hash\n" for each
// file, sorted by path. It does not depend on map order, and it is the one hash an eval run and
// the publish gate compare a draft's content by.
func ManifestSHA256(m Manifest) BlobHash {
	h := sha256.New()

	for _, p := range slices.Sorted(maps.Keys(m)) {
		_, _ = h.Write([]byte(p + "\x00" + string(m[p]) + "\n"))
	}

	return BlobHash(hex.EncodeToString(h.Sum(nil)))
}
