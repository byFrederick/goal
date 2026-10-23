package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// The worker writes one JSON object per line to stdout; every line has "type" and "at"
// (RFC 3339). worker/README.md documents the fields of each type. Progress lines become
// RunEvents with these kinds; the finished line becomes the run's result.
const (
	KindAgentStarted domain.EventKind = "agent_started"
	KindToolCall     domain.EventKind = "tool_call"
	KindToolResult   domain.EventKind = "tool_result"
	KindAgentMessage domain.EventKind = "agent_message"
	KindUsage        domain.EventKind = "usage"
)

var eventKinds = map[string]domain.EventKind{
	"started":     KindAgentStarted,
	"tool_use":    KindToolCall,
	"tool_result": KindToolResult,
	"message":     KindAgentMessage,
	"usage":       KindUsage,
}

const typeFinished = "finished"

// maxLine bounds one stdout line; the finished line carries the whole output.
const maxLine = 16 << 20

// finished is the worker's last line: how the agent ended and what it cost.
type finished struct {
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
}

var errUnknownLine = errors.New("not a worker protocol line")

// parseLine reads one stdout line: a progress event, the finished record, or errUnknownLine.
// Events without a valid "at" are stamped with now.
func parseLine(line []byte, now time.Time) (*ports.RunEvent, *finished, error) {
	var head struct {
		Type string `json:"type"`
		At   string `json:"at"`
	}

	if err := json.Unmarshal(line, &head); err != nil {
		return nil, nil, fmt.Errorf("%w: %w", errUnknownLine, err)
	}

	if head.Type == typeFinished {
		var f finished
		if err := json.Unmarshal(line, &f); err != nil {
			return nil, nil, fmt.Errorf("finished line: %w", err)
		}

		return nil, &f, nil
	}

	kind, ok := eventKinds[head.Type]
	if !ok {
		return nil, nil, fmt.Errorf("%w: type %q", errUnknownLine, head.Type)
	}

	var data map[string]any
	if err := json.Unmarshal(line, &data); err != nil {
		return nil, nil, fmt.Errorf("%s line: %w", head.Type, err)
	}

	delete(data, "type")
	delete(data, "at")

	at, err := time.Parse(time.RFC3339Nano, head.At)
	if err != nil {
		at = now
	}

	return &ports.RunEvent{Kind: kind, At: at, Data: data}, nil, nil
}

// outcome is everything known about a task once its container has exited.
type outcome struct {
	fin      *finished
	exitCode int64
	// stopped is why the runner stopped the container: a context.Canceled error or a timeout.
	// It is nil when the container ran to its end.
	stopped error
	// stderr is the redacted tail of the worker's stderr.
	stderr string
}

// errTimeout marks a task the runner killed because it ran out of time.
var errTimeout = errors.New("timed out")

// resultOf maps an exited task to its run result. A stop the runner made wins over whatever the
// worker reported, so a cancelled task reads as cancelled and a killed one as timed out.
func resultOf(o outcome) domain.RunResult {
	var res domain.RunResult
	if f := o.fin; f != nil {
		res = domain.RunResult{
			CostUSD: f.CostUSD, Turns: f.Turns, InputTokens: f.InputTokens, OutputTokens: f.OutputTokens, SessionID: f.SessionID,
		}
	}

	switch {
	case o.stopped != nil && errors.Is(o.stopped, context.Canceled):
		res.Status, res.Error = domain.RunCancelled, o.stopped.Error()
	case o.stopped != nil:
		res.Status, res.Error = domain.RunFailed, o.stopped.Error()
	case o.fin == nil:
		res.Status, res.Error = domain.RunFailed, exitError(o.exitCode, o.stderr)
	case o.fin.Status == string(domain.RunSucceeded) && hasValue(o.fin.Output):
		res.Status, res.Output = domain.RunSucceeded, o.fin.Output
	case o.fin.Status == string(domain.RunSucceeded):
		res.Status, res.Error = domain.RunFailed, "worker finished without output"
	default:
		res.Status, res.Error = domain.RunFailed, failure(o.fin)
	}

	return res
}

func hasValue(raw json.RawMessage) bool {
	v := bytes.TrimSpace(raw)
	return len(v) > 0 && !bytes.Equal(v, []byte("null"))
}

func failure(f *finished) string {
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

func exitError(code int64, stderr string) string {
	msg := fmt.Sprintf("worker exited with code %d without a result", code)
	if stderr != "" {
		msg += ": " + stderr
	}

	return msg
}

// redactor replaces secret values with a marker wherever they appear in text the runner passes
// on: events, logs and error messages.
type redactor struct {
	r *strings.Replacer
}

// minSecretLen keeps short settings values (true, 1) from being treated as secrets.
const minSecretLen = 8

const redacted = "[redacted]"

func newRedactor(values ...string) redactor {
	var pairs []string

	for _, v := range values {
		if len(v) >= minSecretLen {
			pairs = append(pairs, v, redacted)
		}
	}

	return redactor{r: strings.NewReplacer(pairs...)}
}

func (r redactor) string(s string) string { return r.r.Replace(s) }

// value redacts every string inside a decoded JSON value.
func (r redactor) value(v any) any {
	switch x := v.(type) {
	case string:
		return r.string(x)
	case map[string]any:
		for k, e := range x {
			x[k] = r.value(e)
		}
	case []any:
		for i, e := range x {
			x[i] = r.value(e)
		}
	}

	return v
}

// eachLine calls fn with every non-empty line of r, trimmed. Lines longer than limit are
// dropped, but r is always read to its end so the writer never blocks. fn must not keep the
// slice.
func eachLine(r io.Reader, limit int, fn func([]byte)) error {
	br := bufio.NewReaderSize(r, 64<<10)

	var (
		line    []byte
		tooLong bool
	)

	for {
		chunk, err := br.ReadSlice('\n')

		switch {
		case tooLong:
		case len(line)+len(chunk) > limit:
			tooLong, line = true, line[:0]
		default:
			line = append(line, chunk...)
		}

		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case err != nil && !errors.Is(err, io.EOF):
			return err
		}

		if t := bytes.TrimSpace(line); !tooLong && len(t) > 0 {
			fn(t)
		}

		line, tooLong = line[:0], false

		if errors.Is(err, io.EOF) {
			return nil
		}
	}
}

// tail keeps the last lines written to it.
type tail struct {
	lines []string
	max   int
}

func (t *tail) add(line string) {
	t.lines = append(t.lines, line)
	if len(t.lines) > t.max {
		t.lines = t.lines[len(t.lines)-t.max:]
	}
}

func (t *tail) String() string { return strings.Join(t.lines, "\n") }
