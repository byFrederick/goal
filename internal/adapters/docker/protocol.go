package docker

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// maxLine bounds one stdout line; the finished line carries the whole output.
const maxLine = 16 << 20

// runEvent is a decoded worker event as the runner streams it.
func runEvent(ev domain.Event) ports.RunEvent {
	return ports.RunEvent{Kind: ev.Kind, At: ev.At, Data: ev.Data}
}

// outcome is everything known about a task once its container has exited.
type outcome struct {
	fin      *domain.WorkerFinished
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
	if o.fin != nil {
		res = o.fin.Result()
	}

	switch {
	case o.stopped != nil && errors.Is(o.stopped, context.Canceled):
		res.Status, res.Output, res.Error = domain.RunCancelled, nil, o.stopped.Error()
	case o.stopped != nil:
		res.Status, res.Output, res.Error = domain.RunFailed, nil, o.stopped.Error()
	case o.fin == nil:
		res.Status, res.Error = domain.RunFailed, exitError(o.exitCode, o.stderr)
	}

	return res
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
