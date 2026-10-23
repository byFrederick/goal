package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
)

func TestParseLine_MapsProgressTypesToEventKinds(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		line string
		kind domain.EventKind
		data map[string]any
		at   time.Time
	}{
		{
			line: `{"type":"started","at":"2026-10-23T10:00:00.5Z","session_id":"s1","model":"m"}`,
			kind: KindAgentStarted, data: map[string]any{"session_id": "s1", "model": "m"},
			at: time.Date(2026, 10, 23, 10, 0, 0, 5e8, time.UTC),
		},
		{
			line: `{"type":"tool_use","at":"bad","id":"t1","tool":"Bash","input":"ls"}`,
			kind: KindToolCall, data: map[string]any{"id": "t1", "tool": "Bash", "input": "ls"}, at: now,
		},
		{
			line: `{"type":"tool_result","id":"t1","is_error":false,"summary":"ok"}`,
			kind: KindToolResult, data: map[string]any{"id": "t1", "is_error": false, "summary": "ok"}, at: now,
		},
		{line: `{"type":"message","text":"hi"}`, kind: KindAgentMessage, data: map[string]any{"text": "hi"}, at: now},
		{line: `{"type":"usage","input_tokens":3}`, kind: KindUsage, data: map[string]any{"input_tokens": 3.0}, at: now},
	}

	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			t.Parallel()

			ev, fin, err := parseLine([]byte(tt.line), now)
			if err != nil || fin != nil || ev == nil {
				t.Fatalf("parseLine = %v, %v, %v; want an event", ev, fin, err)
			}

			if ev.Kind != tt.kind || !ev.At.Equal(tt.at) || fmt.Sprint(ev.Data) != fmt.Sprint(tt.data) {
				t.Errorf("event = %+v; want kind %s at %s data %v", *ev, tt.kind, tt.at, tt.data)
			}
		})
	}
}

func TestParseLine_FinishedAndUnknownLines(t *testing.T) {
	t.Parallel()

	ev, fin, err := parseLine([]byte(`{"type":"finished","at":"2026-10-23T10:00:00Z","status":"succeeded","output":{"a":1},"cost_usd":0.5,"turns":3,"input_tokens":10,"output_tokens":20,"session_id":"s"}`), time.Now())
	if err != nil || ev != nil || fin == nil {
		t.Fatalf("parseLine(finished) = %v, %v, %v", ev, fin, err)
	}

	if fin.Status != string(domain.RunSucceeded) || string(fin.Output) != `{"a":1}` || fin.Turns != 3 || fin.CostUSD != 0.5 {
		t.Errorf("finished = %+v", *fin)
	}

	for _, line := range []string{`npm WARN something`, `{"type":"thinking"}`, `[1,2]`} {
		if _, _, err := parseLine([]byte(line), time.Now()); !errors.Is(err, errUnknownLine) {
			t.Errorf("parseLine(%q) error = %v; want errUnknownLine", line, err)
		}
	}
}

func TestResultOf(t *testing.T) {
	t.Parallel()

	ok := &finished{Status: string(domain.RunSucceeded), Output: json.RawMessage(`{"lane":"backend"}`), CostUSD: 1.25, Turns: 7, InputTokens: 100, OutputTokens: 50, SessionID: "s"}
	tests := []struct {
		name      string
		in        outcome
		status    domain.RunStatus
		output    string
		errSubstr string
	}{
		{name: "succeeded", in: outcome{fin: ok}, status: domain.RunSucceeded, output: `{"lane":"backend"}`},
		{name: "succeeded without output", in: outcome{fin: &finished{Status: string(domain.RunSucceeded), Output: json.RawMessage("null")}}, status: domain.RunFailed, errSubstr: "without output"},
		{name: "agent failed", in: outcome{fin: &finished{Status: "failed", Subtype: "error_max_turns", Error: "hit 10 turns"}, exitCode: 1}, status: domain.RunFailed, errSubstr: "error_max_turns: hit 10 turns"},
		{name: "crashed", in: outcome{exitCode: 137, stderr: "out of memory"}, status: domain.RunFailed, errSubstr: "code 137 without a result: out of memory"},
		{name: "cancelled", in: outcome{fin: ok, stopped: context.Canceled}, status: domain.RunCancelled, errSubstr: "canceled"},
		{name: "timed out", in: outcome{fin: ok, stopped: fmt.Errorf("%w after 1s", errTimeout)}, status: domain.RunFailed, errSubstr: "timed out after 1s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := resultOf(tt.in)
			if res.Status != tt.status || string(res.Output) != tt.output || !strings.Contains(res.Error, tt.errSubstr) {
				t.Errorf("resultOf = %+v; want status %s, output %q, error containing %q", res, tt.status, tt.output, tt.errSubstr)
			}

			if tt.in.fin == ok && (res.CostUSD != 1.25 || res.Turns != 7 || res.InputTokens != 100 || res.OutputTokens != 50 || res.SessionID != "s") {
				t.Errorf("resultOf dropped usage: %+v", res)
			}
		})
	}
}

func TestRedactor_HidesSecretsInNestedEventData(t *testing.T) {
	t.Parallel()

	r := newRedactor("sk-oauth-0123456789", "gateway-token-xyz", "true")
	data := map[string]any{
		"summary": "CLAUDE_CODE_OAUTH_TOKEN=sk-oauth-0123456789\n",
		"input":   map[string]any{"headers": []any{"Bearer gateway-token-xyz", 3.0}},
		"flag":    "true",
	}

	r.value(data)

	got := fmt.Sprint(data)
	if strings.Contains(got, "sk-oauth") || strings.Contains(got, "gateway-token") {
		t.Errorf("secrets leaked: %s", got)
	}

	if data["flag"] != "true" {
		t.Errorf("short values must not be redacted: %v", data["flag"])
	}
}

func TestEachLine_DropsOverlongLinesAndKeepsReading(t *testing.T) {
	t.Parallel()

	in := "first\n" + strings.Repeat("x", 200_000) + "\n\n  second  \nlast-without-newline"

	var got []string

	err := eachLine(strings.NewReader(in), 100_000, func(b []byte) { got = append(got, string(b)) })
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"first", "second", "last-without-newline"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("lines = %q; want %q", got, want)
	}
}
