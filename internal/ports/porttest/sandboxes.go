package porttest

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"sync"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

var _ ports.Sandboxes = (*Sandboxes)(nil)

// ExecFunc plays one command in a sandbox: it writes the command's output, must return when ctx
// ends, and returns the exit code. It runs on its own goroutine, once per Exec.
type ExecFunc func(ctx context.Context, c ExecCall, stdout, stderr io.Writer) int

// ExecCall is one command as a real sandbox would run it.
type ExecCall struct {
	Sandbox ports.SandboxSpec
	// Exec is the command; its Env is the sandbox's with the command's own on top.
	ports.Exec
}

// Sandboxes holds sandboxes in memory and plays every command with a scripted ExecFunc. Like a
// real backend it keeps one sandbox per run, merges environments, enforces timeouts, kills
// commands when their ctx ends or their sandbox is deleted, and keeps the stderr tail.
type Sandboxes struct {
	fn ExecFunc

	mu    sync.Mutex
	boxes map[domain.RunID]*sandbox
}

// NewSandboxes returns sandboxes that play fn for every command.
func NewSandboxes(fn ExecFunc) *Sandboxes {
	return &Sandboxes{fn: fn, boxes: map[domain.RunID]*sandbox{}}
}

// Create implements ports.Sandboxes.
func (s *Sandboxes) Create(_ context.Context, spec ports.SandboxSpec) (ports.Sandbox, error) {
	if spec.RunID == "" {
		return nil, errors.New("porttest: sandbox without a run")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if b, ok := s.boxes[spec.RunID]; ok {
		return b, nil
	}

	spec.Env = maps.Clone(spec.Env)
	b := &sandbox{owner: s, spec: spec, kills: map[*process]context.CancelCauseFunc{}}
	s.boxes[spec.RunID] = b

	return b, nil
}

// List implements ports.Sandboxes, in run order.
func (s *Sandboxes) List(context.Context) ([]ports.Leftover, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]ports.Leftover, 0, len(s.boxes))
	for _, b := range s.boxes {
		out = append(out, ports.Leftover{Run: b.spec.RunID, Org: b.spec.OrgID})
	}

	slices.SortFunc(out, func(a, b ports.Leftover) int { return cmp.Compare(a.Run, b.Run) })

	return out, nil
}

// Delete implements ports.Sandboxes.
func (s *Sandboxes) Delete(_ context.Context, run domain.RunID) error {
	s.mu.Lock()
	b, ok := s.boxes[run]
	delete(s.boxes, run)
	s.mu.Unlock()

	if ok {
		b.kill()
	}

	return nil
}

var errSandboxDeleted = errors.New("porttest: sandbox deleted")

type sandbox struct {
	owner *Sandboxes
	spec  ports.SandboxSpec

	mu      sync.Mutex
	deleted bool
	// kills ends the commands still running, by process.
	kills map[*process]context.CancelCauseFunc
}

func (b *sandbox) Delete(ctx context.Context) error { return b.owner.Delete(ctx, b.spec.RunID) }

// kill marks the sandbox deleted and ends its running commands.
func (b *sandbox) kill() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.deleted = true
	for _, cancel := range b.kills {
		cancel(errSandboxDeleted)
	}
}

func (b *sandbox) Exec(ctx context.Context, e ports.Exec) (ports.Process, error) {
	if len(e.Argv) == 0 {
		return nil, errors.New("porttest: exec without a command")
	}

	call := ExecCall{Sandbox: b.spec, Exec: e}
	call.Argv = slices.Clone(e.Argv)
	call.Env = maps.Clone(b.spec.Env)

	if call.Env == nil {
		call.Env = map[string]string{}
	}

	maps.Copy(call.Env, e.Env)

	run, kill := context.WithCancelCause(ctx)
	stdoutR, stdoutW := io.Pipe()
	p := &process{stdout: stdoutR, done: make(chan struct{})}

	b.mu.Lock()
	if b.deleted {
		b.mu.Unlock()
		kill(nil)

		return nil, fmt.Errorf("exec in run %s: %w", b.spec.RunID, errSandboxDeleted)
	}

	b.kills[p] = kill
	b.mu.Unlock()

	go b.play(run, kill, call, stdoutW, p)

	return p, nil
}

// play runs the script and records how the command ended. A killed command's output ends and
// its pending writes fail at once, as a real process's do.
func (b *sandbox) play(run context.Context, kill context.CancelCauseFunc, call ExecCall, stdout *io.PipeWriter, p *process) {
	defer close(p.done)

	script, cancel := run, context.CancelFunc(func() {})
	if call.Timeout > 0 {
		script, cancel = context.WithTimeoutCause(run, call.Timeout, errTimedOut)
	}

	defer cancel()

	stopWrites := context.AfterFunc(script, func() { _ = stdout.Close() })

	var stderr tailWriter

	code := b.owner.fn(script, call, stdout, &stderr)
	cause := context.Cause(script)

	stopWrites()

	_ = stdout.Close()

	b.mu.Lock()
	delete(b.kills, p)
	b.mu.Unlock()
	kill(nil)

	switch {
	case cause == nil:
		p.status = ports.ExitStatus{Code: code, StderrTail: stderr.String()}
	case errors.Is(cause, errTimedOut):
		p.status = ports.ExitStatus{Code: -1, TimedOut: true, StderrTail: stderr.String()}
	case errors.Is(cause, errSandboxDeleted):
		p.err = fmt.Errorf("exec in run %s: %w", call.Sandbox.RunID, errSandboxDeleted)
	default:
		p.err = fmt.Errorf("exec killed: %w", cause)
	}
}

var errTimedOut = errors.New("porttest: exec timed out")

type process struct {
	stdout *io.PipeReader
	done   chan struct{}
	status ports.ExitStatus
	err    error
}

func (p *process) Stdout() io.Reader { return p.stdout }

func (p *process) Wait() (ports.ExitStatus, error) {
	<-p.done

	return p.status, p.err
}

// tailWriter keeps the last ports.MaxStderrTail bytes written to it.
type tailWriter struct {
	mu  sync.Mutex
	buf []byte
}

func (w *tailWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf = append(w.buf, b...)
	if over := len(w.buf) - ports.MaxStderrTail; over > 0 {
		w.buf = slices.Delete(w.buf, 0, over)
	}

	return len(b), nil
}

func (w *tailWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()

	return string(w.buf)
}
