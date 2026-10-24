package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// RunID identifies one agent run (one container working one item at one station).
type RunID string

// Lease is an agent slot's hold on an item. It expires unless heartbeated, so work held by a
// crashed instance returns to the queue.
type Lease struct {
	ItemID    ItemID
	FactoryID FactoryID
	Station   string
	Agent     string
	// Slot is the station slot in [0, capacity); a slot holds at most one lease.
	Slot int
	// AcquiredAt is set once per claim and fences the lease: a transition or heartbeat that
	// names an older AcquiredAt for the same item no longer holds the lease.
	AcquiredAt  time.Time
	HeartbeatAt time.Time
	ExpiresAt   time.Time
}

// Expired reports whether the lease has lapsed at now.
func (l Lease) Expired(now time.Time) bool { return !now.Before(l.ExpiresAt) }

// RunStatus is an agent run's outcome. Values match agent_runs.status.
type RunStatus string

// Run statuses.
const (
	RunRunning   RunStatus = "running"
	RunSucceeded RunStatus = "succeeded"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
)

// Run is one agent run as recorded by the store.
type Run struct {
	ID        RunID
	FactoryID FactoryID
	// DefinitionVersionID is the definition version the run started and finished on; empty
	// for runs from before versions ("version unknown").
	DefinitionVersionID DefinitionVersionID
	ItemID              ItemID
	Station             string
	Agent               string
	Model               string
	Result              RunResult
	StartedAt           time.Time
	EndedAt             time.Time
}

// RunResult is what a runner returns when an agent task ends.
type RunResult struct {
	Status RunStatus
	// Output is the agent's final JSON output; it must satisfy the station's contract.
	Output       json.RawMessage
	CostUSD      float64
	Turns        int
	InputTokens  int64
	OutputTokens int64
	SessionID    string
	// Error explains a failed or cancelled run.
	Error string
}

// Decision is a gate's ruling on an item.
type Decision string

// Gate decisions.
const (
	Approve Decision = "approve"
	Reject  Decision = "reject"
)

// Verdict is a gate station's output: {"decision": "approve"|"reject", "reason": "..."}.
type Verdict struct {
	Decision Decision `json:"decision"`
	Reason   string   `json:"reason,omitempty"`
}

// ErrBadVerdict means a gate station's output is not a usable verdict.
var ErrBadVerdict = errors.New("bad verdict")

// ParseVerdict reads a gate verdict from a run's output. A rejection must carry a reason,
// because the reason is what the upstream agent acts on.
func ParseVerdict(output json.RawMessage) (Verdict, error) {
	var v Verdict
	if err := json.Unmarshal(output, &v); err != nil {
		return Verdict{}, fmt.Errorf("%w: %w", ErrBadVerdict, err)
	}

	switch v.Decision {
	case Approve:
		return v, nil
	case Reject:
		if v.Reason == "" {
			return Verdict{}, fmt.Errorf("%w: a rejection needs a reason", ErrBadVerdict)
		}

		return v, nil
	}

	return Verdict{}, fmt.Errorf("%w: decision %q is not approve or reject", ErrBadVerdict, v.Decision)
}

// LaneFrom returns the "lane" an output declares (for example a spec choosing its engineer),
// or "" when the output does not declare one.
func LaneFrom(output json.RawMessage) string {
	var v struct {
		Lane string `json:"lane"`
	}

	if json.Unmarshal(output, &v) != nil {
		return ""
	}

	return v.Lane
}
