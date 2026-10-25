package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// The worker (goal-agent) writes one JSON object per line to stdout; every line has "type" and
// "at" (RFC 3339). worker/README.md documents the fields of each type. Progress lines become
// item events of these kinds; the finished line becomes the run's result.
const (
	EventAgentStarted EventKind = "agent_started"
	EventToolCall     EventKind = "tool_call"
	EventToolResult   EventKind = "tool_result"
	EventAgentMessage EventKind = "agent_message"
)

var workerEventKinds = map[string]EventKind{
	"started":     EventAgentStarted,
	"tool_use":    EventToolCall,
	"tool_result": EventToolResult,
	"message":     EventAgentMessage,
	"usage":       EventUsage,
}

const workerFinished = "finished"

// ErrNotWorkerLine marks a stdout line that is not part of the worker's protocol (a tool's own
// output, a line of another type); callers skip it.
var ErrNotWorkerLine = errors.New("not a worker protocol line")

// WorkerFinished is the worker's last line: how the agent ended and what it cost.
type WorkerFinished struct {
	// Status is "succeeded" or "failed".
	Status string `json:"status"`
	// Subtype is the SDK's result subtype, for example error_max_turns.
	Subtype      string          `json:"subtype"`
	Output       json.RawMessage `json:"output"`
	Error        string          `json:"error"`
	CostUSD      float64         `json:"cost_usd"`
	Turns        int             `json:"turns"`
	InputTokens  int64           `json:"input_tokens"`
	OutputTokens int64           `json:"output_tokens"`
	SessionID    string          `json:"session_id"`
	// At is when the worker wrote the line.
	At string `json:"at"`
	// PermissionDenials are the tool calls the agent's policy refused, by tool and call id.
	PermissionDenials []struct {
		Tool string `json:"tool"`
		ID   string `json:"id"`
	} `json:"permission_denials"`
}

// DecodeWorkerLine reads one line of the worker's stdout: a progress event (Kind, At and Data
// set; the caller adds the item, run, station and actor) or the finished record. Any other line
// is ErrNotWorkerLine. Events without a valid "at" are stamped with now.
func DecodeWorkerLine(line []byte, now time.Time) (*Event, *WorkerFinished, error) {
	var head struct {
		Type string `json:"type"`
		At   string `json:"at"`
	}

	if err := json.Unmarshal(line, &head); err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrNotWorkerLine, err)
	}

	if head.Type == workerFinished {
		var f WorkerFinished
		if err := json.Unmarshal(line, &f); err != nil {
			return nil, nil, fmt.Errorf("finished line: %w", err)
		}

		return nil, &f, nil
	}

	kind, ok := workerEventKinds[head.Type]
	if !ok {
		return nil, nil, fmt.Errorf("%w: type %q", ErrNotWorkerLine, head.Type)
	}

	var data map[string]any
	if err := json.Unmarshal(line, &data); err != nil {
		return nil, nil, fmt.Errorf("%s line: %w", head.Type, err)
	}

	delete(data, "type")
	delete(data, "at")

	return &Event{Kind: kind, At: stamp(head.At, now), Data: data}, nil, nil
}

// Denials are the run's refused tool calls as tool_denied events, at the line's time (now when it
// has none).
func (f *WorkerFinished) Denials(now time.Time) []Event {
	at := stamp(f.At, now)

	out := make([]Event, 0, len(f.PermissionDenials))
	for _, d := range f.PermissionDenials {
		out = append(out, Event{Kind: EventToolDenied, At: at, Data: map[string]any{"tool": d.Tool, "id": d.ID}})
	}

	return out
}

// Result is the run's result as the worker reported it: succeeded only with an output, failed
// with the worker's reason otherwise. How the process ended (killed, timed out, crashed) is the
// caller's to apply on top.
func (f *WorkerFinished) Result() RunResult {
	res := RunResult{
		CostUSD: f.CostUSD, Turns: f.Turns, InputTokens: f.InputTokens, OutputTokens: f.OutputTokens, SessionID: f.SessionID,
	}

	switch {
	case f.Status == string(RunSucceeded) && hasValue(f.Output):
		res.Status, res.Output = RunSucceeded, f.Output
	case f.Status == string(RunSucceeded):
		res.Status, res.Error = RunFailed, "worker finished without output"
	default:
		res.Status, res.Error = RunFailed, f.failure()
	}

	return res
}

func (f *WorkerFinished) failure() string {
	switch {
	case f.Error != "" && f.Subtype != "":
		return f.Subtype + ": " + f.Error
	case f.Error != "":
		return f.Error
	case f.Subtype != "":
		return f.Subtype
	}

	return "worker reported a failure without a reason"
}

func hasValue(raw json.RawMessage) bool {
	v := bytes.TrimSpace(raw)
	return len(v) > 0 && !bytes.Equal(v, []byte("null"))
}

// stamp parses an RFC 3339 time, or returns now.
func stamp(s string, now time.Time) time.Time {
	at, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return now
	}

	return at
}
