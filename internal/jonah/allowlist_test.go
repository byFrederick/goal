package jonah_test

import (
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/jonah"
)

// allowFactory is the factory.yaml TestFactoryChangeAllowed changes.
const allowFactory = `apiVersion: goal/v1
kind: Factory
metadata: { id: 00000000-0000-4000-8000-0000000000f1, name: acme }
spec:
  runtime: { model: claude-sonnet-5-5, maxUsdPerItem: 5 }
  skills:
    - { name: deliver, source: ./skills/deliver }
    - { name: remote, source: "skills.sh:acme/skills@remote" }
  rules:
    - { name: tests, source: ./rules/tests.md }
  contracts: { change: ./contracts/change.json }
  agents:
    - { name: writer, prompt: ./agents/writer.md, skills: [deliver] }
    - { name: reviewer, prompt: ./agents/reviewer.md, tools: { allow: [Read] } }
  board:
    stations:
      - { id: inbox, kind: queue }
      - { id: build, kind: agent, agent: writer, wip: 2, buffer: 1, gate: { onReject: build, maxRework: 2 } }
      - { id: review, kind: agent, agent: reviewer, checks: [task check] }
      - { id: done, kind: done }
  learning: { jonah: { enabled: true } }
`

// A proposal may change factory.yaml only at the allowlist's paths (ADR 0039 decision 6), compared
// parsed, with agents and skills matched by name and stations by id.
func TestFactoryChangeAllowed(t *testing.T) {
	t.Parallel()

	models := []string{"claude-sonnet-5-5", "claude-opus-5-5"}

	for name, tc := range map[string]struct {
		old, new string
		allowed  bool
	}{
		"nothing":                    {"", "", true},
		"comments and layout":        {"apiVersion: goal/v1\n", "apiVersion: goal/v1 # the API\n", true},
		"an allowed agent model":     {"name: writer, prompt", "name: writer, model: claude-opus-5-5, prompt", true},
		"a model the policy refuses": {"name: writer, prompt", "name: writer, model: claude-haiku-5-5, prompt", false},
		"an agent's skills":          {"skills: [deliver] }", "skills: [deliver, review] }", true},
		"a local skill added":        {"    - { name: deliver, source: ./skills/deliver }\n", "    - { name: deliver, source: ./skills/deliver }\n    - { name: review, source: ./skills/review }\n", true},
		"a skills.sh skill added":    {"    - { name: deliver, source: ./skills/deliver }\n", "    - { name: deliver, source: ./skills/deliver }\n    - { name: x, source: \"skills.sh:acme/skills@x\" }\n", false},
		"a skills.sh skill removed":  {"    - { name: remote, source: \"skills.sh:acme/skills@remote\" }\n", "", false},
		"a local skill moved local":  {"source: ./skills/deliver", "source: ./skills/deliver-v2", true},
		"a rule added":               {"    - { name: tests, source: ./rules/tests.md }\n", "    - { name: tests, source: ./rules/tests.md }\n    - { name: ui, source: ./rules/ui.md }\n", true},
		"a contract changed":         {"change: ./contracts/change.json", "change: ./contracts/change-v2.json", true},
		"a station's wip":            {"wip: 2", "wip: 3", true},
		"a station's buffer":         {"buffer: 1", "buffer: 2", true},
		"a gate's maxRework":         {"maxRework: 2", "maxRework: 3", true},
		"a gate's onReject":          {"onReject: build", "onReject: inbox", false},
		"a station's checks":         {"checks: [task check]", "checks: [task check, rm -rf /]", false},
		"a station's kind":           {"{ id: inbox, kind: queue }", "{ id: inbox, kind: done }", false},
		"a station's agent":          {"agent: reviewer, checks", "agent: writer, checks", false},
		"a gate added":               {"agent: reviewer, checks", "agent: reviewer, gate: { onReject: build }, checks", false},
		"an agent's tools":           {"allow: [Read]", "allow: [Read, Bash]", false},
		"an agent's prompt":          {"prompt: ./agents/writer.md", "prompt: ./agents/other.md", false},
		"an agent added":             {"  board:\n", "    - { name: x, prompt: ./agents/x.md }\n  board:\n", false},
		"the runtime budget":         {"maxUsdPerItem: 5", "maxUsdPerItem: 50", false},
		"learning":                   {"enabled: true", "enabled: false", false},
		"agents reordered": {
			"    - { name: writer, prompt: ./agents/writer.md, skills: [deliver] }\n    - { name: reviewer, prompt: ./agents/reviewer.md, tools: { allow: [Read] } }\n",
			"    - { name: reviewer, prompt: ./agents/reviewer.md, tools: { allow: [Read] } }\n    - { name: writer, prompt: ./agents/writer.md, skills: [deliver] }\n", false,
		},
		"stations reordered": {
			"      - { id: inbox, kind: queue }\n      - { id: build,",
			"      - { id: build,", false,
		},
	} {
		after := allowFactory
		if tc.old != "" {
			if !strings.Contains(after, tc.old) {
				t.Fatalf("%s: %q is not in the factory", name, tc.old)
			}

			after = strings.Replace(after, tc.old, tc.new, 1)
		}

		if name == "stations reordered" {
			after = strings.Replace(after, "      - { id: done, kind: done }\n", "      - { id: done, kind: done }\n      - { id: inbox, kind: queue }\n", 1)
		}

		before, err := config.ProposalCheck{}.ParseFactory([]byte(allowFactory))
		if err != nil {
			t.Fatal(err)
		}

		got, err := config.ProposalCheck{}.ParseFactory([]byte(after))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		if path, ok := jonah.FactoryChangeAllowed(before, got, models); ok != tc.allowed {
			t.Errorf("%s: FactoryChangeAllowed = %q, %v; want allowed %v", name, path, ok, tc.allowed)
		}
	}
}
