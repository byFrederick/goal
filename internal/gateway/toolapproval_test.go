package gateway

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
)

// The plain bodies of the gateway's refusals of a token, and a good report's body.
const (
	bodyUnauthorized = "unauthorized"
	bodyForbidden    = "forbidden"
	goodReport       = `{"connection":"docs","tool":"create_page","input":{}}`
)

// fakeToolApprovals stands in for the engine behind POST /run/tool-approval: it keeps every call
// reported to it and answers with err.
type fakeToolApprovals struct {
	mu       sync.Mutex
	reported []ToolCall
	err      error
}

func (f *fakeToolApprovals) RequestToolApproval(_ context.Context, c ToolCall) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.reported = append(f.reported, c)

	return f.err
}

func (f *fakeToolApprovals) calls() []ToolCall {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.reported)
}

func (f *fakeToolApprovals) answer(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.err = err
}

// syncBuffer is a bytes.Buffer the gateway's handlers may log to concurrently.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// toolApprovalServer serves the gateway's tool approval path the way goald mounts it.
func (h *harness) toolApprovalServer(t *testing.T) string {
	t.Helper()

	ts := httptest.NewServer(http.HandlerFunc(h.gw.ServeToolApproval))
	t.Cleanup(ts.Close)

	return ts.URL + "/run/tool-approval"
}

// reportClaims are the harness's claims for a queued item of its factory, which the token names.
func (h *harness) reportClaims(t *testing.T) domain.TaskClaims {
	t.Helper()

	c := h.claims()
	c.ItemID = h.queue(t, c.Station).ID

	return c
}

// report posts body to url with token (none when empty) and returns the status and body.
func report(t *testing.T, method, url, token, body string) (int, http.Header, string) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set("Content-Type", "application/json")

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	return resp.StatusCode, resp.Header, strings.TrimSpace(string(got))
}

// ADR 0031 decision 4: a run reports an Ask-first call with {connection, tool, input}; the
// gateway passes it on with the token's item, run and agent, the input as canonical JSON, and
// answers 202. Only POST is served.
func TestToolApproval_ReportsTheCallAndAnswers202(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Config{})
	url := h.toolApprovalServer(t)
	c := h.reportClaims(t)
	tok := h.token(t, c)

	status, _, body := report(t, http.MethodPost, url, tok,
		`{"connection":"docs","tool":"create_page","input":{ "title": "Plan", "body": "<b>a</b>" }}`)
	if status != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (%s)", status, body)
	}

	want := []ToolCall{{
		Item: c.ItemID, Run: c.RunID, Agent: agentEngineer, Connection: connDocs, Tool: "create_page",
		Input: []byte(`{"body":"<b>a</b>","title":"Plan"}`),
	}}
	if got := h.approvals.calls(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("reported %s, want %s", mustJSON(t, got), mustJSON(t, want))
	}

	status, header, _ := report(t, http.MethodGet, url, tok, "")
	if status != http.StatusMethodNotAllowed || header.Get("Allow") != http.MethodPost {
		t.Errorf("GET = %d with Allow %q, want 405 with Allow POST", status, header.Get("Allow"))
	}

	if n := len(h.approvals.calls()); n != 1 {
		t.Errorf("%d calls reported, want only the POST's", n)
	}
}

// A report without a valid task token gets 401 with no detail, and one whose token names what
// the organization does not have gets 403; neither is passed on.
func TestToolApproval_RefusesABadToken(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Config{})
	url := h.toolApprovalServer(t)
	c := h.reportClaims(t)

	expired := c
	expired.ExpiresAt = h.clock.Now().Add(-1).Unix()
	outsider := c
	outsider.Agent = "outsider"
	otherItem := c
	otherItem.ItemID = "00000000-0000-7000-8000-0000000000ff"

	tests := []struct {
		name, token string
		status      int
		body        string
	}{
		{name: "no token", status: http.StatusUnauthorized, body: bodyUnauthorized},
		{name: "not a token", token: "not-a-token", status: http.StatusUnauthorized, body: bodyUnauthorized},
		{name: "an expired token", token: h.token(t, expired), status: http.StatusUnauthorized, body: bodyUnauthorized},
		{name: "a token for the API", token: h.tokenFor(t, c, "goal-api"), status: http.StatusUnauthorized, body: bodyUnauthorized},
		{name: "an agent the definition lacks", token: h.token(t, outsider), status: http.StatusForbidden, body: bodyForbidden},
		{name: "item not in the organization", token: h.token(t, otherItem), status: http.StatusForbidden, body: bodyForbidden},
	}

	for _, tt := range tests {
		status, _, body := report(t, http.MethodPost, url, tt.token, goodReport)
		if status != tt.status || body != tt.body {
			t.Errorf("%s: %d %q, want %d %q", tt.name, status, body, tt.status, tt.body)
		}
	}

	if got := h.approvals.calls(); len(got) != 0 {
		t.Errorf("reported %v, want nothing passed on", got)
	}
}

// ADR 0031 decision 4: a malformed report is 400, an input over 16 KiB as canonical JSON is 413,
// and the engine's refusals are 409, with a different body for an open approval and for a run
// that has ended. Only the engine's refusals were passed on.
func TestToolApproval_Refusals(t *testing.T) {
	t.Parallel()

	// big is just over the limit once canonical, and whitespace does not count towards it.
	big := `{"text":"` + strings.Repeat("a", maxToolInputBytes) + `"}`
	padded := `{"text":` + strings.Repeat(" ", maxToolInputBytes) + `"a"}`

	tests := []struct {
		name   string
		body   string
		engine error
		status int
		// passed is whether the call reaches the engine.
		passed bool
		// answer, when set, is the body the run reads.
		answer string
	}{
		{name: "not JSON", body: `{"connection":`, status: http.StatusBadRequest},
		{name: "unknown field", body: `{"connection":"docs","tool":"create_page","input":{},"agent":"reviewer"}`, status: http.StatusBadRequest},
		{name: "two values", body: `{"connection":"docs","tool":"create_page","input":{}} {}`, status: http.StatusBadRequest},
		{name: "no input", body: `{"connection":"docs","tool":"create_page"}`, status: http.StatusBadRequest},
		{name: "bad connection name", body: `{"connection":"Docs!","tool":"create_page","input":{}}`, status: http.StatusBadRequest},
		{name: "connection the token does not grant", body: `{"connection":"issues","tool":"create_page","input":{}}`, status: http.StatusBadRequest},
		{name: "bad tool name", body: `{"connection":"docs","tool":"docs__create page","input":{}}`, status: http.StatusBadRequest},
		{name: "input over 16 KiB", body: `{"connection":"docs","tool":"create_page","input":` + big + `}`, status: http.StatusRequestEntityTooLarge},
		{name: "padded input under 16 KiB", body: `{"connection":"docs","tool":"create_page","input":` + padded + `}`, status: http.StatusAccepted, passed: true},
		{
			name: "an open tool approval or question", body: goodReport,
			engine: fmt.Errorf("engine: %w", ErrToolApprovalOpen), status: http.StatusConflict, passed: true, answer: answerApprovalOpen,
		},
		{
			name: "a run that has ended", body: goodReport,
			engine: fmt.Errorf("engine: %w", ErrRunEnded), status: http.StatusConflict, passed: true, answer: answerRunEnded,
		},
		{
			name: "a call the engine finds malformed", body: goodReport,
			engine: fmt.Errorf("engine: %w", ErrBadToolCall), status: http.StatusBadRequest, passed: true,
		},
		{
			name: "an engine failure", body: goodReport,
			engine: errors.New("database is down"), status: http.StatusServiceUnavailable, passed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, Config{})
			h.approvals.answer(tt.engine)

			status, _, body := report(t, http.MethodPost, h.toolApprovalServer(t), h.token(t, h.reportClaims(t)), tt.body)
			if status != tt.status {
				t.Errorf("status = %d, want %d (%s)", status, tt.status, body)
			}

			if n := len(h.approvals.calls()); (n == 1) != tt.passed || n > 1 {
				t.Errorf("%d calls passed on, want passed = %v", n, tt.passed)
			}

			if strings.Contains(body, "database is down") {
				t.Errorf("body %q tells the run the engine's error", body)
			}

			if tt.answer != "" && body != tt.answer {
				t.Errorf("body = %q, want %q", body, tt.answer)
			}
		})
	}
}

// AGENTS.md rule 7: a tool's input may hold a secret, so no log line of the path carries it,
// whatever the answer; the lines name the connection, the tool and the run.
func TestToolApproval_NeverLogsTheInput(t *testing.T) {
	t.Parallel()

	const marker = "test-token-4b1d"

	for _, engine := range []error{nil, ErrToolApprovalOpen, ErrRunEnded, ErrBadToolCall, errors.New("database is down")} {
		h := newHarness(t, Config{})
		h.approvals.answer(engine)
		url := h.toolApprovalServer(t)
		tok := h.token(t, h.reportClaims(t))

		for _, body := range []string{
			`{"connection":"docs","tool":"create_page","input":{"token":"` + marker + `"}}`,
			`{"connection":"docs","tool":"create_page","input":{"token":"` + marker + strings.Repeat("a", maxToolInputBytes) + `"}}`,
			`{"connection":"docs","tool":"create_page","input":{"token":"` + marker + `"},"extra":"` + marker + `"}`,
		} {
			report(t, http.MethodPost, url, tok, body)
		}

		logs := h.logs.String()
		if strings.Contains(logs, marker) {
			t.Fatalf("engine answer %v: the logs carry the input:\n%s", engine, logs)
		}

		if engine == nil && (!strings.Contains(logs, "connection=docs") || !strings.Contains(logs, "tool=create_page") || !strings.Contains(logs, "run=run-1")) {
			t.Errorf("the logs do not name the connection, tool and run:\n%s", logs)
		}
	}
}
