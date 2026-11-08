package porttest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sync"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// AgentFunc plays one `goal-agent run`: it may write progress lines of the worker's stdout
// protocol to stdout, returns the run's result as the worker would report it, and must return
// when ctx ends. It runs on the command's own goroutine.
type AgentFunc func(ctx context.Context, run ExecCall, stdout io.Writer) domain.RunResult

// Worker plays the goal-agent image in Sandboxes (NewSandboxes(w.Exec)), as apps/worker/README.md's
// sandbox contract says: `goal-agent prepare` writes its one prepared line, `goal-agent run`
// plays an AgentFunc and writes its result as the stdout protocol's finished line, a try's diff
// (domain.PastTryDiffScript) prints what Diff scripted (none by default), and any
// other command is a station check, which exits as scripted (Exit) and passes otherwise. It
// records every command it was given.
type Worker struct {
	agent AgentFunc

	mu            sync.Mutex
	prepareCode   int
	prepareStderr string
	branchExisted bool
	prepared      []domain.WorkerRepository
	exits         map[string][]int
	diff          string
	diffCode      int
	diffs         []ExecCall
	prepares      []ExecCall
	runs          []ExecCall
	checks        []ExecCall
	changes       changes
}

// NewWorker returns a worker whose agent runs play agent.
func NewWorker(agent AgentFunc) *Worker { return &Worker{agent: agent, exits: map[string][]int{}} }

// FailPrepare makes every prepare exit with code, writing stderr, instead of preparing.
func (w *Worker) FailPrepare(code int, stderr string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.prepareCode, w.prepareStderr = code, stderr
}

// BranchExisted makes prepare report that the item's branch already existed on the remote.
func (w *Worker) BranchExisted() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.branchExisted = true
}

// PreparedRepositories makes prepare report rows as its repositories list (ADR 0041 decision 5).
func (w *Worker) PreparedRepositories(rows ...domain.WorkerRepository) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.prepared = rows
}

// Diff makes a try's diff (domain.PastTryDiffScript) print diff and exit with code.
func (w *Worker) Diff(diff string, code int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.diff, w.diffCode = diff, code
}

// Diffs are the try diff commands the worker was given.
func (w *Worker) Diffs() []ExecCall {
	w.mu.Lock()
	defer w.mu.Unlock()

	return slices.Clone(w.diffs)
}

// Exit scripts the exit codes of successive runs of a check command, repeating the last.
func (w *Worker) Exit(command string, codes ...int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.exits[command] = codes
}

// Exec is the worker's ExecFunc.
func (w *Worker) Exec(ctx context.Context, c ExecCall, stdout, stderr io.Writer) int {
	switch {
	case slices.Equal(c.Argv, []string{"goal-agent", "prepare"}):
		return w.prepare(c, stdout, stderr)
	case slices.Equal(c.Argv, []string{"goal-agent", "run"}):
		return w.run(ctx, c, stdout, stderr)
	case slices.Equal(c.Argv, []string{"sh", "-c", domain.PastTryDiffScript}):
		w.mu.Lock()
		w.diffs = append(w.diffs, c)
		diff, code := w.diff, w.diffCode
		w.mu.Unlock()

		_, _ = io.WriteString(stdout, diff)

		return code
	default:
		return w.check(c, stdout)
	}
}

func (w *Worker) prepare(c ExecCall, stdout, stderr io.Writer) int {
	w.mu.Lock()
	w.prepares = append(w.prepares, c)
	code, why, existed, rows := w.prepareCode, w.prepareStderr, w.branchExisted, w.prepared
	w.mu.Unlock()

	w.changes.signal()

	if code != 0 {
		_, _ = io.WriteString(stderr, why)
		return code
	}

	writeLine(stdout, preparedLine{
		Type: "prepared", At: time.Now().UTC().Format(time.RFC3339), Seconds: 0.1, BranchExisted: existed,
		Repositories: rows,
	})

	return 0
}

// run plays the agent and reports its result as the worker does: a started line, then a finished
// line and exit 0, or, for a result without a status, no finished line and exit 1, as a crashed
// worker.
func (w *Worker) run(ctx context.Context, c ExecCall, stdout, stderr io.Writer) int {
	w.mu.Lock()
	w.runs = append(w.runs, c)
	w.mu.Unlock()

	w.changes.signal()

	// goal-agent's first line, once the agent has read its brief: goald records it as agent_started.
	writeLine(stdout, startedLine{Type: "started", At: time.Now().UTC().Format(time.RFC3339)})

	res := w.agent(ctx, c, stdout)
	if ctx.Err() != nil {
		return 1 // killed: the sandbox reports how
	}

	if res.Status == "" {
		_, _ = io.WriteString(stderr, "worker crashed")
		return 1
	}

	status := domain.RunSucceeded
	if res.Status != domain.RunSucceeded {
		status = domain.RunFailed
	}

	fin := domain.WorkerFinished{
		Status: string(status), Output: res.Output, Error: res.Error, CostUSD: res.CostUSD, Turns: res.Turns,
		InputTokens: res.InputTokens, OutputTokens: res.OutputTokens, SessionID: res.SessionID,
		At: time.Now().UTC().Format(time.RFC3339),
	}
	reportRepositories(&fin, res.Repositories)
	writeLine(stdout, finishedLine{Type: "finished", WorkerFinished: fin})

	return 0
}

// reportRepositories puts a result's repositories in the finished line's repositories list, as
// the worker reports them (ADR 0041 decision 5).
func reportRepositories(fin *domain.WorkerFinished, rows []domain.RunRepository) {
	for _, r := range rows {
		fin.Repositories = append(fin.Repositories, domain.WorkerRepository{Repo: r.Repo, PushedHead: r.PushedHead, Wrote: r.Wrote})
	}
}

// startedLine is the worker's first stdout line, written when the agent starts.
type startedLine struct {
	Type string `json:"type"`
	At   string `json:"at"`
}

// finishedLine is the worker's last stdout line.
type finishedLine struct {
	Type string `json:"type"`
	domain.WorkerFinished
}

// preparedLine is goal-agent prepare's one stdout line.
type preparedLine struct {
	Type          string                    `json:"type"`
	At            string                    `json:"at"`
	Seconds       float64                   `json:"seconds"`
	Tools         int                       `json:"tools"`
	BranchExisted bool                      `json:"branch_existed"`
	Repositories  []domain.WorkerRepository `json:"repositories"`
}

func (w *Worker) check(c ExecCall, stdout io.Writer) int {
	w.mu.Lock()
	w.checks = append(w.checks, c)

	command := c.Argv[len(c.Argv)-1]
	code := 0

	if codes := w.exits[command]; len(codes) > 0 {
		code = codes[0]
		if len(codes) > 1 {
			w.exits[command] = codes[1:]
		}
	}
	w.mu.Unlock()

	w.changes.signal()

	_, _ = fmt.Fprintf(stdout, "%s exited %d\n", command, code)

	return code
}

func writeLine(w io.Writer, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // the fake's own values always encode
	}

	_, _ = w.Write(append(b, '\n'))
}

// Prepares returns every prepare so far, in order.
func (w *Worker) Prepares() []ExecCall {
	w.mu.Lock()
	defer w.mu.Unlock()

	return slices.Clone(w.prepares)
}

// Runs returns every agent run so far, in order.
func (w *Worker) Runs() []ExecCall {
	w.mu.Lock()
	defer w.mu.Unlock()

	return slices.Clone(w.runs)
}

// WaitRuns blocks until at least n agent runs have started.
func (w *Worker) WaitRuns(ctx context.Context, n int) error {
	return w.changes.await(ctx, func() bool { return len(w.Runs()) >= n })
}

// Checks returns every check command so far, in order.
func (w *Worker) Checks() []ExecCall {
	w.mu.Lock()
	defer w.mu.Unlock()

	return slices.Clone(w.checks)
}

// Succeed returns an AgentFunc whose runs all succeed with output.
func Succeed(output string) AgentFunc {
	return func(context.Context, ExecCall, io.Writer) domain.RunResult {
		return domain.RunResult{Status: domain.RunSucceeded, Output: json.RawMessage(output)}
	}
}

// Sequence returns an AgentFunc that returns results in order, one per run, repeating the last.
func Sequence(results ...domain.RunResult) AgentFunc {
	var (
		mu sync.Mutex
		i  int
	)

	return func(context.Context, ExecCall, io.Writer) domain.RunResult {
		mu.Lock()
		defer mu.Unlock()

		res := results[min(i, len(results)-1)]
		i++

		return res
	}
}

// Block returns an AgentFunc whose runs last until ctx ends or release is closed, then succeed
// with output.
func Block(release <-chan struct{}, output string) AgentFunc {
	return func(ctx context.Context, _ ExecCall, _ io.Writer) domain.RunResult {
		select {
		case <-release:
			return domain.RunResult{Status: domain.RunSucceeded, Output: json.RawMessage(output)}
		case <-ctx.Done():
			return domain.RunResult{Status: domain.RunCancelled, Error: ctx.Err().Error()}
		}
	}
}
