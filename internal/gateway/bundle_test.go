package gateway

import (
	"bytes"
	"cmp"
	"encoding/json"
	"flag"
	"io"
	"maps"
	"math"
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
func newConnections(t *testing.T, clock *porttest.Clock) (domain.OrgID, *porttest.Connections, *porttest.Tenancy) {
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

	return org.ID, c, tenancy
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
// the item's branch and how a run freshens it, and the runtime's tools. Its version is not on
// secrets, so it carries no connection, no environment and no git login (ADR 0037), and no
// connection's secret value.
func TestServeRun_ServesTheRunsBundle(t *testing.T) {
	t.Parallel()

	h := newHarnessWith(t, Config{}, harnessDeps{yaml: bundleYAML, files: bundleFiles})
	it := h.submit(t, factoryID, "Add a health endpoint", "For the load balancer.")

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
			Rules:        []bundleRule{{Name: "go-errors", SHA256: domain.HashBlob([]byte("Wrap with %w.\n")), Paths: []string{"**/*.go"}, Content: "Wrap with %w.\n"}},
			GrantedCalls: []grantedCall{},
		},
		Files:      map[string][]byte{"agent/prompt.md": []byte("# engineer\n"), "agent/output.schema.json": []byte(`{"type":"object"}`)},
		Repository: &bundleRepository{URL: "https://github.com/acme/api.git", Branch: it.Branch(), FreshBase: domain.FreshBaseMerge},
		Git:        []gitCredential{},
		// The item has no rows, so it gets the default entry; on connections no entry has a token.
		Repositories:     []bundleClone{{URL: "https://github.com/acme/api", Path: "/workspace", Branch: it.Branch(), Write: true}},
		RepositoryLogins: []bundleLogin{},
		ReadOnly:         []string{},
		Tools:            []string{"task@3.46.1"},
	}

	if g, w := mustJSON(t, got), mustJSON(t, want); g != w {
		t.Errorf("bundle\n got %s\nwant %s", g, w)
	}

	expectNoConnections(t, body)
}

// expectNoConnections fails t when the bundle body's spec has a connections key: goald no longer
// reads connections (ADR 0037), and goal-agent defaults the field to [].
func expectNoConnections(t *testing.T, body []byte) {
	t.Helper()

	var raw struct {
		Spec map[string]json.RawMessage `json:"spec"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}

	if c, ok := raw.Spec["connections"]; ok {
		t.Errorf("the bundle's spec has connections %s; want none", c)
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

// A repository that names only a connection, and agent connections the token grants, sign git in
// nowhere and add no CLI tool: goald no longer reads connections (ADR 0037).
func TestServeRun_ConnectionsSignNoGitIn(t *testing.T) {
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

	if len(got.Git) != 0 || !slices.Equal(got.Tools, []string{"task@3.46.1"}) {
		t.Errorf("git credentials %+v, tools %v; want no login and the runtime's tools", got.Git, got.Tools)
	}

	expectNoConnections(t, body)
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
		{name: noToken, status: http.StatusUnauthorized},
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

// jonahRule is the rule file of jonahFiles.
const jonahRule = "rules/go-errors.md"

// jonahFiles are the version's files besides factory.yaml and the engineer's prompt: some a
// review reads, and evals, a readme and a binary file it does not.
var jonahFiles = map[string]string{
	"skills/acme-conventions/SKILL.md": "---\nname: acme-conventions\n---\n",
	jonahRule:                          "Wrap with %w.\n",
	"contracts/change.json":            `{"type":"object"}`,
	"evals/notes.md":                   "# What the evals cover\n",
	"README.md":                        "# acme\n",
	"skills/acme-conventions/logo.png": "\x89PNG\r\n\x1a\n\xff\xfe",
}

func newJonahHarness(t *testing.T, d harnessDeps) *harness {
	t.Helper()

	d.yaml = jonahYAML
	if d.files == nil {
		d.files = jonahFiles
	}

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

	e := domain.Event{FactoryID: it.FactoryID, ItemID: it.ID, At: at, Kind: kind, Station: h.claims().Station, Actor: actor, Data: data}
	if err := h.store.AppendEvents(ctx, e); err != nil {
		t.Fatal(err)
	}

	events := h.store.Events(it.ID)

	return events[len(events)-1].ID
}

// decidedProposals stores a finished review of the harness's factory with four proposals, rejects
// the first 10 days ago and the second 100 days ago, and publishes the fourth; the third stays
// proposed.
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

	file := func(p string) []domain.ProposalEdit {
		return []domain.ProposalEdit{{Path: p, Replacements: []domain.Replacement{{Old: "old\n", New: "new\n"}}}}
	}

	_, stored, err := h.store.FinishJonahReview(ctx, claimed, domain.JonahReviewResult{State: domain.JonahDone, At: now, Proposals: []domain.NewProposal{
		{Title: "Ask for tests", Summary: "The engineer skips tests.", Edits: file(jonahRule)},
		{Title: "An old idea", Summary: "Rejected long ago.", Edits: file(engineerPrompt)},
		{Title: "Still open", Edits: file("factory.yaml")},
		{Title: "Name the checks", Summary: "The reviewer forgets the checks.", Edits: file(jonahRule)},
	}, Observations: []domain.NewObservation{
		{Summary: "Checks are named late", Detail: "Three reviews in a row.", Count: 3, Proposal: 3},
		{Summary: "Health endpoints lack tests", Count: 2},
		{Summary: "The sandbox was lost", Count: 4, NotJonahs: true},
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

	if _, err := h.store.DecideProposal(ctx, factoryID, stored[3].Number, domain.ProposalDecision{
		State: domain.ProposalPublished, PublishedVersionID: version, By: domain.LocalUser, At: now,
	}); err != nil {
		t.Fatal(err)
	}
}

// A Jonah review's bundle names the factory's sent-back events in the token's window in events.md,
// with the ids a proposal cites, and its open proposals and those rejected or published in the last
// 90 days under proposals/, with the policy's threshold and the weekly budget left: no item, no
// repository, no definition, no connection, no rule and no tool (ADR 0039 decision 4).
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
	h.finishedRun(t, it, now.Add(-2*time.Hour), 200) // the line's spend: Jonah's budget is $10, a review's cap $2
	h.finishedJonahRun(t, now.Add(-time.Hour), 8.5)

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

	expectJonahSpec(t, b, c, it)
	expectNoKeys(t, body)

	if math.Abs(b.Spec.MaxUSD-1.5) > 1e-9 {
		t.Errorf("max_usd = %v, want 1.5: the $10 budget less the $8.50 spent, under the $2 cap", b.Spec.MaxUSD)
	}

	if n := len(h.store.Events(it.ID)); n != before {
		t.Errorf("%d events of the item, want %d: a bundle records nothing", n, before)
	}

	ref := "#" + strconv.FormatInt(it.Number, 10)
	events := string(b.Files["events.md"])

	for _, want := range []string{
		"## Event " + strconv.FormatInt(rejected, 10) + "\n\n- Item: " + ref + " Add a health endpoint\n- Station: build\n- Agent: none\n- Kind: rejected\n- Reason:\n\n> No tests.",
		"## Event " + strconv.FormatInt(gate, 10) + "\n\n- Item: " + ref + " Add a health endpoint\n- Station: build\n- Agent: none\n- Kind: gate_rejected\n",
	} {
		if !strings.Contains(events, want) {
			t.Errorf("events.md lacks %q:\n%s", want, events)
		}
	}

	if strings.Contains(events, "Too old.") || strings.Count(events, "## Event ") != 2 {
		t.Errorf("events.md =\n%s\nwant only the two sent-back events in the window", events)
	}
}

// finishedJonahRun records a finished review run of the harness's factory that cost usd, started at.
func (h *harness) finishedJonahRun(t *testing.T, at time.Time, usd float64) {
	t.Helper()

	ctx := domain.WithOrg(t.Context(), h.org)

	id, err := h.store.StartRun(ctx, ports.NewRun{FactoryID: factoryID, Agent: "jonah-reviewer", StartedAt: at, Kind: domain.RunKindJonah})
	if err != nil {
		t.Fatal(err)
	}

	if err := h.store.FinishRun(ctx, id, domain.RunResult{Status: domain.RunSucceeded, CostUSD: usd}, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
}

// A Jonah review's bundle holds its evidence under evidence/: README.md, versions.md and a file for
// each item finished in the window that needed help, built with the stations' medians over the 30
// days before the window's end and the versions published in it.
func TestServeRun_JonahBundleHoldsEvidence(t *testing.T) {
	t.Parallel()

	h := newJonahHarness(t, harnessDeps{})
	now := h.clock.Now()

	// An earlier item's three runs set the build station's median at $1.
	earlier := h.submit(t, factoryID, "Earlier work", "")
	for i := range 3 {
		h.finishedRun(t, earlier, now.Add(-20*24*time.Hour+time.Duration(i)*time.Minute), 1)
	}

	h.finishItem(t, earlier, now.Add(-20*24*time.Hour))

	it := h.submit(t, factoryID, "Add a health endpoint", "Answer 200 on /healthz.")
	h.finishedRun(t, it, now.Add(-2*24*time.Hour), 5)
	h.sendBack(t, it, domain.EventNote, "carol", "", now.Add(-2*24*time.Hour+time.Hour))
	h.finishItem(t, it, now.Add(-24*time.Hour))

	h.publishVersion(t, "Ask for tests")
	h.clock.Advance(time.Hour)

	b := h.fetchBundle(t, h.runServer(t), h.jonahClaims())

	item := string(b.Files["evidence/items/"+strconv.FormatInt(it.Number, 10)+".md"])
	for _, want := range []string{"# #" + strconv.FormatInt(it.Number, 10) + " Add a health endpoint", "> Answer 200 on /healthz.", "| note | build | carol |", "## Outliers", "$5.00"} {
		if !strings.Contains(item, want) {
			t.Errorf("the item's evidence lacks %q:\n%s", want, item)
		}
	}

	if _, ok := b.Files["evidence/items/"+strconv.FormatInt(earlier.Number, 10)+".md"]; ok {
		t.Error("evidence for an item finished before the window, want none")
	}

	if readme := string(b.Files["evidence/README.md"]); !strings.Contains(readme, "1 of the 1 items that finished") {
		t.Errorf("evidence/README.md =\n%s\nwant 1 of the 1 items", readme)
	}

	if versions := string(b.Files["evidence/versions.md"]); !strings.Contains(versions, "## v1") || !strings.Contains(versions, "> Ask for tests") {
		t.Errorf("evidence/versions.md =\n%s\nwant v1 with its message", versions)
	}
}

// finishedRun records a finished run of the build station's engineer on it, started at at, that cost usd.
func (h *harness) finishedRun(t *testing.T, it domain.Item, at time.Time, usd float64) {
	t.Helper()

	ctx := domain.WithOrg(t.Context(), h.org)

	id, err := h.store.StartRun(ctx, ports.NewRun{FactoryID: it.FactoryID, ItemID: it.ID, Station: h.claims().Station, Agent: agentEngineer, Model: "model-a", StartedAt: at})
	if err != nil {
		t.Fatal(err)
	}

	if err := h.store.FinishRun(ctx, id, domain.RunResult{Status: domain.RunSucceeded, CostUSD: usd, Turns: 4}, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
}

// finishItem moves it to the done station, done at at.
func (h *harness) finishItem(t *testing.T, it domain.Item, at time.Time) {
	t.Helper()

	ctx := domain.WithOrg(t.Context(), h.org)

	cur, err := h.store.Item(ctx, it.ID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := h.store.Transition(ctx, ports.Transition{
		ItemID: it.ID, From: cur.Position(), To: domain.Position{Station: "done", State: domain.StateDone}, At: at, DoneAt: at,
	}); err != nil {
		t.Fatal(err)
	}
}

// publishVersion publishes the test factory's first version in h.drafts, now, with message.
func (h *harness) publishVersion(t *testing.T, message string) {
	t.Helper()

	ctx := domain.WithOrg(t.Context(), h.org)

	m, err := h.drafts.PutBlobs(ctx, draftFiles)
	if err != nil {
		t.Fatal(err)
	}

	_, d, err := h.drafts.CreateFactory(ctx, ports.NewFactory{ID: factoryID, Name: "acme-gateway", Manifest: m, By: domain.LocalUser})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := h.drafts.Publish(ctx, ports.Publish{Factory: factoryID, DraftRevision: d.Revision, Name: "acme-gateway", Message: message, By: domain.LocalUser}); err != nil {
		t.Fatal(err)
	}
}

// expectJonahSpec checks that b is a Jonah review's bundle for c: the policy's threshold and
// model, the token's manual, agent and run, and nothing of an item's run.
func expectJonahSpec(t *testing.T, b runBundle, c domain.TaskClaims, it domain.Item) {
	t.Helper()

	j := b.Spec.Jonah
	if j == nil {
		t.Fatalf("spec.jonah missing: %+v", b)
	}

	if j.ProposeAfter != 3 || j.Manual != c.Manual {
		t.Errorf("propose_after, manual = %d, %v; want the policy's 3 and the token's %v", j.ProposeAfter, j.Manual, c.Manual)
	}

	// Everything else is an empty item-less spec: no item, station, brief, connection, rule,
	// granted call, skill, git login, tool or repository, and no file but the box's.
	want := runBundle{
		Spec: bundleSpec{
			FactoryID: c.FactoryID, RunID: c.RunID, Agent: c.Agent, Model: "claude-haiku-5-5", Skills: []string{}, ToolsAllow: []string{},
			ToolsDeny: []string{}, Rules: []bundleRule{}, GrantedCalls: []grantedCall{},
		},
		Files: map[string][]byte{}, Git: []gitCredential{}, Tools: []string{},
	}

	got := b
	got.Spec.Jonah, got.Spec.MaxUSD = nil, 0
	got.Files = map[string][]byte{}

	for p, body := range b.Files {
		if !slices.ContainsFunc([]string{"evidence/", "definition/", "proposals/"}, func(dir string) bool { return strings.HasPrefix(p, dir) }) &&
			p != "files.md" && p != "events.md" && p != "observations.md" {
			got.Files[p] = body
		}
	}

	if b.Spec.MaxUSD <= 0 {
		t.Errorf("max_usd = %v, want the budget left", b.Spec.MaxUSD)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("bundle =\n %+v\nwant (not item %s)\n %+v", got, it.ID, want)
	}
}

// expectNoKeys checks that a served bundle has no repository or definition and its spec no
// knowledge_sources.
func expectNoKeys(t *testing.T, body []byte) {
	t.Helper()

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
}

// A review's box holds every text file of the version under definition/, and files.md names each
// with its sha256 and whether a proposal may edit it; a file that is not text is in neither. Its
// proposals are under proposals/<state>/<number>.md: open ones, and published and rejected ones of
// the last 90 days, a rejected one with its reason.
func TestServeRun_JonahBundleHoldsDefinitionAndProposals(t *testing.T) {
	t.Parallel()

	// With a factory memory, which files.md marks no: the review proposes facts for it instead.
	files := maps.Clone(jonahFiles)
	files["memory.yaml"] = "- text: Use acme in samples\n  added: 2026-11-01\n"

	h := newJonahHarness(t, harnessDeps{files: files})
	h.submit(t, factoryID, "Add a health endpoint", "") // the factory exists once an item does
	h.decidedProposals(t)
	b := h.fetchBundle(t, h.runServer(t), h.jonahClaims())

	expectDefinitionFiles(t, b, files)

	var proposals []string

	for p := range b.Files {
		if strings.HasPrefix(p, "proposals/") {
			proposals = append(proposals, p)
		}
	}

	slices.Sort(proposals)

	if want := []string{"proposals/proposed/3.md", "proposals/published/4.md", "proposals/rejected/1.md"}; !slices.Equal(proposals, want) {
		t.Fatalf("proposals = %v, want %v: none rejected over 90 days ago", proposals, want)
	}

	rejected := string(b.Files["proposals/rejected/1.md"])
	for _, want := range []string{"# Proposal #1: Ask for tests", "- State: rejected", "- Files: " + jonahRule, "- Rejected because:\n\n> Not for Ask for tests.", "> The engineer skips tests."} {
		if !strings.Contains(rejected, want) {
			t.Errorf("proposals/rejected/1.md lacks %q:\n%s", want, rejected)
		}
	}

	if published := string(b.Files["proposals/published/4.md"]); !strings.Contains(published, "- Published as version "+string(version)) {
		t.Errorf("proposals/published/4.md =\n%s\nwant the version it published", published)
	}

	h.expectObservationsFile(t, b)
}

// expectObservationsFile checks observations.md of decidedProposals' review: its open
// observations with the ids a review gives to update them and their counts, and not the one
// that is not Jonah's (ADR 0039 decision 5).
func (h *harness) expectObservationsFile(t *testing.T, b runBundle) {
	t.Helper()

	open, err := h.store.OpenObservations(domain.WithOrg(t.Context(), h.org), factoryID, 10)
	if err != nil || len(open) != 2 {
		t.Fatalf("OpenObservations = %+v, %v; want 2", open, err)
	}

	obs := string(b.Files["observations.md"])

	for _, o := range open {
		for _, want := range []string{"## " + o.Summary, "- id: `" + string(o.ID) + "`", "- Count so far: " + strconv.Itoa(o.Count), "- State: " + string(o.State)} {
			if !strings.Contains(obs, want) {
				t.Errorf("observations.md lacks %q:\n%s", want, obs)
			}
		}
	}

	if !strings.Contains(obs, "> Three reviews in a row.") || strings.Contains(obs, "The sandbox was lost") {
		t.Errorf("observations.md =\n%s\nwant the open observations' details and not the one that is not Jonah's", obs)
	}
}

// expectDefinitionFiles checks TestServeRun_JonahBundleHoldsDefinitionAndProposals's definition/
// and files.md.
func expectDefinitionFiles(t *testing.T, b runBundle, files map[string]string) {
	t.Helper()

	all := map[string]string{engineerPrompt: "# engineer\n", config.FileName: jonahYAML}
	maps.Copy(all, files)
	delete(all, "skills/acme-conventions/logo.png")

	var got []string

	for p, body := range b.Files {
		if rest, ok := strings.CutPrefix(p, "definition/"); ok {
			got = append(got, rest)

			if string(body) != all[rest] {
				t.Errorf("%s = %q, want %q", p, body, all[rest])
			}
		}
	}

	slices.Sort(got)

	if want := slices.Sorted(maps.Keys(all)); !slices.Equal(got, want) {
		t.Errorf("definition files = %v, want %v", got, want)
	}

	index := string(b.Files["files.md"])

	for p, body := range all {
		editable := "no"
		if domain.ProposalPathAllowed(p) && p != config.MemoryFile { // memory takes the review's facts only
			editable = "yes"
		}

		if row := "| `" + p + "` | " + string(domain.HashBlob([]byte(body))) + " | " + editable + " |"; !strings.Contains(index, row) {
			t.Errorf("files.md lacks %q:\n%s", row, index)
		}
	}

	if strings.Contains(index, "logo.png") {
		t.Errorf("files.md names a file that is not text:\n%s", index)
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
		{name: "a version of another factory", claims: otherFactorys, status: http.StatusForbidden},
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
	})
}

// engineerFile is the engineer agent's file in the test factory's draft.
const engineerFile = "agents/engineer.md"

// draftFiles are the test factory's draft files.
var draftFiles = map[string][]byte{
	"factory.yaml":      []byte(factoryYAML),
	engineerFile:        []byte("# engineer\n"),
	"rules/evidence.md": {0xff, 0x00, 'b'},
}

// openDraft opens the test factory's draft with draftFiles and queues a draft request on factory
// f about the engineer, returning it and the draft's revision.
func (h *harness) openDraft(t *testing.T, f domain.FactoryID) (domain.DraftRequest, int64) {
	t.Helper()

	ctx := domain.WithOrg(t.Context(), h.org)

	m, err := h.drafts.PutBlobs(ctx, draftFiles)
	if err != nil {
		t.Fatal(err)
	}

	_, d, err := h.drafts.CreateFactory(ctx, ports.NewFactory{ID: factoryID, Name: "acme-gateway", Manifest: m, By: domain.LocalUser})
	if err != nil {
		t.Fatal(err)
	}

	h.requests.AddFactory(h.org, f)

	r, err := h.requests.Create(ctx, domain.NewDraftRequest{Factory: f, Requester: domain.LocalUser, Text: "Give review a 4th reviewer", Agent: agentEngineer})
	if err != nil {
		t.Fatal(err)
	}

	return r, d.Revision
}

// draftClaims are a draft request's task claims for r at revision.
func (h *harness) draftClaims(r domain.DraftRequest, revision int64) domain.TaskClaims {
	return domain.TaskClaims{
		Kind: domain.TaskKindDraftRequest, OrgID: h.org, FactoryID: factoryID, VersionID: version, Agent: r.Agent, RunID: domain.RunID(r.ID),
		Model: "claude-sonnet-5-5", DraftRequest: r.ID, DraftRevision: revision, ExpiresAt: h.clock.Now().Add(time.Hour).Unix(),
	}
}

// A draft request's bundle has no repository, no git login and no connection: its spec names no
// item and no station but the request, and its definition holds every draft file.
func TestServeRun_DraftRequestBundle(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Config{})
	r, rev := h.openDraft(t, factoryID)

	status, _, body := request(t, http.MethodGet, h.runServer(t), h.token(t, h.draftClaims(r, rev)))
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}

	checkDraftBundleHasNoAccess(t, body)
	checkDraftBundle(t, body, r)

	if strings.Contains(string(body), modelAPIKey) || strings.Contains(string(body), cloneToken) {
		t.Error("the bundle holds a secret value")
	}
}

// checkDraftBundleHasNoAccess checks that a draft request's bundle has no repository, no git
// login and no tool.
func checkDraftBundleHasNoAccess(t *testing.T, body []byte) {
	t.Helper()

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}

	if _, ok := raw["repository"]; ok || string(raw["git_credentials"]) != "[]" || string(raw["tools"]) != "[]" {
		t.Errorf("repository %s, git_credentials %s, tools %s; want none, [] and []", raw["repository"], raw["git_credentials"], raw["tools"])
	}
}

// checkDraftBundle checks that the bundle's spec names the request r and no item, and that its
// definition holds every draft file.
func checkDraftBundle(t *testing.T, body []byte, r domain.DraftRequest) {
	t.Helper()

	var got runBundle
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}

	s := got.Spec
	if s.ItemID != "" || s.Station != "" || s.RunID != domain.RunID(r.ID) || s.FactoryID != factoryID || s.Agent != agentEngineer ||
		len(s.Rules) != 0 || len(s.Skills) != 0 || s.Model != "claude-sonnet-5-5" {
		t.Errorf("spec = %+v, want the request's run with no item, station, connection, rule or skill", s)
	}

	if s.DraftRequest == nil || *s.DraftRequest != (bundleDraftRequest{ID: r.ID, Text: r.Text, Agent: agentEngineer}) {
		t.Errorf("draft_request = %+v, want %s", s.DraftRequest, r.ID)
	}

	checkDraftDefinition(t, got.Definition)
}

// checkDraftDefinition checks that a bundle's definition holds every draft file.
func checkDraftDefinition(t *testing.T, d *bundleDefinition) {
	t.Helper()

	if d == nil || len(d.Files) != len(draftFiles) {
		t.Fatalf("definition = %+v, want the %d draft files", d, len(draftFiles))
	}

	for p, want := range draftFiles {
		if !bytes.Equal(d.Files[p], want) {
			t.Errorf("definition file %s = %q, want %q", p, d.Files[p], want)
		}
	}
}

// A draft that moved past the token's revision is 409; a request of another factory, and a
// draft request's token at any other endpoint of the run, are 403.
func TestServeRun_DraftRequestStaleRevision(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Config{})
	url := h.runServer(t)
	r, rev := h.openDraft(t, factoryID)
	ctx := domain.WithOrg(t.Context(), h.org)

	m, err := h.drafts.PutBlobs(ctx, map[string][]byte{engineerFile: []byte("# engineer, edited\n")})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := h.drafts.SaveDraft(ctx, ports.DraftChange{Factory: factoryID, ExpectedRevision: rev, Set: m, By: domain.LocalUser}); err != nil {
		t.Fatal(err)
	}

	if status, _, body := request(t, http.MethodGet, url, h.token(t, h.draftClaims(r, rev))); status != http.StatusConflict {
		t.Errorf("stale revision: status = %d, want 409 (%s)", status, body)
	}

	if status, _, body := request(t, http.MethodGet, url, h.token(t, h.draftClaims(r, rev+1))); status != http.StatusOK {
		t.Errorf("current revision: status = %d, want 200 (%s)", status, body)
	}

	h.requests.AddFactory(h.org, otherID)

	foreign, err := h.requests.Create(ctx, domain.NewDraftRequest{Factory: otherID, Requester: domain.LocalUser, Text: "Someone else's"})
	if err != nil {
		t.Fatal(err)
	}

	other := h.draftClaims(foreign, rev+1)
	other.Agent = ""

	if status, _, body := request(t, http.MethodGet, url, h.token(t, other)); status != http.StatusForbidden {
		t.Errorf("another factory's request: status = %d, want 403 (%s)", status, body)
	}

	if status, _, body := request(t, http.MethodGet, url+"?notes_after=0", h.token(t, h.draftClaims(r, rev+1))); status != http.StatusForbidden {
		t.Errorf("notes for a draft request: status = %d, want 403 (%s)", status, body)
	}

	if status, _, body := request(t, http.MethodPost, h.url, h.token(t, h.draftClaims(r, rev+1))); status != http.StatusForbidden {
		t.Errorf("tools for a draft request: status = %d, want 403 (%s)", status, body)
	}
}

// secretsBundleYAML is a factory on secrets (ADR 0037) whose repository at url clones with the
// {{github-token}} secret, and on a host other than github.com and gitlab.com with forge.
func secretsBundleYAML(url, forge string) string {
	repo := `{ url: "` + url + `", token: "{{github-token}}" }`
	if forge != "" {
		repo = `{ url: "` + url + `", token: "{{github-token}}", forge: ` + forge + ` }`
	}

	return `apiVersion: goal/v1
kind: Factory
metadata: { id: ` + string(factoryID) + `, name: acme-bundle, purpose: Ship small changes to acme's API. }
spec:
  runtime:
    model: claude-sonnet-5-5
    modelKey: "{{claude-key}}"
    repository: ` + repo + `
  agents:
    - { name: engineer, prompt: ./agents/engineer.md }
  board:
    stations:
      - { id: spec, kind: human }
      - { id: build, kind: agent, agent: engineer }
      - { id: done, kind: done }
`
}

// secretsBundleFiles are the files secretsBundleYAML needs besides factory.yaml.
var secretsBundleFiles = map[string]string{defaultEnvFile: "apiVersion: goal/v1\nkind: Environment\nspec:\n  size: medium\n"}

// serveRunBundle serves the bundle of a run of a new item of the factory yaml describes.
func serveRunBundle(t *testing.T, yaml string, files map[string]string) (runBundle, []byte) {
	t.Helper()

	h := newHarnessWith(t, Config{}, harnessDeps{yaml: yaml, files: files})
	it := h.submit(t, factoryID, "Add a health endpoint", "")
	c := h.claims()
	c.ItemID = it.ID

	status, _, body := request(t, http.MethodGet, h.runServer(t), h.token(t, c))
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}

	var got runBundle
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}

	return got, body
}

// On a version on secrets, the repository token signs git in to the repository's host: one
// stored login whose password is GOAL_REPOSITORY_TOKEN, as the forge names its user, never the
// secret's name or value.
func TestServeRun_GitLoginFromRepositoryToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, url, forge string
		want             gitCredential
	}{
		{
			name: "GitHub", url: "https://github.com/acme/api.git",
			want: gitCredential{Host: "github.com", Username: "x-access-token", PasswordEnv: domain.RepositoryTokenEnv, Store: true},
		},
		{
			name: "GitLab", url: "https://gitlab.com/acme/api.git",
			want: gitCredential{Host: "gitlab.com", Username: "oauth2", PasswordEnv: domain.RepositoryTokenEnv, Store: true},
		},
		{
			name: "a GitLab of its own", url: "https://git.example.com/acme/api.git", forge: "gitlab",
			want: gitCredential{Host: "git.example.com", Username: "oauth2", PasswordEnv: domain.RepositoryTokenEnv, Store: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, body := serveRunBundle(t, secretsBundleYAML(tt.url, tt.forge), secretsBundleFiles)

			if len(got.Git) != 1 || got.Git[0] != tt.want {
				t.Errorf("git credentials %+v; want [%+v]", got.Git, tt.want)
			}

			if got.Repository == nil || got.Repository.URL != tt.url {
				t.Errorf("repository %+v; want %s", got.Repository, tt.url)
			}

			if strings.Contains(string(body), "github-token") {
				t.Errorf("the bundle names the token's secret:\n%s", body)
			}
		})
	}
}

// A public repository on secrets clones without a login.
func TestServeRun_PublicRepositoryWithoutToken(t *testing.T) {
	t.Parallel()

	yaml := strings.Replace(secretsBundleYAML("https://github.com/acme/api.git", ""), `, token: "{{github-token}}"`, "", 1)
	got, body := serveRunBundle(t, yaml, secretsBundleFiles)

	if got.Repository == nil || len(got.Git) != 0 {
		t.Errorf("repository %+v, git credentials %+v; want the repository and no login", got.Repository, got.Git)
	}

	if strings.Contains(string(body), `"store"`) {
		t.Errorf("the bundle has a stored login:\n%s", body)
	}
}

// envCI is the environment file environmentBundle gives the agent.
const envCI = "ci-box"

// environmentBundle serves the bundle of a run on secrets whose agent gets environment ci-box,
// environments/ci-box.yaml being env, with the token granting connTracker.
func environmentBundle(t *testing.T, env string, rules ...domain.NewServerStandingRule) (runBundle, []byte) {
	t.Helper()

	return cappedEnvironmentBundle(t, env, nil, rules...)
}

// serverLinear is the environment MCP server of the tool-mode tests.
const serverLinear = "linear"

// cappedEnvironmentBundle is environmentBundle with mcpCap as the organization policy's MCP tool
// mode cap.
func cappedEnvironmentBundle(
	t *testing.T, env string, mcpCap map[string]domain.ConnectionToolMode, rules ...domain.NewServerStandingRule,
) (runBundle, []byte) {
	t.Helper()

	return environmentBundleOf(t, env, mcpCap, func(*domain.TaskClaims) {}, rules...)
}

// environmentBundleOf is cappedEnvironmentBundle with the token's claims changed by claim.
func environmentBundleOf(
	t *testing.T, env string, mcpCap map[string]domain.ConnectionToolMode, claim func(*domain.TaskClaims),
	rules ...domain.NewServerStandingRule,
) (runBundle, []byte) {
	t.Helper()

	yaml := strings.Replace(secretsBundleYAML("https://github.com/acme/api.git", ""),
		"{ name: engineer, prompt: ./agents/engineer.md }", "{ name: engineer, prompt: ./agents/engineer.md, environment: ci-box }", 1)
	files := maps.Clone(secretsBundleFiles)
	files["environments/ci-box.yaml"] = "apiVersion: goal/v1\nkind: Environment\nspec:\n" + env

	h := newHarnessWith(t, Config{}, harnessDeps{yaml: yaml, files: files, mcpCap: mcpCap})
	it := h.submit(t, factoryID, "Add a health endpoint", "")
	ctx := domain.WithOrg(t.Context(), h.org)

	for _, r := range rules {
		r.Item = it.ID
		if _, err := h.rules.CreateServerStandingRule(ctx, r, domain.LocalUser); err != nil {
			t.Fatal(err)
		}
	}

	c := h.claims()
	c.ItemID, c.Connections = it.ID, []string{connTracker, connDocs}
	claim(&c)

	status, _, body := request(t, http.MethodGet, h.runServer(t), h.token(t, c))
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}

	var got runBundle
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}

	if got.Spec.Environment == nil || got.Spec.Environment.Name != envCI {
		t.Fatalf("environment %+v; want ci-box's", got.Spec.Environment)
	}

	return got, body
}

// ADR 0037 decision 8: on secrets the bundle lists the agent's environment's MCP servers, sorted,
// with their secrets as ${GOAL_SECRET_<NAME>}, and no connection; no value reaches its JSON.
func TestBundle_EnvironmentServersOnSecrets(t *testing.T) {
	t.Parallel()

	got, body := environmentBundle(t, `  mcp:
    playwright:
      command: [npx, "@playwright/mcp@0.0.41"]
      env: {PW_LOGIN: "{{pw-login}}", PW_MODE: headless}
    linear:
      url: https://mcp.linear.example.com/mcp
      headers: {Authorization: "Bearer {{linear-key}}", X-Org: acme}
`)

	want := []bundleMCPServer{
		{
			Name: serverLinear, URL: "https://mcp.linear.example.com/mcp",
			Headers:   map[string]string{"Authorization": "Bearer ${GOAL_SECRET_LINEAR_KEY}", "X-Org": "acme"},
			ToolModes: []bundleToolMode{},
		},
		{
			Name: "playwright", Command: []string{"npx", "@playwright/mcp@0.0.41"},
			Env: map[string]string{"PW_LOGIN": "${GOAL_SECRET_PW_LOGIN}", "PW_MODE": "headless"}, ToolModes: []bundleToolMode{}, //nolint:gosec // a variable's name, not a credential
		},
	}
	if !reflect.DeepEqual(got.Spec.Environment.MCPServers, want) {
		t.Errorf("mcp_servers %+v; want %+v", got.Spec.Environment.MCPServers, want)
	}

	expectNoConnections(t, body)

	for _, value := range []string{modelAPIKey, cloneToken, issuesToken, "{{", "linear-key"} {
		if strings.Contains(string(body), value) {
			t.Errorf("the bundle holds %q:\n%s", value, body)
		}
	}
}

// ADR 0037 decision 8: a server's tool modes follow the file, most specific first, and an active
// standing rule for the factory, environment, server and role lifts an ask, never off.
func TestBundle_EnvironmentToolModesWithStandingRule(t *testing.T) {
	t.Parallel()

	rule := func(tool, agent string) domain.NewServerStandingRule {
		return domain.NewServerStandingRule{Factory: factoryID, Environment: envCI, Server: serverLinear, Tool: tool, Agent: agent}
	}
	got, _ := environmentBundle(t, `  mcp:
    linear:
      url: https://mcp.linear.example.com/mcp
      tools: {create_comment: ask, "delete_*": off, "close_*": ask}
`,
		rule("create_comment", agentEngineer), rule("delete_issue", agentEngineer), rule("close_issue", "reviewer"),
		domain.NewServerStandingRule{Factory: factoryID, Environment: domain.DefaultEnvironment, Server: serverLinear, Tool: "close_pr", Agent: agentEngineer},
	)

	want := []bundleToolMode{
		{Tool: "create_comment", Mode: domain.ToolModeAllow}, {Tool: "delete_*", Mode: domain.ToolModeOff}, {Tool: "close_*", Mode: domain.ToolModeAsk},
	}
	if servers := got.Spec.Environment.MCPServers; len(servers) != 1 || !reflect.DeepEqual(servers[0].ToolModes, want) {
		t.Errorf("mcp_servers %+v; want linear with tool_modes %+v", servers, want)
	}
}

// ADR 0037 decisions 8 and 12: the organization policy's MCP tool mode cap applies to the file's
// modes, the stricter winning: allow under an ask cap is ask, off stays off, and a standing rule
// does not lift an ask the cap keeps.
func TestBundle_EnvironmentToolModesUnderThePolicyCap(t *testing.T) {
	t.Parallel()

	got, _ := cappedEnvironmentBundle(t, `  mcp:
    linear:
      url: https://mcp.linear.example.com/mcp
      tools: {create_issue: allow, create_comment: ask, "delete_*": off}
`, map[string]domain.ConnectionToolMode{"*": domain.ToolModeAsk},
		domain.NewServerStandingRule{Factory: factoryID, Environment: envCI, Server: serverLinear, Tool: "create_comment", Agent: agentEngineer},
	)

	want := []bundleToolMode{
		{Tool: "create_comment", Mode: domain.ToolModeAsk},
		{Tool: "create_issue", Mode: domain.ToolModeAsk},
		{Tool: "delete_*", Mode: domain.ToolModeOff},
		{Tool: "*", Mode: domain.ToolModeAsk},
	}
	if servers := got.Spec.Environment.MCPServers; len(servers) != 1 || !reflect.DeepEqual(servers[0].ToolModes, want) {
		t.Errorf("mcp_servers %+v; want linear with tool_modes %+v", servers, want)
	}
}

// ADR 0037 decisions 5 and 11: the bundle's tools are the environment's, in file order; setup
// keeps its order, files carry each path's GOAL_FILE_<n>, and env and clis are names only.
func TestBundle_EnvironmentToolsSetupAndFiles(t *testing.T) {
	t.Parallel()

	got, body := environmentBundle(t, `  tools: [node@22, go@1.26, "npm:@acme/cli@2", gh]
  setup: [pnpm install --frozen-lockfile, make generate]
  env: {SENTRY_ORG: acme-sentry-org, GH_TOKEN: "{{github-token}}"}
  files:
    ~/.npmrc: "{{npmrc}}"
    /etc/acme.conf: "{{acme-conf}}"
`)

	if want := []string{"node@22", "go@1.26", "npm:@acme/cli@2", "gh"}; !slices.Equal(got.Tools, want) {
		t.Errorf("tools %v; want %v", got.Tools, want)
	}

	e := got.Spec.Environment
	want := bundleEnvironment{
		Name: envCI, MCPServers: []bundleMCPServer{}, Setup: []string{"pnpm install --frozen-lockfile", "make generate"},
		Files: []bundleFile{{Path: "/etc/acme.conf", Env: "GOAL_FILE_1"}, {Path: "~/.npmrc", Env: "GOAL_FILE_2"}},
		Env:   []string{"GH_TOKEN", "SENTRY_ORG"}, CLIs: []string{"node", "go", "npm:@acme/cli", "gh"},
	}

	if !reflect.DeepEqual(*e, want) {
		t.Errorf("environment %+v; want %+v", *e, want)
	}

	if strings.Contains(string(body), "acme-sentry-org") {
		t.Errorf("the bundle holds an env value:\n%s", body)
	}
}

// ADR 0037 decision 12: with Use secrets off the token says NoEnvironment, and the bundle keeps
// the environment's name, setup and tools but gives no variable, file or MCP server, with a secret
// or without.
func TestBundle_EnvironmentNoEnvironmentDropsEnvFilesAndMCP(t *testing.T) {
	t.Parallel()

	got, body := environmentBundleOf(t, `  tools: [node@22, gh]
  setup: [pnpm install --frozen-lockfile]
  env: {SENTRY_ORG: acme-sentry-org, GH_TOKEN: "{{github-token}}"}
  files:
    ~/.npmrc: "{{npmrc}}"
  mcp:
    linear:
      url: https://mcp.linear.example.com/mcp
      headers: {Authorization: "Bearer {{linear-key}}"}
    playwright:
      command: [npx, "@playwright/mcp@0.0.41"]
`, nil, func(c *domain.TaskClaims) { c.NoEnvironment = true })

	if want := []string{"node@22", "gh"}; !slices.Equal(got.Tools, want) {
		t.Errorf("tools %v; want %v", got.Tools, want)
	}

	want := bundleEnvironment{
		Name: envCI, MCPServers: []bundleMCPServer{}, Setup: []string{"pnpm install --frozen-lockfile"},
		Files: []bundleFile{}, Env: []string{}, CLIs: []string{"node", "gh"},
	}
	if !reflect.DeepEqual(*got.Spec.Environment, want) {
		t.Errorf("environment %+v; want %+v", *got.Spec.Environment, want)
	}

	for _, name := range []string{"GOAL_FILE_", "GOAL_SECRET_", "SENTRY_ORG", "GH_TOKEN", "mcp.linear.example.com", "@playwright/mcp"} {
		if strings.Contains(string(body), name) {
			t.Errorf("the bundle holds %q:\n%s", name, body)
		}
	}
}

// A published proposal's file ends with its watch: one line saying there is none before the first
// watch sweep, then its metric, station, verdict and both sides (ADR 0039 decision 14).
func TestServeRun_JonahBundleHoldsPublishedWatch(t *testing.T) {
	t.Parallel()

	h := newJonahHarness(t, harnessDeps{})
	h.submit(t, factoryID, "Add a health endpoint", "") // the factory exists once an item does
	h.decidedProposals(t)

	published := string(h.fetchBundle(t, h.runServer(t), h.jonahClaims()).Files["proposals/published/4.md"])
	if !strings.HasSuffix(published, "\n## Watch\n\nNo watch yet: the first watch sweep after the publish opens it.\n") {
		t.Errorf("proposals/published/4.md =\n%s\nwant it to say it has no watch yet", published)
	}

	ctx := domain.WithOrg(t.Context(), h.org)

	p, err := h.store.Proposal(ctx, factoryID, 4)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := h.store.SaveWatch(ctx, domain.Watch{
		FactoryID: factoryID, VersionID: version, ProposalID: p.ID, Station: tryStation, Metric: domain.MetricSentBackRate,
		Before: domain.WatchSide{N: 12, Value: 0.25}, After: domain.WatchSide{N: 9, Value: 0.1}, Verdict: domain.WatchHelped,
		OpenedAt: h.clock.Now(), UpdatedAt: h.clock.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	b := h.fetchBundle(t, h.runServer(t), h.jonahClaims())

	want := "\n## Watch\n\n- sent_back_rate at build: helped; before 12 items, 0.25; after 9 items, 0.1\n"
	if published := string(b.Files["proposals/published/4.md"]); !strings.HasSuffix(published, want) {
		t.Errorf("proposals/published/4.md =\n%s\nwant it to end with %q", published, want)
	}

	for _, path := range []string{"proposals/proposed/3.md", "proposals/rejected/1.md"} {
		if strings.Contains(string(b.Files[path]), "## Watch") {
			t.Errorf("%s has a watch, want one only on a published proposal", path)
		}
	}
}

// entriesBundleYAML is a factory on secrets with two runtime.repositories entries: api, the
// default, writes with {{github-token}}, and the owner's every repository reads with {{acme-read}};
// its engineer's code is code.
func entriesBundleYAML(code string) string {
	return `apiVersion: goal/v1
kind: Factory
metadata: { id: ` + string(factoryID) + `, name: acme-bundle, purpose: Ship small changes to acme's API. }
spec:
  runtime:
    model: claude-sonnet-5-5
    modelKey: "{{claude-key}}"
    repositories:
      - { name: api, url: "https://github.com/acme/api", token: "{{github-token}}", access: write, default: true }
      - { name: acme, url: "https://github.com/acme/*", token: "{{acme-read}}", access: read }
  agents:
    - { name: engineer, prompt: ./agents/engineer.md, code: ` + code + ` }
  board:
    stations:
      - { id: spec, kind: human }
      - { id: build, kind: agent, agent: engineer }
      - { id: done, kind: done }
`
}

// A station run's bundle carries the item's repositories in the order they joined, each with
// where prepare clones it, the item's branch and whether the agent may push to it; a login per
// entry with the variable of the token the agent's code picks; and the prefixes the run may not
// write. The repository and its stored login stay as before, for today's prepare.
func TestBundle_RepositoriesAndAccessOfTheItem(t *testing.T) {
	t.Parallel()

	const (
		api = "https://github.com/acme/api"
		lib = "https://github.com/acme/lib"
		all = "https://github.com/acme/"
	)

	tests := []struct {
		name     string
		code     string
		repos    []bundleClone
		logins   []bundleLogin
		readOnly []string
	}{
		{
			name: "an agent that writes", code: "write",
			repos: []bundleClone{{URL: api, Path: "/workspace", Write: true}, {URL: lib, Path: "/repos/acme/lib"}},
			logins: []bundleLogin{
				{Prefix: api, Username: forgeUsers["github"], PasswordEnv: envAPIToken, Write: true},
				{Prefix: all, Username: forgeUsers["github"], PasswordEnv: envAcmeToken},
			},
			readOnly: []string{all},
		},
		{
			name: "an agent that only reads", code: "read",
			repos: []bundleClone{{URL: api, Path: "/workspace"}, {URL: lib, Path: "/repos/acme/lib"}},
			logins: []bundleLogin{
				{Prefix: api, Username: forgeUsers["github"], PasswordEnv: envAcmeToken},
				{Prefix: all, Username: forgeUsers["github"], PasswordEnv: envAcmeToken},
			},
			readOnly: []string{api, all},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarnessWith(t, Config{}, harnessDeps{yaml: entriesBundleYAML(tt.code), files: secretsBundleFiles})
			ctx := domain.WithOrg(t.Context(), h.claims().OrgID)

			it, err := h.store.CreateItem(ctx, domain.NewItem{
				FactoryID: factoryID, Title: "Add a health endpoint", Station: h.claims().Station, State: domain.StateQueued, CreatedAt: h.clock.Now(),
				Repositories: []domain.ItemRepository{
					{Repo: strings.TrimPrefix(api, "https://"), Joined: domain.JoinedFiling, Ordinal: 0},
					{Repo: strings.TrimPrefix(lib, "https://"), Joined: domain.JoinedFiling, Ordinal: 1},
				},
			})
			if err != nil {
				t.Fatal(err)
			}

			c := h.claims()
			c.ItemID = it.ID

			status, _, body := request(t, http.MethodGet, h.runServer(t), h.token(t, c))
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200 (%s)", status, body)
			}

			var got runBundle
			if err := json.Unmarshal(body, &got); err != nil {
				t.Fatal(err)
			}

			for i := range tt.repos {
				tt.repos[i].Branch = it.Branch()
			}

			if g, w := mustJSON(t, got.Repositories), mustJSON(t, tt.repos); g != w {
				t.Errorf("repositories\n got %s\nwant %s", g, w)
			}

			if g, w := mustJSON(t, got.RepositoryLogins), mustJSON(t, tt.logins); g != w {
				t.Errorf("repository logins\n got %s\nwant %s", g, w)
			}

			if g, w := mustJSON(t, got.ReadOnly), mustJSON(t, tt.readOnly); g != w {
				t.Errorf("read only\n got %s\nwant %s", g, w)
			}

			wantRepo := &bundleRepository{URL: api, Branch: it.Branch(), FreshBase: domain.FreshBaseRebase}
			wantGit := []gitCredential{{Host: strings.Split(strings.TrimPrefix(api, "https://"), "/")[0], Username: forgeUsers["github"], PasswordEnv: domain.RepositoryTokenEnv, Store: true}}

			if g, w := mustJSON(t, got.Repository), mustJSON(t, wantRepo); g != w {
				t.Errorf("repository %s, want %s as before", g, w)
			}

			if g, w := mustJSON(t, got.Git), mustJSON(t, wantGit); g != w {
				t.Errorf("git credentials %s, want %s as before", g, w)
			}
		})
	}
}
