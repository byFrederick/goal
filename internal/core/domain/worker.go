package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// The worker (goal-agent) writes one JSON object per line to stdout; every line has "type" and
// "at" (RFC 3339). apps/worker/README.md documents the fields of each type. Progress lines become
// item events of these kinds; the finished line becomes the run's result.
const (
	EventAgentStarted EventKind = "agent_started"
	EventToolCall     EventKind = "tool_call"
	EventToolResult   EventKind = "tool_result"
	EventAgentMessage EventKind = "agent_message"
)

var workerEventKinds = map[string]EventKind{
	"started":        EventAgentStarted,
	"tool_use":       EventToolCall,
	"tool_result":    EventToolResult,
	"message":        EventAgentMessage,
	"usage":          EventUsage,
	"note_delivered": EventNoteDelivered,
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
	// CPUSeconds and PeakMemoryBytes are the box's CPU time and peak memory as the worker read
	// them; nil when the line has no such field (a worker that cannot read them).
	CPUSeconds      *float64 `json:"cpu_seconds"`
	PeakMemoryBytes *int64   `json:"peak_memory_bytes"`
	// MemorySource is how the worker measured PeakMemoryBytes, "cgroup" or "machine"; absent
	// without it. Result keeps only those two values, and only with PeakMemoryBytes.
	MemorySource string `json:"memory_source"`
	// BuildCache is what happened to the box's build cache: hit, miss, off or error; absent when
	// the run had no Go or pnpm repository. Result keeps only those four values.
	BuildCache string `json:"build_cache"`
	// Repositories are the repositories the run fetched or pushed, each with its pushed head and
	// whether it wrote (ADR 0041 decision 5).
	Repositories []WorkerRepository `json:"repositories"`
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
	// Who acted and whether a gate required it are goald's to say: an agent's line is no touch.
	delete(data, "actor")
	delete(data, KeyPlanned)

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

// WorkerRepository is one repository of a worker line's repositories list (ADR 0041 decision 5):
// prepare's line reports its base commit and branch head, the finished line its pushed head and
// whether the run wrote.
type WorkerRepository struct {
	Repo       string `json:"repo"`
	BaseCommit string `json:"base_commit"`
	BranchHead string `json:"branch_head"`
	PushedHead string `json:"pushed_head"`
	Wrote      bool   `json:"wrote"`
}

// RunRepositories are a worker line's repositories as a run records them: each by its
// RepositoryKey, with only the commits GitHead accepts, once (the first report of a repository
// wins). A repository that is no RepositoryKey, or reports no commit and no write, is dropped.
func RunRepositories(reported []WorkerRepository) []RunRepository {
	var out []RunRepository

	seen := make(map[string]bool, len(reported))

	for _, r := range reported {
		key, ok := RepositoryKey(r.Repo)
		if !ok || seen[key] {
			continue
		}

		row := RunRepository{
			Repo: key, BaseCommit: GitHead(r.BaseCommit), BranchHead: GitHead(r.BranchHead), PushedHead: GitHead(r.PushedHead),
			Wrote: r.Wrote,
		}
		if row == (RunRepository{Repo: key}) {
			continue
		}

		seen[key] = true

		out = append(out, row)
	}

	return out
}

// memorySource is the line's memory source when it is a known one and the line has a peak memory;
// empty otherwise.
func (f *WorkerFinished) memorySource() MemorySource {
	switch src := MemorySource(f.MemorySource); {
	case f.PeakMemoryBytes == nil:
		return ""
	case src == MemoryCgroup, src == MemoryMachine:
		return src
	default:
		return ""
	}
}

// buildCache is the line's build cache outcome when it is a known one; empty otherwise.
func (f *WorkerFinished) buildCache() BuildCacheOutcome {
	switch bc := BuildCacheOutcome(f.BuildCache); bc {
	case BuildCacheHit, BuildCacheMiss, BuildCacheOff, BuildCacheError:
		return bc
	default:
		return ""
	}
}

// Result is the run's result as the worker reported it: succeeded only with an output, failed
// with the worker's reason otherwise, with the repositories it reported. How the process ended
// (killed, timed out, crashed) is the caller's to apply on top.
func (f *WorkerFinished) Result() RunResult {
	res := RunResult{
		CostUSD: f.CostUSD, Turns: f.Turns, InputTokens: f.InputTokens, OutputTokens: f.OutputTokens, SessionID: f.SessionID,
		Repositories: RunRepositories(f.Repositories), CPUSeconds: f.CPUSeconds, PeakMemoryBytes: f.PeakMemoryBytes,
		PeakMemorySource: f.memorySource(), BuildCache: f.buildCache(),
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

// GitHead is s when it is a full git commit id (40 or 64 lowercase hex digits, SHA-1 or SHA-256)
// and empty otherwise: the box reports heads, and only a commit id is kept from it.
func GitHead(s string) string {
	if len(s) != 40 && len(s) != 64 {
		return ""
	}

	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return ""
		}
	}

	return s
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
