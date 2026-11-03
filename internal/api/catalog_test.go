package api_test

import (
	"net/http"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
)

// /v1/catalog answers goald's templates by name, each with its slots, what it adds and its test.
func TestCatalog_List(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	got := decode[gen.ToolTemplateList](t, h.request(t, http.MethodGet, "/v1/catalog", "", nil), http.StatusOK)
	if len(got.Templates) != 11 || got.Templates[0].Name != "anthropic" || got.Templates[10].Name != "slack" {
		t.Fatalf("templates = %s", body(t, got))
	}

	var gh gen.ToolTemplate

	for _, tpl := range got.Templates {
		if tpl.Name == "github" {
			gh = tpl
		}
	}

	mcp := gh.Adds.Mcp["github"]
	if gh.Title != "GitHub" || len(gh.Secrets) != 1 || gh.Secrets[0].Suggest != "github-token" || gh.Adds.Env["GH_TOKEN"] != "{{token}}" ||
		valueOf(mcp.Url) == "" || mcp.Tools["merge_pull_request"] != gen.TemplateMCPToolsAsk || gh.Test == nil || gh.Test.Expect != 200 {
		t.Errorf("github = %s", body(t, gh))
	}
}
