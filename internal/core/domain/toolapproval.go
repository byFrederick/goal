package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"time"
)

// Event data keys of tool approvals (EventToolApprovalRequested, EventToolApprovalGranted,
// EventToolApprovalDenied); agent and run_id are KeyAgent and KeyRunID.
const (
	KeyConnection         = "connection"
	KeyTool               = "tool"
	KeyInput              = "input"
	KeyAlwaysAllowOffered = "always_allow_offered"
	KeyApprovalID         = "approval_id"
	KeyAlwaysAllow        = "always_allow"
	KeyNote               = "note"
)

// ToolApproval is a run's call of an Ask-first connection tool that waits for a person (ADR 0031
// decision 4), read from its tool_approval_requested event.
type ToolApproval struct {
	// EventID is the tool_approval_requested event's id: the approval's id.
	EventID int64
	// Connection is the organization connection's name.
	Connection string
	Tool       string
	// Input is the call's input as canonical JSON (CanonicalJSON).
	Input json.RawMessage
	// Agent is the calling agent's role name, never its slot.
	Agent   string
	RunID   RunID
	Station string
	At      time.Time
	// AlwaysAllowOffered is whether a person may approve the call with Always allow: its ask
	// comes from the organization connection's mode (ADR 0031 decision 3).
	AlwaysAllowOffered bool
}

// ToolDecision is a person's decision on a tool approval, read from its tool_approval_granted
// or tool_approval_denied event.
type ToolDecision struct {
	Approval ToolApproval
	Granted  bool
	// AlwaysAllow is whether the approval also created a standing rule.
	AlwaysAllow bool
	// Note is the person's reason for a denial, when they gave one.
	Note string
	// By is the person's name, as the event's actor.
	By string
	At time.Time
}

// GrantedCall is one call a person approved, which the run after the approval may make once
// (ADR 0031 decision 5).
type GrantedCall struct {
	Connection string
	Tool       string
	// Input is canonical JSON: the run's call must match it exactly.
	Input json.RawMessage
}

// toolDecisions are the events that decide a tool approval.
var toolDecisions = []EventKind{EventToolApprovalGranted, EventToolApprovalDenied}

// OpenToolApproval finds an item's open tool approval in its events, in append order: the latest
// tool_approval_requested with no tool_approval_granted, tool_approval_denied, moved, paused,
// rejected or cancelled after it (ADR 0031 decision 9).
func OpenToolApproval(events []Event) (ToolApproval, bool) {
	for _, e := range slices.Backward(events) {
		switch {
		case e.Kind == EventToolApprovalRequested:
			return ToolApprovalOf(e), true
		case slices.Contains(toolDecisions, e.Kind) || slices.Contains(withdraws, e.Kind):
			return ToolApproval{}, false
		}
	}

	return ToolApproval{}, false
}

// ToolDecisionsSinceLastRun returns the tool approvals decided after the item's latest
// run_started event, each with its decision, in the order they were decided: what the next
// run's brief carries.
func ToolDecisionsSinceLastRun(events []Event) []ToolDecision {
	since := 0

	for i, e := range slices.Backward(events) {
		if e.Kind == EventRunStarted {
			since = i + 1
			break
		}
	}

	return toolDecisionsIn(events, since, len(events))
}

// GrantedCalls returns the calls granted to run: those approved after the item's run before it
// started and before run started, the run a grant goes to (ADR 0031 decision 5). A grant the run
// does not use ends with it, so a later run gets none; a run with no run_started event gets none.
func GrantedCalls(events []Event, run RunID) []GrantedCall {
	start := slices.IndexFunc(events, func(e Event) bool { return e.Kind == EventRunStarted && e.RunID == run })
	if start < 0 {
		return nil
	}

	since := 0

	for i, e := range slices.Backward(events[:start]) {
		if e.Kind == EventRunStarted {
			since = i + 1
			break
		}
	}

	var calls []GrantedCall

	for _, d := range toolDecisionsIn(events, since, start) {
		if d.Granted {
			calls = append(calls, GrantedCall{Connection: d.Approval.Connection, Tool: d.Approval.Tool, Input: d.Approval.Input})
		}
	}

	return calls
}

// toolDecisionsIn returns the decisions among events[from:to] on approvals requested anywhere
// before them.
func toolDecisionsIn(events []Event, from, to int) []ToolDecision {
	var (
		decisions []ToolDecision
		requested *Event
	)

	for i, e := range events[:to] {
		switch {
		case e.Kind == EventToolApprovalRequested:
			requested = &events[i]
		case slices.Contains(toolDecisions, e.Kind) && requested != nil:
			if i >= from {
				decisions = append(decisions, toolDecisionOf(ToolApprovalOf(*requested), e))
			}

			requested = nil
		case slices.Contains(withdraws, e.Kind):
			requested = nil
		}
	}

	return decisions
}

// ToolApprovalOf reads a tool_approval_requested event. Its data may come from JSON, so the input
// is any JSON value, held as itself or as its encoding.
func ToolApprovalOf(e Event) ToolApproval {
	a := ToolApproval{EventID: e.ID, Station: e.Station, At: e.At, RunID: e.RunID}
	a.Connection, _ = e.Data[KeyConnection].(string)
	a.Tool, _ = e.Data[KeyTool].(string)
	a.Agent, _ = e.Data[KeyAgent].(string)
	a.AlwaysAllowOffered, _ = e.Data[KeyAlwaysAllowOffered].(bool)

	if run, ok := e.Data[KeyRunID].(string); ok && run != "" {
		a.RunID = RunID(run)
	}

	var raw []byte

	switch in := e.Data[KeyInput].(type) {
	case nil:
	case json.RawMessage:
		raw = in
	case []byte:
		raw = in
	default:
		raw, _ = json.Marshal(in)
	}

	if canonical, err := CanonicalJSON(raw); err == nil {
		a.Input = canonical
	}

	return a
}

func toolDecisionOf(a ToolApproval, e Event) ToolDecision {
	d := ToolDecision{Approval: a, Granted: e.Kind == EventToolApprovalGranted, By: e.Actor, At: e.At}
	d.AlwaysAllow, _ = e.Data[KeyAlwaysAllow].(bool)
	d.Note, _ = e.Data[KeyNote].(string)

	return d
}

// CanonicalJSON is raw as canonical JSON (ADR 0031 decision 5): object keys sorted, no
// insignificant whitespace, numbers as written and no HTML escaping. Two encodings of the same
// value are equal as canonical JSON. Anything but one JSON value wraps ErrInvalid.
func CanonicalJSON(raw []byte) (json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("tool input is not JSON: %w: %w", ErrInvalid, err)
	}

	if dec.More() {
		return nil, fmt.Errorf("tool input holds more than one JSON value: %w", ErrInvalid)
	}

	var b bytes.Buffer

	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)

	// A map's keys are sorted when encoded, and the encoder writes no whitespace.
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("encode tool input: %w", err)
	}

	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}

// RunToolModes are the modes of one connection's tools for one agent role in a run (ADR
// 0031 decision 3).
type RunToolModes struct {
	// Modes are the tools that ask first or are off; a tool not in it is ToolModeAllow.
	Modes map[string]ConnectionToolMode
	// AlwaysAllowOffered are the tools, sorted, whose ask a person may answer with Always allow.
	AlwaysAllowOffered []string
}

// EffectiveToolModes combines the organization connection's modes with the organization's
// standing rules for agent, the agent's role: an active rule for (connection, tool, agent) turns
// an ask into allow, and nothing turns off into anything. Every ask that remains comes from the
// organization's mode, so each offers Always allow. The definition's per-tool rule has no input
// here: no definition can carry one today.
func EffectiveToolModes(modes map[string]ConnectionToolMode, rules []StandingRule, connection ConnectionID, agent string) RunToolModes {
	out := RunToolModes{Modes: map[string]ConnectionToolMode{}, AlwaysAllowOffered: []string{}}

	for _, tool := range slices.Sorted(maps.Keys(modes)) {
		switch modes[tool] {
		case ToolModeOff:
			out.Modes[tool] = ToolModeOff
		case ToolModeAsk:
			if slices.ContainsFunc(rules, func(r StandingRule) bool {
				return r.Active() && r.ConnectionID == connection && r.Tool == tool && r.Agent == agent
			}) {
				continue
			}

			out.Modes[tool] = ToolModeAsk
			out.AlwaysAllowOffered = append(out.AlwaysAllowOffered, tool)
		case ToolModeAllow:
		}
	}

	return out
}
