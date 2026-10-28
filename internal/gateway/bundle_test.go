package gateway

import (
	"bytes"
	"cmp"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// The secret values of the harness's connections, which no bundle carries.
const (
	cloneToken   = "acme-repo-s3cr3t-0123456789"   //nolint:gosec // a fake secret the bundle must not carry
	issuesToken  = "acme-issues-s3cr3t-0123456789" //nolint:gosec // a fake secret the bundle must not carry
	modelAPIKey  = "acme-model-s3cr3t-0123456789"  //nolint:gosec // a fake secret the bundle must not carry
	connTracker  = "issues"
	connDocs     = "docs"
	trackerTitle = "Issue tracker"
	fieldToken   = "token"
	kindStdio    = "stdio-server"
)

// testKinds are the platform kinds of the harness's connections: a model's access, git over https
// with an API token, an issue tracker with a CLI and a remote MCP server, and a stdio server.
func testKinds() []domain.ConnectionKind {
	token := domain.KindField{Name: fieldToken, Title: "Token", Secret: true, Required: true}

	return []domain.ConnectionKind{
		{Name: "model-key", Title: "Model key", Platform: true, Spec: domain.KindSpec{
			Fields: []domain.KindField{token}, Env: map[string]string{"MODEL_KEY": "{{token}}"}, Model: true,
		}},
		{Name: "code-host", Title: "Code host", Platform: true, Spec: domain.KindSpec{
			Fields: []domain.KindField{token, {Name: "host", Title: "Host", Default: "github.com"}},
			Env:    map[string]string{"CODE_TOKEN": "{{token}}"},
			Git:    &domain.KindGit{Host: "{{host}}", Username: "x-access-token", PasswordEnv: "CODE_TOKEN"},
		}},
		{Name: "issue-tracker", Title: trackerTitle, Platform: true, Spec: domain.KindSpec{
			Fields: []domain.KindField{token, {Name: "site", Title: "Site", Required: true}},
			Env:    map[string]string{"ISSUES_TOKEN": "{{token}}", "ISSUES_SITE": "{{site}}"},
			CLI:    &domain.KindCLI{Tool: "npm:@acme/issues-cli@2.0.0"},
			MCP:    &domain.KindMCP{URL: "https://{{site}}/mcp", Headers: map[string]string{"Authorization": "Bearer ${ISSUES_TOKEN}"}},
			Docs:   "File and read issues with the issues CLI or its MCP tools.",
		}},
		{Name: kindStdio, Title: "MCP server", Platform: true, Spec: domain.KindSpec{
			Fields: []domain.KindField{{Name: "command", Title: "Command"}}, MCP: &domain.KindMCP{Command: []string{"sh", "-c", "{{command}}"}},
		}},
	}
}

// newConnections makes an organization and its connections of testKinds: claude (the model),
// code (git to github.com), issues, docs (a stdio server) and wiki (a stdio server too).
func newConnections(t *testing.T, clock *porttest.Clock) (domain.OrgID, *porttest.Connections) {
	t.Helper()

	tenancy := porttest.NewTenancy(clock.Now)

	org, err := tenancy.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: "acme", Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}

	c := porttest.NewConnections(porttest.NewMasterKeys().Key("k1"), tenancy)
	if err := c.SyncPlatformKinds(t.Context(), testKinds()); err != nil {
		t.Fatal(err)
	}

	for _, n := range []domain.NewConnection{
		{Name: "claude", Kind: "model-key", Secrets: map[string]string{fieldToken: modelAPIKey}},
		{Name: "code", Kind: "code-host", Secrets: map[string]string{fieldToken: cloneToken}},
		{Name: connTracker, Kind: "issue-tracker", Fields: map[string]string{"site": "issues.example.com"}, Secrets: map[string]string{fieldToken: issuesToken}},
		{Name: connDocs, Kind: kindStdio, Fields: map[string]string{"command": "npx -y @acme/docs-mcp@1.0.0"}},
		{Name: "wiki", Kind: kindStdio, Fields: map[string]string{"command": "npx -y @acme/wiki-mcp@1.0.0"}},
	} {
		if _, err := c.Create(domain.WithOrg(t.Context(), org.ID), n, domain.LocalUser); err != nil {
			t.Fatal(err)
		}
	}

	return org.ID, c
}

// bundleYAML is a factory on connections with a private repository, a contract at the agent's
// station and the limits, tools and tool rules a run carries. Its agent names two organization
// connections its token grants and one (wiki) it does not.
const bundleYAML = `apiVersion: goal/v1
kind: Factory
metadata: { id: ` + string(factoryID) + `, name: acme-bundle, purpose: Ship small changes to acme's API. }
spec:
  runtime:
    model: claude-sonnet-5-5
    modelConnection: claude
    tools: ["task@3.46.1"]
    repository: { url: "https://github.com/acme/api.git", connection: code }
    budget: { maxTurnsPerTask: 40, perItemUsd: 6 }
  connections: [issues, docs, wiki]
  skills:
    - { name: acme-conventions, source: ./skills/acme-conventions }
  rules:
    - { name: go-errors, source: ./rules/go-errors.md }
  contracts: { change: contracts/change.json }
  agents:
    - name: engineer
      prompt: ./agents/engineer.md
      model: claude-opus-5-5
      skills: [acme-conventions]
      connections: [issues, docs, wiki]
      tools:
        allow: [Read, Edit, "Bash(npm test*)", "issues__get_*"]
        deny: ["Bash(git push --force*)", issues__get_secret]
  board:
    stations:
      - { id: spec, kind: human }
      - { id: build, kind: agent, agent: engineer, output: change }
      - { id: done, kind: done }
    flow:
      assemblies: { freshBase: merge }
`

var bundleFiles = map[string]string{
	"skills/acme-conventions/SKILL.md": "---\nname: acme-conventions\n---\n",
	"contracts/change.json":            `{"type":"object"}`,
	"rules/go-errors.md":               "---\ndescription: Wrap errors.\npaths: [\"**/*.go\"]\n---\nWrap with %w.\n",
}

// runServer serves the gateway's run bundle the way goald mounts it.
func (h *harness) runServer(t *testing.T) string {
	t.Helper()

	ts := httptest.NewServer(http.HandlerFunc(h.gw.ServeRun))
	t.Cleanup(ts.Close)

	return ts.URL + "/run"
}

// submit creates an item of factory f in the harness's organization, with spec's output.
func (h *harness) submit(t *testing.T, f domain.FactoryID, title, body string) domain.Item {
	t.Helper()

	ctx := domain.WithOrg(t.Context(), h.claims().OrgID)
	at := h.clock.Now()

	it, err := h.store.CreateItem(ctx, domain.NewItem{
		FactoryID: f, Title: title, Body: body, Station: "spec", State: domain.StateQueued, CreatedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}

	it, err = h.store.Transition(ctx, ports.Transition{
		ItemID: it.ID, From: it.Position(), To: domain.Position{Station: h.claims().Station, State: domain.StateQueued},
		OutputStation: "spec", Output: json.RawMessage(`{"lane":"backend"}`), At: at,
	})
	if err != nil {
		t.Fatal(err)
	}

	return it
}

// request sends method to url with token (none when empty) and returns the status, headers and
// body of the answer.
func request(t *testing.T, method, url, token string) (int, http.Header, []byte) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), method, url, nil)
	if err != nil {
		t.Fatal(err)
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	return resp.StatusCode, resp.Header, body
}

// The bundle is the run's task, its agent's files from the run's version, the repository with
// the item's branch and how a run freshens it, and what the run's connections give the box without a value: the agent's
// connections its token grants (env names, MCP servers with headers naming env variables, docs),
// a git login per connection that signs git in, naming the variable of its password, and the
// runtime's tools with the connections' CLI tools.
func TestServeRun_ServesTheRunsBundle(t *testing.T) {
	t.Parallel()

	h := newHarnessWith(t, Config{}, harnessDeps{yaml: bundleYAML, files: bundleFiles})
	it := h.submit(t, factoryID, "Add a health endpoint", "For the load balancer.")
	h.setToolModes(t, connTracker, map[string]domain.ConnectionToolMode{toolComment: domain.ToolModeAsk, toolDelete: domain.ToolModeOff})

	c := h.claims()
	c.ItemID, c.Connections = it.ID, []string{connTracker, connDocs}

	status, header, body := request(t, http.MethodGet, h.runServer(t), h.token(t, c))
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}

	if ct, cc := header.Get("Content-Type"), header.Get("Cache-Control"); ct != "application/json" || cc != "no-store" {
		t.Errorf("Content-Type %q, Cache-Control %q; want application/json, no-store", ct, cc)
	}

	for _, secret := range []string{cloneToken, issuesToken, modelAPIKey} {
		if strings.Contains(string(body), secret) {
			t.Fatalf("the bundle carries a connection's secret value:\n%s", body)
		}
	}

	// The worker's Vitest parses the same file with its Bundle schema, so both ends agree.
	checkGolden(t, body)

	var got runBundle
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}

	want := runBundle{
		Spec: bundleSpec{
			FactoryID: factoryID, ItemID: it.ID, RunID: c.RunID, Station: c.Station, Agent: c.Agent, Model: "claude-opus-5-5",
			Skills: []string{"acme-conventions"}, ToolsAllow: []string{"Read", "Edit", "Bash(npm test*)", "issues__get_*"},
			ToolsDeny: []string{"Bash(git push --force*)", "issues__get_secret"}, Purpose: "Ship small changes to acme's API.",
			Brief:  "Add a health endpoint\n\nFor the load balancer.",
			Inputs: map[string]json.RawMessage{"spec": json.RawMessage(`{"lane":"backend"}`)},
			Output: "change", MaxTurns: 40, MaxUSD: 6,
			Connections: []bundleConnection{
				{
					Name: connTracker, Kind: "issue-tracker", Title: trackerTitle, Env: []string{"ISSUES_SITE", "ISSUES_TOKEN"},
					MCP:                &domain.KindMCP{URL: "https://issues.example.com/mcp", Headers: map[string]string{"Authorization": "Bearer ${ISSUES_TOKEN}"}},
					Docs:               "File and read issues with the issues CLI or its MCP tools.",
					ToolModes:          map[string]domain.ConnectionToolMode{toolComment: domain.ToolModeAsk, toolDelete: domain.ToolModeOff},
					AlwaysAllowOffered: []string{toolComment},
				},
				{
					Name: connDocs, Kind: kindStdio, Title: "MCP server", Env: []string{},
					MCP:       &domain.KindMCP{Command: []string{"sh", "-c", "npx -y @acme/docs-mcp@1.0.0"}},
					ToolModes: map[string]domain.ConnectionToolMode{}, AlwaysAllowOffered: []string{},
				},
			},
			Rules:        []bundleRule{{Name: "go-errors", SHA256: domain.HashBlob([]byte("Wrap with %w.\n")), Paths: []string{"**/*.go"}, Content: "Wrap with %w.\n"}},
			GrantedCalls: []grantedCall{},
		},
		Files:      map[string][]byte{"agent/prompt.md": []byte("# engineer\n"), "agent/output.schema.json": []byte(`{"type":"object"}`)},
		Repository: &bundleRepository{URL: "https://github.com/acme/api.git", Branch: it.Branch(), FreshBase: domain.FreshBaseMerge},
		Git:        []gitCredential{{Host: "github.com", Username: "x-access-token", PasswordEnv: "CODE_TOKEN"}},
		Tools:      []string{"task@3.46.1", "npm:@acme/issues-cli@2.0.0"},
	}

	if g, w := mustJSON(t, got), mustJSON(t, want); g != w {
		t.Errorf("bundle\n got %s\nwant %s", g, w)
	}
}

// A part on its assembly's integration branch gets the branch and base the engine put in its
// task token (ADR 0033).
func TestServeRun_BundleBaseFromTheTaskToken(t *testing.T) {
	t.Parallel()

	h := newHarnessWith(t, Config{}, harnessDeps{yaml: bundleYAML, files: bundleFiles})
	it := h.submit(t, factoryID, "Add a health endpoint", "")
	c := h.claims()
	c.ItemID, c.Branch, c.Base = it.ID, "goal/9-store", "goal/a8-big-request"

	status, _, body := request(t, http.MethodGet, h.runServer(t), h.token(t, c))
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}

	var got runBundle
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}

	want := bundleRepository{URL: "https://github.com/acme/api.git", Branch: c.Branch, Base: c.Base, FreshBase: domain.FreshBaseMerge}
	if got.Repository == nil || *got.Repository != want {
		t.Errorf("repository = %+v, want %+v", got.Repository, want)
	}

	if !strings.Contains(string(body), `"base":"goal/a8-big-request"`) {
		t.Errorf("the bundle names no base:\n%s", body)
	}
}

// A repository connection that is also one of the agent's signs git in once.
func TestServeRun_OneGitLoginPerCredential(t *testing.T) {
	t.Parallel()

	yaml := strings.ReplaceAll(strings.ReplaceAll(bundleYAML, "connections: [issues, docs, wiki]\n  skills", "connections: [issues, docs, wiki, code]\n  skills"),
		"connections: [issues, docs, wiki]\n      tools", "connections: [code, issues]\n      tools")
	h := newHarnessWith(t, Config{}, harnessDeps{yaml: yaml, files: bundleFiles})
	it := h.submit(t, factoryID, "Add a health endpoint", "")
	c := h.claims()
	c.ItemID, c.Connections = it.ID, []string{"code", connTracker}

	status, _, body := request(t, http.MethodGet, h.runServer(t), h.token(t, c))
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}

	var got runBundle
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}

	if len(got.Git) != 1 || len(got.Spec.Connections) != 2 || got.Spec.Connections[0].Name != "code" {
		t.Errorf("git credentials %+v and connections %+v; want one login and the agent's two connections in its order", got.Git, got.Spec.Connections)
	}
}

// A part never plans an assembly, so its output schema has no plan for structured output to
// produce; an item that is not a part, and a contract without a plan, get the contract as is.
func TestServeRun_PartsOutputSchemaHasNoPlan(t *testing.T) {
	t.Parallel()

	const (
		withPlan    = `{"type":"object","required":["lane","plan"],"properties":{"lane":{"type":"string"},"plan":{"type":"array"}},"additionalProperties":false}`
		withoutPlan = `{"type":"object","required":["lane"],"properties":{"lane":{"type":"string"}},"additionalProperties":false}`
	)

	tests := []struct {
		name     string
		contract string
		part     bool
		want     string
	}{
		{name: "part", contract: withPlan, part: true, want: withoutPlan},
		{name: "item", contract: withPlan, want: withPlan},
		{name: "part without a plan", contract: withoutPlan, part: true, want: withoutPlan},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarnessWith(t, Config{}, harnessDeps{yaml: bundleYAML, files: bundleFiles, schema: tt.contract})
			it := h.submit(t, factoryID, "Add a health endpoint", "")
			c := h.claims()

			if tt.part {
				part, err := h.store.CreateItem(domain.WithOrg(t.Context(), c.OrgID), domain.NewItem{
					FactoryID: factoryID, ParentID: it.ID, Title: "Store", Body: "Add the table.",
					Station: c.Station, State: domain.StateQueued, CreatedAt: h.clock.Now(),
				})
				if err != nil {
					t.Fatal(err)
				}

				it = part
			}

			c.ItemID = it.ID

			status, _, body := request(t, http.MethodGet, h.runServer(t), h.token(t, c))
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200 (%s)", status, body)
			}

			var got runBundle
			if err := json.Unmarshal(body, &got); err != nil {
				t.Fatal(err)
			}

			var g, w any
			if err := json.Unmarshal(got.Files["agent/output.schema.json"], &g); err != nil {
				t.Fatal(err)
			}

			if err := json.Unmarshal([]byte(tt.want), &w); err != nil {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(g, w) {
				t.Errorf("output schema %s, want %s", got.Files["agent/output.schema.json"], tt.want)
			}
		})
	}
}

// A factory without a repository leaves the workspace empty: no repository, no credential.
func TestServeRun_WithoutARepository(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Config{})
	it := h.submit(t, factoryID, "Write the spec", "")
	c := h.claims()
	c.ItemID = it.ID

	status, _, body := request(t, http.MethodGet, h.runServer(t), h.token(t, c))
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}

	if _, ok := got["repository"]; ok || string(got["git_credentials"]) != "[]" {
		t.Errorf("repository %s, git_credentials %s; want none and []", got["repository"], got["git_credentials"])
	}
}

func TestServeRun_RefusesWhatTheTokenDoesNotName(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Config{})
	url := h.runServer(t)
	it := h.submit(t, factoryID, "Write the spec", "")
	foreign := h.submit(t, otherID, "Someone else's", "")

	valid := h.claims()
	valid.ItemID = it.ID
	expired := valid
	expired.ExpiresAt = h.clock.Now().Add(-time.Second).Unix()
	stranger := valid
	stranger.Agent = "stranger"
	noStation := valid
	noStation.Station = "deploy"
	otherItem := valid
	otherItem.ItemID = foreign.ID
	noItem := valid
	noItem.ItemID = "item-gone"
	otherFactorys := valid
	otherFactorys.VersionID = otherVersion
	// A token of another organization naming this one's item and version: the item is looked up
	// in the token's organization, where it is not.
	otherOrg := valid
	otherOrg.OrgID = "01900000-0000-7000-8000-00000000beef"

	tests := []struct {
		name   string
		method string
		token  string
		status int
	}{
		{name: "no token", status: http.StatusUnauthorized},
		{name: "garbage", token: "not-a-token", status: http.StatusUnauthorized},
		{name: "expired", token: h.token(t, expired), status: http.StatusUnauthorized},
		{name: "another audience", token: h.tokenFor(t, valid, "goal-api"), status: http.StatusUnauthorized},
		{name: "agent not in the definition", token: h.token(t, stranger), status: http.StatusForbidden},
		{name: "station not in the definition", token: h.token(t, noStation), status: http.StatusForbidden},
		{name: "another factory's item", token: h.token(t, otherItem), status: http.StatusForbidden},
		{name: "no such item", token: h.token(t, noItem), status: http.StatusForbidden},
		{name: "another factory's version", token: h.token(t, otherFactorys), status: http.StatusForbidden},
		{name: "another organization's item", token: h.token(t, otherOrg), status: http.StatusForbidden},
		{name: "not a GET", method: http.MethodPost, token: h.token(t, valid), status: http.StatusMethodNotAllowed},
		{name: "valid", token: h.token(t, valid), status: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, header, body := request(t, cmp.Or(tt.method, http.MethodGet), url, tt.token)
			if status != tt.status {
				t.Fatalf("status = %d, want %d (%s)", status, tt.status, body)
			}

			switch s := strings.TrimSpace(string(body)); tt.status {
			case http.StatusUnauthorized, http.StatusForbidden:
				if s != "unauthorized" && s != "forbidden" {
					t.Errorf("body = %q, want no detail", s)
				}
			case http.StatusMethodNotAllowed:
				if header.Get("Allow") != http.MethodGet {
					t.Errorf("Allow = %q, want GET", header.Get("Allow"))
				}
			}
		})
	}
}

const bundleGolden = "testdata/run-bundle.json"

var updateGolden = flag.Bool("update", false, "rewrite "+bundleGolden+" from the served bundle")

// checkGolden compares a served bundle with bundleGolden, indented (-update rewrites it).
func checkGolden(t *testing.T, body []byte) {
	t.Helper()

	var indented bytes.Buffer
	if err := json.Indent(&indented, body, "", "  "); err != nil {
		t.Fatal(err)
	}

	if *updateGolden {
		if err := os.WriteFile(bundleGolden, indented.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	want, err := os.ReadFile(bundleGolden)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(indented.Bytes(), want) {
		t.Errorf("the bundle differs from %s (go test -run %s -update rewrites it):\n%s", bundleGolden, t.Name(), indented.Bytes())
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()

	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

// rulesYAML is a factory whose rules apply to every run (secrets) or are scoped to an agent
// (designer), a station (review), a lane (backend), a work type (incident) and, for the reviewer,
// the lane and work type of the work it reviews.
const rulesYAML = `apiVersion: goal/v1
kind: Factory
metadata: { id: ` + string(factoryID) + `, name: acme-rules }
spec:
  runtime: { modelConnection: claude }
  rules:
    - { name: secrets, source: ./rules/secrets.md }
    - { name: design, source: ./rules/design.md }
    - { name: reviewing, source: ./rules/reviewing.md }
    - { name: backend, source: ./rules/backend.md }
    - { name: incidents, source: ./rules/incidents.md }
    - { name: backend-features, source: ./rules/backend-features.md }
  agents:
    - { name: engineer, prompt: ./agents/engineer.md }
    - { name: designer, prompt: ./agents/engineer.md }
    - { name: reviewer, prompt: ./agents/engineer.md }
  board:
    workTypes: [feature, incident]
    stations:
      - { id: build, kind: agent, route: { backend: engineer, frontend: designer } }
      - { id: review, kind: agent, agent: reviewer }
      - { id: done, kind: done }
`

var rulesFiles = map[string]string{
	"rules/secrets.md":          "---\ndescription: Keep secrets out.\n---\nNever print a token.\n",
	"rules/design.md":           "---\ndescription: Design.\nappliesTo: { agents: [designer] }\n---\nUse the design system.\n",
	"rules/reviewing.md":        "---\ndescription: Review.\nappliesTo: { stations: [review] }\n---\nRead the diff.\n",
	"rules/backend.md":          "---\ndescription: Backend.\nappliesTo: { lanes: [backend] }\npaths: [\"**/*.go\"]\n---\nWrap errors.\n",
	"rules/incidents.md":        "---\ndescription: Incidents.\nappliesTo: { workTypes: [incident] }\n---\nPage someone.\n",
	"rules/backend-features.md": "---\ndescription: Features.\nappliesTo: { agents: [reviewer], lanes: [backend], workTypes: [feature] }\n---\nCheck the tests.\n",
}

// queue creates an item of the harness's factory of work type feature in lane backend at station.
func (h *harness) queue(t *testing.T, station string) domain.Item {
	t.Helper()

	it, err := h.store.CreateItem(domain.WithOrg(t.Context(), h.claims().OrgID), domain.NewItem{
		FactoryID: factoryID, Type: "feature", Lane: "backend", Title: "Add a health endpoint",
		Station: station, State: domain.StateQueued, CreatedAt: h.clock.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	return it
}

// fetchBundle serves claims c their bundle and decodes it.
func (h *harness) fetchBundle(t *testing.T, url string, c domain.TaskClaims) runBundle {
	t.Helper()

	status, _, body := request(t, http.MethodGet, url, h.token(t, c))
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}

	var b runBundle
	if err := json.Unmarshal(body, &b); err != nil {
		t.Fatal(err)
	}

	return b
}

// bundleRuleOf is rule name of rulesFiles as the harness's definitions serve it: its body under
// the body's hash.
func bundleRuleOf(name string, paths ...string) bundleRule {
	_, body, _ := strings.Cut(strings.TrimPrefix(rulesFiles["rules/"+name+".md"], "---\n"), "\n---\n")

	return bundleRule{Name: name, SHA256: domain.HashBlob([]byte(body)), Paths: paths, Content: body}
}

// A run gets the rules whose appliesTo matches its agent, station and its item's lane and work
// type; a reviewer the same way, by the lane and work type of the item it reviews.
func TestBundle_RulesSelectedByAgentStationLaneAndWorkType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, station, agent string
		want                 []bundleRule
	}{
		{name: agentEngineer, station: "build", agent: agentEngineer, want: []bundleRule{bundleRuleOf("secrets"), bundleRuleOf("backend", "**/*.go")}},
		{name: "reviewer", station: "review", agent: "reviewer", want: []bundleRule{
			bundleRuleOf("secrets"), bundleRuleOf("reviewing"), bundleRuleOf("backend", "**/*.go"), bundleRuleOf("backend-features"),
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarnessWith(t, Config{}, harnessDeps{yaml: rulesYAML, files: rulesFiles})
			c := h.claims()
			c.ItemID, c.Station, c.Agent, c.Connections = h.queue(t, tt.station).ID, tt.station, tt.agent, nil

			got := h.fetchBundle(t, h.runServer(t), c)

			if g, w := mustJSON(t, got.Spec.Rules), mustJSON(t, tt.want); g != w {
				t.Errorf("rules\n got %s\nwant %s", g, w)
			}
		})
	}
}

// The run's rules are recorded once, by name and hash, however often its bundle is fetched.
func TestBundle_RulesAppliedEventWrittenOncePerRun(t *testing.T) {
	t.Parallel()

	h := newHarnessWith(t, Config{}, harnessDeps{yaml: rulesYAML, files: rulesFiles})
	it := h.queue(t, "build")
	c := h.claims()
	c.ItemID, c.Connections = it.ID, nil
	url := h.runServer(t)

	h.fetchBundle(t, url, c)
	h.fetchBundle(t, url, c)

	applied := rulesApplied(h.store.Events(it.ID))
	if len(applied) != 1 {
		t.Fatalf("%d rules_applied events, want 1: %+v", len(applied), applied)
	}

	e := applied[0]
	if e.RunID != c.RunID || e.Station != "build" || e.Actor != agentEngineer || e.FactoryID != factoryID {
		t.Errorf("event %+v; want run %s at build by engineer", e, c.RunID)
	}

	want := []map[string]any{
		{"name": "secrets", "sha256": string(bundleRuleOf("secrets").SHA256)},
		{"name": "backend", "sha256": string(bundleRuleOf("backend").SHA256)},
	}
	if g, w := mustJSON(t, e.Data["rules"]), mustJSON(t, want); g != w {
		t.Errorf("rules %s, want %s", g, w)
	}

	// Another run of the item records its own.
	c.RunID = "run-2"
	h.fetchBundle(t, url, c)

	if all := rulesApplied(h.store.Events(it.ID)); len(all) != 2 || all[1].RunID != c.RunID {
		t.Errorf("after another run's bundle, rules_applied events %+v; want one per run", all)
	}
}

// A run that no rule applies to records none.
func TestBundle_NoRulesAppliedEventWithoutRules(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Config{})
	it := h.submit(t, factoryID, "Write the spec", "")
	c := h.claims()
	c.ItemID = it.ID

	if b := h.fetchBundle(t, h.runServer(t), c); b.Spec.Rules == nil || len(b.Spec.Rules) != 0 {
		t.Errorf("rules %v, want []", b.Spec.Rules)
	}

	if applied := rulesApplied(h.store.Events(it.ID)); len(applied) != 0 {
		t.Errorf("rules_applied events %+v, want none", applied)
	}
}

func rulesApplied(events []domain.Event) []domain.Event {
	var out []domain.Event

	for _, e := range events {
		if e.Kind == domain.EventRulesApplied {
			out = append(out, e)
		}
	}

	return out
}

// The bundle's model is the one the engine chose at run start, in the task token, and its deny
// rules are the agent's followed by the organization's denied commands, each once.
func TestBundle_ModelAndDeniedCommandsFromTheTaskToken(t *testing.T) {
	t.Parallel()

	h := newHarnessWith(t, Config{}, harnessDeps{yaml: bundleYAML, files: bundleFiles})
	it := h.submit(t, factoryID, "Add a health endpoint", "")
	c := h.claims()
	c.ItemID, c.Model = it.ID, "claude-haiku-5-5"
	c.DeniedCommands = []string{"Bash(terraform apply:*)", "Bash(git push --force*)"}

	status, _, body := request(t, http.MethodGet, h.runServer(t), h.token(t, c))
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}

	var got runBundle
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}

	if got.Spec.Model != "claude-haiku-5-5" {
		t.Errorf("model %q, want the token's claude-haiku-5-5", got.Spec.Model)
	}

	want := []string{"Bash(git push --force*)", "issues__get_secret", "Bash(terraform apply:*)"}
	if !slices.Equal(got.Spec.ToolsDeny, want) {
		t.Errorf("tools_deny %q, want %q", got.Spec.ToolsDeny, want)
	}
}

// Tools of the issue tracker the tool mode tests set modes on.
const (
	toolComment = "create_comment"
	toolEdit    = "edit_issue"
	toolDelete  = "delete_issue"
)

// setToolModes sets modes on the harness organization's connection name.
func (h *harness) setToolModes(t *testing.T, name string, modes map[string]domain.ConnectionToolMode) domain.Connection {
	t.Helper()

	conn, err := h.connections.Update(domain.WithOrg(t.Context(), h.org), name, domain.ConnectionPatch{ToolModes: modes}, domain.LocalUser)
	if err != nil {
		t.Fatal(err)
	}

	return conn
}

// ADR 0031 decision 3: each connection of the bundle carries its tools that ask first or are off
// and the asks that offer Always allow. An active standing rule for the run's agent turns an ask
// into allow; one for another agent, or a revoked one, does not; nothing turns off into anything.
// A connection with no modes has an empty map and list.
func TestBundle_ToolModesAndAlwaysAllowOffered(t *testing.T) {
	t.Parallel()

	h := newHarnessWith(t, Config{}, harnessDeps{yaml: bundleYAML, files: bundleFiles})
	it := h.submit(t, factoryID, "Add a health endpoint", "For the load balancer.")
	ctx := domain.WithOrg(t.Context(), h.org)

	h.setToolModes(t, connTracker, map[string]domain.ConnectionToolMode{
		toolComment: domain.ToolModeAsk, "close_issue": domain.ToolModeAsk, toolEdit: domain.ToolModeAsk,
		toolDelete: domain.ToolModeOff, "get_issue": domain.ToolModeAllow,
	})

	for _, rule := range []domain.NewStandingRule{
		{Connection: connTracker, Tool: "close_issue", Agent: agentEngineer, Item: it.ID},
		{Connection: connTracker, Tool: toolDelete, Agent: agentEngineer, Item: it.ID},
		{Connection: connTracker, Tool: toolComment, Agent: "reviewer", Item: it.ID},
		{Connection: connTracker, Tool: toolEdit, Agent: agentEngineer, Item: it.ID},
	} {
		r, err := h.connections.CreateStandingRule(ctx, rule, domain.LocalUser)
		if err != nil {
			t.Fatal(err)
		}

		if rule.Tool == toolEdit {
			if _, err := h.connections.RevokeStandingRule(ctx, r.ID, domain.LocalUser); err != nil {
				t.Fatal(err)
			}
		}
	}

	c := h.claims()
	c.ItemID, c.Connections = it.ID, []string{connTracker, connDocs}
	b := h.fetchBundle(t, h.runServer(t), c)

	if len(b.Spec.Connections) != 2 {
		t.Fatalf("connections = %+v, want issues and docs", b.Spec.Connections)
	}

	issues, docs := b.Spec.Connections[0], b.Spec.Connections[1]

	wantModes := map[string]domain.ConnectionToolMode{
		toolComment: domain.ToolModeAsk, toolEdit: domain.ToolModeAsk, toolDelete: domain.ToolModeOff,
	}
	if !reflect.DeepEqual(issues.ToolModes, wantModes) {
		t.Errorf("issues tool_modes = %v, want %v", issues.ToolModes, wantModes)
	}

	if want := []string{toolComment, toolEdit}; !slices.Equal(issues.AlwaysAllowOffered, want) {
		t.Errorf("issues always_allow_offered = %v, want %v", issues.AlwaysAllowOffered, want)
	}

	if docs.ToolModes == nil || len(docs.ToolModes) != 0 || docs.AlwaysAllowOffered == nil || len(docs.AlwaysAllowOffered) != 0 {
		t.Errorf("docs tool_modes %v, always_allow_offered %v; want an empty map and list", docs.ToolModes, docs.AlwaysAllowOffered)
	}
}

// ADR 0031 decision 5: the bundle carries the calls approved for its run, those decided after the
// run before it, as {connection, tool, input} with the input as canonical JSON; a denied call, a
// withdrawn one and one granted to an earlier run are not among them, and a run with none gets [].
func TestBundle_GrantedCallsOfTheRun(t *testing.T) {
	t.Parallel()

	h := newHarnessWith(t, Config{}, harnessDeps{yaml: bundleYAML, files: bundleFiles})
	it := h.submit(t, factoryID, "Add a health endpoint", "For the load balancer.")
	ctx := domain.WithOrg(t.Context(), h.org)

	ev := func(kind domain.EventKind, run domain.RunID, data map[string]any) domain.Event {
		return domain.Event{FactoryID: factoryID, ItemID: it.ID, RunID: run, At: h.clock.Now(), Kind: kind, Station: h.claims().Station, Actor: agentEngineer, Data: data}
	}
	asked := func(run domain.RunID, tool, input string) domain.Event {
		return ev(domain.EventToolApprovalRequested, run, map[string]any{
			domain.KeyConnection: connTracker, domain.KeyTool: tool, domain.KeyInput: json.RawMessage(input),
			domain.KeyAgent: agentEngineer, domain.KeyRunID: string(run), domain.KeyAlwaysAllowOffered: true,
		})
	}

	if err := h.store.AppendEvents(ctx,
		ev(domain.EventRunStarted, "run-0", nil),
		asked("run-0", toolComment, `{"body":"early"}`),
		ev(domain.EventToolApprovalGranted, "", map[string]any{domain.KeyAlwaysAllow: false}),
		ev(domain.EventRunStarted, "run-1", nil),
		asked("run-1", toolComment, `{ "issue": 7, "body": "Done" }`),
		ev(domain.EventToolApprovalGranted, "", map[string]any{domain.KeyAlwaysAllow: false}),
		asked("run-1", "close_issue", `{"issue":7}`),
		ev(domain.EventToolApprovalDenied, "", map[string]any{domain.KeyNote: "not yet"}),
		ev(domain.EventRunStarted, "run-2", nil),
	); err != nil {
		t.Fatal(err)
	}

	url := h.runServer(t)
	c := h.claims()
	c.ItemID, c.Connections = it.ID, []string{connTracker}

	c.RunID = "run-2"
	want := []grantedCall{{Connection: connTracker, Tool: toolComment, Input: json.RawMessage(`{"body":"Done","issue":7}`)}}

	if got := h.fetchBundle(t, url, c).Spec.GrantedCalls; mustJSON(t, got) != mustJSON(t, want) {
		t.Errorf("granted_calls of run-2 = %s, want %s", mustJSON(t, got), mustJSON(t, want))
	}

	c.RunID = "run-3" // a run with no run_started yet, so no grants
	if got := h.fetchBundle(t, url, c).Spec.GrantedCalls; got == nil || len(got) != 0 {
		t.Errorf("granted_calls of a run without grants = %v, want []", got)
	}
}

// jonahYAML is the harness's factory with a Jonah policy: threshold 3 and its own model.
const jonahYAML = factoryYAML + `  learning:
    jonah: { proposeAfter: 3, model: claude-haiku-5-5 }
`

// jonahFiles are the version's files besides factory.yaml and the engineer's prompt: some a
// review reads, and evals, a readme and a binary file it does not.
var jonahFiles = map[string]string{
	"skills/acme-conventions/SKILL.md": "---\nname: acme-conventions\n---\n",
	"rules/go-errors.md":               "Wrap with %w.\n",
	"contracts/change.json":            `{"type":"object"}`,
	"evals/notes.md":                   "# What the evals cover\n",
	"README.md":                        "# acme\n",
	"skills/acme-conventions/logo.png": "\x89PNG\r\n\x1a\n\xff\xfe",
}

func newJonahHarness(t *testing.T, d harnessDeps) *harness {
	t.Helper()

	d.yaml, d.files = jonahYAML, jonahFiles

	return newHarnessWith(t, Config{}, d)
}

// sendBack appends a person's touch of kind on it at the build station, at at, with reason, and
// returns its event's id.
func (h *harness) sendBack(t *testing.T, it domain.Item, kind domain.EventKind, actor, reason string, at time.Time) int64 {
	t.Helper()

	data := map[string]any{"actor": "user:" + actor}
	if reason != "" {
		data["reason"] = reason
	}

	ctx := domain.WithOrg(t.Context(), h.org)
	if err := h.store.AppendEvents(ctx, domain.Event{FactoryID: it.FactoryID, ItemID: it.ID, At: at, Kind: kind, Station: "build", Actor: actor, Data: data}); err != nil {
		t.Fatal(err)
	}

	events := h.store.Events(it.ID)

	return events[len(events)-1].ID
}

// decidedProposals stores a finished review of the harness's factory with three proposals and
// rejects the first 10 days ago and the second 100 days ago; the third stays proposed.
func (h *harness) decidedProposals(t *testing.T) {
	t.Helper()

	ctx := domain.WithOrg(t.Context(), h.org)
	now := h.clock.Now()

	if _, err := h.store.QueueJonahReview(ctx, domain.NewJonahReview{
		FactoryID: factoryID, VersionID: version, Trigger: domain.JonahThreshold,
		WindowStart: now.Add(-time.Hour), WindowEnd: now, By: domain.LocalUser, At: now,
	}); err != nil {
		t.Fatal(err)
	}

	claimed, err := h.store.ClaimJonahReview(ctx, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	file := func(p string) []domain.ProposalFile { return []domain.ProposalFile{{Path: p, Content: "new\n"}} }

	_, stored, err := h.store.FinishJonahReview(ctx, claimed, domain.JonahReviewResult{State: domain.JonahDone, At: now, Proposals: []domain.NewProposal{
		{Title: "Ask for tests", Summary: "The engineer skips tests.", Files: file("rules/go-errors.md")},
		{Title: "An old idea", Summary: "Rejected long ago.", Files: file("agents/engineer.md")},
		{Title: "Still open", Files: file("factory.yaml")},
	}})
	if err != nil {
		t.Fatal(err)
	}

	for i, ago := range []time.Duration{10 * 24 * time.Hour, 100 * 24 * time.Hour} {
		if _, err := h.store.DecideProposal(ctx, factoryID, stored[i].Number, domain.ProposalDecision{
			State: domain.ProposalRejected, RejectReason: "Not for " + stored[i].Title + ".", By: domain.LocalUser, At: now.Add(-ago),
		}); err != nil {
			t.Fatal(err)
		}
	}
}

// A Jonah review's bundle is its version's definition files, the factory's sent-back events in
// the token's window and its proposals rejected in the last 90 days, with the policy's threshold:
// no item, no repository, no definition, no connection, no knowledge, no rule and no file.
func TestServeRun_JonahBundle(t *testing.T) {
	t.Parallel()

	h := newJonahHarness(t, harnessDeps{})
	it := h.submit(t, factoryID, "Add a health endpoint", "")
	now := h.clock.Now()

	rejected := h.sendBack(t, it, domain.EventRejected, "alice", "No tests.", now.Add(-2*24*time.Hour))
	gate := h.sendBack(t, it, domain.EventGateRejected, "bob", "", now.Add(-24*time.Hour))
	h.sendBack(t, it, domain.EventRejected, "alice", "Too old.", now.Add(-10*24*time.Hour)) // before the window
	h.sendBack(t, it, domain.EventNote, "carol", "", now.Add(-3*time.Hour))                 // not sent back
	h.decidedProposals(t)

	c := h.jonahClaims()
	c.Manual = true
	before := len(h.store.Events(it.ID))

	status, _, body := request(t, http.MethodGet, h.runServer(t), h.token(t, c))
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}

	var b runBundle
	if err := json.Unmarshal(body, &b); err != nil {
		t.Fatal(err)
	}

	j := b.Spec.Jonah
	if j == nil {
		t.Fatalf("spec.jonah missing: %s", body)
	}

	if j.ProposeAfter != 3 || !j.Manual {
		t.Errorf("propose_after, manual = %d, %v; want the policy's 3 and the token's true", j.ProposeAfter, j.Manual)
	}

	ref := "#" + strconv.FormatInt(it.Number, 10)
	wantEvents := []jonahEvent{
		{ID: strconv.FormatInt(rejected, 10), ItemRef: ref, Title: it.Title, Station: "build", Kind: string(domain.EventRejected), Reason: "No tests."},
		{ID: strconv.FormatInt(gate, 10), ItemRef: ref, Title: it.Title, Station: "build", Kind: string(domain.EventGateRejected)},
	}
	if !reflect.DeepEqual(j.Events, wantEvents) {
		t.Errorf("events =\n %+v\nwant\n %+v", j.Events, wantEvents)
	}

	wantRejected := []jonahRejected{{
		Title: "Ask for tests", Summary: "The engineer skips tests.", Paths: []string{"rules/go-errors.md"}, Reason: "Not for Ask for tests.",
	}}
	if !reflect.DeepEqual(j.Rejected, wantRejected) {
		t.Errorf("rejected =\n %+v\nwant\n %+v", j.Rejected, wantRejected)
	}

	s := b.Spec
	if s.ItemID != "" || s.Station != "" || s.Agent != "reviewer" || s.RunID != "run-jonah" || s.Model != "claude-haiku-5-5" || s.Brief != "" {
		t.Errorf("spec = %+v; want no item, station or brief, the token's agent and run and the policy's model", s)
	}

	if len(s.Connections) != 0 || len(s.Rules) != 0 || len(s.GrantedCalls) != 0 || len(s.Skills) != 0 || len(b.Files) != 0 || len(b.Git) != 0 ||
		len(b.Tools) != 0 || b.Repository != nil {
		t.Errorf("bundle = %+v; want no connection, rule, granted call, skill, file, git login, tool or repository", b)
	}

	var top, spec map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		t.Fatal(err)
	}

	if err := json.Unmarshal(top["spec"], &spec); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"repository", "definition"} {
		if _, ok := top[key]; ok {
			t.Errorf("bundle has %s, want none", key)
		}
	}

	if _, ok := spec["knowledge_sources"]; ok {
		t.Error("spec has knowledge_sources, want none")
	}

	if n := len(h.store.Events(it.ID)); n != before {
		t.Errorf("%d events of the item, want %d: a bundle records nothing", n, before)
	}
}

// A review reads only the files a proposal may edit, factory.yaml, agents/, skills/, rules/ and
// contracts/, each as text under its sha256, sorted by path: never evals/, another path or a
// file that is not text.
func TestServeRun_JonahBundleOnlyAllowlistedFiles(t *testing.T) {
	t.Parallel()

	h := newJonahHarness(t, harnessDeps{})
	b := h.fetchBundle(t, h.runServer(t), h.jonahClaims())

	want := []string{"agents/engineer.md", "contracts/change.json", config.FileName, "rules/go-errors.md", "skills/acme-conventions/SKILL.md"}

	var got []string

	for _, f := range b.Spec.Jonah.Files {
		got = append(got, f.Path)

		body := cmp.Or(jonahFiles[f.Path], map[string]string{"agents/engineer.md": "# engineer\n"}[f.Path])
		if f.Path == config.FileName {
			body = jonahYAML
		}

		if f.Content != body || f.SHA256 != string(domain.HashBlob([]byte(body))) {
			t.Errorf("%s = %q under %s, want %q under its sha256", f.Path, f.Content, f.SHA256, body)
		}
	}

	if !slices.Equal(got, want) {
		t.Errorf("files = %v, want %v", got, want)
	}
}

// A jonah token is served only its own factory's version in its own organization, names no
// connection and needs Deps.Jonah; without Deps.Jonah other tokens are served as before.
func TestServeRun_RefusesJonahTokens(t *testing.T) {
	t.Parallel()

	h := newJonahHarness(t, harnessDeps{})
	url := h.runServer(t)

	otherFactorys := h.jonahClaims()
	otherFactorys.VersionID = otherVersion
	otherOrg := h.jonahClaims()
	otherOrg.OrgID = "01900000-0000-7000-8000-00000000beef"
	withConnections := h.jonahClaims()
	withConnections.Connections = []string{"docs"}
	noWindow := h.jonahClaims()
	noWindow.WindowStart, noWindow.WindowEnd = 0, 0

	tests := []struct {
		name   string
		claims domain.TaskClaims
		status int
	}{
		{name: "another factory's version", claims: otherFactorys, status: http.StatusForbidden},
		{name: "another organization", claims: otherOrg, status: http.StatusForbidden},
		{name: "names connections", claims: withConnections, status: http.StatusUnauthorized},
		{name: "no window", claims: noWindow, status: http.StatusUnauthorized},
		{name: "valid", claims: h.jonahClaims(), status: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tok, err := domain.SignTaskToken(h.key, tt.claims)
			if err != nil {
				// SignTaskToken refuses what VerifyTaskToken would: sign it as another issuer would.
				tok = h.tokenFor(t, tt.claims, domain.TaskAudience)
			}

			if status, _, body := request(t, http.MethodGet, url, tok); status != tt.status {
				t.Errorf("status = %d, want %d (%s)", status, tt.status, body)
			}
		})
	}

	t.Run("without Deps.Jonah", func(t *testing.T) {
		t.Parallel()

		h := newJonahHarness(t, harnessDeps{noJonah: true})
		url := h.runServer(t)

		if status, _, body := request(t, http.MethodGet, url, h.token(t, h.jonahClaims())); status != http.StatusServiceUnavailable {
			t.Errorf("jonah token: status = %d, want 503 (%s)", status, body)
		}

		station := h.claims()
		station.ItemID = h.submit(t, factoryID, "Write the spec", "").ID

		if status, _, body := request(t, http.MethodGet, url, h.token(t, station)); status != http.StatusOK {
			t.Errorf("station token: status = %d, want 200 (%s)", status, body)
		}

		eval := station
		eval.Kind, eval.Connections = domain.TaskKindEval, nil

		if status, _, body := request(t, http.MethodGet, url, h.token(t, eval)); status != http.StatusOK {
			t.Errorf("eval token: status = %d, want 200 (%s)", status, body)
		}
	})
}
