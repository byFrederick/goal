package gateway

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

const (
	factoryID   domain.FactoryID = "8d4f2b1a-3c5e-4f60-9a7b-1c2d3e4f5a6b"
	itemID      domain.ItemID    = "item-1"
	tracker                      = "tracker"
	trackerCred                  = "acme-tracker-s3cr3t-0123456789"
)

// factoryYAML declares one served connection (tracker), one the token does not grant (wiki),
// one with missing credentials (crm) and two kinds the gateway does not serve yet.
const factoryYAML = `apiVersion: goal/v1
kind: Factory
metadata: { id: ` + string(factoryID) + `, name: acme-gateway }
spec:
  runtime: { runner: docker, image: ghcr.io/acme/agent:1.0.0, auth: anthropic-api-key }
  knowledgeBase: { collections: [notes] }
  connections:
    - { name: knowledge, type: builtin }
    - name: tracker
      type: mcp
      transport: http
      url: UPSTREAM
      headers: { Authorization: "Bearer env:ACME_TRACKER_TOKEN", X-Acme-Team: platform }
    - { name: wiki, type: mcp, transport: http, url: UPSTREAM }
    - { name: postgres, type: mcp, transport: stdio, image: "mcp/postgres:latest" }
    - name: crm
      type: mcp
      transport: http
      url: UPSTREAM
      headers: { Authorization: "Bearer env:ACME_CRM_TOKEN" }
  agents:
    - name: engineer
      prompt: ./agents/engineer.md
      connections: [knowledge, tracker, wiki, postgres, crm]
      tools:
        allow: [Read, Bash, "tracker__get_*", tracker__echo, tracker__whoami, tracker__leak, tracker__big]
        deny: ["Bash(git push*)", tracker__get_secret]
  board:
    stations:
      - { id: build, kind: agent, agent: engineer }
      - { id: done, kind: done }
`

type definitions map[domain.FactoryID]*config.Factory

func (d definitions) Factory(_ context.Context, id domain.FactoryID) (*config.Factory, error) {
	f, ok := d[id]
	if !ok {
		return nil, fmt.Errorf("factory %s: %w", id, ports.ErrNotFound)
	}

	return f, nil
}

// upstream is a fake remote MCP server that records the Authorization headers it receives.
type upstream struct {
	mu    sync.Mutex
	auths []string
}

func (u *upstream) seen() []string {
	u.mu.Lock()
	defer u.mu.Unlock()

	return slices.Clone(u.auths)
}

func startUpstream(t *testing.T) (*upstream, string) {
	t.Helper()

	srv := mcp.NewServer(&mcp.Implementation{Name: "acme-tracker", Version: "1"}, nil)

	type text struct {
		Text string `json:"text"`
	}

	echo := func(_ context.Context, _ *mcp.CallToolRequest, in text) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: in.Text}}}, nil, nil
	}
	for _, name := range []string{"echo", "get_issue", "get_secret", "delete_repo"} {
		mcp.AddTool(srv, &mcp.Tool{Name: name}, echo)
	}

	// whoami returns the credential it received; leak fails with it in a protocol error.
	mcp.AddTool(srv, &mcp.Tool{Name: "whoami"}, func(_ context.Context, req *mcp.CallToolRequest, _ text) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "you are " + req.Extra.Header.Get("Authorization")}}}, nil, nil
	})

	anyInput := json.RawMessage(`{"type":"object"}`)
	srv.AddTool(&mcp.Tool{Name: "leak", InputSchema: anyInput}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return nil, &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: "bad credential " + req.Extra.Header.Get("Authorization")}
	})
	srv.AddTool(&mcp.Tool{Name: "big", InputSchema: anyInput}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: strings.Repeat("x", 64<<10)}}}, nil
	})

	u := &upstream{}
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		u.auths = append(u.auths, r.Header.Get("Authorization"))
		u.mu.Unlock()
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)

	return u, ts.URL
}

// syncBuffer collects logs written from many goroutines.
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

type harness struct {
	url   string
	up    *upstream
	store *porttest.Store
	logs  *syncBuffer
	clock *porttest.Clock
	key   []byte
}

func newHarness(t *testing.T, cfg Config) *harness {
	t.Helper()

	up, upURL := startUpstream(t)

	f, err := config.Load(fstest.MapFS{
		"factory.yaml":       {Data: []byte(strings.ReplaceAll(factoryYAML, "UPSTREAM", upURL))},
		"agents/engineer.md": {Data: []byte("# engineer\n")},
	})
	if err != nil {
		t.Fatal(err)
	}

	h := &harness{
		up: up, store: porttest.NewStore(), logs: &syncBuffer{},
		clock: porttest.NewClock(time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)), key: []byte("gateway-test-key"),
	}

	cfg.Key = h.key

	gw, err := New(Deps{
		Definitions: definitions{factoryID: f},
		Secrets:     porttest.Secrets{"ACME_TRACKER_TOKEN": trackerCred},
		Events:      h.store,
		Clock:       h.clock,
		Logger:      slog.New(slog.NewJSONHandler(h.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(gw)
	t.Cleanup(ts.Close)
	h.url = ts.URL

	return h
}

func (h *harness) claims() domain.TaskClaims {
	return domain.TaskClaims{
		FactoryID: factoryID, ItemID: itemID, Station: "build", Agent: "engineer", RunID: "run-1",
		Connections: []string{"knowledge", tracker, "postgres", "crm"},
		ExpiresAt:   h.clock.Now().Add(time.Hour).Unix(),
	}
}

func (h *harness) token(t *testing.T, c domain.TaskClaims) string {
	t.Helper()

	tok, err := domain.SignTaskToken(h.key, c)
	if err != nil {
		t.Fatal(err)
	}

	return tok
}

// bearer adds an agent's task token, as the worker does.
type bearer string

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+string(b))

	return http.DefaultTransport.RoundTrip(r)
}

// legacyProtocol is a version the worker's TypeScript SDK negotiates with an initialize
// handshake; the Go client defaults to the newest, sessionless protocol.
const legacyProtocol = "2025-06-18"

// connect opens an agent's session; an empty protocol means the client's newest.
func (h *harness) connect(t *testing.T, protocol string) *mcp.ClientSession {
	t.Helper()

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "acme-agent", Version: "1"}, nil).Connect(t.Context(),
		&mcp.StreamableClientTransport{
			Endpoint:             h.url,
			HTTPClient:           &http.Client{Transport: bearer(h.token(t, h.claims()))},
			DisableStandaloneSSE: true,
		}, &mcp.ClientSessionOptions{ProtocolVersion: protocol})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = cs.Close() })

	return cs
}

func (h *harness) events() []domain.Event { return h.store.Events(itemID) }

func toolNames(res *mcp.ListToolsResult) []string {
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}

	slices.Sort(names)

	return names
}

func TestGateway_ListsOnlyPermittedToolsOfGrantedConnections(t *testing.T) {
	t.Parallel()

	for _, protocol := range []string{"", legacyProtocol} {
		t.Run("protocol "+cmp.Or(protocol, "latest"), func(t *testing.T) {
			t.Parallel()

			testListTools(t, protocol)
		})
	}
}

func testListTools(t *testing.T, protocol string) {
	t.Helper()

	h := newHarness(t, Config{})
	cs := h.connect(t, protocol)

	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"tracker__big", "tracker__echo", "tracker__get_issue", "tracker__leak", "tracker__whoami"}
	if got := toolNames(res); !slices.Equal(got, want) {
		t.Fatalf("tools = %v, want %v", got, want)
	}

	status, _ := res.Meta[metaConnections].(map[string]any)
	wantStatus := map[string]string{
		tracker:     "available",
		"knowledge": "unavailable: builtin" + reasonKind,
		"postgres":  "unavailable: " + reasonStdio,
		"crm":       "unavailable: " + reasonCredentials,
	}

	if len(status) != len(wantStatus) {
		t.Fatalf("connection status = %v, want %v", status, wantStatus)
	}

	for name, want := range wantStatus {
		if status[name] != want {
			t.Errorf("connection %s status = %v, want %q", name, status[name], want)
		}
	}

	if instr := cs.InitializeResult().Instructions; !strings.Contains(instr, "Connection postgres is unavailable") {
		t.Errorf("instructions do not report the stdio connection: %q", instr)
	}

	if logs := h.logs.String(); !strings.Contains(logs, `"connection":"postgres"`) {
		t.Errorf("no warning logged for the stdio connection:\n%s", logs)
	}
}

func TestGateway_CallProxiesWithHostCredentialsAndAudits(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Config{})
	cs := h.connect(t, legacyProtocol)

	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "tracker__echo", Arguments: map[string]any{"text": "hello"}})
	if err != nil {
		t.Fatal(err)
	}

	if text, _ := res.Content[0].(*mcp.TextContent); res.IsError || text == nil || text.Text != "hello" {
		t.Fatalf("result = %+v, want the upstream's echo", res)
	}

	if seen := h.up.seen(); !slices.Contains(seen, "Bearer "+trackerCred) {
		t.Fatalf("upstream saw Authorization %q, want the resolved credential", seen)
	}

	events := h.events()
	if len(events) != 1 {
		t.Fatalf("events = %+v, want one audit event", events)
	}

	checkEchoEvent(t, events[0])
}

func checkEchoEvent(t *testing.T, e domain.Event) {
	t.Helper()

	if e.Kind != EventToolCall || e.RunID != "run-1" || e.Actor != "engineer" || e.Station != "build" {
		t.Fatalf("event = %+v", e)
	}

	d := e.Data
	if d["connection"] != tracker || d["tool"] != "echo" || d["outcome"] != outcomeOK || d["ok"] != true {
		t.Fatalf("event data = %v", d)
	}

	if d["request_bytes"] == 0 || d["response_bytes"] == 0 {
		t.Fatalf("event sizes = %v, want both set", d)
	}

	if strings.Contains(fmt.Sprint(d), "hello") {
		t.Fatalf("event data carries arguments or results: %v", d)
	}
}

func TestGateway_RejectsToolsTheAgentMayNotCall(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Config{})
	cs := h.connect(t, legacyProtocol)

	for _, name := range []string{
		"tracker__get_secret",  // denied
		"tracker__delete_repo", // not allowed
		"wiki__echo",           // connection not in the token
		"postgres__query",      // connection not served
		"tracker",              // no tool
	} {
		_, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
		if err == nil {
			t.Errorf("CallTool(%s) succeeded, want it rejected", name)
		}
	}

	for _, e := range h.events() {
		if e.Data["outcome"] != outcomeDenied {
			t.Errorf("event %v, want a denied audit event", e.Data)
		}
	}

	if n := len(h.events()); n != 5 {
		t.Fatalf("audit events = %d, want one per rejected call", n)
	}

	if seen := h.up.seen(); len(seen) != 0 {
		t.Fatalf("rejected calls reached the upstream %d times", len(seen))
	}
}

func TestGateway_RejectsRequestsWithoutAValidToken(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Config{})

	valid := h.claims()
	expired := valid
	expired.ExpiresAt = h.clock.Now().Add(-time.Second).Unix()
	stranger := valid
	stranger.Agent = "stranger"

	other, err := domain.SignTaskToken([]byte("other-key"), valid)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		header string
		status int
	}{
		{name: "no header", status: http.StatusUnauthorized},
		{name: "not bearer", header: "Basic " + h.token(t, valid), status: http.StatusUnauthorized},
		{name: "garbage", header: "Bearer not-a-token", status: http.StatusUnauthorized},
		{name: "other key", header: "Bearer " + other, status: http.StatusUnauthorized},
		{name: "expired", header: "Bearer " + h.token(t, expired), status: http.StatusUnauthorized},
		{name: "agent not in the definition", header: "Bearer " + h.token(t, stranger), status: http.StatusForbidden},
	}

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, h.url, strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}

			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")

			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()

			got, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != tt.status {
				t.Fatalf("status = %d, want %d (%s)", resp.StatusCode, tt.status, got)
			}

			if s := strings.TrimSpace(string(got)); s != "unauthorized" && s != "forbidden" {
				t.Fatalf("body = %q, want no detail", s)
			}
		})
	}
}

func TestGateway_NeverLeaksTheCredential(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Config{})
	cs := h.connect(t, legacyProtocol)

	var returned []string

	for _, name := range []string{"tracker__whoami", "tracker__leak"} {
		res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{"text": ""}})
		if err != nil {
			returned = append(returned, err.Error())

			continue
		}

		raw, _ := json.Marshal(res)
		returned = append(returned, string(raw))
	}

	if seen := h.up.seen(); !slices.Contains(seen, "Bearer "+trackerCred) {
		t.Fatalf("upstream never saw the credential; the test proves nothing")
	}

	for where, text := range map[string]string{
		"results": strings.Join(returned, "\n"),
		"logs":    h.logs.String(),
		"events":  fmt.Sprintf("%+v", h.events()),
	} {
		if strings.Contains(text, trackerCred) {
			t.Errorf("the credential leaked into %s:\n%s", where, text)
		}
	}

	if !strings.Contains(returned[0], redacted) {
		t.Errorf("whoami result = %s, want the credential redacted", returned[0])
	}
}

func TestGateway_CapsUpstreamResponses(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Config{MaxResponseBytes: 16 << 10})
	cs := h.connect(t, legacyProtocol)

	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "tracker__big", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}

	if !res.IsError {
		t.Fatalf("result of %d content items is not an error", len(res.Content))
	}

	if e := h.events(); len(e) != 1 || e[0].Data["outcome"] != outcomeFailed {
		t.Fatalf("events = %+v, want one failed call", e)
	}
}
