package api_test

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/api"
	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// The route the environment tests call, acme's default environment file in the draft and the
// MCP server URLs the tests' environments name.
const (
	serverLinear  = "linear"
	serverDocs    = "docs"
	envAcmeKey    = "ACME_KEY"
	authorization = "Authorization"
	envTestPath   = acmeFactory + "/environments/default/test"
	draftEnvFile  = acmeDraft + "/files/environments%2Fdefault.yaml"
	linearURL     = "https://mcp.linear.example.com/mcp"
	docsURL       = "https://mcp.docs.example.com/mcp"
	templateMCP   = "https://mcp.acme.example.com/mcp"
	testedEnvYAML = `apiVersion: goal/v1
kind: Environment
spec:
  env:
    ACME_KEY: "{{acme-token}}"
    ACME_URL: https://api.example.com
  files:
    ~/.acme/key: "{{acme-token}}"
  mcp:
    linear:
      url: ` + linearURL + `
      headers: {Authorization: "Bearer {{acme-token}}"}
    docs:
      url: ` + docsURL + `
    playwright:
      command: [npx, "@playwright/mcp@0.0.41"]
`
)

// draftEnvironment opens acme's draft, unless it is open, and writes doc as its default
// environment.
func (h *harness) draftEnvironment(t *testing.T, doc string) {
	t.Helper()

	if r := h.request(t, http.MethodPost, acmeDraft, `{}`, nil); r.status != http.StatusCreated && r.status != http.StatusOK {
		t.Fatalf("open the draft: %d %s", r.status, r.body)
	}

	file := h.request(t, http.MethodGet, draftEnvFile, "", nil)
	if file.status != http.StatusOK {
		t.Fatalf("read the default environment: %d %s", file.status, file.body)
	}

	decode[gen.SaveResult](t, h.request(t, http.MethodPut, draftEnvFile, doc, http.Header{ifMatchHeader: {file.header.Get("ETag")}}), http.StatusOK)
}

// A test of the draft calls tools/list on each remote MCP server with its headers' secrets
// opened, answers the tool names sorted and keeps them; a stdio server is not tested; server
// picks one, and an unknown one is 404.
func TestEnvironmentTest_ToolsList(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createSecret(t)
	h.draftEnvironment(t, testedEnvYAML)
	h.tools.Serve(linearURL, "list_issues", "create_issue")
	h.tools.Serve(docsURL, "search")

	got := decode[gen.EnvironmentTestResult](t, h.request(t, http.MethodPost, envTestPath, `{}`, nil), http.StatusOK)
	if got.Environment != defaultEnvironment || got.Version != "draft" {
		t.Errorf("test = %s; want the draft's default environment", body(t, got))
	}

	want := []serverSum{
		{Server: serverDocs, Tested: true, Ok: true, Tools: "search", At: true},
		{Server: serverLinear, Tested: true, Ok: true, Tools: "create_issue,list_issues", At: true},
		{Server: "playwright"},
	}
	if sums := summaries(got.Servers); !slices.Equal(sums, want) {
		t.Errorf("servers = %+v; want %+v", sums, want)
	}

	if h.calledWith(linearURL)[authorization] != "Bearer "+secretValue {
		t.Errorf("called = %v; want linear with its secret opened", h.tools.Called())
	}

	kept, err := h.envTests.List(inOrg(t), acmeID)
	if err != nil || keptServers(kept) != "docs:ok,linear:ok" {
		t.Errorf("kept = %+v, %v; want docs and linear", kept, err)
	}

	one := decode[gen.EnvironmentTestResult](t, h.request(t, http.MethodPost, envTestPath, `{"server":"linear"}`, nil), http.StatusOK)
	if names := summaries(one.Servers); len(names) != 1 || names[0].Server != serverLinear {
		t.Errorf("server linear = %s", body(t, one))
	}

	refused(t, "an unknown server", h.request(t, http.MethodPost, envTestPath, `{"server":"sentry"}`, nil),
		http.StatusNotFound, "environment-server-not-found")
}

// The test keeps the names of the tools a server marked read-only, beside every tool's name.
func TestEnvironmentTest_KeepsReadOnlyHints(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createSecret(t)
	h.draftEnvironment(t, testedEnvYAML)
	h.tools.Serve(linearURL, "create_issue")
	h.tools.ServeReadOnly(linearURL, "list_issues")

	decode[gen.EnvironmentTestResult](t, h.request(t, http.MethodPost, envTestPath, `{"server":"linear"}`, nil), http.StatusOK)

	kept, err := h.envTests.List(inOrg(t), acmeID)
	if err != nil || len(kept) != 1 || !slices.Equal(kept[0].Tools, []string{"create_issue", "list_issues"}) ||
		!slices.Equal(kept[0].ReadOnly, []string{"list_issues"}) {
		t.Errorf("kept = %+v, %v; want linear's tools with list_issues read-only", kept, err)
	}
}

// defaultEnvironment is the environment every factory has.
const defaultEnvironment = "default"

// serverSum is what a test of a server answered, comparable.
type serverSum struct {
	Server     string
	Tested, Ok bool
	Tools      string
	At         bool
	ErrorHolds string
}

// summaries are servers as serverSums.
func summaries(servers []gen.ServerTest) []serverSum {
	out := make([]serverSum, 0, len(servers))
	for _, s := range servers {
		out = append(out, serverSum{Server: s.Server, Tested: s.Tested, Ok: s.Ok, Tools: strings.Join(s.Tools, ","), At: s.At != nil})
	}

	return out
}

// keptServers are the kept tests as server:ok or server:failed, in order.
func keptServers(kept []domain.EnvironmentTest) string {
	parts := make([]string, 0, len(kept))
	for _, k := range kept {
		parts = append(parts, k.Server+":"+map[bool]string{true: "ok", false: "failed"}[k.OK])
	}

	return strings.Join(parts, ",")
}

// calledWith are the headers the tool lister got for url's last call, or none.
func (h *harness) calledWith(url string) map[string]string {
	called := h.tools.Called()
	if i := slices.IndexFunc(called, func(e ports.MCPEndpoint) bool { return e.URL == url }); i >= 0 {
		return called[i].Headers
	}

	return nil
}

// A secret value a server echoes in its refusal is in neither the answer nor what is kept, and a
// missing secret fails the server's test naming it.
func TestEnvironmentTest_NoValueInAnswer(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createSecret(t)
	h.draftEnvironment(t, testedEnvYAML)
	h.tools.Fail(linearURL, errors.New("401 Unauthorized: the key Bearer "+secretValue+" is revoked"))

	r := h.request(t, http.MethodPost, envTestPath, `{"server":"linear"}`, nil)
	if strings.Contains(string(r.body), secretValue) {
		t.Fatalf("the answer holds the value: %s", r.body)
	}

	got := decode[gen.EnvironmentTestResult](t, r, http.StatusOK)
	if s := got.Servers[0]; s.Ok || !strings.Contains(valueOf(s.Error), "[secret]") {
		t.Errorf("linear = %s; want a scrubbed failure", body(t, s))
	}

	kept, err := h.envTests.List(inOrg(t), acmeID)
	if err != nil || len(kept) != 1 || strings.Contains(kept[0].Error, secretValue) || kept[0].OK {
		t.Errorf("kept = %+v, %v; want one scrubbed failure", kept, err)
	}

	h.draftEnvironment(t, strings.ReplaceAll(testedEnvYAML, secretName, "gone-key"))

	missing := decode[gen.EnvironmentTestResult](t, h.request(t, http.MethodPost, envTestPath, `{"server":"linear"}`, nil), http.StatusOK)
	if s := missing.Servers[0]; s.Ok || !strings.Contains(valueOf(s.Error), "gone-key") {
		t.Errorf("linear with a missing secret = %s", body(t, s))
	}
}

// An environment the version does not have is 404, and so is an unknown template.
func TestEnvironmentTest_UnknownEnvironment(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.openDraft(t)

	refused(t, "an unknown environment", h.request(t, http.MethodPost, acmeFactory+"/environments/review/test", `{}`, nil),
		http.StatusNotFound, "environment-not-found")
	refused(t, "a path for a name", h.request(t, http.MethodPost, acmeFactory+"/environments/..%2Ffactory/test", `{}`, nil),
		http.StatusNotFound, "environment-not-found")
	refused(t, "an unknown template", h.request(t, http.MethodPost, envTestPath, `{"template":{"name":"nope","secrets":{}}}`, nil),
		http.StatusNotFound, "template-not-found")

	// The published version is tested too: acme's v1 has a default environment with no server.
	got := decode[gen.EnvironmentTestResult](t, h.request(t, http.MethodPost, envTestPath, `{"version":"current"}`, nil), http.StatusOK)
	if got.Version != "1" || len(got.Servers) != 0 {
		t.Errorf("current = %s", body(t, got))
	}
}

// upstream is an https API that answers 200 to GET /v1/me with the secret as a bearer token, and
// 401 to anything else; auth is the Authorization it last got.
func upstream(t *testing.T) (srv *httptest.Server, auth *string) {
	t.Helper()

	auth = new(string)
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*auth = r.Header.Get(authorization)
		if r.URL.Path != "/v1/me" || *auth != "Bearer "+secretValue {
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	t.Cleanup(srv.Close)

	return srv, auth
}

// withAcmeTemplate adds the acme-api template to the catalog, and makes the connection client
// trust srv's certificate.
func withAcmeTemplate(srv *httptest.Server) func(*harness, *api.Deps) {
	return func(h *harness, d *api.Deps) {
		if tr, ok := srv.Client().Transport.(*http.Transport); ok {
			h.connTransport.TLSClientConfig = tr.TLSClientConfig
		}

		bearer := map[string]string{authorization: "Bearer {{key}}"}
		d.Catalog = append(d.Catalog, domain.ToolTemplate{
			Name: "acme-api", Title: acmeTitle, Description: "The acme API.",
			Secrets: []domain.TemplateSecret{{Slot: "key", Title: "Key", Suggest: "acme-key"}},
			Env:     map[string]string{"ACME_URL": "https://api.example.com", envAcmeKey: "{{key}}"},
			MCP:     map[string]domain.TemplateMCP{acmeSlug: {URL: templateMCP, Headers: bearer}},
			Test:    &domain.TemplateTest{Method: http.MethodGet, URL: "${ACME_URL}/v1/me", Headers: bearer, Expect: http.StatusOK},
		})
	}
}

// templateSum is what a template's HTTP test answered, comparable.
type templateSum struct {
	Ok     bool
	Status int
	Error  bool
}

// templateSummary is r as a templateSum; the zero one without r.
func templateSummary(r *gen.TemplateTestResult) templateSum {
	if r == nil {
		return templateSum{}
	}

	return templateSum{Ok: r.Ok, Status: valueOf(r.Status), Error: r.Error != nil}
}

// A template test runs its HTTP test with the secrets its slots take and lists its MCP server's
// tools, keeping that result as the environment's; a slot without a secret is 400.
func TestEnvironmentTest_Template(t *testing.T) {
	t.Parallel()

	srv, auth := upstream(t)
	h := newHarnessLogged(t, nil, nil, withAcmeTemplate(srv))
	h.createSecret(t)
	h.openDraft(t)
	h.tools.Serve(templateMCP, "get_widget")

	req := func(url string) string {
		return `{"template":{"name":"acme-api","secrets":{"key":"acme-token"},"env":{"ACME_URL":"` + url + `"}}}`
	}

	got := decode[gen.EnvironmentTestResult](t, h.request(t, http.MethodPost, envTestPath, req(srv.URL), nil), http.StatusOK)
	if sum := templateSummary(got.Template); sum != (templateSum{Ok: true, Status: http.StatusOK}) || *auth != "Bearer "+secretValue {
		t.Errorf("template = %+v (auth %q); want it passed with the secret", sum, *auth)
	}

	if sums := summaries(got.Servers); !slices.Equal(sums, []serverSum{{Server: acmeSlug, Tested: true, Ok: true, Tools: "get_widget", At: true}}) {
		t.Errorf("servers = %+v", sums)
	}

	if kept, err := h.envTests.List(inOrg(t), acmeID); err != nil || keptServers(kept) != "acme:ok" {
		t.Errorf("kept = %+v, %v; want acme", kept, err)
	}

	r := h.request(t, http.MethodPost, envTestPath, req(srv.URL+"/wrong"), nil)
	failed := decode[gen.EnvironmentTestResult](t, r, http.StatusOK)

	if sum := templateSummary(failed.Template); sum != (templateSum{Status: http.StatusUnauthorized, Error: true}) || strings.Contains(string(r.body), secretValue) {
		t.Errorf("failed template = %s", r.body)
	}

	refused(t, "a slot without a secret", h.request(t, http.MethodPost, envTestPath, `{"template":{"name":"acme-api","secrets":{}}}`, nil),
		http.StatusBadRequest, "environment-test-invalid")
}

// A template test resolves the template with the request's parameters: github with an Enterprise
// host sends GET /api/v3/user to that host with the token, its local MCP server stays untested
// while acme does not list the host, and a value that is not a host is 400.
func TestEnvironmentTest_TemplateParameters(t *testing.T) {
	t.Parallel()

	var got struct{ host, path, auth string }

	srv := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got.host, got.path, got.auth = r.Host, r.URL.Path, r.Header.Get(authorization)
	}))
	t.Cleanup(srv.Close)

	// The connection client dials srv for every host and trusts its certificate as ghe.example.com's.
	h := newHarnessLogged(t, nil, nil, func(h *harness, _ *api.Deps) {
		tr, _ := srv.Client().Transport.(*http.Transport)
		h.connTransport.TLSClientConfig = &tls.Config{RootCAs: tr.TLSClientConfig.RootCAs, ServerName: "example.com", MinVersion: tls.VersionTLS12}
		h.connTransport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
		}
	})
	h.createSecret(t)
	h.openDraft(t)

	req := func(host string) string {
		return `{"template":{"name":"github","secrets":{"token":"acme-token"},"parameters":{"host":"` + host + `"}}}`
	}

	r := decode[gen.EnvironmentTestResult](t, h.request(t, http.MethodPost, envTestPath, req("ghe.example.com"), nil), http.StatusOK)
	if sum := templateSummary(r.Template); sum != (templateSum{Ok: true, Status: http.StatusOK}) {
		t.Errorf("template = %+v; want it passed", sum)
	}

	if got.host != "ghe.example.com" || got.path != "/api/v3/user" || got.auth != "Bearer "+secretValue {
		t.Errorf("sent %s %s with %q; want ghe.example.com /api/v3/user with the token", got.host, got.path, got.auth)
	}

	// The host is not one acme lists: the local server is not started.
	if sums := summaries(r.Servers); !slices.Equal(sums, []serverSum{{Server: kindGitHub}}) || len(h.tools.Called()) != 0 {
		t.Errorf("servers = %+v, called %v; want the local server untested", sums, h.tools.Called())
	}

	refused(t, "a host with a path", h.request(t, http.MethodPost, envTestPath, req("evil.example.com/x"), nil),
		http.StatusBadRequest, "environment-test-invalid")
	refused(t, "an unknown parameter", h.request(t, http.MethodPost, envTestPath,
		`{"template":{"name":"github","secrets":{"token":"acme-token"},"parameters":{"region":"eu"}}}`, nil),
		http.StatusBadRequest, "environment-test-invalid")
}

// stdioEnvYAML is an environment whose stdio servers run the github template's Enterprise
// command, which the tool catalog ships, and commands it does not.
const stdioEnvYAML = `apiVersion: goal/v1
kind: Environment
spec:
  mcp:
    github:
      command: [github-mcp-server, stdio]
      env: {GITHUB_PERSONAL_ACCESS_TOKEN: "{{acme-token}}", GITHUB_HOST: "https://ghe.example.com"}
    playwright:
      command: [npx, "@playwright/mcp@0.0.41"]
      env: {TOKEN: "{{acme-token}}"}
    toolsets:
      command: [github-mcp-server, stdio, --toolsets, all]
      env: {GITHUB_PERSONAL_ACCESS_TOKEN: "{{acme-token}}"}
    shell:
      command: [sh, -c, "github-mcp-server stdio"]
`

// A stdio server whose command is not exactly one the tool catalog ships is refused: no secret is
// opened, no process starts, and its result says goald starts only the catalog's commands.
func TestEnvironmentTest_StdioOutsideCatalogRefused(t *testing.T) {
	t.Parallel()

	secrets := &countedSecrets{}
	h := newHarnessLogged(t, nil, nil, func(_ *harness, d *api.Deps) {
		secrets.OrgSecrets = d.OrgSecrets
		d.OrgSecrets = secrets
	})
	h.createSecret(t)
	h.draftEnvironment(t, stdioEnvYAML)

	for _, server := range []string{"playwright", "toolsets", "shell"} {
		got := decode[gen.EnvironmentTestResult](t, h.request(t, http.MethodPost, envTestPath, `{"server":"`+server+`"}`, nil), http.StatusOK)

		if len(got.Servers) != 1 || got.Servers[0].Tested || got.Servers[0].Ok ||
			!strings.Contains(valueOf(got.Servers[0].Error), "only the tool catalog's commands") {
			t.Errorf("%s: test = %s; want it refused as outside the catalog", server, body(t, got))
		}
	}

	if n := secrets.resolves.Load(); n != 0 {
		t.Errorf("secrets were opened %d times; want none", n)
	}

	if called := h.tools.Called(); len(called) != 0 {
		t.Errorf("started %v; want nothing", called)
	}

	if kept, err := h.envTests.List(inOrg(t), acmeID); err != nil || len(kept) != 0 {
		t.Errorf("kept = %+v, %v; want nothing", kept, err)
	}
}

// gheHost is the GitHub Enterprise host the stdio tests list, and stdioGitHub the github
// template's Enterprise MCP server command.
const gheHost = "ghe.example.com"

var stdioGitHub = []string{"github-mcp-server", "stdio"}

// stdioHarness is a harness whose acme lists gheHost and whose tool lister serves stdioGitHub
// with get_issue read-only.
func stdioHarness(t *testing.T) *harness {
	t.Helper()

	h := newHarness(t)
	h.createSecret(t)
	h.listHosts(t, domain.CodeHost{Host: gheHost, Kind: domain.CodeHostGitHub})

	h.tools.Serve(porttest.StdioKey(stdioGitHub...), "merge_pull_request")
	h.tools.ServeReadOnly(porttest.StdioKey(stdioGitHub...), "get_issue")

	return h
}

// startedGitHub reports whether e is stdioGitHub started with the opened token and gheHost.
func startedGitHub(e ports.MCPEndpoint) bool {
	return slices.Equal(e.Command, stdioGitHub) && e.Env["GITHUB_PERSONAL_ACCESS_TOKEN"] == secretValue &&
		e.Env["GITHUB_HOST"] == "https://"+gheHost
}

// githubTested is the summary of the github server tested with its tools.
var githubTested = []serverSum{{Server: kindGitHub, Tested: true, Ok: true, Tools: "get_issue,merge_pull_request", At: true}}

// A stdio server of an environment file with a catalog command starts with its env's secrets
// opened and keeps its tools and read-only tools. A URL in its env on a host acme does not list
// starts nothing.
func TestEnvironmentTest_StdioFromCatalog(t *testing.T) {
	t.Parallel()

	h := stdioHarness(t)
	h.draftEnvironment(t, stdioEnvYAML)

	got := decode[gen.EnvironmentTestResult](t, h.request(t, http.MethodPost, envTestPath, `{"server":"github"}`, nil), http.StatusOK)
	if sums := summaries(got.Servers); !slices.Equal(sums, githubTested) {
		t.Errorf("servers = %+v; want github tested", sums)
	}

	if called := h.tools.Called(); len(called) != 1 || !startedGitHub(called[0]) {
		t.Fatalf("called = %v; want github-mcp-server with its token opened", called)
	}

	kept, err := h.envTests.List(inOrg(t), acmeID)
	if err != nil || len(kept) != 1 || !slices.Equal(kept[0].ReadOnly, []string{"get_issue"}) {
		t.Errorf("kept = %+v, %v; want github with get_issue read-only", kept, err)
	}

	h.listHosts(t)

	unlisted := decode[gen.EnvironmentTestResult](t, h.request(t, http.MethodPost, envTestPath, `{"server":"github"}`, nil), http.StatusOK)
	if s := unlisted.Servers[0]; s.Tested || !strings.Contains(valueOf(s.Error), gheHost) || len(h.tools.Called()) != 1 {
		t.Errorf("test with an unlisted host = %s; want it refused naming the host", body(t, unlisted))
	}
}

// A template test of github with an Enterprise host acme lists starts the otherwise lines' stdio
// server with the slot's value.
func TestEnvironmentTest_StdioTemplate(t *testing.T) {
	t.Parallel()

	h := stdioHarness(t)
	h.openDraft(t)

	tpl := `{"template":{"name":"github","secrets":{"token":"acme-token"},"parameters":{"host":"` + gheHost + `"}},"server":"github"}`

	r := decode[gen.EnvironmentTestResult](t, h.request(t, http.MethodPost, envTestPath, tpl, nil), http.StatusOK)
	if sums := summaries(r.Servers); !slices.Equal(sums, githubTested) {
		t.Errorf("template servers = %+v; want the otherwise lines' server tested", sums)
	}

	if called := h.tools.Called(); len(called) != 1 || !startedGitHub(called[0]) {
		t.Errorf("called = %v; want the template's server with the slot's value", called)
	}
}
