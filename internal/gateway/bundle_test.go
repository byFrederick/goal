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
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
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
`

// legacyBundleYAML is the factory of a version published before connections: the repository is
// cloned with a token from goald's environment.
const legacyBundleYAML = `apiVersion: goal/v1
kind: Factory
metadata: { id: ` + string(factoryID) + `, name: acme-bundle }
spec:
  runtime:
    runner: docker
    image: ghcr.io/acme/agent:1.0.0
    auth: anthropic-api-key
    repository: { url: "https://github.com/acme/api.git", token: "env:ACME_REPO_TOKEN" }
  agents:
    - { name: engineer, prompt: ./agents/engineer.md }
  board:
    stations:
      - { id: spec, kind: human }
      - { id: build, kind: agent, agent: engineer }
      - { id: done, kind: done }
`

var bundleFiles = map[string]string{
	"skills/acme-conventions/SKILL.md": "---\nname: acme-conventions\n---\n",
	"contracts/change.json":            `{"type":"object"}`,
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
// the item's branch, and what the run's connections give the box without a value: the agent's
// connections its token grants (env names, MCP servers with headers naming env variables, docs),
// a git login per connection that signs git in, naming the variable of its password, and the
// runtime's tools with the connections' CLI tools.
func TestServeRun_ServesTheRunsBundle(t *testing.T) {
	t.Parallel()

	h := newHarnessWith(t, Config{}, harnessDeps{yaml: bundleYAML, files: bundleFiles, secrets: porttest.Secrets{}})
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
			Connections: []bundleConnection{
				{
					Name: connTracker, Kind: "issue-tracker", Title: trackerTitle, Env: []string{"ISSUES_SITE", "ISSUES_TOKEN"},
					MCP:  &domain.KindMCP{URL: "https://issues.example.com/mcp", Headers: map[string]string{"Authorization": "Bearer ${ISSUES_TOKEN}"}},
					Docs: "File and read issues with the issues CLI or its MCP tools.",
				},
				{
					Name: connDocs, Kind: kindStdio, Title: "MCP server", Env: []string{},
					MCP: &domain.KindMCP{Command: []string{"sh", "-c", "npx -y @acme/docs-mcp@1.0.0"}},
				},
			},
		},
		Files:      map[string][]byte{"agent/prompt.md": []byte("# engineer\n"), "agent/output.schema.json": []byte(`{"type":"object"}`)},
		Repository: &bundleRepository{URL: "https://github.com/acme/api.git", Branch: it.Branch()},
		Git:        []gitCredential{{Host: "github.com", Username: repositoryUser, PasswordEnv: "CODE_TOKEN"}},
		Tools:      []string{"task@3.46.1", "npm:@acme/issues-cli@2.0.0"},
	}

	if g, w := mustJSON(t, got), mustJSON(t, want); g != w {
		t.Errorf("bundle\n got %s\nwant %s", g, w)
	}
}

// A version published before connections clones with the repository token, whose variable the
// git credential names; its runs get no connection in the bundle.
func TestServeRun_LegacyRepositoryToken(t *testing.T) {
	t.Parallel()

	h := newHarnessWith(t, Config{}, harnessDeps{yaml: legacyBundleYAML, secrets: porttest.Secrets{"ACME_REPO_TOKEN": cloneToken}})
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

	want := []gitCredential{{Host: "github.com", Username: repositoryUser, PasswordEnv: domain.RunEnvRepositoryToken}}
	if !reflect.DeepEqual(got.Git, want) || len(got.Spec.Connections) != 0 || strings.Contains(string(body), cloneToken) {
		t.Errorf("git credentials %+v and connections %+v; want %+v and none, without the token", got.Git, got.Spec.Connections, want)
	}
}

// A repository connection that is also one of the agent's signs git in once.
func TestServeRun_OneGitLoginPerCredential(t *testing.T) {
	t.Parallel()

	yaml := strings.ReplaceAll(strings.ReplaceAll(bundleYAML, "connections: [issues, docs, wiki]\n  skills", "connections: [issues, docs, wiki, code]\n  skills"),
		"connections: [issues, docs, wiki]\n      tools", "connections: [code, issues]\n      tools")
	h := newHarnessWith(t, Config{}, harnessDeps{yaml: yaml, files: bundleFiles, secrets: porttest.Secrets{}})
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
