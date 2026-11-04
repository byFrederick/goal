package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// PastTryID identifies one try of a past item (ADR 0039 decision 9).
type PastTryID string

// PastTryState is where a past-item try is. Values match tries.state.
type PastTryState string

// Past-item try states: queued until a goald claims it, running under its lease, done once its run
// ended or could not be carried out.
const (
	PastTryQueued  PastTryState = "queued"
	PastTryRunning PastTryState = "running"
	PastTryDone    PastTryState = "done"
)

// PastTryVerdict is how a done try came out (ADR 0039 decision 10). Values match tries.verdict.
// Empty is a try whose run ended and is waiting for the grader.
type PastTryVerdict string

// Past-item try verdicts. Code grades first: a try whose output fails the proposed contract or
// one of the station's checks is not fixed, and the grader never sees it. Fixed, not fixed and
// unknown carry their reason (PastTryResult.Reason).
const (
	// PastTryFixed is a try that fixed what sent the item back.
	PastTryFixed PastTryVerdict = "fixed"
	// PastTryNotFixed is a try that did not: its output failed the contract or a check, or the
	// grader said so.
	PastTryNotFixed PastTryVerdict = "not_fixed"
	// PastTryUnknown is a try whose grade cannot be trusted, as when the grader misread the item.
	PastTryUnknown PastTryVerdict = "unknown"
	// PastTryError is a try whose run could not be carried out: a commit that is gone, a lost
	// box, a prepare or setup that failed, or any other infrastructure failure. It is never a
	// failing try.
	PastTryError PastTryVerdict = "error"
)

// graded reports whether v is a grade, which needs its reason.
func (v PastTryVerdict) graded() bool {
	return v == PastTryFixed || v == PastTryNotFixed || v == PastTryUnknown
}

// Bounds of past-item tries (ADR 0039 decision 11).
const (
	// MaxPastTryItems is how many of the items a proposal cites it is tried on, newest first.
	MaxPastTryItems = 5
	// MaxPastTryDiff bounds the diff a try stores, in bytes (tries.diff).
	MaxPastTryDiff = 1 << 20
	// MaxPastTryError bounds a try's error, in characters (tries.error).
	MaxPastTryError = 2000
	// MaxPastTryReason bounds the reason of a try's grade, in characters (tries.reason).
	MaxPastTryReason = 2000
)

// PastTryStartRef is the ref prepare points at the commit a try's agent starts from, once the
// recorded commit is checked out and brought up to date with its base (apps/worker/src/prepare.ts).
const PastTryStartRef = "refs/goal/try-start"

// PastTryDiffScript is the shell command the engine runs in a try's workspace once its agent
// ended: the change against PastTryStartRef, new files included, committed or not.
const PastTryDiffScript = "git add --all --intent-to-add && git diff --no-color --no-ext-diff --binary " + PastTryStartRef

// PastTry is one try of a proposal on a past item: a rerun of Station, where the proposal reaches,
// from what OriginalRun recorded (its brief and commit), in a box with only the model key.
type PastTry struct {
	ID         PastTryID
	FactoryID  FactoryID
	ProposalID ProposalID
	// ProposalNumber is the proposal's number in its factory, read with the try.
	ProposalNumber int64
	ItemID         ItemID
	Station        string
	OriginalRunID  RunID
	// TryRunID is the try's own run, of kind try; empty until it ran.
	TryRunID RunID
	Model    string
	State    PastTryState
	Verdict  PastTryVerdict
	Error    string
	// Reason is why the try was graded as it was, first; empty without a grade.
	Reason string
	// Diff is the try's change against the commit it started from.
	Diff       string
	CostUSD    float64
	LeaseUntil time.Time
	// Attempts counts the claims, the first included.
	Attempts   int
	CreatedAt  time.Time
	FinishedAt time.Time
}

// NewPastTry is one try to queue.
type NewPastTry struct {
	ProposalID    ProposalID
	ItemID        ItemID
	Station       string
	OriginalRunID RunID
	Model         string
}

// Check reports what makes n impossible to queue, wrapping ErrInvalid.
func (n NewPastTry) Check() error {
	if n.ProposalID == "" || n.ItemID == "" || n.Station == "" || n.OriginalRunID == "" || n.Model == "" {
		return fmt.Errorf("past try: it needs a proposal, an item, a station, an original run and a model: %w", ErrInvalid)
	}

	return nil
}

// PastTryResult is how a claimed try ended: done, with no verdict and its run's diff and cost,
// graded with its reason, or with verdict error and why.
type PastTryResult struct {
	Verdict PastTryVerdict
	Error   string
	// Reason is why the try was graded Verdict: what failed first for a try code graded.
	Reason   string
	Diff     string
	CostUSD  float64
	TryRunID RunID
	At       time.Time
}

// Check reports what makes r impossible to store, wrapping ErrInvalid: an unknown verdict, an
// error verdict without its error, a grade without its reason or a reason without a grade, a
// negative cost, or a diff, error or reason past its bound.
func (r PastTryResult) Check() error {
	if err := r.checkVerdict(); err != nil {
		return err
	}

	switch {
	case r.CostUSD < 0:
		return fmt.Errorf("past try result: negative cost: %w", ErrInvalid)
	case len(r.Diff) > MaxPastTryDiff:
		return fmt.Errorf("past try result: the diff is over %d bytes: %w", MaxPastTryDiff, ErrInvalid)
	case len([]rune(r.Error)) > MaxPastTryError:
		return fmt.Errorf("past try result: the error is over %d characters: %w", MaxPastTryError, ErrInvalid)
	case r.At.IsZero():
		return fmt.Errorf("past try result: no finish time: %w", ErrInvalid)
	}

	return nil
}

// checkVerdict refuses an unknown verdict, an error without its error, and a grade without its
// reason, a reason without a grade or one past its bound.
func (r PastTryResult) checkVerdict() error {
	switch {
	case r.Verdict != "" && r.Verdict != PastTryError && !r.Verdict.graded():
		return fmt.Errorf("past try result: verdict %q: %w", r.Verdict, ErrInvalid)
	case r.Verdict == PastTryError && r.Error == "":
		return fmt.Errorf("past try result: an error needs its reason: %w", ErrInvalid)
	case r.Verdict.graded() && strings.TrimSpace(r.Reason) == "":
		return fmt.Errorf("past try result: verdict %s needs its reason: %w", r.Verdict, ErrInvalid)
	case !r.Verdict.graded() && r.Reason != "":
		return fmt.Errorf("past try result: a reason needs a grade: %w", ErrInvalid)
	case len([]rune(r.Reason)) > MaxPastTryReason:
		return fmt.Errorf("past try result: the reason is over %d characters: %w", MaxPastTryReason, ErrInvalid)
	}

	return nil
}

// PastTryInput is what a try reruns (ADR 0039 decision 9): the original run as the store recorded
// it, with its brief and heads, and the item's outputs by station from the runs that finished
// before the original run started.
type PastTryInput struct {
	Run     Run
	Outputs map[string]json.RawMessage
}

// TriableAgent reports whether agent a of l can be tried on a past item (ADR 0039 decision 9): its
// agent needs no MCP server and no secret to do its work. On connections it names no connection
// besides the model's; on secrets its environment has no MCP servers and no files. An environment's
// env is allowed, for setup alone. False when l has no such agent or environment.
func (l Line) TriableAgent(name string) bool {
	a, ok := l.Agent(name)
	if !ok || len(a.Connections) > 0 {
		return false
	}

	if !l.OnSecrets() {
		return true
	}

	env, ok := l.Environment(a.Environment)

	return ok && len(env.MCP) == 0 && len(env.Files) == 0
}
