package api_test

import (
	"net/http"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
)

// /v1/catalog answers goald's templates by name, each with its slots, its parameters, what it adds
// and its test.
func TestCatalog_List(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	got := decode[gen.ToolTemplateList](t, h.request(t, http.MethodGet, "/v1/catalog", "", nil), http.StatusOK)
	if len(got.Templates) != 12 {
		t.Fatalf("templates = %s; want goald's 12", body(t, got))
	}

	var gh gen.ToolTemplate

	for _, tpl := range got.Templates {
		if tpl.Name == kindGitHub {
			gh = tpl
		}
	}

	want := `{"adds":{"env":{"GH_TOKEN":"{{token}}"},"files":{},"mcp":{"github":{"headers":{"Authorization":"Bearer {{token}}"},` +
		`"tools":{"delete_*":"ask","merge_pull_request":"ask"},"url":"https://api.githubcopilot.com/mcp/"}},"tools":["gh"]},` +
		`"description":"The gh CLI and GitHub's MCP server with a fine-grained token for the repositories agents may use.","name":"github",` +
		`"parameters":[{"default":"github.com","description":"github.com, or your GitHub Enterprise Server's host name.","name":"host",` +
		`"otherwise":{"env":{"GH_ENTERPRISE_TOKEN":"{{token}}","GH_HOST":"{{host}}"},"mcp":{"github":{"command":["github-mcp-server","stdio"],` +
		`"env":{"GITHUB_HOST":"https://{{host}}","GITHUB_PERSONAL_ACCESS_TOKEN":"{{token}}"},"tools":{"delete_*":"ask","merge_pull_request":"ask"}}},` +
		`"test":{"expect":200,"headers":{"Authorization":"Bearer {{token}}"},"method":"GET","url":"https://{{host}}/api/v3/user"},` +
		`"tools":["gh","github:github/github-mcp-server"]},"title":"Host"}],` +
		`"secrets":[{"description":"A fine-grained token: only the repositories and permissions agents need.","slot":"token","suggest":"github-token","title":"Token"}],` +
		`"test":{"expect":200,"headers":{"Authorization":"Bearer {{token}}"},"method":"GET","url":"https://api.github.com/user"},"title":"GitHub"}`
	if got := body(t, gh); got != want {
		t.Errorf("github = %s\nwant %s", got, want)
	}
}
