package domain

import (
	"encoding/json"
	"fmt"
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

// PastTryVerdict is how a done try came out. Values match tries.verdict. Empty is a try whose run
// ended and is waiting to be graded (software#597 adds fixed, not fixed and unknown).
type PastTryVerdict string

// PastTryError is a try whose run could not be carried out: a commit that is gone, a lost box, a
// prepare or setup that failed, or any other infrastructure failure. It is never a failing try.
const PastTryError PastTryVerdict = "error"

// Bounds of past-item tries (ADR 0039 decision 11).
const (
	// MaxPastTryItems is how many of the items a proposal cites it is tried on, newest first.
	MaxPastTryItems = 5
	// MaxPastTryDiff bounds the diff a try stores, in bytes (tries.diff).
	MaxPastTryDiff = 1 << 20
	// MaxPastTryError bounds a try's error, in characters (tries.error).
	MaxPastTryError = 2000
)

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

// PastTryResult is how a claimed try ended: done, with no verdict and its run's diff and cost, or
// with verdict error and why.
type PastTryResult struct {
	Verdict  PastTryVerdict
	Error    string
	Diff     string
	CostUSD  float64
	TryRunID RunID
	At       time.Time
}

// Check reports what makes r impossible to store, wrapping ErrInvalid: a verdict other than
// error, an error verdict without its reason, a negative cost, or a diff or error past its bound.
func (r PastTryResult) Check() error {
	switch {
	case r.Verdict != "" && r.Verdict != PastTryError:
		return fmt.Errorf("past try result: verdict %q: %w", r.Verdict, ErrInvalid)
	case r.Verdict == PastTryError && r.Error == "":
		return fmt.Errorf("past try result: an error needs its reason: %w", ErrInvalid)
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
