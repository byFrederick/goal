package catalog_test

import (
	"errors"
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
		"anthropic", "claude-subscription", "confluence", "datadog", "github", "gitlab", "http-api-key", "jira",
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
	if gh.Env["GH_TOKEN"] != "{{token}}" || gh.MCP["github"].URL == "" || gh.MCP["github"].Tools["merge_pull_request"] != "ask" {
		t.Errorf("github = %+v", gh)
	}

	r, err := gh.RenderTest(map[string]string{"token": "test-token"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if r.URL != "https://api.github.com/user" || r.Headers["Authorization"] != "Bearer test-token" || r.Expect != 200 {
		t.Errorf("rendered = %#v", r)
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

	r, err := jira.RenderTest(map[string]string{"token": "test-token"}, map[string]string{"JIRA_URL": "https://example.atlassian.net"})
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
