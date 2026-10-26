package config

import (
	"cmp"
	"reflect"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
)

func TestLoad_ParsesRules(t *testing.T) {
	t.Parallel()

	f, err := LoadDir("testdata/line")
	if err != nil {
		t.Fatal(err)
	}

	want := []Rule{
		{
			Name: "secrets", Source: "./rules/secrets.md",
			Description: "Never write a credential where it can be read back.",
			Body:        "Credentials reach a run only as environment variables. Never print them, commit them or put\nthem in an output.\n",
		},
		{
			Name: "go-errors", Source: "./rules/go-errors.md",
			Description: "Wrap every returned error with what was being done.",
			AppliesTo: domain.RuleScope{
				Agents: []string{"engineer"}, Stations: []string{"build"}, Lanes: []string{"backend"},
				WorkTypes: []string{"feature", "incident"},
			},
			Paths: []string{"**/*.go"},
			Body:  "Wrap with %w and say what failed: `fmt.Errorf(\"read config: %w\", err)`.\n",
		},
	}

	if !reflect.DeepEqual(f.Spec.Rules, want) {
		t.Errorf("rules =\n%+v\nwant\n%+v", f.Spec.Rules, want)
	}
}

// ruleEntry declares one rule, secrets, in baseFactory.
const ruleEntry = "  rules:\n    - { name: secrets, source: ./rules/secrets.md } #here\n"

// withRule is baseFactory with the secrets rule declared by entry, a station routing lanes
// backend and frontend, and work types feature and fix.
func withRule(entry string) string {
	return edited(specAgents, entry+specAgents,
		workAgentLine, "        route: { backend: worker, frontend: worker }\n",
		"  board:\n", "  board:\n    workTypes: [feature, fix]\n")
}

// ruleFile is a rule file with the given frontmatter.
func ruleFile(front string) map[string]string {
	return map[string]string{"rules/secrets.md": "---\n" + front + "---\n\nKeep secrets out of outputs.\n"}
}

// Every problem with a rule is blocking, named after the rule and the value at fault, and located
// at the rule's entry in factory.yaml and under the Agents section's rules.
func TestRules_ProblemsNameTheRule(t *testing.T) {
	t.Parallel()

	const desc = "description: Keep secrets out.\n"

	tests := []struct {
		name    string
		entry   string
		files   map[string]string
		code    string
		pointer string
		message string
	}{
		{
			name: "unknown agent", files: ruleFile(desc + "appliesTo: { agents: [worker, ghost] }\n"),
			code: codeUnknownAgent, message: `rule secrets: appliesTo.agents: unknown agent "ghost"`,
		},
		{
			name: "unknown station", files: ruleFile(desc + "appliesTo: { stations: [nowhere] }\n"),
			code: codeUnknownStation, message: `rule secrets: appliesTo.stations: unknown station "nowhere"`,
		},
		{
			name: "lane no route declares", files: ruleFile(desc + "appliesTo: { lanes: [backend, mobile] }\n"),
			code: codeUnknownLane, message: `rule secrets: appliesTo.lanes: unknown lane "mobile"`,
		},
		{
			name: "work type the board does not declare", files: ruleFile(desc + "appliesTo: { workTypes: [chore] }\n"),
			code: codeUnknownWorkType, message: `rule secrets: appliesTo.workTypes: unknown work type "chore"`,
		},
		{
			name: "source file missing", code: codeRuleNotFound, message: "rule secrets: ./rules/secrets.md not found",
		},
		{
			name:  "source outside rules",
			entry: "  rules:\n    - { name: secrets, source: ./rules/../agents/worker.md } #here\n",
			code:  codeRuleInvalid, message: "rule secrets: ./rules/../agents/worker.md is not a markdown file in rules/",
		},
		{
			name:  "source not markdown",
			entry: "  rules:\n    - { name: secrets, source: ./rules/secrets.txt } #here\n",
			code:  codeSchemaInvalid, pointer: "/agents/rules/0/source", message: "spec.rules[0].source",
		},
		{
			name: "no frontmatter", files: map[string]string{"rules/secrets.md": "Keep secrets out.\n"},
			code: codeRuleInvalid, message: "rule secrets: ./rules/secrets.md has no frontmatter",
		},
		{
			name: "frontmatter does not parse", files: ruleFile("description: [Keep\n"),
			code: codeRuleInvalid, message: "rule secrets: frontmatter does not parse",
		},
		{
			name: "misspelt frontmatter key", files: ruleFile(desc + "applies_to: { agents: [worker] }\n"),
			code: codeRuleInvalid, message: "rule secrets: frontmatter does not parse",
		},
		{
			name: "description missing", files: ruleFile("paths: [\"*.go\"]\n"),
			code: codeRuleInvalid, message: "rule secrets: description is required",
		},
		{
			name: "description of several lines", files: ruleFile("description: |\n  one\n  two\n"),
			code: codeRuleInvalid, message: "rule secrets: description must be one line",
		},
		{
			name: "empty appliesTo list", files: ruleFile(desc + "appliesTo: { agents: [] }\n"),
			code: codeRuleInvalid, message: "rule secrets: appliesTo.agents is empty",
		},
		{
			name: "empty path", files: ruleFile(desc + "paths: [\"\"]\n"),
			code: codeRuleInvalid, message: "rule secrets: paths has an empty glob",
		},
		{
			name:  "duplicate name",
			entry: ruleEntry + "    - { name: secrets, source: ./rules/secrets.md }\n",
			files: ruleFile(desc), code: codeDuplicateName, pointer: "/agents/rules/1/name",
			message: "rule secrets: the name is used by another rule",
		},
		{
			name:  "unknown key in an entry",
			entry: "  rules:\n    - { name: secrets, source: ./rules/secrets.md, scope: all } #here\n",
			files: ruleFile(desc), code: codeSchemaUnknownKey, pointer: "/agents/rules/0/scope", message: "scope",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			doc := withRule(cmp.Or(tt.entry, ruleEntry))
			_, problems := Check(checkTree(doc, tt.files))

			i := indexOf(problems, tt.code)
			if i < 0 {
				t.Fatalf("no %s problem in %+v", tt.code, problems)
			}

			p := problems[i]
			if !p.Blocking() || p.Section != domain.SectionAgents || p.Pointer != cmp.Or(tt.pointer, "/agents/rules/0/source") ||
				!strings.Contains(p.Message, tt.message) {
				t.Errorf("problem = %+v; want a blocking agents problem at %s saying %q", p, cmp.Or(tt.pointer, "/agents/rules/0/source"), tt.message)
			}

			if tt.code != codeDuplicateName && p.Line != markedLine(doc) {
				t.Errorf("problem at line %d; want the rule's entry, line %d", p.Line, markedLine(doc))
			}
		})
	}
}

// A rule whose names all exist has no problem.
func TestRules_ValidRuleHasNoProblem(t *testing.T) {
	t.Parallel()

	front := "description: Keep secrets out.\nappliesTo: { agents: [worker], stations: [work], lanes: [frontend], workTypes: [fix] }\npaths: [\"**/*.go\"]\n"

	if _, problems := Check(checkTree(withRule(ruleEntry), ruleFile(front))); len(problems) != 0 {
		t.Errorf("problems = %+v", problems)
	}
}

func TestModelPointer_Rules(t *testing.T) {
	t.Parallel()

	for path, want := range map[string]string{
		"spec.rules.0.source": "/agents/rules/0/source",
		"spec.rules.2.name":   "/agents/rules/2/name",
		"spec.rules":          "/agents/rules",
	} {
		if section, pointer := ModelPointer(strings.Split(path, ".")); section != domain.SectionAgents || pointer != want {
			t.Errorf("ModelPointer(%s) = %s %s; want agents %s", path, section, pointer, want)
		}
	}
}
