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
				if p.Check != config.CheckPolicy || p.Code != config.CodeModelNotAllowed || p.Severity != domain.SeverityError {
					t.Errorf("problem %+v, want an error of check policy, code model-not-allowed", p)
				}

				if at := f.Locate(tc.want[i]); p.Pointer != at.Pointer || p.File != at.File || p.Line != at.Line || p.Line == 0 {
					t.Errorf("problem at %s:%d %q, want at %s:%d %q", p.File, p.Line, p.Pointer, at.File, at.Line, at.Pointer)
				}

				for _, w := range tc.message {
					if !strings.Contains(p.Message, w) {
						t.Errorf("message %q does not name %q", p.Message, w)
					}
				}
			}
		})
	}
}
