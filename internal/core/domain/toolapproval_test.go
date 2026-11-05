package domain

import (
	"encoding/json"
	"errors"
	"path"
	"reflect"
	"testing"
	"time"
)

// The tests' tool approvals: the agent role, its run and the connection's tools.
const (
	roleArchitect        = "architect"
	runOne         RunID = "run-1"
	toolCloseIssue       = "close_issue"
	toolEditIssue        = "edit_issue"
	toolLabelIssue       = "label_issue"
	toolDeleteRepo       = "delete_repo"
	globCreate           = "create_*"
	globDelete           = "delete_*"
)

// requested is a tool_approval_requested event as the engine writes it; input is its data as the
// store holds it (raw JSON in memory, decoded values from PostgreSQL).
func requested(id int64, tool string, input any) Event {
	return Event{
		ID: id, At: qt0.Add(time.Duration(id) * time.Minute), Kind: EventToolApprovalRequested, Station: stationBuild,
		Actor: roleArchitect, RunID: runOne,
		Data: map[string]any{
			KeyConnection: "tracker", KeyTool: tool, KeyInput: input, KeyAgent: roleArchitect, KeyRunID: runOne,
			KeyAlwaysAllowOffered: true,
		},
	}
}

func granted(id int64) Event {
	return Event{
		ID: id, At: qt0.Add(time.Duration(id) * time.Minute), Kind: EventToolApprovalGranted, Actor: answerer,
		Data: map[string]any{KeyApprovalID: id - 1, KeyAlwaysAllow: false},
	}
}

func denied(id int64, note string) Event {
	return Event{
		ID: id, At: qt0.Add(time.Duration(id) * time.Minute), Kind: EventToolApprovalDenied, Actor: answerer,
		Data: map[string]any{KeyApprovalID: id - 1, KeyNote: note},
	}
}

func started(id int64, run RunID) Event { return Event{ID: id, Kind: EventRunStarted, RunID: run} }

func TestOpenToolApproval(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		events []Event
		want   string // the open approval's tool; empty for none
	}{
		{name: "no events"},
		{name: "requested", events: []Event{ev(1, EventRunStarted), requested(2, toolCreateComment, json.RawMessage(`{}`))}, want: toolCreateComment},
		{name: "granted", events: []Event{requested(1, toolCreateComment, nil), granted(2)}},
		{name: "denied", events: []Event{requested(1, toolCreateComment, nil), denied(2, "")}},
		{name: "requested again after a grant", events: []Event{
			requested(1, toolCreateComment, nil), granted(2), ev(3, EventRunStarted), requested(4, toolCloseIssue, nil),
		}, want: toolCloseIssue},
		{name: "withdrawn by a move", events: []Event{requested(1, toolCreateComment, nil), ev(2, EventMoved)}},
		{name: "withdrawn by a pause", events: []Event{requested(1, toolCreateComment, nil), ev(2, EventPaused)}},
		{name: "withdrawn by a rejection", events: []Event{requested(1, toolCreateComment, nil), ev(2, EventRejected)}},
		{name: "withdrawn by a cancel", events: []Event{requested(1, toolCreateComment, nil), ev(2, EventCancelled)}},
		{name: "a note does not withdraw it", events: []Event{requested(1, toolCreateComment, nil), ev(2, EventNote)}, want: toolCreateComment},
		{name: "an answered question does not decide it", events: []Event{
			requested(1, toolCreateComment, nil), answered(2, "yes", nil),
		}, want: toolCreateComment},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a, ok := OpenToolApproval(tt.events)
			if ok != (tt.want != "") || a.Tool != tt.want {
				t.Errorf("OpenToolApproval = %+v, %v; want %q", a, ok, tt.want)
			}
		})
	}
}

// The input reads the same whether the store kept the raw JSON or decoded it.
func TestToolApprovalOf_ReadsTheInputAsCanonicalJSON(t *testing.T) {
	t.Parallel()

	for name, input := range map[string]any{
		"raw":     json.RawMessage(`{ "body": "LGTM", "issue": 7 }`),
		"decoded": map[string]any{"issue": float64(7), "body": "LGTM"},
	} {
		a := ToolApprovalOf(requested(3, toolCreateComment, input))
		if string(a.Input) != `{"body":"LGTM","issue":7}` || a.Connection != "tracker" || a.Agent != roleArchitect ||
			a.RunID != runOne || a.EventID != 3 || !a.AlwaysAllowOffered {
			t.Errorf("%s: ToolApprovalOf = %+v (input %s)", name, a, a.Input)
		}
	}
}

func TestGrantedCalls(t *testing.T) {
	t.Parallel()

	events := []Event{
		started(1, runOne),
		requested(2, toolCreateComment, json.RawMessage(`{"body":"one"}`)), granted(3),
		started(4, "run-2"),
		requested(5, toolCreateComment, json.RawMessage(`{"body":"two"}`)), denied(6, "Not yet"),
		started(7, "run-3"),
		requested(8, toolCloseIssue, json.RawMessage(`{"issue":7}`)), ev(9, EventMoved),
		started(10, "run-4"),
	}

	want := []GrantedCall{{Connection: "tracker", Tool: toolCreateComment, Input: json.RawMessage(`{"body":"one"}`)}}
	if got := GrantedCalls(events, "run-2"); !reflect.DeepEqual(got, want) {
		t.Errorf("GrantedCalls(run-2) = %+v; want the call granted before it started", got)
	}

	for _, run := range []RunID{runOne, "run-3", "run-4", "run-unknown"} {
		if got := GrantedCalls(events, run); len(got) != 0 {
			t.Errorf("GrantedCalls(%s) = %+v; want none: denied, withdrawn, a later run or no run", run, got)
		}
	}
}

func TestToolDecisionsSinceLastRun(t *testing.T) {
	t.Parallel()

	events := []Event{
		started(1, runOne), requested(2, toolCreateComment, json.RawMessage(`{}`)), granted(3),
		started(4, "run-2"), requested(5, toolCloseIssue, json.RawMessage(`{"issue":7}`)), denied(6, "Keep it open"),
	}

	got := ToolDecisionsSinceLastRun(events)
	if len(got) != 1 || got[0].Granted || got[0].Note != "Keep it open" || got[0].By != answerer ||
		got[0].Approval.Tool != toolCloseIssue || string(got[0].Approval.Input) != `{"issue":7}` {
		t.Fatalf("ToolDecisionsSinceLastRun = %+v; want the one denial since the latest run started", got)
	}

	if none := ToolDecisionsSinceLastRun(append(events, started(7, "run-3"))); len(none) != 0 {
		t.Errorf("ToolDecisionsSinceLastRun after the next run started = %+v; want none", none)
	}
}

func TestCanonicalJSON(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ in, want string }{
		{in: `{"b": 1, "a": {"d": [1, 2], "c": null}}`, want: `{"a":{"c":null,"d":[1,2]},"b":1}`},
		{in: "\n [ \"x\" , true ] \t", want: `["x",true]`},
		{in: `{"n": 12345678901234567890, "f": 1.50}`, want: `{"f":1.50,"n":12345678901234567890}`},
		{in: `{"html": "<a href=\"x\">&</a>"}`, want: `{"html":"<a href=\"x\">&</a>"}`},
		{in: `"text"`, want: `"text"`},
	} {
		got, err := CanonicalJSON([]byte(tt.in))
		if err != nil || string(got) != tt.want {
			t.Errorf("CanonicalJSON(%s) = %s, %v; want %s", tt.in, got, err, tt.want)
		}
	}

	for _, in := range []string{``, `{`, `{} {}`, `nope`} {
		if _, err := CanonicalJSON([]byte(in)); !errors.Is(err, ErrInvalid) {
			t.Errorf("CanonicalJSON(%q) = %v; want ErrInvalid", in, err)
		}
	}
}

func TestEffectiveToolModes(t *testing.T) {
	t.Parallel()

	const conn, other ConnectionID = "c-1", "c-2"

	revokedAt, revoker := qt0, LocalUser
	modes := map[string]ConnectionToolMode{
		"read_issue": ToolModeAllow, toolCreateComment: ToolModeAsk, toolCloseIssue: ToolModeAsk,
		toolDeleteRepo: ToolModeOff, toolEditIssue: ToolModeAsk, toolLabelIssue: ToolModeAsk,
	}
	rules := []StandingRule{
		{ConnectionID: conn, Tool: toolCreateComment, Agent: roleArchitect},                                          // active: allow
		{ConnectionID: conn, Tool: toolCloseIssue, Agent: roleArchitect, RevokedAt: &revokedAt, RevokedBy: &revoker}, // revoked
		{ConnectionID: conn, Tool: toolEditIssue, Agent: "reviewer"},                                                 // another role
		{ConnectionID: other, Tool: toolLabelIssue, Agent: roleArchitect},                                            // another connection
		{ConnectionID: conn, Tool: toolDeleteRepo, Agent: roleArchitect},                                             // never lifts off
	}

	got := EffectiveToolModes(modes, rules, conn, roleArchitect)
	want := RunToolModes{
		Modes: map[string]ConnectionToolMode{
			toolCloseIssue: ToolModeAsk, toolDeleteRepo: ToolModeOff, toolEditIssue: ToolModeAsk, toolLabelIssue: ToolModeAsk,
		},
		AlwaysAllowOffered: []string{toolCloseIssue, toolEditIssue, toolLabelIssue},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("EffectiveToolModes = %+v; want %+v", got, want)
	}

	server := []StandingRule{{Factory: "f-1", Environment: DefaultEnvironment, Server: "acme-git", Tool: toolCreateComment, Agent: roleArchitect}}
	if lifted := EffectiveToolModes(modes, server, "", roleArchitect); lifted.Modes[toolCreateComment] != ToolModeAsk {
		t.Errorf("EffectiveToolModes with a server's rule = %+v; want %s still asking", lifted, toolCreateComment)
	}

	if none := EffectiveToolModes(nil, nil, conn, roleArchitect); len(none.Modes) != 0 || len(none.AlwaysAllowOffered) != 0 {
		t.Errorf("EffectiveToolModes without modes = %+v; want every tool allowed", none)
	}
}

// A tool's mode is its most specific matching pattern's, and allow when none matches.
func TestResolveToolMode(t *testing.T) {
	t.Parallel()

	modes := map[string]ConnectionToolMode{"*": ToolModeOff, "get_*": ToolModeAllow, "get_secret": ToolModeAsk}

	for tool, want := range map[string]ConnectionToolMode{
		"get_issue": ToolModeAllow, "get_secret": ToolModeAsk, toolDeleteRepo: ToolModeOff,
	} {
		if got := ResolveToolMode(modes, tool); got != want {
			t.Errorf("ResolveToolMode(%s) = %s; want %s", tool, got, want)
		}
	}

	if got := ResolveToolMode(nil, toolDeleteRepo); got != ToolModeAllow {
		t.Errorf("ResolveToolMode without modes = %s; want allow", got)
	}
}

func TestServerToolModes(t *testing.T) {
	t.Parallel()

	const (
		factory, otherFactory FactoryID = "f-1", "f-2"
		server                          = "acme-git"
		env                             = DefaultEnvironment
	)

	revokedAt, revoker := qt0, LocalUser
	rule := func(tool string) StandingRule {
		return StandingRule{Factory: factory, Environment: env, Server: server, Tool: tool, Agent: roleArchitect}
	}
	other := func(tool string, change func(*StandingRule)) StandingRule {
		r := rule(tool)
		change(&r)

		return r
	}

	tests := []struct {
		name  string
		file  map[string]ConnectionToolMode
		cap   map[string]ConnectionToolMode
		rules []StandingRule
		// want is each tool's mode as the worker resolves it: the first entry it matches.
		want map[string]ConnectionToolMode
		// list, when set, is the whole ordered list.
		list []ToolModeEntry
	}{
		{
			name: "no modes allows every tool",
			want: map[string]ConnectionToolMode{"anything": ToolModeAllow},
			list: []ToolModeEntry{},
		},
		{
			name: "the most specific pattern comes first",
			file: map[string]ConnectionToolMode{"*": ToolModeAsk, globDelete: ToolModeOff, "delete_branch": ToolModeAllow, "*_issue": ToolModeAllow, globCreate: ToolModeAsk},
			want: map[string]ConnectionToolMode{
				"delete_branch": ToolModeAllow, toolDeleteRepo: ToolModeOff, "read_issue": ToolModeAllow,
				"create_issue": ToolModeAsk, "push": ToolModeAsk,
			},
			list: []ToolModeEntry{
				{"delete_branch", ToolModeAllow},
				{globCreate, ToolModeAsk},
				{globDelete, ToolModeOff},
				{"*_issue", ToolModeAllow},
				{"*", ToolModeAsk},
			},
		},
		{
			name: "an active rule lifts an ask of its own factory, environment, server and role, never off",
			file: map[string]ConnectionToolMode{toolCreateComment: ToolModeAsk, "close_*": ToolModeAsk, globDelete: ToolModeOff, toolEditIssue: ToolModeAsk, "label_*": ToolModeAsk},
			rules: []StandingRule{
				rule(toolCreateComment), // an exact ask
				rule(toolCloseIssue),    // an ask from a glob
				rule(toolDeleteRepo),    // off stays off
				other(toolEditIssue, func(r *StandingRule) { r.RevokedAt, r.RevokedBy = &revokedAt, &revoker }),
				other(toolEditIssue, func(r *StandingRule) { r.Agent = "reviewer" }),
				other(toolEditIssue, func(r *StandingRule) { r.Server = "acme-docs" }),
				other(toolEditIssue, func(r *StandingRule) { r.Environment = "staging" }),
				other(toolEditIssue, func(r *StandingRule) { r.Factory = otherFactory }),
				{ConnectionID: "c-1", Connection: server, Tool: toolLabelIssue, Agent: roleArchitect},
			},
			want: map[string]ConnectionToolMode{
				toolCreateComment: ToolModeAllow, toolCloseIssue: ToolModeAllow, "close_pr": ToolModeAsk, toolDeleteRepo: ToolModeOff,
				toolEditIssue: ToolModeAsk, toolLabelIssue: ToolModeAsk, "get_doc": ToolModeAllow,
			},
		},
		{
			name: "a cap makes a mode stricter, never looser",
			file: map[string]ConnectionToolMode{globCreate: ToolModeAllow, globDelete: ToolModeOff, "get_doc": ToolModeAllow},
			cap:  map[string]ConnectionToolMode{"*": ToolModeAsk, globDelete: ToolModeAllow},
			want: map[string]ConnectionToolMode{
				"create_issue": ToolModeAsk, toolDeleteRepo: ToolModeOff, "get_doc": ToolModeAsk, "push": ToolModeAsk,
			},
		},
		{
			name: "a cap glob that overlaps a file glob",
			file: map[string]ConnectionToolMode{globCreate: ToolModeAllow, "*": ToolModeAllow},
			cap:  map[string]ConnectionToolMode{"*_repo": ToolModeOff, "x*": ToolModeAsk},
			want: map[string]ConnectionToolMode{"create_repo": ToolModeOff, "push_repo": ToolModeOff, "xray": ToolModeAsk, "push": ToolModeAllow},
		},
		{
			name:  "a lifted ask under a cap of ask stays ask",
			file:  map[string]ConnectionToolMode{toolCreateComment: ToolModeAsk, toolCloseIssue: ToolModeAsk},
			cap:   map[string]ConnectionToolMode{toolCreateComment: ToolModeAsk},
			rules: []StandingRule{rule(toolCreateComment), rule(toolCloseIssue)},
			want:  map[string]ConnectionToolMode{toolCreateComment: ToolModeAsk, toolCloseIssue: ToolModeAllow},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ServerToolModes(tt.file, tt.cap, tt.rules, factory, env, server, roleArchitect)

			for tool, want := range tt.want {
				if mode := firstMatch(t, got, tool); mode != want {
					t.Errorf("%s resolves to %s in %+v; want %s", tool, mode, got, want)
				}
			}

			if tt.list != nil && !reflect.DeepEqual(got, tt.list) {
				t.Errorf("ServerToolModes = %+v; want %+v", got, tt.list)
			}
		})
	}
}

// firstMatch is tool's mode as the worker resolves it from modes: the first entry whose pattern
// matches it as path.Match does, and allow when none does.
func firstMatch(t *testing.T, modes []ToolModeEntry, tool string) ConnectionToolMode {
	t.Helper()

	for _, m := range modes {
		ok, err := path.Match(m.Tool, tool)
		if err != nil {
			t.Fatalf("pattern %q: %v", m.Tool, err)
		}

		if ok {
			return m.Mode
		}
	}

	return ToolModeAllow
}
