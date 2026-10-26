package domain

import (
	"strings"
	"testing"
)

func TestCardActivity(t *testing.T) {
	t.Parallel()

	call := func(tool, input string) map[string]any {
		return map[string]any{ToolCallTool: tool, ToolCallInput: input}
	}
	long := strings.Repeat("a", 100)

	tests := []struct {
		name string
		data map[string]any
		want string
	}{
		{"edit", call("Edit", `{"file_path":"internal/x.go","old_string":"a","new_string":"b"}`), "Editing internal/x.go"},
		{"write", call("Write", `{"file_path":"docs/a.md","content":"x"}`), "Editing docs/a.md"},
		{"multi edit", call("MultiEdit", `{"file_path":"a.go","edits":[]}`), "Editing a.go"},
		{"notebook edit", call("NotebookEdit", `{"notebook_path":"n.ipynb"}`), "Editing n.ipynb"},
		{"bash first line only", call("Bash", `{"command":"go test ./...\necho done","description":"d"}`), "Running go test ./..."},
		{"read", call("Read", `{"file_path":"/workspace/AGENTS.md"}`), "Reading /workspace/AGENTS.md"},
		{"grep", call("Grep", `{"pattern":"LiveTurns","path":"."}`), "Searching LiveTurns"},
		{"glob", call("Glob", `{"pattern":"**/*.go"}`), "Searching **/*.go"},
		{"other tool", call("WebFetch", `{"url":"https://example.com"}`), "Using WebFetch"},
		{"mcp tool", call("mcp__goal__knowledge__search", `{"query":"x"}`), "Using mcp__goal__knowledge__search"},
		{"input cut short", call("Edit", `{"file_path":"internal/x.go","old_string":"aaa…`), "Using Edit"},
		{"input not json", call("Bash", "ls"), "Using Bash"},
		{"missing field", call("Edit", `{"content":"x"}`), "Using Edit"},
		{"no tool", map[string]any{ToolCallInput: `{}`}, ""},
		{"nil data", nil, ""},
		{"github token", call("Bash", `{"command":"gh auth login --with-token ghp_abcdefABCDEF0123456789"}`), "Running gh auth login --with-token ***"},
		{"github pat", call("Bash", `{"command":"echo github_pat_11AB_cdef"}`), "Running echo ***"},
		{"oauth token", call("Bash", `{"command":"x gho_abc"}`), "Running x ***"},
		{"api key", call("Bash", `{"command":"curl -d sk-ant-api03-xyz"}`), "Running curl -d ***"},
		{"aws key", call("Bash", `{"command":"aws AKIAIOSFODNN7EXAMPLE s3 ls"}`), "Running aws *** s3 ls"},
		{"bearer", call("Bash", `{"command":"curl -H 'Authorization: Bearer abc.def' x"}`), "Running curl -H 'Authorization: Bearer *** x"},
		{"named secret", call("Bash", `{"command":"GH_TOKEN=abc123 gh pr list"}`), "Running GH_TOKEN=*** gh pr list"},
		{"named key lower case", call("Bash", `{"command":"api_key=\"a b\" run"}`), "Running api_key=*** run"},
		{"password", call("Bash", `{"command":"DB_PASSWORD=hunter2"}`), "Running DB_PASSWORD=***"},
		{"url userinfo", call("Bash", `{"command":"git clone https://x:s3cret@example.com/r.git"}`), "Running git clone https://***@example.com/r.git"},
		{"capped", call("Read", `{"file_path":"`+long+`"}`), "Reading " + strings.Repeat("a", 71) + "…"},
		{"secret cut by the cap is redacted first", call("Bash", `{"command":"`+strings.Repeat("x", 69)+` ghp_abcdefghijklmnop"}`), "Running " + strings.Repeat("x", 69) + " *…"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := CardActivity(tt.data)
			if got != tt.want {
				t.Errorf("CardActivity = %q, want %q", got, tt.want)
			}

			if n := len([]rune(got)); n > maxActivity {
				t.Errorf("CardActivity has %d runes, more than %d", n, maxActivity)
			}
		})
	}
}

func TestPlanSteps(t *testing.T) {
	t.Parallel()

	call := func(tool, input string) map[string]any {
		return map[string]any{ToolCallTool: tool, ToolCallInput: input}
	}

	tests := []struct {
		name string
		data map[string]any
		want PlanProgress
		ok   bool
	}{
		{
			"two of three done",
			call(ToolTodoWrite, `{"todos":[{"content":"a","status":"completed"},{"content":"b","status":"in_progress"},{"content":"c","status":"completed"}]}`),
			PlanProgress{Done: 2, Total: 3},
			true,
		},
		{"none done", call(ToolTodoWrite, `{"todos":[{"status":"pending"}]}`), PlanProgress{Done: 0, Total: 1}, true},
		{"empty list", call(ToolTodoWrite, `{"todos":[]}`), PlanProgress{}, false},
		{"no todos", call(ToolTodoWrite, `{}`), PlanProgress{}, false},
		{"cut short", call(ToolTodoWrite, `{"todos":[{"status":"completed"},{"sta…`), PlanProgress{}, false},
		{"another tool", call("Edit", `{"todos":[{"status":"completed"}]}`), PlanProgress{}, false},
		{"input not a string", map[string]any{ToolCallTool: ToolTodoWrite, ToolCallInput: 3.0}, PlanProgress{}, false},
		{"nil data", nil, PlanProgress{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := PlanSteps(tt.data)
			if got != tt.want || ok != tt.ok {
				t.Errorf("PlanSteps = %+v, %v; want %+v, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}
