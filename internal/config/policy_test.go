package config_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
)

// policyYAML is a factory whose runtime names runtimeModel and whose agents name the models
// after it ("" for none).
func policyYAML(runtimeModel string, agentModels ...string) string {
	var b strings.Builder

	b.WriteString("apiVersion: goal/v1\nkind: Factory\nmetadata:\n  id: 0b6f1c9e-5d0a-4c39-9a51-6f1f3f0e2a10\n  name: acme\n")
	b.WriteString("spec:\n  runtime:\n    modelConnection: claude\n")

	if runtimeModel != "" {
		b.WriteString("    model: " + runtimeModel + "\n")
	}

	b.WriteString("  agents:\n")

	for i, m := range agentModels {
		name := []string{"engineer", "reviewer"}[i]
		b.WriteString("    - name: " + name + "\n      prompt: ./agents/a.md\n")

		if m != "" {
			b.WriteString("      model: " + m + "\n")
		}
	}

	b.WriteString("  board:\n    stations:\n      - { id: inbox, kind: queue }\n      - { id: build, kind: agent, agent: engineer, wip: 1 }\n      - { id: done, kind: done }\n")

	return b.String()
}

// A model outside the organization's allowlist is an error at its key that names the agent (or
// runtime) and the model; a factory whose models are all allowed, or that names none, has none.
func TestModelProblems(t *testing.T) {
	t.Parallel()

	allowed := []string{"claude-sonnet-5-5", "claude-haiku-5-5"}

	cases := []struct {
		name    string
		yaml    string
		want    []string // pointers of the problems
		message []string // what each message names
	}{
		{name: "no model", yaml: policyYAML("", "", "")},
		{name: "all allowed", yaml: policyYAML("claude-sonnet-5-5", "claude-haiku-5-5", "")},
		{
			name: "runtime", yaml: policyYAML("claude-opus-5-5", "", ""),
			want: []string{"/spec/runtime/model"}, message: []string{"runtime", "claude-opus-5-5"},
		},
		{
			name: "agent", yaml: policyYAML("claude-sonnet-5-5", "", "claude-opus-5-5"),
			want: []string{"/spec/agents/1/model"}, message: []string{"reviewer", "claude-opus-5-5"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f, err := config.Load(fstest.MapFS{
				config.FileName: {Data: []byte(tc.yaml)}, "agents/a.md": {Data: []byte("# agent\n")},
			})
			if err != nil {
				t.Fatal(err)
			}

			got := config.ModelProblems(f, allowed)
			if len(got) != len(tc.want) {
				t.Fatalf("problems %+v, want at %v", got, tc.want)
			}

			for i, p := range got {
				expectModelProblem(t, p, f.Locate(tc.want[i]), tc.message)
			}
		})
	}
}

// expectModelProblem fails the test unless p is a model-not-allowed error of the policy check,
// located as want, whose message names each of names.
func expectModelProblem(t *testing.T, p domain.DefinitionProblem, want config.Location, names []string) {
	t.Helper()

	if p.Check != config.CheckPolicy || p.Code != config.CodeModelNotAllowed || p.Severity != domain.SeverityError {
		t.Errorf("problem %+v, want an error of check policy, code model-not-allowed", p)
	}

	if p.Pointer != want.Pointer || p.File != want.File || p.Line != want.Line || p.Line == 0 {
		t.Errorf("problem at %s:%d %q, want at %s:%d %q", p.File, p.Line, p.Pointer, want.File, want.Line, want.Pointer)
	}

	for _, w := range names {
		if !strings.Contains(p.Message, w) {
			t.Errorf("message %q does not name %q", p.Message, w)
		}
	}
}

// ADR 0037 decisions 8 and 12: each mcp.<server>.tools entry the organization's cap makes
// stricter is a warning at that entry, naming the server, the tool, the file's mode and the
// effective one; an entry the cap leaves alone, or a factory without a cap, has none.
func TestMCPCapProblems(t *testing.T) {
	t.Parallel()

	const deletes = "delete_*"

	env := "apiVersion: goal/v1\nkind: Environment\nspec:\n  mcp:\n    linear:\n      url: https://mcp.linear.example.com/mcp\n" +
		"      tools: {create_comment: allow, close_issue: ask, \"delete_*\": allow, list_issues: off}\n"

	f, err := config.Load(fstest.MapFS{
		config.FileName: {Data: []byte(policyYAML("", ""))}, "agents/a.md": {Data: []byte("# agent\n")},
		"environments/default.yaml": {Data: []byte(env)},
	})
	if err != nil {
		t.Fatal(err)
	}

	envs := f.Environments()
	if len(envs) != 1 {
		t.Fatalf("environments %+v, want default", envs)
	}

	cases := []struct {
		name string
		cap  map[string]domain.ConnectionToolMode
		want map[string][]string // tool entry → what its message names
	}{
		{name: "no cap"},
		{name: "a cap looser than the file", cap: map[string]domain.ConnectionToolMode{"*": domain.ToolModeAllow}},
		{
			name: "ask everything", cap: map[string]domain.ConnectionToolMode{"*": domain.ToolModeAsk},
			want: map[string][]string{
				"create_comment": {"linear", "create_comment", "allow", "ask"},
				deletes:          {"linear", deletes, "allow", "ask"},
			},
		},
		{
			name: "a glob entry under an exact cap", cap: map[string]domain.ConnectionToolMode{"delete_project": domain.ToolModeOff},
			want: map[string][]string{deletes: {"linear", "delete_project", deletes, "allow", "off"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := config.MCPCapProblems(f, tc.cap)
			if len(got) != len(tc.want) {
				t.Fatalf("problems %+v, want one for each of %v", got, tc.want)
			}

			for _, p := range got {
				expectCapProblem(t, envs[0], p, tc.want)
			}
		})
	}
}

// expectCapProblem fails the test unless p is an mcp-tool-mode-capped warning of the policy check
// at one of want's tool entries of the linear server in env, whose message names what want lists
// for it.
func expectCapProblem(t *testing.T, env config.Environment, p domain.DefinitionProblem, want map[string][]string) {
	t.Helper()

	if p.Check != config.CheckPolicy || p.Code != config.CodeMCPToolModeCapped || p.Severity != domain.SeverityWarning {
		t.Errorf("problem %+v, want a warning of check policy, code mcp-tool-mode-capped", p)
	}

	for tool, names := range want {
		w := env.Locate("spec", "mcp", "linear", "tools", tool)
		if w.Pointer != p.Pointer {
			continue
		}

		if p.File != w.File || p.Line != w.Line || p.Line == 0 {
			t.Errorf("problem at %s:%d, want at %s:%d", p.File, p.Line, w.File, w.Line)
		}

		for _, n := range names {
			if !strings.Contains(p.Message, n) {
				t.Errorf("message %q does not name %q", p.Message, n)
			}
		}

		return
	}

	t.Errorf("problem at %q, want one at the tools %v", p.Pointer, want)
}
