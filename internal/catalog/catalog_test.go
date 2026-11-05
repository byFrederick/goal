package catalog_test

import (
	"errors"
	"maps"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/catalog"
	"github.com/byFrederick/goal/internal/core/domain"
)

// Every template goald ships loads, one for each of today's connection kinds but aws and
// bedrock, sorted by name, each with a title, a description and a suggested name per secret.
func TestTemplates_LoadAll(t *testing.T) {
	t.Parallel()

	got, err := catalog.Templates()
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	for _, tpl := range got {
		names = append(names, tpl.Name)

		if tpl.Title == "" || tpl.Description == "" {
			t.Errorf("%s has no title or description", tpl.Name)
		}

		for _, s := range tpl.Secrets {
			if s.Suggest == "" {
				t.Errorf("%s: slot %s suggests no secret name", tpl.Name, s.Slot)
			}
		}
	}

	want := []string{
		"anthropic", "claude-subscription", "confluence", "datadog", "github", "github-read", "gitlab", "http-api-key", "jira",
		"mcp-server", "pagerduty", "slack",
	}
	if !slices.Equal(names, want) {
		t.Errorf("templates = %v; want %v", names, want)
	}

	if slices.Contains(names, "aws") || slices.Contains(names, "bedrock") {
		t.Errorf("templates = %v; aws and bedrock are dropped", names)
	}
}

// GitHub's template adds gh, GH_TOKEN, the remote MCP server with suggested modes and a test
// that renders with its secret.
func TestTemplates_GitHub(t *testing.T) {
	t.Parallel()

	got, err := catalog.Templates()
	if err != nil {
		t.Fatal(err)
	}

	i := slices.IndexFunc(got, func(tpl domain.ToolTemplate) bool { return tpl.Name == "github" })
	if i < 0 {
		t.Fatal("no github template")
	}

	gh := got[i]
	if gh.Env["GH_TOKEN"] != "{{token}}" || gh.MCP["github"].URL == "" || gh.MCP["github"].Tools["merge_pull_request"] != "ask" ||
		gh.MCP["github"].Tools["delete_*"] != "ask" {
		t.Errorf("github = %+v", gh)
	}

	r, err := gh.RenderTest(map[string]string{tokenSlot: "test-token"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if r.URL != "https://api.github.com/user" || r.Headers["Authorization"] != "Bearer test-token" || r.Expect != 200 {
		t.Errorf("rendered = %#v", r)
	}
}

// templateNamed is the catalog's template name.
func templateNamed(t *testing.T, name string) domain.ToolTemplate {
	t.Helper()

	got, err := catalog.Templates()
	if err != nil {
		t.Fatal(err)
	}

	i := slices.IndexFunc(got, func(tpl domain.ToolTemplate) bool { return tpl.Name == name })
	if i < 0 {
		t.Fatalf("no %s template", name)
	}

	return got[i]
}

// The GitHub templates' token slot, the value a test puts in it and their host parameter.
const (
	tokenSlot = "token"
	tokenRef  = "{{" + tokenSlot + "}}"
	testToken = "test-token"
	hostParam = "host"
)

// github-read adds gh, GH_TOKEN with a read-only token, the remote MCP server in its read-only
// mode and the api.github.com test.
func TestTemplates_GitHubRead(t *testing.T) {
	t.Parallel()

	gh := templateNamed(t, "github-read")

	if len(gh.Secrets) != 1 || gh.Secrets[0].Slot != tokenSlot || gh.Secrets[0].Suggest != "github-read-token" ||
		!strings.Contains(strings.ToLower(gh.Secrets[0].Description), "a read-only fine-grained token") {
		t.Errorf("secrets = %+v", gh.Secrets)
	}

	if !slices.Equal(gh.Tools, []string{"gh"}) || !maps.Equal(gh.Env, map[string]string{"GH_TOKEN": tokenRef}) {
		t.Errorf("tools = %v, env = %v", gh.Tools, gh.Env)
	}

	m := gh.MCP["github"]
	if m.URL != "https://api.githubcopilot.com/mcp/" ||
		!maps.Equal(m.Headers, map[string]string{"Authorization": "Bearer " + tokenRef, "X-MCP-Readonly": "true"}) {
		t.Errorf("mcp = %+v", m)
	}

	r, err := gh.RenderTest(map[string]string{tokenSlot: testToken}, nil)
	if err != nil || r.URL != "https://api.github.com/user" || r.Headers["Authorization"] != "Bearer "+testToken {
		t.Errorf("rendered = %#v, %v", r, err)
	}
}

// With an Enterprise host, both GitHub templates set GH_HOST and GH_ENTERPRISE_TOKEN, run the
// local github-mcp-server against the host and test its API; github.com, or no value, keeps the
// default lines.
func TestTemplates_GitHubEnterprise(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		command []string
	}{
		{"github", []string{"github-mcp-server", "stdio"}},
		{"github-read", []string{"github-mcp-server", "stdio", "--read-only"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tpl := templateNamed(t, tc.name)

			got, err := tpl.Resolve(map[string]string{hostParam: "ghe.example.com"})
			if err != nil {
				t.Fatal(err)
			}

			if !maps.Equal(got.Env, map[string]string{"GH_HOST": "ghe.example.com", "GH_ENTERPRISE_TOKEN": tokenRef}) {
				t.Errorf("env = %v; want GH_HOST and GH_ENTERPRISE_TOKEN, no GH_TOKEN", got.Env)
			}

			m := got.MCP["github"]
			if m.URL != "" || !slices.Equal(m.Command, tc.command) ||
				!maps.Equal(m.Env, map[string]string{"GITHUB_PERSONAL_ACCESS_TOKEN": tokenRef, "GITHUB_HOST": "https://ghe.example.com"}) {
				t.Errorf("mcp = %+v", m)
			}

			r, err := got.RenderTest(map[string]string{tokenSlot: testToken}, nil)
			if err != nil || r.Method != http.MethodGet || r.URL != "https://ghe.example.com/api/v3/user" {
				t.Errorf("rendered = %#v, %v", r, err)
			}

			for _, params := range []map[string]string{nil, {hostParam: "github.com"}} {
				if same, err := tpl.Resolve(params); err != nil || !reflect.DeepEqual(same, tpl) {
					t.Errorf("Resolve(%v) = %+v, %v; want the default lines", params, same, err)
				}
			}
		})
	}
}

// The commands goald may start to test a stdio server are the GitHub templates' Enterprise ones.
func TestTemplates_CatalogCommands(t *testing.T) {
	t.Parallel()

	got, err := catalog.Templates()
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		command []string
		want    bool
	}{
		{[]string{"github-mcp-server", "stdio"}, true},
		{[]string{"github-mcp-server", "stdio", "--read-only"}, true},
		{[]string{"github-mcp-server", "http"}, false},
		{[]string{"npx", "-y", "@modelcontextprotocol/server-github"}, false},
	} {
		if ok := domain.CatalogCommand(got, tc.command); ok != tc.want {
			t.Errorf("CatalogCommand(%q) = %v; want %v", tc.command, ok, tc.want)
		}
	}
}

// Resolve refuses a value that is not a host, an unknown parameter, and names neither's value.
func TestResolve_RefusesValue(t *testing.T) {
	t.Parallel()

	tpl := templateNamed(t, "github")

	for _, params := range []map[string]string{
		{hostParam: "evil.example.com/x"}, {hostParam: "https://x"}, {hostParam: ""}, {hostParam: "a b"}, {"region": "eu"},
	} {
		if _, err := tpl.Resolve(params); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("Resolve(%v) = %v; want ErrInvalid", params, err)
		}
	}

	if _, err := tpl.Resolve(map[string]string{hostParam: "ghe.example.com:8443"}); err != nil {
		t.Errorf("Resolve with a port = %v", err)
	}
}

// Jira's test reads its plain site from env, which the caller may change.
func TestTemplates_RenderTestWithEnv(t *testing.T) {
	t.Parallel()

	got, err := catalog.Templates()
	if err != nil {
		t.Fatal(err)
	}

	jira := got[slices.IndexFunc(got, func(tpl domain.ToolTemplate) bool { return tpl.Name == "jira" })]

	r, err := jira.RenderTest(map[string]string{tokenSlot: "test-token"}, map[string]string{"JIRA_URL": "https://example.atlassian.net"})
	if err != nil {
		t.Fatal(err)
	}

	if r.URL != "https://example.atlassian.net/rest/api/3/myself" || r.Basic == nil || r.Basic.Password != "test-token" {
		t.Errorf("rendered = %#v", r)
	}

	if _, err := jira.RenderTest(nil, nil); !errors.Is(err, domain.ErrInvalid) || strings.Contains(err.Error(), "test-token") {
		t.Errorf("render without the secret = %v; want ErrInvalid naming the slot", err)
	}
}

const base = `apiVersion: goal/v1
kind: Template
metadata: {name: acme, title: Acme, description: The acme API.}
spec:
  secrets:
    - {slot: key, title: API key, suggest: acme-key}
`

// Parse refuses a template that breaks a rule, naming it.
func TestParse_Refuses(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, spec, want string }{
		{"a slot with no secret", "  env: {ACME_KEY: \"{{token}}\"}\n", "{{token}}"},
		{"a remote server with a command", "  mcp:\n    acme:\n      url: https://mcp.example.com/mcp\n      command: [acme-mcp]\n", "never both"},
		{"a bad mode", "  mcp:\n    acme:\n      url: https://mcp.example.com/mcp\n      tools: {delete_issue: maybe}\n", "maybe"},
		{"a relative file path", "  files: {acme/config: \"{{key}}\"}\n", "acme/config"},
		{"an unknown key", "  connections: [acme]\n", "connections"},
		{"a reserved env name", "  env: {GOAL_KEY: \"{{key}}\"}\n", "GOAL_KEY"},
		{"a test of an unknown env", "  test: {method: GET, url: \"${ACME_URL}/me\", expect: 200}\n", "ACME_URL"},
		{"a parameter named like a slot", "  parameters:\n    - {name: key, title: Key, default: example.com}\n", "parameter key"},
		{"a parameter declared twice", "  parameters:\n    - {name: host, title: Host, default: example.com}\n    - {name: host, title: Host, default: example.com}\n", "twice"},
		{"a parameter without a default", "  parameters:\n    - {name: host, title: Host}\n", "default"},
		{"two parameters with otherwise", "  parameters:\n    - {name: host, title: Host, default: example.com, otherwise: {env: {ACME_HOST: \"{{host}}\"}}}\n" +
			"    - {name: region, title: Region, default: eu, otherwise: {env: {ACME_REGION: \"{{region}}\"}}}\n", "otherwise"},
		{"a reference to neither", "  parameters:\n    - {name: host, title: Host, default: example.com, otherwise: {env: {ACME_HOST: \"{{site}}\"}}}\n", "{{site}}"},
		{"a default line that refers to a parameter", "  parameters:\n    - {name: host, title: Host, default: example.com}\n  env: {ACME_HOST: \"{{host}}\"}\n", "{{host}}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := catalog.Parse([]byte(base + tc.spec))
			if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Parse = %v; want ErrInvalid naming %q", err, tc.want)
			}
		})
	}
}

// Parse reads a template whole, defaulting its test's expected status to 200.
func TestParse_Reads(t *testing.T) {
	t.Parallel()

	got, err := catalog.Parse([]byte(base + `  tools: [acme@1]
  env: {ACME_URL: "https://api.example.com", ACME_KEY: "{{key}}"}
  files: {"~/.acme/key": "{{key}}"}
  mcp:
    acme:
      url: https://mcp.example.com/mcp
      headers: {Authorization: "Bearer {{key}}"}
      tools: {"delete_*": ask}
  test: {method: GET, url: "${ACME_URL}/me", headers: {Authorization: "Bearer {{key}}"}}
`))
	if err != nil {
		t.Fatal(err)
	}

	if got.Name != "acme" || got.Title != "Acme" || len(got.Secrets) != 1 || got.Secrets[0].Suggest != "acme-key" ||
		got.Tools[0] != "acme@1" || got.Files["~/.acme/key"] != "{{key}}" || got.MCP["acme"].Tools["delete_*"] != "ask" ||
		got.Test == nil || got.Test.Expect != 200 {
		t.Errorf("Parse = %+v", got)
	}
}
