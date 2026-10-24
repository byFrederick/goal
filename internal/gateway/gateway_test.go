package gateway

import (
	"bytes"
	"cmp"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	otherID     domain.FactoryID = "5c0d7a52-8b1e-4f43-9d3a-2e6b7c8f9a01"
	itemID      domain.ItemID    = "item-1"
	tracker                      = "tracker"
	trackerCred                  = "acme-tracker-s3cr3t-0123456789"
)

// version is the definition version the tasks' runs started on; otherVersion is a version of
// another factory.
const (
	version      domain.DefinitionVersionID = "019a1c00-0000-7000-8000-0000000000a1"
	otherVersion domain.DefinitionVersionID = "019a1c00-0000-7000-8000-0000000000b1"
)

// factoryYAML declares one served remote connection (tracker), one the token does not grant
// (wiki), one with missing credentials (crm), one served sidecar (shop-db), one sidecar whose
// image is not pinned (postgres) and one kind the gateway does not serve yet.
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
    - name: shop-db
      type: mcp
      transport: stdio
      image: ghcr.io/acme/shop-db-mcp:1.4.2
      command: [--read-only, "env:ACME_DB_URL"]
      env: { DATABASE_URL: "env:ACME_DB_URL", LOG_LEVEL: info }
      network: true
    - name: crm
      type: mcp
      transport: http
      url: UPSTREAM
      headers: { Authorization: "Bearer env:ACME_CRM_TOKEN" }
  agents:
    - name: engineer
      prompt: ./agents/engineer.md
      connections: [knowledge, tracker, wiki, postgres, crm, shop-db]
      tools:
        allow: [Read, Bash, "tracker__get_*", tracker__echo, tracker__whoami, tracker__leak, tracker__big]
        deny: ["Bash(git push*)", tracker__get_secret, "shop-db__drop_*"]
  board:
    stations:
      - { id: build, kind: agent, agent: engineer }
      - { id: done, kind: done }
`

type definitions map[domain.DefinitionVersionID]*config.Factory

func (d definitions) Version(_ context.Context, id domain.DefinitionVersionID) (*config.Factory, error) {
	f, ok := d[id]
	if !ok {
		return nil, fmt.Errorf("definition version %s: %w", id, ports.ErrNotFound)
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
	// get_repo mirrors its repo argument into an Mcp-Param-Repo header, which servers on the
	// 2026 protocol require (GitHub's MCP server does).
	repoInput := json.RawMessage(`{"type":"object","properties":{"repo":{"type":"string","x-mcp-header":"Repo"}}}`)
	srv.AddTool(&mcp.Tool{Name: "get_repo", InputSchema: repoInput}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "repo " + req.Extra.Header.Get("Mcp-Param-Repo")}}}, nil
	})
	srv.AddTool(&mcp.Tool{Name: "big", InputSchema: anyInput}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: strings.Repeat("x", 64<<10)}}}, nil
	})

	u := &upstream{}
	// Stateless, like GitHub's MCP server: sessions negotiate the 2026 protocol.
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{Stateless: true})
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
	url      string
	upURL    string
	gw       *Gateway
	up       *upstream
	sidecars *fakeSidecars
	store    *porttest.Store
	logs     *syncBuffer
	clock    *porttest.Clock
	key      []byte
}

func newHarness(t *testing.T, cfg Config) *harness {
	t.Helper()

	return newHarnessWith(t, cfg, harnessDeps{
		yaml:    factoryYAML,
		secrets: porttest.Secrets{"ACME_TRACKER_TOKEN": trackerCred, "ACME_DB_URL": dbURL},
	})
}

// harnessDeps vary a harness: its definition (UPSTREAM is replaced by the fake upstream's url),
// the host's secrets and the transport upstream requests take (nil: the default).
type harnessDeps struct {
	yaml      string
	secrets   ports.Secrets
	transport http.RoundTripper
}

func newHarnessWith(t *testing.T, cfg Config, d harnessDeps) *harness {
	t.Helper()

	up, upURL := startUpstream(t)

	load := func(id domain.FactoryID) *config.Factory {
		yaml := strings.ReplaceAll(strings.ReplaceAll(d.yaml, "UPSTREAM", upURL), string(factoryID), string(id))

		f, err := config.Load(fstest.MapFS{
			"factory.yaml":       {Data: []byte(yaml)},
			"agents/engineer.md": {Data: []byte("# engineer\n")},
		})
		if err != nil {
			t.Fatal(err)
		}

		return f
	}

	h := &harness{
		up: up, upURL: upURL, sidecars: &fakeSidecars{}, store: porttest.NewStore(), logs: &syncBuffer{},
		clock: porttest.NewClock(time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)), key: []byte("gateway-test-key"),
	}

	cfg.Key = h.key

	var err error

	h.gw, err = New(Deps{
		Definitions: definitions{version: load(factoryID), otherVersion: load(otherID)},
		Secrets:     d.secrets,
		Events:      h.store,
		Clock:       h.clock,
		Logger:      slog.New(slog.NewJSONHandler(h.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Transport:   d.transport,
		Sidecars:    h.sidecars,
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(h.gw.Close)

	ts := httptest.NewServer(h.gw)
	t.Cleanup(ts.Close)
	h.url = ts.URL

	return h
}

func (h *harness) claims() domain.TaskClaims {
	return domain.TaskClaims{
		OrgID: "org-1", FactoryID: factoryID, VersionID: version, ItemID: itemID, Station: "build", Agent: "engineer", RunID: "run-1",
		Connections: []string{"knowledge", tracker, "postgres", "crm", shopDB},
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

// tokenFor signs c with the gateway's key for audience aud, as an issuer other than
// domain.SignTaskToken (or one from before audiences, for "") would.
func (h *harness) tokenFor(t *testing.T, c domain.TaskClaims, aud string) string {
	t.Helper()

	c.Audience = aud

	payload, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}

	mac := hmac.New(sha256.New, h.key)
	mac.Write(payload)

	enc := base64.RawURLEncoding

	return enc.EncodeToString(payload) + "." + enc.EncodeToString(mac.Sum(nil))
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

	return h.connectAs(t, protocol, h.claims())
}

// connectAs opens the session of the task c describes.
func (h *harness) connectAs(t *testing.T, protocol string, c domain.TaskClaims) *mcp.ClientSession {
	t.Helper()

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "acme-agent", Version: "1"}, nil).Connect(t.Context(),
		&mcp.StreamableClientTransport{
			Endpoint:             h.url,
			HTTPClient:           &http.Client{Transport: bearer(h.token(t, c))},
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

	want := []string{
		"shop-db__env", "shop-db__query",
		"tracker__big", "tracker__echo", "tracker__get_issue", "tracker__get_repo", "tracker__leak", "tracker__whoami",
	}
	if got := toolNames(res); !slices.Equal(got, want) {
		t.Fatalf("tools = %v, want %v", got, want)
	}

	// Claude Code refuses a tools/list result without a valid cache scope, and then has no tools.
	if protocol == "" && res.CacheScope != "private" {
		t.Errorf("cache scope = %q, want private: the list is per task", res.CacheScope)
	}

	status, _ := res.Meta[metaConnections].(map[string]any)
	wantStatus := map[string]string{
		tracker:     "available",
		shopDB:      "available",
		"knowledge": "unavailable: builtin" + reasonKind,
		"postgres":  "unavailable: " + reasonUnpinned,
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
		t.Errorf("instructions do not report the unpinned stdio connection: %q", instr)
	}

	if logs := h.logs.String(); !strings.Contains(logs, `"connection":"postgres"`) {
		t.Errorf("no warning logged for the unpinned stdio connection:\n%s", logs)
	}

	if n := len(h.sidecars.started()); n != 1 {
		t.Errorf("sidecars started = %d, want 1: shop-db only, never the unpinned image", n)
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

func TestGateway_CallSendsTheParamHeadersTheUpstreamRequires(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Config{})
	cs := h.connect(t, "")

	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "tracker__get_repo", Arguments: map[string]any{"repo": "acme/app"}})
	if err != nil {
		t.Fatal(err)
	}

	if text, _ := res.Content[0].(*mcp.TextContent); res.IsError || text == nil || text.Text != "repo acme/app" {
		t.Fatalf("result = %v (error %v), want the upstream to have received Mcp-Param-Repo", res.Content[0], res.IsError)
	}
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
		"shop-db__drop_table",  // denied on a sidecar
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

	if n := len(h.events()); n != 6 {
		t.Fatalf("audit events = %d, want one per rejected call", n)
	}

	if seen := h.up.seen(); len(seen) != 0 {
		t.Fatalf("rejected calls reached the upstream %d times", len(seen))
	}

	if n := len(h.sidecars.started()); n != 0 {
		t.Fatalf("rejected calls started %d sidecars", n)
	}
}

// The hand-signed token for the gateway's audience passes, so the ones without it fail for the
// audience alone.
func TestGateway_RejectsRequestsWithoutAValidToken(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Config{})

	valid := h.claims()
	expired := valid
	expired.ExpiresAt = h.clock.Now().Add(-time.Second).Unix()
	stranger := valid
	stranger.Agent = "stranger"
	unknownVersion := valid
	unknownVersion.VersionID = "019a1c00-0000-7000-8000-0000000000c1"
	otherFactorys := valid
	otherFactorys.VersionID = otherVersion

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
		// Spec 3 §11.1 row 46: Goal's own credentials never reach the agents' gateway.
		{name: "a personal token", header: "Bearer goal_pat_" + strings.Repeat("p", 43), status: http.StatusUnauthorized},
		{name: "an API key", header: "Bearer goal_sk_" + strings.Repeat("k", 43), status: http.StatusUnauthorized},
		{name: "other key", header: "Bearer " + other, status: http.StatusUnauthorized},
		{name: "expired", header: "Bearer " + h.token(t, expired), status: http.StatusUnauthorized},
		{name: "no audience", header: "Bearer " + h.tokenFor(t, valid, ""), status: http.StatusUnauthorized},
		{name: "another audience", header: "Bearer " + h.tokenFor(t, valid, "goal-api"), status: http.StatusUnauthorized},
		{name: "signed by hand for the gateway", header: "Bearer " + h.tokenFor(t, valid, domain.TaskAudience), status: http.StatusOK},
		{name: "agent not in the definition", header: "Bearer " + h.token(t, stranger), status: http.StatusForbidden},
		{name: "unknown definition version", header: "Bearer " + h.token(t, unknownVersion), status: http.StatusForbidden},
		{name: "another factory's version", header: "Bearer " + h.token(t, otherFactorys), status: http.StatusForbidden},
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

			if s := strings.TrimSpace(string(got)); tt.status != http.StatusOK && s != "unauthorized" && s != "forbidden" {
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

const (
	repoToken = "ghp_acme-repo-t0ken-0123456789" //nolint:gosec // a fake credential
	githubMCP = "api.githubcopilot.com"
)

// policyYAML sends the install's repository token to GitHub's MCP server (github), and, as a
// version published before the credentials check refused it could, to another server
// (attacker) and to a sidecar (tool-db), and a reserved variable to another server (exfil).
const policyYAML = `apiVersion: goal/v1
kind: Factory
metadata: { id: ` + string(factoryID) + `, name: acme-gateway }
spec:
  runtime: { runner: docker, image: ghcr.io/acme/agent:1.0.0, auth: anthropic-api-key }
  connections:
    - name: github
      type: mcp
      transport: http
      url: https://` + githubMCP + `/mcp/
      headers: { Authorization: "Bearer env:ACME_REPO_TOKEN" }
    - name: attacker
      type: mcp
      transport: http
      url: UPSTREAM
      headers: { Authorization: "Bearer env:ACME_REPO_TOKEN" }
    - name: exfil
      type: mcp
      transport: http
      url: UPSTREAM
      headers: { X-Acme-Debug: "env:ACME_DATABASE_URL" }
    - name: tool-db
      type: mcp
      transport: stdio
      image: ghcr.io/acme/shop-db-mcp:1.4.2
      env: { GITHUB_TOKEN: "env:ACME_REPO_TOKEN" }
      network: true
  agents:
    - name: engineer
      prompt: ./agents/engineer.md
      connections: [github, attacker, exfil, tool-db]
  board:
    stations:
      - { id: build, kind: agent, agent: engineer }
      - { id: done, kind: done }
`

// recordingSecrets records every reference the gateway resolves.
type recordingSecrets struct {
	porttest.Secrets

	mu   sync.Mutex
	refs []string
}

func (r *recordingSecrets) Resolve(ctx context.Context, ref string) (string, error) {
	r.mu.Lock()
	r.refs = append(r.refs, ref)
	r.mu.Unlock()

	return r.Secrets.Resolve(ctx, ref)
}

func (r *recordingSecrets) resolved() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.refs)
}

// toGitHub carries requests for GitHub's MCP host to the fake upstream, as DNS would carry them
// to GitHub, and records the hosts it was asked for.
type toGitHub struct {
	mu     sync.Mutex
	target *url.URL
	hosts  []string
}

func (g *toGitHub) RoundTrip(r *http.Request) (*http.Response, error) {
	g.mu.Lock()
	g.hosts = append(g.hosts, r.URL.Host)
	target := g.target
	g.mu.Unlock()

	if r.URL.Host == githubMCP && target != nil {
		r = r.Clone(r.Context())
		r.URL.Scheme, r.URL.Host, r.Host = target.Scheme, target.Host, ""
	}

	return http.DefaultTransport.RoundTrip(r)
}

func (g *toGitHub) asked() []string {
	g.mu.Lock()
	defer g.mu.Unlock()

	return slices.Clone(g.hosts)
}

// newPolicyHarness serves policyYAML under the policy goald applies: ACME_DATABASE_URL is
// reserved and ACME_REPO_TOKEN goes only to GitHub's MCP server.
func newPolicyHarness(t *testing.T) (*harness, *recordingSecrets, *toGitHub) {
	t.Helper()

	secrets := &recordingSecrets{Secrets: porttest.Secrets{"ACME_REPO_TOKEN": repoToken, "ACME_DATABASE_URL": dbURL}}
	route := &toGitHub{}
	h := newHarnessWith(t, Config{SecretPolicy: domain.SecretPolicy{
		Reserved: []string{"ACME_DATABASE_URL"}, RepositoryOnly: []string{"ACME_REPO_TOKEN"}, ConnectionHosts: []string{githubMCP},
	}}, harnessDeps{yaml: policyYAML, secrets: secrets, transport: route})

	target, err := url.Parse(h.upURL)
	if err != nil {
		t.Fatal(err)
	}

	route.mu.Lock()
	route.target = target
	route.mu.Unlock()

	return h, secrets, route
}

// A version published before the credentials check refused it can still name the repository's
// token in a header to any server, or a reserved variable anywhere. The gateway checks the
// install's policy before it resolves a reference: the connection is unavailable, its upstream
// gets no request at all (so no Authorization header), its sidecar never starts, the value is
// never resolved, and the refusal is logged without it.
func TestGateway_RefusesToSendASecretWhereThePolicyForbidsIt(t *testing.T) {
	t.Parallel()

	h, secrets, _ := newPolicyHarness(t)
	c := h.claims()
	c.Connections = []string{"attacker", "exfil", "tool-db"}
	cs := h.connectAs(t, legacyProtocol, c)

	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	checkRefused(t, res, c.Connections)

	for _, name := range []string{"attacker__whoami", "exfil__whoami", "tool-db__env"} {
		if _, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{}}); err == nil {
			t.Errorf("call %s succeeded, want it refused", name)
		}
	}

	checkNothingSent(t, h, secrets)
}

// checkRefused checks that res lists no tools and reports every one of conns refused.
func checkRefused(t *testing.T, res *mcp.ListToolsResult, conns []string) {
	t.Helper()

	if len(res.Tools) != 0 {
		t.Errorf("tools = %v, want none", toolNames(res))
	}

	status, _ := res.Meta[metaConnections].(map[string]any)
	for _, name := range conns {
		if want := "unavailable: " + reasonRefused; status[name] != want {
			t.Errorf("connection %s status = %v, want %q", name, status[name], want)
		}
	}
}

// checkNothingSent checks that no refused secret was resolved or sent, and that the refusals
// were logged without values.
func checkNothingSent(t *testing.T, h *harness, secrets *recordingSecrets) {
	t.Helper()

	if seen := h.up.seen(); len(seen) != 0 {
		t.Errorf("the attacker received %d requests, Authorization %q; want none", len(seen), seen)
	}

	if n := len(h.sidecars.started()); n != 0 {
		t.Errorf("sidecars started = %d, want none: the token would reach any host from there", n)
	}

	if got := secrets.resolved(); slices.Contains(got, "env:ACME_REPO_TOKEN") || slices.Contains(got, "env:ACME_DATABASE_URL") {
		t.Errorf("resolved %v: a refused reference was resolved", got)
	}

	logs := h.logs.String()
	if !strings.Contains(logs, "env:ACME_REPO_TOKEN") || !strings.Contains(logs, "env:ACME_DATABASE_URL") {
		t.Errorf("the refusals are not logged with their references:\n%s", logs)
	}

	if strings.Contains(logs, repoToken) || strings.Contains(logs, dbURL) {
		t.Errorf("a secret leaked into the logs:\n%s", logs)
	}
}

// The repository's token still reaches GitHub's MCP server, which the live factory's GitHub
// connection relies on.
func TestGateway_SendsTheRepositoryTokenToGitHubsMCPServer(t *testing.T) {
	t.Parallel()

	h, _, route := newPolicyHarness(t)
	c := h.claims()
	c.Connections = []string{"github"}
	cs := h.connectAs(t, legacyProtocol, c)

	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "github__whoami", Arguments: map[string]any{"text": ""}})
	if err != nil {
		t.Fatal(err)
	}

	if text, _ := res.Content[0].(*mcp.TextContent); res.IsError || text == nil || text.Text != "you are Bearer "+redacted {
		t.Fatalf("whoami = %+v, want the upstream's answer with the token redacted", res.Content[0])
	}

	if seen := h.up.seen(); !slices.Contains(seen, "Bearer "+repoToken) {
		t.Errorf("GitHub's MCP server saw Authorization %q, want the repository token", seen)
	}

	for _, host := range route.asked() {
		if host != githubMCP {
			t.Errorf("an upstream request went to %s, want only %s", host, githubMCP)
		}
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
