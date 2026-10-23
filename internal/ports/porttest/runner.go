package porttest

import (
	"context"
	"encoding/json"
	"slices"
	"sync"

	"github.com/byFrederick/factory/internal/domain"
	"github.com/byFrederick/factory/internal/ports"
)

var _ ports.Runner = (*Runner)(nil)

// RunFunc plays one agent task: it may send progress events, must return when ctx ends, and
// returns the task's result. It runs on its own goroutine.
type RunFunc func(ctx context.Context, t ports.TaskSpec, events chan<- ports.RunEvent) domain.RunResult

// Runner runs every task with a scripted RunFunc and records what it was asked to start.
type Runner struct {
	fn RunFunc

	mu      sync.Mutex
	started []ports.TaskSpec
	changes changes
}

// NewRunner returns a runner that plays fn for every task.
func NewRunner(fn RunFunc) *Runner { return &Runner{fn: fn} }

// Start implements ports.Runner.
func (r *Runner) Start(ctx context.Context, t ports.TaskSpec) (ports.Task, error) {
	r.mu.Lock()
	r.started = append(r.started, t)
	r.mu.Unlock()

	r.changes.signal()

	tk := &task{events: make(chan ports.RunEvent), done: make(chan struct{})}

	go func() {
		res := r.fn(ctx, t, tk.events)
		if ctx.Err() != nil && res.Status == "" {
			res = domain.RunResult{Status: domain.RunCancelled, Error: ctx.Err().Error()}
		}

		tk.res = res

		close(tk.events)
		close(tk.done)
	}()

	return tk, nil
}

// Started returns the specs of every task started so far, in order.
func (r *Runner) Started() []ports.TaskSpec {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.started)
}

// WaitStarted blocks until at least n tasks have started.
func (r *Runner) WaitStarted(ctx context.Context, n int) error {
	return r.changes.await(ctx, func() bool { return len(r.Started()) >= n })
}

type task struct {
	events chan ports.RunEvent
	done   chan struct{}
	res    domain.RunResult
}

func (t *task) Events() <-chan ports.RunEvent { return t.events }

func (t *task) Wait() (domain.RunResult, error) {
	<-t.done

	return t.res, nil
}

// Succeed returns a RunFunc whose tasks all succeed with output.
func Succeed(output string) RunFunc {
	return func(context.Context, ports.TaskSpec, chan<- ports.RunEvent) domain.RunResult {
		return domain.RunResult{Status: domain.RunSucceeded, Output: json.RawMessage(output)}
	}
}

// Sequence returns a RunFunc that returns results in order, one per task, repeating the last.
func Sequence(results ...domain.RunResult) RunFunc {
	var (
		mu sync.Mutex
		i  int
	)

	return func(context.Context, ports.TaskSpec, chan<- ports.RunEvent) domain.RunResult {
		mu.Lock()
		defer mu.Unlock()

		res := results[min(i, len(results)-1)]
		i++

		return res
	}
}

// Block returns a RunFunc whose tasks run until ctx ends or release is closed, then
// succeed with output.
func Block(release <-chan struct{}, output string) RunFunc {
	return func(ctx context.Context, _ ports.TaskSpec, _ chan<- ports.RunEvent) domain.RunResult {
		select {
		case <-release:
			return domain.RunResult{Status: domain.RunSucceeded, Output: json.RawMessage(output)}
		case <-ctx.Done():
			return domain.RunResult{Status: domain.RunCancelled, Error: ctx.Err().Error()}
		}
	}
}
