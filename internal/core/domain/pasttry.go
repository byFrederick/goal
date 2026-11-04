package domain

import (
	"encoding/json"
	"fmt"
	"slices"
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
	// ModelTriesPerItem is how many tries a model proposal (Try another model, ADR 0039 decision
	// 13) gets per item, and the highest attempt a try of one item may have (tries.attempt).
	ModelTriesPerItem = 3
)

// PastTryStartRef is the ref prepare points at the commit a try's agent starts from, once the
// recorded commit is checked out and brought up to date with its base (apps/worker/src/prepare.ts).
const PastTryStartRef = "refs/goal/try-start"

// PastTryDiffScript is the shell command the engine runs in a try's workspace once its agent
// ended: the change against PastTryStartRef, new files included, committed or not.
const PastTryDiffScript = "git add --all --intent-to-add && git diff --no-color --no-ext-diff --binary " + PastTryStartRef

// PastTry is one try on a past item: a rerun of Station, where a change reaches, from what
// OriginalRun recorded (its brief and commit), in a box with only the model key. A proposal's try
// runs on the proposal's version and edits; a publish's try of a kept test (ADR 0039 decision 12)
// has no proposal and keeps its own VersionID and Edits, the draft as whole files.
type PastTry struct {
	ID        PastTryID
	FactoryID FactoryID
	// ProposalID is the proposal tried; empty for a publish's try.
	ProposalID ProposalID
	// ProposalNumber is the proposal's number in its factory, read with the try; 0 without one.
	ProposalNumber int64
	// KeptTestID is the kept test the try runs, empty for a try of an item a proposal cites.
	KeptTestID KeptTestID
	// ContentKey is a kept-test try's domain.ContentKey of the definition it runs on.
	ContentKey string
	// VersionID and Edits are what a publish's try runs on; empty for a proposal's.
	VersionID DefinitionVersionID
	Edits     []ProposalEdit
	ItemID    ItemID
	// Attempt numbers the proposal's tries of the item, from 1: a model proposal has
	// ModelTriesPerItem of each.
	Attempt       int
	Station       string
	OriginalRunID RunID
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

// NewPastTry is one try to queue. Attempt 0 is the first, as 1 is. A kept test's try names the
// test and the ContentKey of what it runs on; a publish's has no proposal.
type NewPastTry struct {
	ProposalID    ProposalID
	KeptTestID    KeptTestID
	ContentKey    string
	ItemID        ItemID
	Attempt       int
	Station       string
	OriginalRunID RunID
	Model         string
}

// Check reports what makes n impossible to queue, wrapping ErrInvalid.
func (n NewPastTry) Check() error {
	if (n.ProposalID == "" && n.KeptTestID == "") || n.ItemID == "" || n.Station == "" || n.OriginalRunID == "" || n.Model == "" {
		return fmt.Errorf("past try: it needs a proposal or a kept test, an item, a station, an original run and a model: %w", ErrInvalid)
	}

	if n.KeptTestID != "" && (n.ContentKey == "" || len(n.ContentKey) > MaxContentKey) {
		return fmt.Errorf("past try: a kept test's try needs its content key: %w", ErrInvalid)
	}

	if n.Attempt < 0 || n.Attempt > ModelTriesPerItem {
		return fmt.Errorf("past try: attempt %d is not 1 to %d: %w", n.Attempt, ModelTriesPerItem, ErrInvalid)
	}

	return nil
}

// TryAttempt is n's attempt as stored: 1 when it names none.
func (n NewPastTry) TryAttempt() int { return max(n.Attempt, 1) }

// MaxContentKey bounds a content key, in bytes (tries.content_key).
const MaxContentKey = 100

// ContentKey is the key of a definition's content, files by path: ManifestSHA256 of their
// manifest. Two definitions share a key exactly when their files match, so a publish of a tried
// proposal's content finds the proposal's kept-test tries (ADR 0039 decision 12).
func ContentKey(files map[string][]byte) string {
	m := make(Manifest, len(files))
	for p, b := range files {
		m[p] = HashBlob(b)
	}

	return string(ManifestSHA256(m))
}

// ApplyEdits is files with edits applied in order, as a proposal's try runs on them: a new map,
// files unchanged. ErrEditDoesNotApply when an edit does not apply.
func ApplyEdits(files map[string][]byte, edits []ProposalEdit) (map[string][]byte, error) {
	out := make(map[string][]byte, len(files)+len(edits))
	for p, b := range files {
		out[p] = b
	}

	for _, e := range edits {
		content, ok := out[e.Path]

		edited, err := e.Apply(content, ok)
		if err != nil {
			return nil, err
		}

		out[e.Path] = []byte(edited)
	}

	return out, nil
}

// KeptTestResult is how a kept test's try came out (ADR 0039 decision 12).
type KeptTestResult string

// Kept-test results: running while its try is queued or runs, passed when it passed code grading
// and, at a gate, approved (verdict fixed), failed otherwise (not_fixed), and error when it could
// not be carried out, never failed.
const (
	KeptTestRunning KeptTestResult = "running"
	KeptTestPassed  KeptTestResult = "passed"
	KeptTestFailed  KeptTestResult = "failed"
	KeptTestError   KeptTestResult = "error"
)

// KeptResult is t's result as a kept test's try: running until it is done, then by its verdict.
// A done try with no verdict or unknown, which a kept test's never is, is error.
func (t PastTry) KeptResult() KeptTestResult {
	switch {
	case t.State != PastTryDone:
		return KeptTestRunning
	case t.Verdict == PastTryFixed:
		return KeptTestPassed
	case t.Verdict == PastTryNotFixed:
		return KeptTestFailed
	default:
		return KeptTestError
	}
}

// KeptResultOf is the result of a kept test's tries of one content, as a publish shows it: running
// while any runs, failed when any failed, passed when any passed, error otherwise, with the try
// that decided it; false when there are none.
func KeptResultOf(tries []PastTry) (KeptTestResult, PastTry, bool) {
	for _, r := range []KeptTestResult{KeptTestRunning, KeptTestFailed, KeptTestPassed, KeptTestError} {
		for _, t := range tries {
			if t.KeptResult() == r {
				return r, t, true
			}
		}
	}

	return "", PastTry{}, false
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

// AgentItem is one of an agent's finished items a model proposal is tried on (ADR 0039 decision
// 13), with how it went on the line: Run is the item's newest finished run of the agent that was a
// claim's first, the run a try reruns; SentBackAt are the stations of its rejected and
// gate_rejected events, oldest first, one per send-back; CostUSD is what the item cost.
type AgentItem struct {
	ItemID     ItemID
	ItemNumber int64
	ItemTitle  string
	Run        Run
	SentBackAt []string
	CostUSD    float64
	DoneAt     time.Time
}

// LineResult is how a went on the line, in words: "passed first time, $0.42" or "sent back 2
// times at review, $1.10".
func (a AgentItem) LineResult() string {
	cost := fmt.Sprintf("$%.2f", a.CostUSD)

	switch n := len(a.SentBackAt); n {
	case 0:
		return "passed first time, " + cost
	case 1:
		return "sent back once at " + a.SentBackAt[0] + ", " + cost
	default:
		return fmt.Sprintf("sent back %d times at %s, %s", n, joinStations(a.SentBackAt), cost)
	}
}

// joinStations is the distinct stations of at, in order, joined as a list in words.
func joinStations(at []string) string {
	var distinct []string

	for _, s := range at {
		if !slices.Contains(distinct, s) {
			distinct = append(distinct, s)
		}
	}

	if len(distinct) == 1 {
		return distinct[0]
	}

	return strings.Join(distinct[:len(distinct)-1], ", ") + " and " + distinct[len(distinct)-1]
}
