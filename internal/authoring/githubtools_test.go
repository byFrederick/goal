package authoring_test

import (
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// gitHubEnv is acmeEnvironment with GitHub's remote MCP server, whose tools are the YAML tools.
func gitHubEnv(tools string) string {
	return acmeEnvironment + "  mcp:\n    github: { url: \"https://api.githubcopilot.com/mcp/\", tools: " + tools + " }\n"
}

// withCode is acmeYAML whose engineer says code: code.
func withCode(code string) string {
	return strings.Replace(acmeYAML, "      prompt: ./agents/engineer.md\n", "      prompt: ./agents/engineer.md\n      code: "+code+"\n", 1)
}

// withCheckTrigger is withCode(code) with a schedule trigger whose check runs the engineer.
func withCheckTrigger(code string) string {
	return withCode(code) + "    triggers:\n" +
		"      - name: incidents\n        on: schedule\n        cron: \"0 6 * * *\"\n" +
		"        create: { type: fix, station: inbox, title: Incident }\n        check: { agent: engineer, brief: Look for new incidents. }\n"
}

// The GitHub tools warnings judge each tool by the readOnlyHint its server's kept test found, for
// an agent that only reads code and for every check, never for an agent that writes.
func TestSecretsCheck_GitHubTools(t *testing.T) {
	t.Parallel()

	writes := []string{"create_issue", "delete_file", "get_issue", "list_issues", "merge_pull_request", "push_files", "update_issue", "create_branch"}
	reads := []string{"get_issue", "list_issues"}
	passed := func(tools, readOnly []string) *domain.EnvironmentTest {
		return &domain.EnvironmentTest{Environment: "default", Server: "github", OK: true, Tools: tools, ReadOnly: readOnly}
	}

	for _, tt := range []struct {
		name, yaml, env string
		test            *domain.EnvironmentTest
		code            string
		pointer         string
		inMessage       []string
	}{
		{
			name: "reader", yaml: withCode("read"), env: gitHubEnv("{}"), test: passed(writes, reads),
			code: authoring.CodeGitHubToolsNotReadOnly, pointer: "/spec/agents/0/code",
			inMessage: []string{"agent engineer", "environment default", "server github", "2026-10-08",
				"create_branch, create_issue, delete_file, merge_pull_request, push_files and 1 more;"},
		},
		{name: "writer", yaml: withCode("write"), env: gitHubEnv("{}"), test: passed(writes, reads)},
		{
			name: "check", yaml: withCheckTrigger("write"), env: gitHubEnv(`{ "*": ask, "create_issue": allow }`), test: passed(writes, reads),
			code: authoring.CodeGitHubToolsNotReadOnly, pointer: "/spec/board/triggers/0/check/agent",
			inMessage: []string{"trigger incidents", "check agent engineer", ": create_issue;"},
		},
		{
			name: "untested", yaml: withCode("read"), env: gitHubEnv("{}"),
			code: authoring.CodeGitHubToolsUnknown, pointer: "/spec/agents/0/code", inMessage: []string{"server github", "unknown until the server is tested"},
		},
		{
			name: "failed test", yaml: withCode("read"), env: gitHubEnv("{}"),
			test: &domain.EnvironmentTest{Environment: "default", Server: "github", Error: "401 Unauthorized"},
			code: authoring.CodeGitHubToolsUnknown, pointer: "/spec/agents/0/code",
		},
		{name: "read-only", yaml: withCode("read"), env: gitHubEnv("{}"), test: passed(reads, reads)},
		{name: "read-only by mode", yaml: withCode("read"), env: gitHubEnv(`{ "*": off, "get_*": allow, "list_*": ask }`), test: passed(writes, reads)},
		{name: "not GitHub", yaml: withCode("read"), env: acmeEnvironment + "  mcp:\n    pager: { url: \"https://pager.example.com/mcp\" }\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			files := acmeFiles(tt.yaml)
			files[envFile] = []byte(tt.env)

			fs := fstest.MapFS{}
			for p, b := range files {
				fs[p] = &fstest.MapFile{Data: b}
			}

			f, parsed := config.Check(fs)
			if f == nil {
				t.Fatalf("the definition does not parse: %+v", parsed)
			}

			org, _ := domain.OrgFrom(h.ctx)
			tests := porttest.NewEnvironmentTests()
			tests.AddFactory(org, domain.FactoryID(f.Metadata.ID))

			if tt.test != nil {
				kept := *tt.test
				kept.Factory, kept.At = domain.FactoryID(f.Metadata.ID), time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)

				if err := tests.Record(h.ctx, kept); err != nil {
					t.Fatal(err)
				}
			}

			problems, err := authoring.SecretsCheck{Secrets: h.secrets, Hosts: h.tenancy, Tests: tests}.Check(h.ctx, f)
			if err != nil {
				t.Fatal(err)
			}

			var got []domain.DefinitionProblem

			for _, p := range problems {
				if p.Code == authoring.CodeGitHubToolsNotReadOnly || p.Code == authoring.CodeGitHubToolsUnknown {
					got = append(got, p)
				}
			}

			if tt.code == "" {
				if len(got) > 0 {
					t.Fatalf("warnings = %+v; want none", got)
				}

				return
			}

			if len(got) != 1 || got[0].Code != tt.code || got[0].Severity != domain.SeverityWarning {
				t.Fatalf("warnings = %+v; want one %s", got, tt.code)
			}

			if want := f.Locate(tt.pointer); got[0].File != yamlFile || got[0].Pointer != want.Pointer || got[0].Line != want.Line || got[0].Line == 0 {
				t.Errorf("warning at %s %s line %d; want %s line %d", got[0].File, got[0].Pointer, got[0].Line, want.Pointer, want.Line)
			}

			for _, s := range tt.inMessage {
				if !strings.Contains(got[0].Message, s) {
					t.Errorf("message %q does not name %q", got[0].Message, s)
				}
			}
		})
	}

	t.Run("without tests", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		files := acmeFiles(withCode("read"))
		files[envFile] = []byte(gitHubEnv("{}"))

		fs := fstest.MapFS{}
		for p, b := range files {
			fs[p] = &fstest.MapFile{Data: b}
		}

		f, _ := config.Check(fs)

		problems, err := authoring.SecretsCheck{Secrets: h.secrets, Hosts: h.tenancy}.Check(h.ctx, f)
		if err != nil {
			t.Fatal(err)
		}

		for _, p := range problems {
			if strings.HasPrefix(p.Code, "github-tools-") {
				t.Errorf("problem %+v; want no GitHub tools warning without Tests", p)
			}
		}
	})
}
