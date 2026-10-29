package config

import (
	"cmp"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/byFrederick/goal/internal/core/domain"
)

// baseFactory is a valid factory.yaml that each case edits. Cases mark the line a problem must
// point at with a "#here" comment.
const baseFactory = `apiVersion: goal/v1
kind: Factory
metadata:
  id: 00000000-0000-4000-8000-000000000010
  name: acme-check
spec:
  runtime:
    modelConnection: claude
  agents:
    - name: worker
      prompt: ./agents/worker.md
  board:
    stations:
      - id: inbox
        kind: queue
      - id: work
        kind: agent
        agent: worker
      - id: done
        kind: done
`

const (
	workAgentLine = "        agent: worker\n"
	doneStation   = "      - id: done\n"
	agentPrompt   = "      prompt: ./agents/worker.md\n"
	runtimeModel  = "    modelConnection: claude\n"
	specAgents    = "  agents:\n"
	routeBackend  = "/line/stations/1/route/backend"
	// answerContract is the output contract file the contract cases write.
	answerContract = "contracts/answer.schema.json"
)

// edited applies old → new replacements to baseFactory.
func edited(pairs ...string) string {
	doc := baseFactory
	for i := 0; i+1 < len(pairs); i += 2 {
		doc = strings.Replace(doc, pairs[i], pairs[i+1], 1)
	}

	return doc
}

func checkTree(doc string, extra map[string]string) fstest.MapFS {
	files := fstest.MapFS{"agents/worker.md": {Data: []byte("# worker\n")}}
	if doc != "" {
		files[FileName] = &fstest.MapFile{Data: []byte(doc)}
	}

	for p, body := range extra {
		files[p] = &fstest.MapFile{Data: []byte(body)}
	}

	return files
}

// markedLine is the 1-based line of doc that carries the #here marker, 0 when none does.
func markedLine(doc string) int {
	for i, l := range strings.Split(doc, "\n") {
		if strings.Contains(l, "#here") {
			return i + 1
		}
	}

	return 0
}

// locatedCases are the cases of TestCheck_EveryProblemIsLocated, one per problem code.
var locatedCases = []struct {
	name, doc     string
	extra         map[string]string
	code, section string
	pointer, file string
	severity      domain.ProblemSeverity
	anyLine       bool // a parser error: the line is the parser's
	wantFixes     int
}{
	{
		name: "factory.yaml does not parse", doc: edited("kind: Factory\n", "kind: [Factory\n"),
		code: "yaml-invalid", section: domain.SectionFiles, anyLine: true,
	},
	{
		name: "another YAML file does not parse", doc: baseFactory, extra: map[string]string{"evals/evals.yaml": "a: [b\n"},
		code: "yaml-invalid", section: domain.SectionFiles, file: "evals/evals.yaml", anyLine: true,
	},
	{
		name: "a JSON file does not parse", doc: baseFactory, extra: map[string]string{answerContract: "{"},
		code: "json-invalid", section: domain.SectionFiles, file: answerContract,
	},
	{name: "no factory.yaml", code: "factory-yaml-missing", section: domain.SectionFiles},
	{
		name: "missing required key", doc: edited("      - id: work\n        kind: agent\n", "      - id: work #here\n"),
		code: "schema-required", section: domain.SectionLine, pointer: "/line/stations/1/kind",
	},
	{
		name: "unknown key", doc: edited(workAgentLine, workAgentLine+"        colour: red #here\n"),
		code: "schema-unknown-key", section: domain.SectionLine, pointer: "/line/stations/1/colour",
	},
	{
		name: "value not allowed", doc: edited("        kind: agent\n", "        kind: robot #here\n", workAgentLine, ""),
		code: "schema-enum", section: domain.SectionLine, pointer: "/line/stations/1/kind", wantFixes: 1,
	},
	{
		name: "wrong type", doc: edited(workAgentLine, workAgentLine+"        agents: two #here\n"),
		code: "schema-type", section: domain.SectionLine, pointer: "/line/stations/1/agents",
	},
	{
		name: "none of the forms matches", doc: edited("      - id: work\n", "      - id: work #here\n", workAgentLine, ""),
		code: "schema-invalid", section: domain.SectionLine, pointer: "/line/stations/1",
	},
	{
		name: "outside the model", doc: edited(specAgents, "  contracts:\n    answer: 3 #here\n"+specAgents),
		code: "schema-type", section: domain.SectionFiles,
	},
	{
		name: "last station not done", doc: edited("        kind: done\n", "        kind: human #here\n"),
		code: "last-station-not-done", section: domain.SectionLine, pointer: "/line/stations/2/kind",
	},
	{
		name: "gate not upstream", doc: edited(workAgentLine, workAgentLine+"        gate: { onReject: done } #here\n"),
		code: "gate-not-upstream", section: domain.SectionLine, pointer: "/line/stations/1/gate/on_reject",
	},
	{
		name: "gate to no station", doc: edited(workAgentLine, workAgentLine+"        gate: { onReject: nowhere } #here\n"),
		code: "unknown-station", section: domain.SectionLine, pointer: "/line/stations/1/gate/on_reject",
	},
	{
		name: "buffer away from the constraint",
		doc:  edited("        kind: queue\n", "        kind: queue\n        buffer: 2 #here\n") + "    flow: { constraint: work }\n",
		code: "buffer-not-at-constraint", severity: domain.SeverityWarning, section: domain.SectionLine, pointer: "/line/stations/0/buffer",
	},
	{
		name: "integration lands at no merge station",
		doc:  baseFactory + "    flow:\n      assemblies:\n        integration: { landAt: work } #here\n",
		code: "integration-land-at-not-merge", section: domain.SectionLine, pointer: "/line/flow/assemblies/integration/land_at",
	},
	{
		name: "integration branch without a merge station",
		doc:  baseFactory + "    flow:\n      assemblies:\n        integration: { mode: branch } #here\n",
		code: "integration-without-merge-station", section: domain.SectionLine, pointer: "/line/flow/assemblies/integration/mode",
	},
	{
		name: "agents beyond wip", doc: edited(workAgentLine, workAgentLine+"        agents: 3 #here\n        wip: 2\n"),
		code: "agents-exceed-wip", section: domain.SectionLine, pointer: "/line/stations/1/agents", wantFixes: 2,
	},
	{
		name: "command check without run", doc: edited(workAgentLine, workAgentLine+"        checks: [{ kind: command }] #here\n"),
		code: "check-run-missing", section: domain.SectionLine, pointer: "/line/stations/1/checks/0",
	},
	{
		name: "duplicate station", doc: edited(doneStation, "      - { id: work, kind: queue } #here\n"+doneStation),
		code: "duplicate-name", section: domain.SectionLine, pointer: "/line/stations/2/id",
	},
	{
		name: "unknown agent", doc: edited(workAgentLine, "        agent: ghost #here\n"),
		code: codeUnknownAgent, section: domain.SectionLine, pointer: "/line/stations/1/agent",
	},
	{
		name: "unknown routed agent", doc: edited(workAgentLine, "        route: { backend: ghost } #here\n"),
		code: codeUnknownAgent, section: domain.SectionLine, pointer: routeBackend,
	},
	{
		name: "unknown routed agent with a count", doc: edited(workAgentLine, "        route: { backend: { agent: ghost, agents: 1 } } #here\n"),
		code: codeUnknownAgent, section: domain.SectionLine, pointer: routeBackend,
	},
	{
		name: "route counts beyond the station's agents",
		doc:  edited(workAgentLine, "        route: { backend: { agent: worker, agents: 2 }, frontend: worker } #here\n"),
		code: "route-agents-exceed-station", section: domain.SectionLine, pointer: "/line/stations/1/route",
	},
	{
		name: "route count of zero", doc: edited(workAgentLine, "        route: { backend: { agent: worker, agents: 0 } } #here\n"),
		code: "schema-invalid", section: domain.SectionLine, pointer: routeBackend,
	},
	{
		name: "route entry with an unknown key", doc: edited(workAgentLine, "        route: { backend: { agent: worker, colour: red } } #here\n"),
		code: "schema-invalid", section: domain.SectionLine, pointer: routeBackend,
	},
	{
		name: "unknown skill", doc: edited(agentPrompt, agentPrompt+"      skills: [nope] #here\n"),
		code: "unknown-skill", section: domain.SectionAgents, pointer: "/agents/agents/0/skills/0",
	},
	{
		name: "unknown connection", doc: edited(agentPrompt, agentPrompt+"      connections: [nope] #here\n"),
		code: "unknown-connection", section: domain.SectionAgents, pointer: "/agents/agents/0/connections/0",
	},
	{
		name: "unknown contract", doc: edited(workAgentLine, workAgentLine+"        output: answer #here\n"),
		code: "unknown-contract", section: domain.SectionLine, pointer: "/line/stations/1/output",
	},
	{
		name: "unknown collection", doc: baseFactory + "  learning:\n    librarian: { agent: worker, writes: [ghosts] } #here\n",
		code: "unknown-collection", section: domain.SectionFiles,
	},
	{
		name: "unknown librarian", doc: baseFactory + "  learning:\n    librarian: { agent: ghost } #here\n",
		code: codeUnknownAgent, section: domain.SectionLine, pointer: "/line/librarian",
	},
	{
		name: "prompt not found", doc: edited(agentPrompt, "      prompt: ./agents/missing.md #here\n"),
		code: "prompt-not-found", section: domain.SectionAgents, pointer: "/agents/agents/0/prompt",
	},
	{
		name: "skill folder not found", doc: edited(specAgents, "  skills:\n    - { name: local, source: ./skills/local } #here\n"+specAgents),
		code: "skill-not-found", section: domain.SectionAgents, pointer: "/agents/skills/0/source",
	},
	{
		name: "contract not found", doc: edited(specAgents, "  contracts:\n    answer: ./contracts/answer.json #here\n"+specAgents),
		code: "contract-not-found", section: domain.SectionFiles,
	},
	{
		name: "contract not a tool schema",
		doc: edited(specAgents, "  contracts:\n    answer: ./contracts/answer.schema.json #here\n"+specAgents,
			workAgentLine, workAgentLine+"        output: answer\n"),
		extra: map[string]string{answerContract: `{"oneOf": [{"type": "object"}]}`},
		code:  codeContractNotToolSchema, section: domain.SectionFiles,
	},
	{
		name: "built-in ask", doc: edited(agentPrompt, agentPrompt+"      builtins: { read: ask } #here\n"),
		code: "builtin-ask-unavailable", severity: domain.SeverityWarning, section: domain.SectionAgents, pointer: "/agents/agents/0/builtins/read",
	},
	{
		name: "removed runtime key", doc: edited(runtimeModel, runtimeModel+"    image: ghcr.io/acme/agent:1.0.0 #here\n"),
		code: codeFieldRemoved, severity: domain.SeverityError, section: domain.SectionBasics, pointer: "/basics/image", wantFixes: 1,
	},
	{
		name: "removed runner", doc: edited(runtimeModel, runtimeModel+"    runner: docker #here\n"),
		code: codeFieldRemoved, severity: domain.SeverityError, section: domain.SectionBasics, pointer: "/basics/runner", wantFixes: 1,
	},
	{
		name: "removed model auth", doc: edited(runtimeModel, runtimeModel+"    auth: claude-subscription #here\n"),
		code: codeFieldRemoved, severity: domain.SeverityError, section: domain.SectionBasics, pointer: "/basics/auth", wantFixes: 1,
	},
	{
		name: "removed secrets provider", doc: edited(specAgents, "  secrets: { provider: env } #here\n"+specAgents),
		code: codeFieldRemoved, severity: domain.SeverityError, section: domain.SectionBasics, pointer: "/basics/secrets", wantFixes: 1,
	},
	{
		name: "removed knowledge base", doc: edited(specAgents, "  knowledgeBase: { collections: [notes] } #here\n"+specAgents),
		code: codeFieldRemoved, severity: domain.SeverityError, section: domain.SectionBasics, pointer: "/basics/knowledge_base", wantFixes: 1,
	},
	{
		name: "repository token", doc: edited(runtimeModel, runtimeModel+"    repository: { url: \"https://github.com/acme/app\", token: env:ACME_TOKEN } #here\n"),
		code: codeEnvReferenceRemoved, severity: domain.SeverityError, section: domain.SectionBasics, pointer: "/basics/repository/token", wantFixes: 1,
	},
	{
		name: "inline connection", doc: edited(specAgents, "  connections:\n    - { name: tickets, type: mcp, transport: http, url: \"https://tickets.example.com/mcp\" } #here\n"+specAgents),
		code: codeInlineRemoved, severity: domain.SeverityError, section: domain.SectionConnections, pointer: "/connections/0", wantFixes: 1,
	},
	{
		name: "connector form", doc: edited(specAgents, "  connections:\n    - { connector: playwright, name: browser } #here\n"+specAgents),
		code: codeInlineRemoved, severity: domain.SeverityError, section: domain.SectionConnections, pointer: "/connections/0", wantFixes: 1,
	},
	{
		name: "tool spec", doc: edited(runtimeModel, runtimeModel+"    tools: [\"go@1.26.10\", \"go 1.26; rm -rf /\"] #here\n"),
		code: codeToolSpecInvalid, section: domain.SectionBasics, pointer: "/basics/tools/1", wantFixes: 1,
	},
	{
		name: "trigger cron", doc: withTriggers(`        cron: "0 6 * * MON-FRI"`, `        cron: "0 25 * * *" #here`),
		code: codeCronInvalid, section: domain.SectionFiles,
	},
	{
		name: "trigger time zone", doc: withTriggers("        timeZone: Europe/Berlin\n", "        timeZone: Local #here\n"),
		code: codeTimeZoneInvalid, section: domain.SectionFiles,
	},
	{
		name: "trigger placeholder", doc: withTriggers(`          title: "Sweep {{ repo }}"`, `          title: "Sweep {{}}" #here`),
		code: codePlaceholderInvalid, section: domain.SectionFiles,
	},
	{
		name: "starter work type", doc: withStarter("    type: docs\n", "    type: chore #here\n"),
		code: codeUnknownWorkType, section: domain.SectionFiles,
	},
}

// Every problem config reports names its code and severity and is located for both editors:
// the Visual section and model pointer, and the file and line of factory.yaml.
func TestCheck_EveryProblemIsLocated(t *testing.T) {
	t.Parallel()

	for _, tt := range locatedCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, problems := Check(checkTree(tt.doc, tt.extra))

			i := indexOf(problems, tt.code)
			if i < 0 {
				t.Fatalf("no %s problem in %+v", tt.code, problems)
			}

			want := problems[i]
			want.Severity, want.Section, want.Pointer, want.File = cmp.Or(tt.severity, domain.SeverityError), tt.section, tt.pointer, cmp.Or(tt.file, FileName)

			if !tt.anyLine {
				want.Line = markedLine(tt.doc)
			}

			if p := problems[i]; !reflect.DeepEqual(p, want) || p.Check == "" || p.Line == 0 && tt.anyLine || len(p.Fixes) != tt.wantFixes {
				t.Errorf("problem =\n%+v\nwant\n%+v, with a check and %d fixes", p, want, tt.wantFixes)
			}
		})
	}
}

func indexOf(problems []domain.DefinitionProblem, code string) int {
	for i, p := range problems {
		if p.Code == code {
			return i
		}
	}

	return -1
}

// A fix is a patch on the model section, so the Visual editor can apply it as it is.
func TestCheck_FixesPatchTheirSection(t *testing.T) {
	t.Parallel()

	_, problems := Check(checkTree(edited(workAgentLine, workAgentLine+"        agents: 3\n        wip: 2\n"), nil))

	p := problems[indexOf(problems, "agents-exceed-wip")]
	want := []domain.ProblemFix{
		{Label: "Set WIP to 3", Section: domain.SectionLine, Patch: []domain.PatchOp{{Op: "replace", Path: "/stations/1/wip", Value: json.RawMessage("3")}}},
		{Label: "Set agents to 2", Section: domain.SectionLine, Patch: []domain.PatchOp{{Op: "replace", Path: "/stations/1/agents", Value: json.RawMessage("2")}}},
	}

	if !reflect.DeepEqual(p.Fixes, want) {
		t.Errorf("fixes =\n%+v\nwant\n%+v", p.Fixes, want)
	}
}

// Each removed form's fix takes it out of the definition, or puts an organization connection of
// the same name in an inline connection's place, so the agents that name it keep it.
func TestCheck_RemovedFormsAreFixed(t *testing.T) {
	t.Parallel()

	remove := func(label, path string) domain.ProblemFix {
		return domain.ProblemFix{Label: label, Section: domain.SectionBasics, Patch: []domain.PatchOp{{Op: "remove", Path: path}}}
	}

	tests := []struct {
		name, doc, code string
		want            domain.ProblemFix
	}{
		{
			name: "runtime image", doc: edited(runtimeModel, runtimeModel+"    image: ghcr.io/acme/agent:1.0.0\n"), code: codeFieldRemoved,
			want: remove("Remove runtime.image", "/image"),
		},
		{
			name: "knowledge base", doc: edited(specAgents, "  knowledgeBase: { collections: [notes] }\n"+specAgents), code: codeFieldRemoved,
			want: remove("Remove knowledgeBase", "/knowledge_base"),
		},
		{
			name: "repository token", code: codeEnvReferenceRemoved,
			doc:  edited(runtimeModel, runtimeModel+"    repository: { url: \"https://github.com/acme/app\", token: env:ACME_TOKEN }\n"),
			want: remove("Remove the token", "/repository/token"),
		},
		{
			name: "tool spec", doc: edited(runtimeModel, runtimeModel+"    tools: [\"go 1.26\"]\n"), code: codeToolSpecInvalid,
			want: remove("Remove go 1.26", "/tools/0"),
		},
		{
			name: "inline connection", code: codeInlineRemoved,
			doc: edited(specAgents, "  connections:\n    - { name: tickets, type: mcp, transport: http, url: \"https://tickets.example.com/mcp\" }\n"+
				specAgents, agentPrompt, agentPrompt+"      connections: [tickets]\n"),
			want: domain.ProblemFix{
				Label: "Use the organization's connection tickets", Section: domain.SectionConnections,
				Patch: []domain.PatchOp{{Op: "replace", Path: "/0", Value: json.RawMessage(`{"name":"tickets"}`)}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, problems := Check(checkTree(tt.doc, nil))

			i := indexOf(problems, tt.code)
			if i < 0 {
				t.Fatalf("no %s problem in %+v", tt.code, problems)
			}

			if got := problems[i].Fixes; !reflect.DeepEqual(got, []domain.ProblemFix{tt.want}) {
				t.Errorf("fixes =\n%+v\nwant\n%+v", got, tt.want)
			}
		})
	}
}

// A definition in the new form names organization connections, which agents list like any
// connection; it has no removed form to warn about.
func TestCheck_OrganizationConnectionsAreNames(t *testing.T) {
	t.Parallel()

	f, problems := Check(checkTree(edited(
		runtimeModel, runtimeModel+"    size: large\n    tools: [\"go@1.26.10\", \"npm:@acme/cli@2\", \"aqua:cli/cli\", task]\n"+
			"    repository: { url: \"https://github.com/acme/app\", connection: github }\n",
		specAgents, "  connections: [github, datadog]\n  knowledge:\n    sources: [handbook]\n"+specAgents,
		agentPrompt, agentPrompt+"      connections: [datadog]\n",
	), nil))
	if len(problems) > 0 {
		t.Fatalf("problems = %+v, want none", problems)
	}

	if names := []string{f.Spec.Connections[0].Name, f.Spec.Connections[1].Name}; !reflect.DeepEqual(names, []string{"github", "datadog"}) ||
		f.Spec.Connections[0].Inline() {
		t.Errorf("connections = %+v, want the organization's github and datadog", f.Spec.Connections)
	}
}

// specContract is shaped like the software factory's spec contract: a top-level object whose
// properties nest arrays of objects with enums and patterns, and no top-level combinator.
const specContract = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "required": ["title", "lane", "acceptance"],
  "additionalProperties": false,
  "properties": {
    "title": { "type": "string", "minLength": 10 },
    "lane": { "type": "string", "enum": ["backend", "frontend", "infra"] },
    "acceptance": { "type": "array", "minItems": 1, "items": { "type": "string" } },
    "touches": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["path"],
        "properties": {
          "path": { "type": "string", "pattern": "^[a-z]" },
          "kind": { "type": "string", "enum": ["add", "change", "remove"] }
        }
      }
    }
  }
}`

// The output contract becomes the model's tool input_schema, which takes only a plain object at
// the top level: a contract that is not one is refused before it can fail every run.
func TestCheck_OutputContractsAreToolSchemas(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, contract string
		keyword        string // the keyword the problem names; "" for no problem
	}{
		{name: "plain object", contract: `{"type": "object", "properties": {"a": {"type": "string"}}}`},
		{
			name:     "object with $schema, $defs and required",
			contract: `{"$schema": "https://json-schema.org/draft/2020-12/schema", "type": "object", "$defs": {"a": {"type": "string"}}, "required": ["a"], "properties": {"a": {"$ref": "#/$defs/a"}}}`,
		},
		{
			name:     "combinators nested under properties and items",
			contract: `{"type": "object", "properties": {"a": {"oneOf": [{"type": "string"}, {"type": "integer"}]}, "b": {"type": "array", "items": {"anyOf": [{"type": "string"}, {"type": "null"}]}}}}`,
		},
		{name: "the software factory's spec contract", contract: specContract},
		{name: "top-level oneOf", contract: `{"type": "object", "oneOf": [{"required": ["a"]}, {"required": ["b"]}]}`, keyword: "oneOf"},
		{name: "top-level anyOf", contract: `{"anyOf": [{"type": "object"}, {"type": "object"}]}`, keyword: "anyOf"},
		{name: "top-level allOf", contract: `{"type": "object", "allOf": [{"required": ["a"]}]}`, keyword: "allOf"},
		{name: "top-level not", contract: `{"type": "object", "not": {"required": ["a"]}}`},
		{
			name:     "top-level if/then, as the software factory's review contract",
			contract: `{"type": "object", "required": ["decision"], "properties": {"decision": {"type": "string", "enum": ["approve", "reject"]}, "reason": {"type": "string"}}, "if": {"properties": {"decision": {"const": "reject"}}}, "then": {"required": ["decision", "reason"]}}`,
		},
		{
			name:     "top-level if/then/else",
			contract: `{"type": "object", "if": {"required": ["a"]}, "then": {"required": ["b"]}, "else": {"required": ["c"]}}`,
		},
		{name: "type array", contract: `{"type": "array", "items": {"type": "string"}}`, keyword: "type"},
		{name: "type missing", contract: `{"properties": {"a": {"type": "string"}}}`, keyword: "type"},
	}

	doc := edited(specAgents, "  contracts:\n    answer: ./contracts/answer.schema.json\n"+specAgents,
		workAgentLine, workAgentLine+"        output: answer\n")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, problems := Check(checkTree(doc, map[string]string{answerContract: tt.contract}))

			if tt.keyword == "" {
				if len(problems) > 0 {
					t.Fatalf("problems = %+v, want none", problems)
				}

				return
			}

			if len(problems) != 1 || problems[0].Code != codeContractNotToolSchema || problems[0].Severity != domain.SeverityError {
				t.Fatalf("problems = %+v, want one %s error", problems, codeContractNotToolSchema)
			}

			msg := problems[0].Message
			for _, part := range []string{"contract answer (./contracts/answer.schema.json)", tt.keyword, "in Go"} {
				if !strings.Contains(msg, part) {
					t.Errorf("message %q does not name %q", msg, part)
				}
			}
		})
	}
}

// Only output contracts become a tool schema, each checked once; a missing or unparsable one is
// reported by its own check.
func TestCheck_OutputContractChecksOnlyWhatRuns(t *testing.T) {
	t.Parallel()

	oneOf := `{"oneOf": [{"type": "object"}]}`
	twice := edited(specAgents, "  contracts:\n    answer: ./contracts/answer.schema.json\n    spare: ./contracts/spare.schema.json\n"+specAgents,
		workAgentLine, workAgentLine+"        output: answer\n",
		doneStation, "      - id: again\n        kind: agent\n        agent: worker\n        output: answer\n"+doneStation)

	tests := []struct {
		name  string
		extra map[string]string
		want  []string
	}{
		{
			name:  "two stations, one contract; an unused one is not checked",
			extra: map[string]string{answerContract: oneOf, "contracts/spare.schema.json": oneOf},
			want:  []string{codeContractNotToolSchema},
		},
		{
			name:  "does not parse",
			extra: map[string]string{answerContract: "{", "contracts/spare.schema.json": oneOf},
			want:  []string{codeJSONInvalid},
		},
		{name: "missing", extra: map[string]string{"contracts/spare.schema.json": oneOf}, want: []string{codeContractNotFound}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, problems := Check(checkTree(twice, tt.extra))

			got := names(problems, func(p domain.DefinitionProblem) string { return p.Code })
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("codes = %v, want %v (%+v)", got, tt.want, problems)
			}
		})
	}
}

// Warnings do not stop a factory from loading; errors do, each with its place and code.
func TestLoad_FailsOnErrorsOnly(t *testing.T) {
	t.Parallel()

	f, err := Load(checkTree(edited(agentPrompt, agentPrompt+"      builtins: { read: ask }\n"), nil))
	if err != nil {
		t.Fatalf("a warning failed the load: %v", err)
	}

	if w := f.Warnings(); len(w) != 1 || !strings.Contains(w[0], "read is ask") {
		t.Errorf("warnings = %q, want the ask warning", w)
	}

	_, err = Load(checkTree(edited(workAgentLine, "        agent: ghost\n"), nil))
	if err == nil || !strings.Contains(err.Error(), `factory.yaml:18:16: station work: unknown agent "ghost" (unknown-agent)`) {
		t.Errorf("err = %v, want the located unknown agent", err)
	}
}

// A problem another package finds in a definition (the credentials check) is located the same way.
func TestLocate_PlacesAPathOrItsNearestParent(t *testing.T) {
	t.Parallel()

	f, err := Load(checkTree(baseFactory, nil))
	if err != nil {
		t.Fatal(err)
	}

	if l := f.Locate("/spec/board/stations/1/agent"); l.Line != 18 || l.Column != 16 || l.EndColumn != 22 || l.Pointer != "/line/stations/1/agent" {
		t.Errorf("existing path = %+v, want line 18, columns 16-22", l)
	}

	if l := f.Locate("/spec/runtime/repository/token"); l.Line != 7 || l.Section != domain.SectionBasics || l.Pointer != "/basics/repository/token" {
		t.Errorf("missing path = %+v, want the runtime key on line 7", l)
	}
}

// Check takes any text a person types in the YAML editor: it never panics, and every problem
// it reports has a code, a check and a severity.
func FuzzCheck(f *testing.F) {
	for _, c := range locatedCases {
		f.Add(c.doc)
	}

	f.Fuzz(func(t *testing.T, doc string) {
		_, problems := Check(checkTree(doc, nil))
		for _, p := range problems {
			if p.Code == "" || p.Check == "" || p.Severity == "" {
				t.Fatalf("incomplete problem %+v", p)
			}
		}
	})
}

// mergeLine is baseFactory with a gated agent station whose approval merges, between work and done.
var mergeLine = edited(doneStation, "      - id: land\n        kind: agent\n        agent: worker\n"+
	"        gate: { onReject: work }\n        action: merge\n"+doneStation)

// flow.assemblies.integration.landAt names an agent station with a gate and action merge, and mode
// branch needs a merge station on the line; each problem names its code.
func TestCheck_AssemblyIntegration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, doc, integration, code string
	}{
		{name: "landAt names no station", doc: mergeLine, integration: "{ landAt: nowhere }", code: codeUnknownStation},
		{name: "landAt at an agent station without a gate", doc: mergeLine, integration: "{ landAt: work }", code: codeIntegrationLandAtNotMerge},
		{name: "landAt at a queue", doc: mergeLine, integration: "{ landAt: inbox }", code: codeIntegrationLandAtNotMerge},
		{
			name: "landAt at a gate that does not merge", integration: "{ landAt: land }",
			doc: strings.Replace(mergeLine, "        action: merge\n", "", 1), code: codeIntegrationLandAtNotMerge,
		},
		{
			name: "landAt at a human merge station", integration: "{ landAt: land }",
			doc: edited(doneStation, "      - { id: land, kind: human, action: merge }\n"+doneStation), code: codeIntegrationLandAtNotMerge,
		},
		{name: "branch without a merge station", doc: baseFactory, integration: "{ mode: branch }", code: codeIntegrationWithoutMerge},
		{name: "branch with a merge station", doc: mergeLine, integration: "{ mode: branch, landAt: land }"},
		{name: "trunk without a merge station", doc: baseFactory, integration: "{ mode: trunk, minParts: 2 }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, problems := Check(checkTree(tt.doc+"    flow:\n      assemblies:\n        integration: "+tt.integration+"\n", nil))

			if tt.code == "" {
				if len(problems) > 0 {
					t.Errorf("problems %+v, want none", problems)
				}

				return
			}

			if indexOf(problems, tt.code) < 0 {
				t.Errorf("problems %+v, want %s", problems, tt.code)
			}
		})
	}
}
