package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestDecodeWorkerLine_MapsProgressTypesToEventKinds(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		line string
		kind EventKind
		data map[string]any
		at   time.Time
	}{
		{
			line: `{"type":"started","at":"2026-10-23T10:00:00.5Z","session_id":"s1","model":"m"}`,
			kind: EventAgentStarted, data: map[string]any{"session_id": "s1", "model": "m"},
			at: time.Date(2026, 10, 23, 10, 0, 0, 5e8, time.UTC),
		},
		{
			line: `{"type":"tool_use","at":"bad","id":"t1","tool":"Bash","input":"ls"}`,
			kind: EventToolCall, data: map[string]any{"id": "t1", "tool": "Bash", "input": "ls"}, at: now,
		},
		{
			line: `{"type":"tool_result","id":"t1","is_error":false,"summary":"ok"}`,
			kind: EventToolResult, data: map[string]any{"id": "t1", "is_error": false, "summary": "ok"}, at: now,
		},
		{line: `{"type":"message","text":"hi"}`, kind: EventAgentMessage, data: map[string]any{"text": "hi"}, at: now},
		{line: `{"type":"usage","input_tokens":3}`, kind: EventUsage, data: map[string]any{"input_tokens": 3.0}, at: now},
		{
			line: `{"type":"note_delivered","at":"2026-10-23T10:00:01Z","note_id":42}`,
			kind: EventNoteDelivered, data: map[string]any{"note_id": float64(42)},
			at: time.Date(2026, 10, 23, 10, 0, 1, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			t.Parallel()

			ev, fin, err := DecodeWorkerLine([]byte(tt.line), now)
			if err != nil || fin != nil || ev == nil {
				t.Fatalf("DecodeWorkerLine = %v, %v, %v; want an event", ev, fin, err)
			}

			if ev.Kind != tt.kind || !ev.At.Equal(tt.at) || fmt.Sprint(ev.Data) != fmt.Sprint(tt.data) {
				t.Errorf("event = %+v; want kind %s at %s data %v", *ev, tt.kind, tt.at, tt.data)
			}
		})
	}
}

// A line that claims a person did it, or that a gate required it, is still the agent's: a box
// writing "actor":"user:<id>" makes no human touch (spec 4 §4.9).
func TestDecodeWorkerLine_DropsWhoAndWhyOnlyGoaldSays(t *testing.T) {
	t.Parallel()

	ev, _, err := DecodeWorkerLine([]byte(`{"type":"message","text":"hi","actor":"user:u1","planned":true}`), time.Now())
	if err != nil || ev == nil {
		t.Fatalf("DecodeWorkerLine = %v, %v; want an event", ev, err)
	}

	if fmt.Sprint(ev.Data) != fmt.Sprint(map[string]any{"text": "hi"}) {
		t.Errorf("event data %v, want only the text", ev.Data)
	}
}

func TestDecodeWorkerLine_FinishedAndUnknownLines(t *testing.T) {
	t.Parallel()

	ev, fin, err := DecodeWorkerLine([]byte(`{"type":"finished","at":"2026-10-23T10:00:00Z","status":"succeeded","output":{"a":1},"cost_usd":0.5,"turns":3,"input_tokens":10,"output_tokens":20,"session_id":"s"}`), time.Now())
	if err != nil || ev != nil || fin == nil {
		t.Fatalf("DecodeWorkerLine(finished) = %v, %v, %v", ev, fin, err)
	}

	if fin.Status != string(RunSucceeded) || string(fin.Output) != `{"a":1}` || fin.Turns != 3 || fin.CostUSD != 0.5 {
		t.Errorf("finished = %+v", *fin)
	}

	for _, line := range []string{`npm WARN something`, `{"type":"thinking"}`, `[1,2]`} {
		if _, _, err := DecodeWorkerLine([]byte(line), time.Now()); !errors.Is(err, ErrNotWorkerLine) {
			t.Errorf("DecodeWorkerLine(%q) error = %v; want ErrNotWorkerLine", line, err)
		}
	}
}

func TestWorkerFinished_DenialsAtTheLinesTime(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

	_, fin, err := DecodeWorkerLine([]byte(`{"type":"finished","at":"2026-10-23T10:00:00Z","status":"failed","permission_denials":[{"tool":"Bash","id":"t1"},{"tool":"WebFetch","id":"t2"}]}`), now)
	if err != nil {
		t.Fatal(err)
	}

	got := fin.Denials(now)
	if len(got) != 2 || got[1].Kind != EventToolDenied || got[1].Data["tool"] != "WebFetch" || got[1].Data["id"] != "t2" ||
		!got[0].At.Equal(time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("Denials = %+v; want two tool_denied events at the line's time", got)
	}

	if got := (&WorkerFinished{PermissionDenials: fin.PermissionDenials}).Denials(now); !got[0].At.Equal(now) {
		t.Errorf("Denials without a time at %s; want now", got[0].At)
	}
}

func TestWorkerFinished_Result(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		in        WorkerFinished
		status    RunStatus
		output    string
		errSubstr string
	}{
		{
			name: "succeeded", status: RunSucceeded, output: `{"lane":"backend"}`,
			in: WorkerFinished{Status: "succeeded", Output: json.RawMessage(`{"lane":"backend"}`), CostUSD: 1.25, Turns: 7, InputTokens: 100, OutputTokens: 50, SessionID: "s"},
		},
		{name: "succeeded without output", in: WorkerFinished{Status: "succeeded", Output: json.RawMessage("null")}, status: RunFailed, errSubstr: "without output"},
		{name: "agent failed", in: WorkerFinished{Status: "failed", Subtype: "error_max_turns", Error: "hit 10 turns"}, status: RunFailed, errSubstr: "error_max_turns: hit 10 turns"},
		{name: "failed with a subtype only", in: WorkerFinished{Status: "failed", Subtype: "error_during_execution"}, status: RunFailed, errSubstr: "error_during_execution"},
		{name: "failed without a reason", in: WorkerFinished{Status: "failed"}, status: RunFailed, errSubstr: "without a reason"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := tt.in.Result()
			if res.Status != tt.status || string(res.Output) != tt.output || !strings.Contains(res.Error, tt.errSubstr) {
				t.Errorf("Result = %+v; want status %s, output %q, error containing %q", res, tt.status, tt.output, tt.errSubstr)
			}

			if res.CostUSD != tt.in.CostUSD || res.Turns != tt.in.Turns || res.InputTokens != tt.in.InputTokens ||
				res.OutputTokens != tt.in.OutputTokens || res.SessionID != tt.in.SessionID {
				t.Errorf("Result dropped usage: %+v", res)
			}
		})
	}
}
