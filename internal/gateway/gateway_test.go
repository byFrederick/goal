package gateway

import (
	"cmp"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

const (
	factoryID domain.FactoryID = "8d4f2b1a-3c5e-4f60-9a7b-1c2d3e4f5a6b"
	otherID   domain.FactoryID = "5c0d7a52-8b1e-4f43-9d3a-2e6b7c8f9a01"
	itemID    domain.ItemID    = "item-1"
	// agentEngineer is the agent of the tasks' runs.
	agentEngineer = "engineer"
)

// version is the definition version the tasks' runs started on; otherVersion is a version of
// another factory.
const (
	version      domain.DefinitionVersionID = "019a1c00-0000-7000-8000-0000000000a1"
	otherVersion domain.DefinitionVersionID = "019a1c00-0000-7000-8000-0000000000b1"
)

// factoryYAML gives its engineer the organization connection docs, which runs reach from their
// own box: the gateway serves none of a connection's tools.
const factoryYAML = `apiVersion: goal/v1
kind: Factory
metadata: { id: ` + string(factoryID) + `, name: acme-gateway }
spec:
  runtime: { modelConnection: claude }
  connections: [docs]
  agents:
    - name: engineer
      prompt: ./agents/engineer.md
      connections: [docs]
      tools:
        allow: [Read, Bash, "docs__search_*"]
        deny: ["Bash(git push*)"]
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

// AgentFiles stands in for the definitions cache's: a prompt naming the agent and, with a
// contract, its schema.
func (d definitions) AgentFiles(_ context.Context, id domain.DefinitionVersionID, agent domain.AgentSpec, output string) (map[string][]byte, error) {
	if _, ok := d[id]; !ok {
		return nil, fmt.Errorf("definition version %s: %w", id, ports.ErrNotFound)
	}

	files := map[string][]byte{"prompt.md": []byte("# " + agent.Name + "\n")}
	if output != "" {
		files["output.schema.json"] = []byte(`{"type":"object"}`)
	}

	return files, nil
}

// Rules stands in for the definitions cache's: the version's rules that apply to run, each under
// the hash of its body with its body as content.
func (d definitions) Rules(_ context.Context, id domain.DefinitionVersionID, run domain.RuleTarget) ([]domain.RunRule, error) {
	f, ok := d[id]
	if !ok {
		return nil, fmt.Errorf("definition version %s: %w", id, ports.ErrNotFound)
	}

	var declared []domain.Rule
	for _, r := range f.Spec.Rules {
		declared = append(declared, domain.Rule{Name: r.Name, AppliesTo: r.AppliesTo, Paths: r.Paths, Body: r.Body})
	}

	var out []domain.RunRule
	for _, r := range domain.EffectiveRules(declared, run) {
		out = append(out, domain.RunRule{Name: r.Name, SHA256: domain.HashBlob([]byte(r.Body)), Paths: r.Paths, Content: r.Body})
	}

	return out, nil
}

type harness struct {
	url   string
	gw    *Gateway
	store *porttest.Store
	// org is the organization of the tasks; connections hold its connections (bundle_test.go).
	org         domain.OrgID
	connections *porttest.Connections
	clock       *porttest.Clock
	key         []byte
}

func newHarness(t *testing.T, cfg Config) *harness {
	t.Helper()

	return newHarnessWith(t, cfg, harnessDeps{yaml: factoryYAML})
}

// harnessDeps vary a harness: its definition and its other files, by path.
type harnessDeps struct {
	yaml  string
	files map[string]string
	// schema, when set, is the output.schema.json of every agent with a contract.
	schema string
}

// withSchema is definitions whose agents with a contract get schema as their output schema.
type withSchema struct {
	definitions
	schema []byte
}

func (d withSchema) AgentFiles(ctx context.Context, id domain.DefinitionVersionID, agent domain.AgentSpec, output string) (map[string][]byte, error) {
	files, err := d.definitions.AgentFiles(ctx, id, agent, output)
	if err == nil && output != "" {
		files["output.schema.json"] = d.schema
	}

	return files, err
}

func newHarnessWith(t *testing.T, cfg Config, d harnessDeps) *harness {
	t.Helper()

	load := func(id domain.FactoryID) *config.Factory {
		files := fstest.MapFS{
			"factory.yaml":       {Data: []byte(strings.ReplaceAll(d.yaml, string(factoryID), string(id)))},
			"agents/engineer.md": {Data: []byte("# engineer\n")},
		}
		for name, body := range d.files {
			files[name] = &fstest.MapFile{Data: []byte(body)}
		}

		f, err := config.Load(files)
		if err != nil {
			t.Fatal(err)
		}

		return f
	}

	h := &harness{
		store: porttest.NewStore(), clock: porttest.NewClock(time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)), key: []byte("gateway-test-key"),
	}
	h.org, h.connections = newConnections(t, h.clock)

	cfg.Key = h.key

	base := definitions{version: load(factoryID), otherVersion: load(otherID)}

	var (
		defs Definitions = base
		err  error
	)

	if d.schema != "" {
		defs = withSchema{definitions: base, schema: []byte(d.schema)}
	}

	h.gw, err = New(Deps{
		Definitions: defs,
		Items:       h.store,
		Connections: h.connections,
		Events:      h.store,
		Clock:       h.clock,
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(h.gw)
	t.Cleanup(ts.Close)
	h.url = ts.URL

	return h
}

func (h *harness) claims() domain.TaskClaims {
	return domain.TaskClaims{
		OrgID: h.org, FactoryID: factoryID, VersionID: version, ItemID: itemID, Station: "build", Agent: agentEngineer, RunID: "run-1",
		Connections: []string{"docs"}, ExpiresAt: h.clock.Now().Add(time.Hour).Unix(),
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

// The gateway proxies no connection (spec 4 §5.9): an agent's organization connections are its
// box's, so none of their tools is listed, whatever protocol the agent speaks, and the list is
// the task's own.
func TestGateway_ServesNoConnectionsTools(t *testing.T) {
	t.Parallel()

	for _, protocol := range []string{"", legacyProtocol} {
		t.Run("protocol "+cmp.Or(protocol, "latest"), func(t *testing.T) {
			t.Parallel()

			res, err := newHarness(t, Config{}).connect(t, protocol).ListTools(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}

			if len(res.Tools) != 0 {
				t.Errorf("tools = %+v, want none of the connections'", res.Tools)
			}

			// Claude Code refuses a tools/list result without a valid cache scope, and then has no tools.
			if protocol == "" && res.CacheScope != "private" {
				t.Errorf("cache scope = %q, want private: the list is per task", res.CacheScope)
			}
		})
	}
}

// A call of a tool the gateway does not serve, a connection's included, is refused like an
// unknown tool, and audited as denied, without its arguments.
func TestGateway_RefusesToolsItDoesNotServe(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Config{})
	cs := h.connect(t, legacyProtocol)

	for _, name := range []string{"docs__search_pages", "tracker__echo", "docs"} {
		if _, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{"text": "hello"}}); err == nil {
			t.Errorf("CallTool(%s) succeeded, want it refused", name)
		}
	}

	events := h.events()
	if len(events) != 3 {
		t.Fatalf("audit events = %+v, want one per refused call", events)
	}

	for _, e := range events {
		if e.Kind != EventToolCall || e.RunID != "run-1" || e.Actor != agentEngineer || e.Data["outcome"] != outcomeDenied ||
			strings.Contains(fmt.Sprint(e.Data), "hello") {
			t.Errorf("event %+v, want a denied audit event of the run without its arguments", e)
		}
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
