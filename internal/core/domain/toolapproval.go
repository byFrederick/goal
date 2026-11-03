package domain

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"
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
// organization's mode, so each offers Always allow. A rule keyed by server lifts nothing here. The
// definition's per-tool rule has no input here: no definition can carry one today.
func EffectiveToolModes(modes map[string]ConnectionToolMode, rules []StandingRule, connection ConnectionID, agent string) RunToolModes {
	return effectiveToolModes(modes, rules, func(r StandingRule, tool string) bool {
		return r.ConnectionID != "" && r.ConnectionID == connection && r.Tool == tool && r.Agent == agent
	})
}

// ToolModeEntry is one entry of an MCP server's tool modes in a run: a tool name or a glob, as
// path.Match reads it, and its mode.
type ToolModeEntry struct {
	Tool string
	Mode ConnectionToolMode
}

// ServerToolModes are the modes of one MCP server's tools for agent, the agent's role, in a run on
// secrets (ADR 0037 decisions 8 and 12), as an ordered list: a tool's mode is the first entry whose
// pattern matches it, and a tool no entry matches is allow.
//
// file is the environment file's mcp.<server>.tools, a tool name or glob to its mode, where the
// most specific pattern wins: an exact name beats any glob, a glob with more characters other than
// * beats one with fewer, and a tie goes to the pattern that sorts first. An active standing rule
// for (factory, environment, server, tool, agent) whose tool resolves to ask in file lifts that
// tool to allow; nothing lifts off. limit is the organization policy's cap in the same form: the
// stricter of a tool's mode and the cap's wins (off, then ask, then allow), after any rule.
//
// Where a cap pattern and a file pattern match the same tool, the list folds both in; when the
// tools a glob's entry covers can resolve to different caps, its entry takes the strictest of them,
// so the list is never looser than the cap. Every ask in the list offers Always allow.
func ServerToolModes(
	file map[string]string, limit map[string]ConnectionToolMode, rules []StandingRule, factory FactoryID, environment, server, agent string,
) []ToolModeEntry {
	modes := make(map[string]ConnectionToolMode, len(file))
	for p, m := range file {
		modes[p] = ConnectionToolMode(m)
	}

	lifted := map[string]bool{}

	for _, r := range rules {
		if r.Active() && r.ConnectionID == "" && r.Factory == factory && r.Environment == environment && r.Server == server &&
			r.Agent == agent && resolveToolMode(modes, r.Tool) == ToolModeAsk {
			lifted[r.Tool] = true
		}
	}

	patterns := slices.Collect(maps.Keys(modes))
	for p := range limit {
		if _, ok := modes[p]; !ok {
			patterns = append(patterns, p)
		}
	}

	for tool := range lifted {
		if _, ok := modes[tool]; !ok {
			if _, ok := limit[tool]; !ok {
				patterns = append(patterns, tool)
			}
		}
	}

	slices.SortFunc(patterns, compareToolPatterns)

	out := make([]ToolModeEntry, 0, len(patterns))

	for i, p := range patterns {
		mode := modeFor(modes, patterns[i+1:], p)
		if lifted[p] {
			mode = ToolModeAllow
		}

		out = append(out, ToolModeEntry{Tool: p, Mode: stricterMode(mode, modeFor(limit, patterns[i+1:], p))})
	}

	return out
}

// modeFor is the mode modes give the tools whose first entry is p, the patterns after p being
// later: p's own mode when modes has it, the mode of exact name p, or otherwise the strictest of
// the later patterns of modes that may match a tool p matches, and allow.
func modeFor(modes map[string]ConnectionToolMode, later []string, p string) ConnectionToolMode {
	if m, ok := modes[p]; ok {
		return m
	}

	if !isToolGlob(p) {
		return resolveToolMode(modes, p)
	}

	mode := ToolModeAllow

	for _, q := range later {
		if m, ok := modes[q]; ok && toolGlobsOverlap(p, q) {
			mode = stricterMode(mode, m)
		}
	}

	return mode
}

// resolveToolMode is tool's mode in modes: its most specific matching pattern's, and allow when
// none matches.
func resolveToolMode(modes map[string]ConnectionToolMode, tool string) ConnectionToolMode {
	patterns := slices.SortedFunc(maps.Keys(modes), compareToolPatterns)
	for _, p := range patterns {
		if ok, err := path.Match(p, tool); err == nil && ok {
			return modes[p]
		}
	}

	return ToolModeAllow
}

// compareToolPatterns orders tool patterns most specific first: exact names, then globs by their
// count of characters other than *, more first, then by the patterns themselves.
func compareToolPatterns(a, b string) int {
	if ga, gb := isToolGlob(a), isToolGlob(b); ga != gb {
		if ga {
			return 1
		}

		return -1
	}

	return cmp.Or(cmp.Compare(len(b)-strings.Count(b, "*"), len(a)-strings.Count(a, "*")), strings.Compare(a, b))
}

// isToolGlob reports whether p holds a character path.Match reads as more than itself.
func isToolGlob(p string) bool { return strings.ContainsAny(p, `*?[\`) }

// toolModeRank orders modes by strictness; anything but allow and ask counts as off.
func toolModeRank(m ConnectionToolMode) int {
	switch m {
	case ToolModeAllow:
		return 0
	case ToolModeAsk:
		return 1
	default:
		return 2
	}
}

// stricterMode is the stricter of a and b.
func stricterMode(a, b ConnectionToolMode) ConnectionToolMode {
	if toolModeRank(b) > toolModeRank(a) {
		return b
	}

	return a
}

// toolGlobsOverlap reports whether a tool name may match both globs. It is exact for *, ? and
// literal characters, and counts a character class as matching any character, so it may say true
// where no name matches both, never false where one does.
func toolGlobsOverlap(a, b string) bool {
	x, y := globTokens(a), globTokens(b)
	seen := map[[2]int]bool{}

	var overlap func(i, j int) bool

	overlap = func(i, j int) bool {
		if done, ok := seen[[2]int{i, j}]; ok {
			return done
		}

		var r bool

		switch {
		case i < len(x) && x[i] == "*":
			r = overlap(i+1, j) || (j < len(y) && overlap(i, j+1))
		case j < len(y) && y[j] == "*":
			r = overlap(i, j+1) || (i < len(x) && overlap(i+1, j))
		case i == len(x) || j == len(y):
			r = i == len(x) && j == len(y)
		default:
			r = globCharsOverlap(x[i], y[j]) && overlap(i+1, j+1)
		}

		seen[[2]int{i, j}] = r

		return r
	}

	return overlap(0, 0)
}

// globTokens splits a glob into tokens: "*", "?", a character class "[…]" or one literal
// character, an escaped one without its backslash.
func globTokens(p string) []string {
	var out []string

	for i := 0; i < len(p); i++ {
		switch p[i] {
		case '*', '?':
			out = append(out, p[i:i+1])
		case '[':
			end := strings.IndexByte(p[i:], ']')
			if end < 0 {
				end = len(p) - i - 1
			}

			out = append(out, p[i:i+end+1])
			i += end
		case '\\':
			if i+1 < len(p) {
				i++
			}

			out = append(out, "\\"+p[i:i+1])
		default:
			out = append(out, "\\"+p[i:i+1])
		}
	}

	return out
}

// globCharsOverlap reports whether one character may match both tokens: two literals only when
// equal, a literal and a class when the class matches it, and anything else always.
func globCharsOverlap(a, b string) bool {
	la, lb := strings.HasPrefix(a, "\\"), strings.HasPrefix(b, "\\")

	switch {
	case la && lb:
		return a == b
	case la && strings.HasPrefix(b, "["):
		ok, err := path.Match(b, a[1:])
		return err != nil || ok
	case lb && strings.HasPrefix(a, "["):
		ok, err := path.Match(a, b[1:])
		return err != nil || ok
	default:
		return true
	}
}

// effectiveToolModes are modes with each ask that an active rule of rules matches (lifts) turned
// into allow.
func effectiveToolModes(modes map[string]ConnectionToolMode, rules []StandingRule, lifts func(r StandingRule, tool string) bool) RunToolModes {
	out := RunToolModes{Modes: map[string]ConnectionToolMode{}, AlwaysAllowOffered: []string{}}

	for _, tool := range slices.Sorted(maps.Keys(modes)) {
		switch modes[tool] {
		case ToolModeOff:
			out.Modes[tool] = ToolModeOff
		case ToolModeAsk:
			if slices.ContainsFunc(rules, func(r StandingRule) bool { return r.Active() && lifts(r, tool) }) {
				continue
			}

			out.Modes[tool] = ToolModeAsk
			out.AlwaysAllowOffered = append(out.AlwaysAllowOffered, tool)
		case ToolModeAllow:
		}
	}

	return out
}
