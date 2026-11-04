package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// TriggerCheck is a schedule trigger's check (ADR 0040 decision 19): instead of filing an item
// each time the trigger fires, it queues a check, a run of Agent that looks at the source with
// the agent's environment and answers what to file (CheckAnswer).
type TriggerCheck struct {
	// Agent is an agent of the factory; the check runs with its environment, tools and model.
	Agent string
	// Brief is what to look for and what is worth an item: the text, a brief file's read.
	Brief string
	// MaxUSD is the most one check may spend, and Timeout how long it may run.
	MaxUSD  float64
	Timeout time.Duration
}

// What a check takes when its trigger sets none, and the most often its cron may fire.
const (
	DefaultCheckMaxUSD  = 0.10
	DefaultCheckTimeout = 5 * time.Minute
	CheckCronFloor      = 5 * time.Minute
)

// CheckState is where a checked firing's check is.
type CheckState string

// A check is queued when its trigger fires, running under a lease, and done with its answer's
// decisions or failed with the reason.
const (
	CheckQueued  CheckState = "queued"
	CheckRunning CheckState = "running"
	CheckDone    CheckState = "done"
	CheckFailed  CheckState = "failed"
)

// Live reports whether a check in state s is still in flight: queued or running.
func (s CheckState) Live() bool { return s == CheckQueued || s == CheckRunning }

// The reasons goald records for a check it does not run, or that ended without an answer.
const (
	// StillChecking skips a firing while the trigger's check is queued or running.
	StillChecking = "still checking"
	// CheckLinePaused skips a firing while the factory's line is paused.
	CheckLinePaused = "the line is paused"
	// CheckLeaseExpired fails a check whose lease ran out.
	CheckLeaseExpired = "lease expired"
)

// Limits of a check's input and answer.
const (
	// MaxCheckState is the most bytes of state a check saves for the next one.
	MaxCheckState = 4096
	// MaxCheckOpenItems is the most open items of its trigger a check is given.
	MaxCheckOpenItems = 50
	// CheckClosedWindow is how far back a check is given the keys whose items closed, and
	// MaxCheckClosed the most of them.
	CheckClosedWindow = 7 * 24 * time.Hour
	MaxCheckClosed    = 200
)

// CheckLease is a running check's lease: its firing, the instance holding it, the token that
// fences its writes, and until when it holds.
type CheckLease struct {
	FiringID  string
	FactoryID FactoryID
	Trigger   string
	Holder    string
	Token     string
	Until     time.Time
}

// CheckResult is how a check ended: done with its decisions (the answer, as CheckAnswer.Decisions
// gives it) and the state it saved, nil to keep the saved one; or failed with Reason. RunID and
// CostUSD are its run's, when one started.
type CheckResult struct {
	State     CheckState
	RunID     RunID
	CostUSD   float64
	Decisions json.RawMessage
	Saved     json.RawMessage
	Reason    string
	At        time.Time
}

// Check refuses a result that is neither done with decisions nor failed with a reason, or saves
// more than MaxCheckState bytes; the error wraps ErrInvalid.
func (r CheckResult) Check() error {
	switch {
	case r.State == CheckDone && len(r.Decisions) == 0:
		return fmt.Errorf("a done check needs its decisions: %w", ErrInvalid)
	case r.State == CheckFailed && strings.TrimSpace(r.Reason) == "":
		return fmt.Errorf("a failed check needs its reason: %w", ErrInvalid)
	case r.State != CheckDone && r.State != CheckFailed:
		return fmt.Errorf("a check ends done or failed, not %q: %w", r.State, ErrInvalid)
	case r.State == CheckFailed && (len(r.Decisions) > 0 || len(r.Saved) > 0):
		return fmt.Errorf("a failed check keeps no decisions and saves no state: %w", ErrInvalid)
	case len(r.Saved) > MaxCheckState:
		return fmt.Errorf("a check saves at most %d bytes of state: %w", MaxCheckState, ErrInvalid)
	case r.At.IsZero():
		return fmt.Errorf("a check's result needs when it ended: %w", ErrInvalid)
	}

	return nil
}

// CheckInput is what a check is given (ADR 0040 decision 19): besides its brief, the open items
// its trigger filed, the keys whose items closed in the last CheckClosedWindow, the last done
// check's decisions and the state it saved, each JSON null when there is none.
type CheckInput struct {
	Open          []CheckOpenItem `json:"open"`
	Closed        []CheckClosed   `json:"closed"`
	LastDecisions json.RawMessage `json:"last_decisions"`
	State         json.RawMessage `json:"state"`
}

// CheckOpenItem is an open item the trigger filed: the source's key it was filed for, its ref
// (#<number>), title, state and the text of its last note ("" without one).
type CheckOpenItem struct {
	Key      string `json:"key"`
	Ref      string `json:"ref"`
	Title    string `json:"title"`
	State    string `json:"state"`
	LastNote string `json:"last_note"`
}

// CheckClosed is a key whose item closed: how (done or cancelled), why (the reason given, "" for
// none) and when.
type CheckClosed struct {
	Key string    `json:"key"`
	Ref string    `json:"ref"`
	How string    `json:"how"`
	Why string    `json:"why"`
	At  time.Time `json:"at"`
}

// CheckAnswer is a check's answer, in a fixed contract like a plan's (CheckAnswerSchema): the
// items to file, notes for open items, what it looked at and left, and the state the next check
// gets.
type CheckAnswer struct {
	File  []CheckFile     `json:"file"`
	Note  []CheckNote     `json:"note"`
	Skip  []CheckSkip     `json:"skip"`
	State json.RawMessage `json:"state,omitempty"`
}

// CheckFile is an item to file: the source's key, a title and a brief; optionally its type, lane,
// priority, repositories and start (a time or a wait, as an item's), and reopen, the reason to
// file a key again whose item closed.
type CheckFile struct {
	Key          string   `json:"key"`
	Title        string   `json:"title"`
	Brief        string   `json:"brief"`
	Type         string   `json:"type,omitempty"`
	Lane         string   `json:"lane,omitempty"`
	Priority     int      `json:"priority,omitempty"`
	Repositories []string `json:"repositories,omitempty"`
	Start        string   `json:"start,omitempty"`
	Reopen       string   `json:"reopen,omitempty"`
}

// CheckNote is a note for the open item filed for Key.
type CheckNote struct {
	Key  string `json:"key"`
	Text string `json:"text"`
}

// CheckSkip is what the check looked at and left, with why.
type CheckSkip struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
}

// Limits of a check's answer.
const (
	maxCheckKey    = 200
	maxCheckTitle  = 300
	maxCheckBrief  = 20_000
	maxCheckText   = 4000
	maxCheckReason = 500
	maxCheckList   = 100
)

// CheckAnswerSchema is the answer contract as a JSON Schema, which the gateway gives a check's
// agent as its output schema; ParseCheckAnswer checks the same rules and more.
const CheckAnswerSchema = `{
  "type": "object",
  "required": ["file", "note", "skip"],
  "additionalProperties": false,
  "properties": {
    "file": {
      "type": "array",
      "description": "Items to file, one per key.",
      "items": {
        "type": "object",
        "required": ["key", "title", "brief"],
        "additionalProperties": false,
        "properties": {
          "key": {"type": "string", "description": "The source's own id for it, such as an incident's."},
          "title": {"type": "string"},
          "brief": {"type": "string"},
          "type": {"type": "string"},
          "lane": {"type": "string"},
          "priority": {"type": "integer"},
          "repositories": {"type": "array", "items": {"type": "string"}},
          "start": {"type": "string", "description": "When the item may start: a time, or a wait such as 36h, 7d or 2w."},
          "reopen": {"type": "string", "description": "Why to file a key again whose item closed."}
        }
      }
    },
    "note": {
      "type": "array",
      "description": "Notes for open items, by key.",
      "items": {
        "type": "object",
        "required": ["key", "text"],
        "additionalProperties": false,
        "properties": {"key": {"type": "string"}, "text": {"type": "string"}}
      }
    },
    "skip": {
      "type": "array",
      "description": "What you looked at and left, with why.",
      "items": {
        "type": "object",
        "required": ["key", "reason"],
        "additionalProperties": false,
        "properties": {"key": {"type": "string"}, "reason": {"type": "string"}}
      }
    },
    "state": {"type": "object", "description": "What the next check gets, at most 4 KiB."}
  }
}`

// ParseCheckAnswer reads a check's answer from its run's output: file, note and skip lists (each
// absent list empty), each entry with its key and its required text within its limits, a file's
// start a time or a wait, at most one entry per key in each list, and a state that is a JSON
// object of at most MaxCheckState bytes. Anything else is ErrInvalid with the reason.
func ParseCheckAnswer(output json.RawMessage) (CheckAnswer, error) {
	dec := json.NewDecoder(bytes.NewReader(output))
	dec.DisallowUnknownFields()

	var a CheckAnswer
	if err := dec.Decode(&a); err != nil {
		return CheckAnswer{}, fmt.Errorf("check answer: %w: %w", ErrInvalid, err)
	}

	if err := a.check(); err != nil {
		return CheckAnswer{}, fmt.Errorf("check answer: %w", err)
	}

	a.File, a.Note, a.Skip = nonNilSlice(a.File), nonNilSlice(a.Note), nonNilSlice(a.Skip)

	return a, nil
}

// Decisions is a as the firing keeps it: the answer without its state, which goes to
// trigger_state.
func (a CheckAnswer) Decisions() json.RawMessage {
	b, _ := json.Marshal(CheckAnswer{File: nonNilSlice(a.File), Note: nonNilSlice(a.Note), Skip: nonNilSlice(a.Skip)}) // plain fields: never fails

	return b
}

func (a CheckAnswer) check() error {
	if err := checkState(a.State); err != nil {
		return err
	}

	files := map[string]bool{}

	for i, f := range a.File {
		switch {
		case badText(f.Key, maxCheckKey) || badText(f.Title, maxCheckTitle) || badText(f.Brief, maxCheckBrief):
			return fmt.Errorf("file %d needs a key, a title and a brief of at most %d, %d and %d characters: %w", i, maxCheckKey, maxCheckTitle, maxCheckBrief, ErrInvalid)
		case files[f.Key]:
			return fmt.Errorf("file %d: key %q is filed twice: %w", i, f.Key, ErrInvalid)
		case len(f.Repositories) > maxCheckList:
			return fmt.Errorf("file %d names more than %d repositories: %w", i, maxCheckList, ErrInvalid)
		case utf8.RuneCountInString(f.Reopen) > maxCheckReason:
			return fmt.Errorf("file %d: reopen is at most %d characters: %w", i, maxCheckReason, ErrInvalid)
		}

		if err := checkStart(f.Start); err != nil {
			return fmt.Errorf("file %d: %w", i, err)
		}

		files[f.Key] = true
	}

	for i, n := range a.Note {
		if badText(n.Key, maxCheckKey) || badText(n.Text, maxCheckText) {
			return fmt.Errorf("note %d needs a key and a text of at most %d and %d characters: %w", i, maxCheckKey, maxCheckText, ErrInvalid)
		}
	}

	for i, s := range a.Skip {
		if badText(s.Key, maxCheckKey) || badText(s.Reason, maxCheckReason) {
			return fmt.Errorf("skip %d needs a key and a reason of at most %d and %d characters: %w", i, maxCheckKey, maxCheckReason, ErrInvalid)
		}
	}

	if len(a.File)+len(a.Note)+len(a.Skip) > 3*maxCheckList {
		return fmt.Errorf("an answer has at most %d entries: %w", 3*maxCheckList, ErrInvalid)
	}

	return nil
}

// checkState refuses a saved state that is not a JSON object or is over MaxCheckState bytes; none
// is fine.
func checkState(s json.RawMessage) error {
	if len(s) == 0 || string(s) == "null" {
		return nil
	}

	if len(s) > MaxCheckState {
		return fmt.Errorf("state is %d bytes, over %d: %w", len(s), MaxCheckState, ErrInvalid)
	}

	var obj map[string]json.RawMessage
	if json.Unmarshal(s, &obj) != nil || obj == nil {
		return fmt.Errorf("state must be a JSON object: %w", ErrInvalid)
	}

	return nil
}

// checkStart refuses a start that is neither a time nor a wait; none is fine.
func checkStart(s string) error {
	if s == "" {
		return nil
	}

	if _, err := ParseWait(s); err == nil {
		return nil
	}

	_, err := ParseStartTime(s, nil)

	return err
}

// badText reports whether s is blank or longer than n characters.
func badText(s string, n int) bool {
	return strings.TrimSpace(s) == "" || utf8.RuneCountInString(s) > n
}

func nonNilSlice[T any](s []T) []T {
	if s == nil {
		return []T{}
	}

	return s
}
