package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
)

func TestResultOf(t *testing.T) {
	t.Parallel()

	ok := &domain.WorkerFinished{Status: string(domain.RunSucceeded), Output: json.RawMessage(`{"lane":"backend"}`), CostUSD: 1.25, Turns: 7, InputTokens: 100, OutputTokens: 50, SessionID: "s"}
	tests := []struct {
		name      string
		in        outcome
		status    domain.RunStatus
		output    string
		errSubstr string
	}{
		{name: "succeeded", in: outcome{fin: ok}, status: domain.RunSucceeded, output: `{"lane":"backend"}`},
		{name: "agent failed", in: outcome{fin: &domain.WorkerFinished{Status: "failed", Subtype: "error_max_turns", Error: "hit 10 turns"}, exitCode: 1}, status: domain.RunFailed, errSubstr: "error_max_turns: hit 10 turns"},
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
