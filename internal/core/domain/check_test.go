package domain_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

func TestParseCheckAnswer(t *testing.T) {
	t.Parallel()

	bigState := `{"since": "` + strings.Repeat("x", domain.MaxCheckState) + `"}`

	for _, tt := range []struct {
		name, output, wantErr string
	}{
		{name: "everything", output: `{
			"file": [{"key": "INC-1", "title": "Checkout is down", "brief": "Look at it", "type": "incident", "lane": "backend",
			          "priority": 2, "repositories": ["acme/app"], "start": "36h", "reopen": "it came back"},
			         {"key": "INC-2", "title": "Slow search", "brief": "b", "start": "2026-11-05T09:00:00Z"}],
			"note": [{"key": "INC-0", "text": "still down"}],
			"skip": [{"key": "INC-3", "reason": "a test page"}],
			"state": {"since": "2026-11-03T14:05:00Z"}}`},
		{name: "empty lists", output: `{"file": [], "note": [], "skip": []}`},
		{name: "lists left out", output: `{}`},
		{name: "not JSON", output: `the incidents look fine`, wantErr: "check answer"},
		{name: "unknown field", output: `{"file": [], "items": []}`, wantErr: "items"},
		{name: "file without a key", output: `{"file": [{"title": "t", "brief": "b"}]}`, wantErr: "file 0 needs a key"},
		{name: "file without a brief", output: `{"file": [{"key": "k", "title": "t", "brief": " "}]}`, wantErr: "file 0 needs a key"},
		{name: "key filed twice", output: `{"file": [{"key": "k", "title": "t", "brief": "b"}, {"key": "k", "title": "u", "brief": "c"}]}`, wantErr: "filed twice"},
		{name: "bad start", output: `{"file": [{"key": "k", "title": "t", "brief": "b", "start": "soon"}]}`, wantErr: "file 0"},
		{name: "note without text", output: `{"note": [{"key": "k", "text": ""}]}`, wantErr: "note 0"},
		{name: "skip without a reason", output: `{"skip": [{"key": "k"}]}`, wantErr: "skip 0"},
		{name: "state not an object", output: `{"state": [1, 2]}`, wantErr: "JSON object"},
		{name: "state over 4 KiB", output: `{"state": ` + bigState + `}`, wantErr: "over 4096"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a, err := domain.ParseCheckAnswer(json.RawMessage(tt.output))

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ParseCheckAnswer: %v", err)
				}

				if a.File == nil || a.Note == nil || a.Skip == nil {
					t.Errorf("answer = %+v, want empty lists, not nil", a)
				}

				return
			}

			if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %v, want ErrInvalid naming %q", err, tt.wantErr)
			}
		})
	}
}

func TestCheckAnswer_DecisionsLeaveOutTheState(t *testing.T) {
	t.Parallel()

	a, err := domain.ParseCheckAnswer(json.RawMessage(`{"skip": [{"key": "k", "reason": "r"}], "state": {"since": "x"}}`))
	if err != nil {
		t.Fatal(err)
	}

	if got, want := string(a.Decisions()), `{"file":[],"note":[],"skip":[{"key":"k","reason":"r"}]}`; got != want {
		t.Errorf("decisions = %s, want %s", got, want)
	}
}

func TestCheckAnswerSchema_IsAToolSchema(t *testing.T) {
	t.Parallel()

	var s map[string]any
	if err := json.Unmarshal([]byte(domain.CheckAnswerSchema), &s); err != nil {
		t.Fatal(err)
	}

	if s["type"] != "object" || s["oneOf"] != nil || s["anyOf"] != nil {
		t.Errorf("schema top level = %v, want a plain object", s["type"])
	}
}

func TestCheckResult_Check(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

	for _, tt := range []struct {
		name string
		r    domain.CheckResult
		ok   bool
	}{
		{"done", domain.CheckResult{State: domain.CheckDone, Decisions: json.RawMessage(`{}`), At: at}, true},
		{"failed", domain.CheckResult{State: domain.CheckFailed, Reason: "timed out", At: at}, true},
		{"done without decisions", domain.CheckResult{State: domain.CheckDone, At: at}, false},
		{"failed without reason", domain.CheckResult{State: domain.CheckFailed, At: at}, false},
		{"failed saving state", domain.CheckResult{State: domain.CheckFailed, Reason: "r", Saved: json.RawMessage(`{}`), At: at}, false},
		{"running", domain.CheckResult{State: domain.CheckRunning, At: at}, false},
		{"no time", domain.CheckResult{State: domain.CheckFailed, Reason: "r"}, false},
	} {
		if err := tt.r.Check(); (err == nil) != tt.ok {
			t.Errorf("%s: Check() = %v, want ok %v", tt.name, err, tt.ok)
		}
	}
}
