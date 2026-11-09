package main

import (
	"errors"
	"strings"
	"testing"
)

// renderFixture renders a definition page from a schema whose one top-level field, spec, holds
// the given properties and, after them, the given schema keys (allOf, oneOf, $defs entries…).
func renderFixture(t *testing.T, props, extra string) string {
	t.Helper()

	schema := `{"properties": {"spec": {"type": "object", "properties": {` + props + `}` + extra + `}}}`

	page, err := renderDefinitionPage([]byte(schema))
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	return page
}

func assertLines(t *testing.T, page string, want ...string) {
	t.Helper()

	for _, line := range want {
		if !strings.Contains(page, line+"\n") {
			t.Errorf("page lacks the line %q:\n%s", line, page)
		}
	}
}

func TestDefinitionPage_RendersBounds(t *testing.T) {
	t.Parallel()

	page := renderFixture(t, `
		"agents": {"type": "integer", "minimum": 1, "maximum": 20},
		"wip": {"type": "integer", "minimum": 1, "default": 2},
		"rate": {"type": "number", "maximum": 1},
		"spend": {"type": "number", "exclusiveMinimum": 0},
		"name": {"type": "string", "maxLength": 40},
		"title": {"type": "string", "minLength": 1, "maxLength": 200},
		"brief": {"type": "string", "minLength": 1},
		"code": {"type": "string", "minLength": 3, "maxLength": 3},
		"stations": {"type": "array", "minItems": 2, "items": {"type": "string"}},
		"labels": {"type": "array", "minItems": 1, "maxItems": 20, "items": {"type": "string"}},
		"tools": {"type": "array", "maxItems": 50, "items": {"type": "string", "maxLength": 200}}`, "")

	assertLines(t, page,
		"- `agents`: integer, 1 to 20.",
		"- `wip`: integer, at least 1, default `2`.",
		"- `rate`: number, at most 1.",
		"- `spend`: number, more than 0.",
		"- `name`: string, at most 40 characters.",
		"- `title`: string, 1 to 200 characters.",
		"- `brief`: string, at least 1 character.",
		"- `code`: string, 3 characters.",
		"- `stations`: array of string, at least 2 items.",
		"- `labels`: array of string, 1 to 20 items.",
		"- `tools`: array of string, at most 50 items.",
	)
}

func TestDefinitionPage_RendersStringShorthand(t *testing.T) {
	t.Parallel()

	page := renderFixture(t, `
		"checks": {
			"type": "array",
			"description": "Checks the output must pass.",
			"items": {
				"description": "A check, or its shorthand: a command line.",
				"if": {"type": "string"},
				"then": {"minLength": 1},
				"else": {"type": "object", "properties": {"run": {"type": "string"}}}
			}
		}`, "")

	assertLines(t, page,
		"- `checks`: array of string or object. Checks the output must pass. A check, or its shorthand: a command line.",
		"  - `run`: string.",
	)
}

func TestDefinitionPage_RendersRules(t *testing.T) {
	t.Parallel()

	page := renderFixture(t, `
		"kind": {"enum": ["agent", "human"]},
		"agent": {"type": "string"},
		"route": {"type": "object"},
		"image": {"type": "string"},
		"command": {"type": "string"},
		"action": {"enum": ["merge"]},
		"source": {"oneOf": [{"type": "string"}, {"type": "object", "properties": {"name": {"type": "string"}}}]}`, `,
		"allOf": [
			{"if": {"properties": {"kind": {"const": "agent"}}}, "then": {"oneOf": [{"required": ["agent"]}, {"required": ["route"]}]}},
			{"anyOf": [{"required": ["image"]}, {"required": ["command"]}]},
			{"description": "A station with action is a human station.", "if": {"required": ["action"]}, "then": {"properties": {"kind": {"const": "human"}}}},
			{"if": {"properties": {"kind": {"const": "human"}}}, "then": {"properties": {"note": {"type": "string"}}}}
		]`)

	assertLines(t, page,
		"- Rules:",
		"  - When `kind` is `\"agent\"`, exactly one of `agent` or `route` is set.",
		"  - At least one of `image` or `command` is set.",
		"  - A station with action is a human station.",
		"- `note`: string; only when `kind` is `\"human\"`.",
		"  - `name`: string.",
	)

	if strings.Count(page, "Rules:") != 1 {
		t.Errorf("want one Rules list, the variants of source and the fields of the last allOf entry are no rules:\n%s", page)
	}
}

func TestDefinitionPage_RendersRulesInsideAConditionalVariant(t *testing.T) {
	t.Parallel()

	page := renderFixture(t, `"type": {"enum": ["mcp", "aws"]}`, `,
		"allOf": [{
			"if": {"properties": {"type": {"const": "mcp"}}},
			"then": {
				"properties": {"url": {"type": "string"}, "image": {"type": "string"}},
				"allOf": [{"oneOf": [{"required": ["url"]}, {"required": ["image"]}]}]
			}
		}]`)

	assertLines(t, page,
		"- `url`: string; only when `type` is `\"mcp\"`.",
		"- Rules:",
		"  - When `type` is `\"mcp\"`, exactly one of `url` or `image` is set.",
	)
}

func TestDefinitionPage_RuleItCannotDeriveFails(t *testing.T) {
	t.Parallel()

	_, err := renderDefinitionPage([]byte(`{"properties": {"spec": {"type": "object",
		"properties": {"kind": {"enum": ["agent"]}, "gate": {"type": "object"}},
		"allOf": [{"if": {"required": ["action"]}, "then": {"required": ["gate"]}}]}}}`))

	if !errors.Is(err, errSchema) {
		t.Fatalf("want errSchema, got %v", err)
	}

	if !strings.Contains(err.Error(), "spec: allOf[0]") {
		t.Errorf("want the error to name the rule's path, got %v", err)
	}
}
