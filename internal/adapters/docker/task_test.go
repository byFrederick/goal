package docker

import (
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Every tool call the agent's policy refused, as the worker's finished line lists them, becomes a
// tool_denied event when the run ends, after the run's progress events.
func TestRelay_RecordsEachDeniedCallAsAnEvent(t *testing.T) {
	t.Parallel()

	tk := &task{log: slog.New(slog.DiscardHandler), redact: newRedactor(), events: make(chan ports.RunEvent, 8)}
	stdout := `{"type":"tool_use","at":"2026-10-23T12:00:00Z","id":"tu_1","tool":"Bash","input":"{}"}` + "\n" +
		`{"type":"finished","at":"2026-10-23T12:01:00Z","status":"succeeded","output":{},` +
		`"permission_denials":[{"tool":"Bash","id":"tu_7"},{"tool":"mcp__goal__github__merge_pr","id":"tu_9"}]}` + "\n"

	fin := tk.relay(t.Context(), strings.NewReader(stdout))
	close(tk.events)

	if fin == nil || fin.Status != "succeeded" {
		t.Fatalf("finished = %+v; want the worker's result", fin)
	}

	var denied []ports.RunEvent

	kinds := []domain.EventKind{}

	for ev := range tk.events {
		kinds = append(kinds, ev.Kind)

		if ev.Kind == domain.EventToolDenied {
			denied = append(denied, ev)
		}
	}

	if want := []domain.EventKind{KindToolCall, domain.EventToolDenied, domain.EventToolDenied}; !reflect.DeepEqual(kinds, want) {
		t.Fatalf("events %v; want %v", kinds, want)
	}

	at := time.Date(2026, 10, 23, 12, 1, 0, 0, time.UTC)
	want := []ports.RunEvent{
		{Kind: domain.EventToolDenied, At: at, Data: map[string]any{"tool": "Bash", "id": "tu_7"}},
		{Kind: domain.EventToolDenied, At: at, Data: map[string]any{"tool": "mcp__goal__github__merge_pr", "id": "tu_9"}},
	}

	if !reflect.DeepEqual(denied, want) {
		t.Errorf("tool_denied events %+v; want %+v", denied, want)
	}
}
