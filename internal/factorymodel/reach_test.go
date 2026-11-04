package factorymodel

import (
	"maps"
	"reflect"
	"strings"
	"testing"
)

// reachDoc is a line of two agent stations: build, whose engineer loads the lint skill and
// answers the answer contract, and review, which the secrets rule applies to.
const reachDoc = `apiVersion: goal/v1
kind: Factory
metadata: { id: 3a1f6c2e-7b4d-4e85-9c0a-5d6e7f8a9b11, name: acme-reach }
spec:
  runtime: { model: claude-sonnet-5-5, modelKey: "{{claude-key}}" }
  skills:
    - { name: lint, source: ./skills/lint }
  rules:
    - { name: secrets, source: ./rules/secrets.md }
  contracts:
    answer: ./contracts/answer.schema.json
  agents:
    - { name: engineer, prompt: ./agents/engineer.md, skills: [lint] }
    - { name: reviewer, prompt: ./agents/reviewer.md }
  board:
    stations:
      - { id: inbox, kind: queue }
      - { id: build, kind: agent, agent: engineer, output: answer }
      - { id: review, kind: agent, agent: reviewer, wip: 2 }
      - { id: done, kind: done }
`

func reachFiles(extra map[string]string) map[string]string {
	out := map[string]string{
		factoryFile:                    reachDoc,
		"agents/engineer.md":           "Build it.\n",
		"agents/reviewer.md":           "Review it.\n",
		"skills/lint/SKILL.md":         "---\nname: lint\ndescription: Lint the change.\n---\n\nRun the linter.\n",
		"rules/secrets.md":             "---\ndescription: Keep secrets out.\nappliesTo: { stations: [review] }\n---\n\nNever print a secret.\n",
		"contracts/answer.schema.json": `{"type": "object", "properties": {"summary": {"type": "string"}}}`,
	}

	maps.Copy(out, extra)

	return out
}

func TestStationsReached(t *testing.T) {
	t.Parallel()

	doc := func(old, nw string) map[string]string {
		return map[string]string{factoryFile: strings.Replace(reachDoc, old, nw, 1)}
	}

	tests := []struct {
		name string
		to   map[string]string
		want []string
	}{
		{"nothing changed", nil, nil},
		{"the agent file", map[string]string{"agents/engineer.md": "Build it with tests.\n"}, []string{"build"}},
		{"a rule that applies", map[string]string{"rules/secrets.md": "---\ndescription: Keep secrets out.\nappliesTo: { stations: [review] }\n---\n\nNever log a secret.\n"}, []string{"review"}},
		{"a rule's scope", map[string]string{"rules/secrets.md": "---\ndescription: Keep secrets out.\n---\n\nNever print a secret.\n"}, []string{"build", "review"}},
		{"a loaded skill", map[string]string{"skills/lint/SKILL.md": "---\nname: lint\ndescription: Lint the change.\n---\n\nRun the linter twice.\n"}, []string{"build"}},
		{"the contract", map[string]string{"contracts/answer.schema.json": `{"type": "object", "properties": {"summary": {"type": "string"}, "pr": {"type": "string"}}}`}, []string{"build"}},
		{"the agent's model", doc("prompt: ./agents/reviewer.md }", "prompt: ./agents/reviewer.md, model: claude-opus-5-5 }"), []string{"review"}},
		{"the runtime's model reaches every agent without one", doc("model: claude-sonnet-5-5", "model: claude-haiku-5-5"), []string{"build", "review"}},
		{"the station's agent", doc("agent: reviewer, wip: 2", "agent: engineer, wip: 2"), []string{"review"}},
		{"a WIP limit reaches no agent", doc("wip: 2", "wip: 3"), nil},
		{"another agent's file", map[string]string{"agents/reviewer.md": "Review it closely.\n"}, []string{"review"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := StationsReached(snapshot(reachFiles(nil)), snapshot(reachFiles(tc.to))); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("StationsReached = %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("a version that does not parse reaches every station", func(t *testing.T) {
		t.Parallel()

		got := StationsReached(snapshot(map[string]string{factoryFile: "spec: ["}), snapshot(reachFiles(nil)))
		if !reflect.DeepEqual(got, []string{"build", "review"}) {
			t.Errorf("StationsReached = %q, want every agent station", got)
		}
	})
}
