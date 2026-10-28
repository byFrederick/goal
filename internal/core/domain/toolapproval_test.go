package domain

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

// requested is a tool_approval_requested event as the engine writes it; input is its data as the
// store holds it (raw JSON in memory, decoded values from PostgreSQL).
func requested(id int64, tool string, input any) Event {
	return Event{
		ID: id, At: qt0.Add(time.Duration(id) * time.Minute), Kind: EventToolApprovalRequested, Station: "build",
		Actor: "architect", RunID: "run-1",
		Data: map[string]any{
			KeyConnection: "tracker", KeyTool: tool, KeyInput: input, KeyAgent: "architect", KeyRunID: "run-1",
			KeyAlwaysAllowOffered: true,
		},
	}
}

func granted(id int64) Event {
	return Event{ID: id, At: qt0.Add(time.Duration(id) * time.Minute), Kind: EventToolApprovalGranted, Actor: answerer,
		Data: map[string]any{KeyApprovalID: id - 1, KeyAlwaysAllow: false}}
}

func denied(id int64, note string) Event {
	return Event{ID: id, At: qt0.Add(time.Duration(id) * time.Minute), Kind: EventToolApprovalDenied, Actor: answerer,
		Data: map[string]any{KeyApprovalID: id - 1, KeyNote: note}}
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
		{name: "requested", events: []Event{ev(1, EventRunStarted), requested(2, "create_comment", json.RawMessage(`{}`))}, want: "create_comment"},
		{name: "granted", events: []Event{requested(1, "create_comment", nil), granted(2)}},
		{name: "denied", events: []Event{requested(1, "create_comment", nil), denied(2, "")}},
		{name: "requested again after a grant", events: []Event{
			requested(1, "create_comment", nil), granted(2), ev(3, EventRunStarted), requested(4, "close_issue", nil),
		}, want: "close_issue"},
		{name: "withdrawn by a move", events: []Event{requested(1, "create_comment", nil), ev(2, EventMoved)}},
		{name: "withdrawn by a pause", events: []Event{requested(1, "create_comment", nil), ev(2, EventPaused)}},
		{name: "withdrawn by a rejection", events: []Event{requested(1, "create_comment", nil), ev(2, EventRejected)}},
		{name: "withdrawn by a cancel", events: []Event{requested(1, "create_comment", nil), ev(2, EventCancelled)}},
		{name: "a note does not withdraw it", events: []Event{requested(1, "create_comment", nil), ev(2, EventNote)}, want: "create_comment"},
		{name: "an answered question does not decide it", events: []Event{
			requested(1, "create_comment", nil), answered(2, "yes", nil),
		}, want: "create_comment"},
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
		a := ToolApprovalOf(requested(3, "create_comment", input))
		if string(a.Input) != `{"body":"LGTM","issue":7}` || a.Connection != "tracker" || a.Agent != "architect" ||
			a.RunID != "run-1" || a.EventID != 3 || !a.AlwaysAllowOffered {
			t.Errorf("%s: ToolApprovalOf = %+v (input %s)", name, a, a.Input)
		}
	}
}

func TestGrantedCalls(t *testing.T) {
	t.Parallel()

	events := []Event{
		started(1, "run-1"),
		requested(2, "create_comment", json.RawMessage(`{"body":"one"}`)), granted(3),
		started(4, "run-2"),
		requested(5, "create_comment", json.RawMessage(`{"body":"two"}`)), denied(6, "Not yet"),
		started(7, "run-3"),
		requested(8, "close_issue", json.RawMessage(`{"issue":7}`)), ev(9, EventMoved),
		started(10, "run-4"),
	}

	want := []GrantedCall{{Connection: "tracker", Tool: "create_comment", Input: json.RawMessage(`{"body":"one"}`)}}
	if got := GrantedCalls(events, "run-2"); !reflect.DeepEqual(got, want) {
		t.Errorf("GrantedCalls(run-2) = %+v; want the call granted before it started", got)
	}

	for _, run := range []RunID{"run-1", "run-3", "run-4", "run-unknown"} {
		if got := GrantedCalls(events, run); len(got) != 0 {
			t.Errorf("GrantedCalls(%s) = %+v; want none: denied, withdrawn, a later run or no run", run, got)
		}
	}
}

func TestToolDecisionsSinceLastRun(t *testing.T) {
	t.Parallel()

	events := []Event{
		started(1, "run-1"), requested(2, "create_comment", json.RawMessage(`{}`)), granted(3),
		started(4, "run-2"), requested(5, "close_issue", json.RawMessage(`{"issue":7}`)), denied(6, "Keep it open"),
	}

	got := ToolDecisionsSinceLastRun(events)
	if len(got) != 1 || got[0].Granted || got[0].Note != "Keep it open" || got[0].By != answerer ||
		got[0].Approval.Tool != "close_issue" || string(got[0].Approval.Input) != `{"issue":7}` {
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
		"read_issue": ToolModeAllow, "create_comment": ToolModeAsk, "close_issue": ToolModeAsk,
		"delete_repo": ToolModeOff, "edit_issue": ToolModeAsk, "label_issue": ToolModeAsk,
	}
	rules := []StandingRule{
		{ConnectionID: conn, Tool: "create_comment", Agent: "architect"},                                          // active: allow
		{ConnectionID: conn, Tool: "close_issue", Agent: "architect", RevokedAt: &revokedAt, RevokedBy: &revoker}, // revoked
		{ConnectionID: conn, Tool: "edit_issue", Agent: "reviewer"},                                               // another role
		{ConnectionID: other, Tool: "label_issue", Agent: "architect"},                                            // another connection
		{ConnectionID: conn, Tool: "delete_repo", Agent: "architect"},                                             // never lifts off
	}

	got := EffectiveToolModes(modes, rules, conn, "architect")
	want := RunToolModes{
		Modes: map[string]ConnectionToolMode{
			"close_issue": ToolModeAsk, "delete_repo": ToolModeOff, "edit_issue": ToolModeAsk, "label_issue": ToolModeAsk,
		},
		AlwaysAllowOffered: []string{"close_issue", "edit_issue", "label_issue"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("EffectiveToolModes = %+v; want %+v", got, want)
	}

	if none := EffectiveToolModes(nil, nil, conn, "architect"); len(none.Modes) != 0 || len(none.AlwaysAllowOffered) != 0 {
		t.Errorf("EffectiveToolModes without modes = %+v; want every tool allowed", none)
	}
}
