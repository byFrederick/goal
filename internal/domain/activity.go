package domain

import (
	"encoding/json"
	"regexp"
	"strings"
)

// ToolTodoWrite is the Claude Agent SDK's tool that replaces the agent's todo list; its latest
// call is the run's plan (PlanSteps).
const ToolTodoWrite = "TodoWrite"

// maxActivity caps a card's activity line, in runes, the ellipsis included.
const maxActivity = 80

// PlanProgress is how far a run is through its plan: the todos it completed of all it listed.
type PlanProgress struct {
	Done  int
	Total int
}

// activityVerbs maps the Claude Agent SDK's tools to what a card says the agent is doing and the
// input field that names what it does it to. The tool names are the SDK's, not configuration.
var activityVerbs = map[string]struct{ verb, field string }{
	"Edit":         {"Editing", "file_path"},
	"Write":        {"Editing", "file_path"},
	"MultiEdit":    {"Editing", "file_path"},
	"NotebookEdit": {"Editing", "notebook_path"},
	"Bash":         {"Running", "command"},
	"Read":         {"Reading", "file_path"},
	"Grep":         {"Searching", "pattern"},
	"Glob":         {"Searching", "pattern"},
}

// secretShapes are strings that look like credentials, each with what replaces it. They match a
// bare prefix too, so a token cut short is hidden as well.
var secretShapes = []struct {
	re   *regexp.Regexp
	with string
}{
	{regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^/\s@]+@`), "${1}***@"},
	{regexp.MustCompile(`(?i)\b([a-z0-9_]*(?:token|secret|password|key)[a-z0-9_]*)=(?:"[^"]*"?|'[^']*'?|\S*)`), "${1}=***"},
	{regexp.MustCompile(`(?i)\bbearer\s+\S+`), "Bearer ***"},
	{regexp.MustCompile(`\b(?:gh[pousr]_|github_pat_)[A-Za-z0-9_]*`), "***"},
	{regexp.MustCompile(`\bsk-[A-Za-z0-9_-]*`), "***"},
	{regexp.MustCompile(`\bAKIA[0-9A-Z]+`), "***"},
}

// CardActivity says in one short line what a tool_call event's data ({tool, input}, input the
// tool's input as JSON, possibly cut short) shows the agent doing: "Editing <path>", "Running
// <command>" (its first line), "Reading <path>", "Searching <pattern>", else "Using <tool>".
// Credential-shaped strings become *** before the line is capped at 80 runes. It is "" when the
// data names no tool.
func CardActivity(data map[string]any) string {
	tool, _ := data["tool"].(string)
	if tool == "" {
		return ""
	}

	line := "Using " + tool

	if v, ok := activityVerbs[tool]; ok {
		if what := inputField(data, v.field); what != "" {
			line = v.verb + " " + what
		}
	}

	return capRunes(redact(line), maxActivity)
}

// PlanSteps reads a TodoWrite call's data as the run's plan progress. It is false for another
// tool, for input that does not parse (the worker cut it short) and for an empty list.
func PlanSteps(data map[string]any) (PlanProgress, bool) {
	if tool, _ := data["tool"].(string); tool != ToolTodoWrite {
		return PlanProgress{}, false
	}

	input, _ := data["input"].(string)

	var plan struct {
		Todos []struct {
			Status string `json:"status"`
		} `json:"todos"`
	}

	if err := json.Unmarshal([]byte(input), &plan); err != nil || len(plan.Todos) == 0 {
		return PlanProgress{}, false
	}

	p := PlanProgress{Total: len(plan.Todos)}

	for _, td := range plan.Todos {
		if td.Status == "completed" {
			p.Done++
		}
	}

	return p, true
}

// inputField is the first line of field in a tool call's JSON input, trimmed; "" when the input
// does not parse or has no such string.
func inputField(data map[string]any, field string) string {
	input, _ := data["input"].(string)

	var fields map[string]any
	if err := json.Unmarshal([]byte(input), &fields); err != nil {
		return ""
	}

	s, _ := fields[field].(string)
	first, _, _ := strings.Cut(strings.TrimSpace(s), "\n")

	return strings.TrimSpace(first)
}

func redact(s string) string {
	for _, sh := range secretShapes {
		s = sh.re.ReplaceAllString(s, sh.with)
	}

	return s
}

// capRunes cuts s to at most n runes, ending it with an ellipsis when it was longer.
func capRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}

	return string(r[:n-1]) + "…"
}
