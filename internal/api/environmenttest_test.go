package api_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/api"
	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The route the environment tests call, acme's default environment file in the draft and the
// MCP server URLs the tests' environments name.
const (
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
	if got.Environment != "default" || got.Version != "draft" || len(got.Servers) != 3 {
		t.Fatalf("test = %s", body(t, got))
	}

	docs, linear, playwright := got.Servers[0], got.Servers[1], got.Servers[2]
	if linear.Server != "linear" || !linear.Tested || !linear.Ok || !slices.Equal(linear.Tools, []string{"create_issue", "list_issues"}) || linear.At == nil {
		t.Errorf("linear = %s", body(t, linear))
	}

	if docs.Server != "docs" || !docs.Ok || playwright.Server != "playwright" || playwright.Tested || playwright.Ok {
		t.Errorf("docs, playwright = %s, %s", body(t, docs), body(t, playwright))
	}

	called := h.tools.Called()
	if i := slices.IndexFunc(called, func(e ports.MCPEndpoint) bool { return e.URL == linearURL }); i < 0 || called[i].Headers["Authorization"] != "Bearer "+secretValue {
		t.Errorf("called = %v; want linear with its secret opened", called)
	}

	kept, err := h.envTests.List(inOrg(t), acmeID)
	if err != nil || len(kept) != 2 || kept[1].Server != "linear" || !kept[1].OK || len(kept[1].Tools) != 2 {
		t.Errorf("kept = %+v, %v; want docs and linear", kept, err)
	}

	one := decode[gen.EnvironmentTestResult](t, h.request(t, http.MethodPost, envTestPath, `{"server":"linear"}`, nil), http.StatusOK)
	if len(one.Servers) != 1 || one.Servers[0].Server != "linear" {
		t.Errorf("server linear = %s", body(t, one))
	}

	refused(t, "an unknown server", h.request(t, http.MethodPost, envTestPath, `{"server":"sentry"}`, nil),
		http.StatusNotFound, "environment-server-not-found")
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

	h.draftEnvironment(t, strings.ReplaceAll(testedEnvYAML, "acme-token", "gone-key"))

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

// A template test runs its HTTP test with the secrets its slots take and lists its MCP server's
// tools, keeping that result as the environment's; a slot without a secret is 400.
func TestEnvironmentTest_Template(t *testing.T) {
	t.Parallel()

	var auth string

	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/me" || auth != "Bearer "+secretValue {
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	t.Cleanup(upstream.Close)

	h := newHarnessLogged(t, nil, nil, func(h *harness, d *api.Deps) {
		h.connTransport.TLSClientConfig = upstream.Client().Transport.(*http.Transport).TLSClientConfig
		d.Catalog = append(d.Catalog, domain.ToolTemplate{
			Name: "acme-api", Title: "Acme", Description: "The acme API.",
			Secrets: []domain.TemplateSecret{{Slot: "key", Title: "Key", Suggest: "acme-key"}},
			Env:     map[string]string{"ACME_URL": "https://api.example.com", "ACME_KEY": "{{key}}"},
			MCP:     map[string]domain.TemplateMCP{"acme": {URL: templateMCP, Headers: map[string]string{"Authorization": "Bearer {{key}}"}}},
			Test:    &domain.TemplateTest{Method: http.MethodGet, URL: "${ACME_URL}/v1/me", Headers: map[string]string{"Authorization": "Bearer {{key}}"}, Expect: 200},
		})
	})
	h.createSecret(t)
	h.openDraft(t)
	h.tools.Serve(templateMCP, "get_widget")

	req := `{"template":{"name":"acme-api","secrets":{"key":"acme-token"},"env":{"ACME_URL":"` + upstream.URL + `"}}}`

	got := decode[gen.EnvironmentTestResult](t, h.request(t, http.MethodPost, envTestPath, req, nil), http.StatusOK)
	if got.Template == nil || !got.Template.Ok || valueOf(got.Template.Status) != 200 || auth != "Bearer "+secretValue {
		t.Errorf("template = %s (auth %q)", body(t, got), auth)
	}

	if len(got.Servers) != 1 || got.Servers[0].Server != "acme" || !got.Servers[0].Ok || got.Servers[0].Tools[0] != "get_widget" {
		t.Errorf("servers = %s", body(t, got.Servers))
	}

	if kept, err := h.envTests.List(inOrg(t), acmeID); err != nil || len(kept) != 1 || kept[0].Server != "acme" {
		t.Errorf("kept = %+v, %v; want acme", kept, err)
	}

	failed := decode[gen.EnvironmentTestResult](t, h.request(t, http.MethodPost, envTestPath,
		`{"template":{"name":"acme-api","secrets":{"key":"acme-token"},"env":{"ACME_URL":"`+upstream.URL+`/wrong"}}}`, nil), http.StatusOK)
	if failed.Template == nil || failed.Template.Ok || valueOf(failed.Template.Status) != http.StatusUnauthorized ||
		strings.Contains(valueOf(failed.Template.Error), secretValue) {
		t.Errorf("failed template = %s", body(t, failed))
	}

	refused(t, "a slot without a secret", h.request(t, http.MethodPost, envTestPath, `{"template":{"name":"acme-api","secrets":{}}}`, nil),
		http.StatusBadRequest, "environment-test-invalid")
}
