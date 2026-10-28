package engine

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// RunTask runs one agent run end to end, the lifecycle a line's runs and Jonah's reviews share:
// it records the run (StartRun, of req's kind), heartbeats req's lease when it has one, signs the
// run's task token, resolves its connections, creates a sandbox and prepares it, runs
// `goal-agent run` there with req's brief, records the run's result and cost (FinishRun) and
// deletes the sandbox. It returns the run's result, ErrLeaseLost when the lease was lost, an error
// wrapping ports.ErrSandboxLost when the sandbox was, domain.ErrBadRunRequest when req.Check
// refuses req, and any other error when the run could not be carried out.
//
// A Jonah run (ADR 0034 decision 1) holds no lease and works no item, so no item event is written
// for it: its sandbox's prepare gets no repository's git login, as it clones none, and its agent
// only the model connection's env. The line's claims run through the same lifecycle (runOnce),
// with a sandbox their attempts share and their output's checks under the same heartbeat.
func (e *Engine) RunTask(ctx context.Context, req domain.RunRequest) (domain.RunResult, error) {
	if err := req.Check(); err != nil {
		return domain.RunResult{}, err
	}

	req.Timeout = cmp.Or(req.Timeout, e.cfg.TaskTimeout)
	ctx = domain.WithOrg(ctx, req.OrgID)

	var b box
	defer e.deleteBox(ctx, e.log.With(slog.String("factory", string(req.FactoryID)), slog.String("kind", string(req.Kind))), &b)

	// A station run's events go on its item, as at its station.
	var it domain.Item
	if req.Item != "" {
		it = domain.Item{ID: req.Item, FactoryID: req.FactoryID, Station: req.Station}
	}

	return e.runTask(ctx, runTask{RunRequest: req, item: it, own: req.Connections, box: &b})
}

// runTask is one run of the lifecycle: its request and what the caller adds to it.
type runTask struct {
	domain.RunRequest

	// item is the item the run works, whose events record it; zero for a Jonah run.
	item domain.Item
	// own are the connections whose env the agent gets besides the model's: the request's
	// Connections, or the line's subset of them.
	own []string
	// box is the sandbox the run opens when it has none, and runs in.
	box *box
	// workspace, when set, is what the brief says the sandbox's workspace holds once prepared.
	workspace func(b *box, env runEnv) string
	// then, when set, runs after a succeeded run in its sandbox, under the lease's heartbeat.
	then func(ctx context.Context, runID domain.RunID, res domain.RunResult, scrub scrubber) error
}

// runTask records a run and, while heartbeating the lease when it has one, runs it in its
// sandbox (runInBox). It returns ErrLeaseLost when the lease was lost on the way.
func (e *Engine) runTask(ctx context.Context, t runTask) (domain.RunResult, error) {
	runID, err := e.store.StartRun(ctx, ports.NewRun{
		FactoryID: t.FactoryID, DefinitionVersionID: t.Version, ItemID: t.Item, Station: t.Station,
		Agent: t.Agent, Model: t.Model, StartedAt: e.clock.Now(), Kind: t.Kind,
	})
	if err != nil {
		return domain.RunResult{}, fmt.Errorf("record run: %w", err)
	}

	taskCtx, stop := context.WithTimeout(ctx, t.Timeout)
	defer stop()

	if t.Lease == nil {
		return e.runInBox(taskCtx, t, runID)
	}

	// Heartbeat from before the start: pulling the image, preparing the sandbox and the checks
	// can all outlast the TTL.
	var (
		beats sync.WaitGroup
		lost  bool
	)

	beatCtx, stopBeat := context.WithCancel(taskCtx)

	beats.Go(func() { lost = e.heartbeat(beatCtx, *t.Lease, stop) })

	res, err := e.runInBox(taskCtx, t, runID)

	stopBeat()
	beats.Wait()

	if lost {
		return res, ports.ErrLeaseLost
	}

	return res, err
}

// runInBox opens the run's sandbox when it has none, runs the agent there, records its result
// and, when it succeeded, runs what the caller runs after it (runTask.then).
func (e *Engine) runInBox(ctx context.Context, t runTask, runID domain.RunID) (domain.RunResult, error) {
	token, err := e.gatewayToken(ctx, t.RunRequest, runID)

	var (
		env   runEnv
		scrub scrubber
	)

	if err == nil {
		env, err = e.runEnv(ctx, t.RunRequest, t.own, runID)
		scrub = newScrubber(append(env.secrets, token)...)
	}

	if err == nil {
		prepareEnv := maps.Clone(env.prepare)
		prepareEnv[envGatewayToken] = token
		err = e.open(ctx, t, runID, prepareEnv, scrub)
	}

	if err != nil {
		e.finishRun(ctx, runID, failure(ctx, err))
		return domain.RunResult{}, err
	}

	agentEnv := maps.Clone(env.agent)
	agentEnv[envGatewayToken] = token

	note := ""
	if t.workspace != nil {
		note = t.workspace(t.box, env)
	}

	if f := joinParts(t.Brief, note); f != "" {
		agentEnv[envFeedback] = f
	}

	res, lost := e.agent(ctx, t, runID, agentEnv, scrub)
	e.finishRun(ctx, runID, res)

	if lost != nil {
		return res, lost
	}

	if res.Status != domain.RunSucceeded || t.then == nil {
		return res, nil
	}

	return res, t.then(ctx, runID, res, scrub)
}

// agent runs `goal-agent run` in the run's sandbox with env, relays the events it reports and
// returns its result. How the command ended (cancelled, timed out, crashed) wins over what the
// worker reported; what reaches the store is scrubbed of the run's secrets. When the sandbox was
// lost on the way the run failed, and the scrubbed error wrapping ports.ErrSandboxLost is returned too.
func (e *Engine) agent(ctx context.Context, t runTask, runID domain.RunID, env map[string]string, scrub scrubber) (domain.RunResult, error) {
	var fin *domain.WorkerFinished

	started := e.clock.Now()

	st, err := execLines(ctx, t.box.sandbox, ports.Exec{Argv: argvAgent, Env: env, Timeout: t.Timeout}, func(line []byte) {
		ev, f, err := domain.DecodeWorkerLine(line, e.clock.Now())

		switch {
		case errors.Is(err, domain.ErrNotWorkerLine):
		case err != nil:
			e.log.DebugContext(ctx, "ignored worker stdout line", slog.String("run", string(runID)), slog.Any("error", err))
		case f != nil:
			fin = f

			for _, d := range f.Denials(e.clock.Now()) {
				e.relay(ctx, t, runID, d, scrub)
			}
		default:
			e.relay(ctx, t, runID, *ev, scrub)
		}
	})

	var (
		res  domain.RunResult
		lost error
	)

	if fin != nil {
		res = fin.Result()
	}

	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded), err == nil && st.TimedOut:
		res.Status, res.Output, res.Error = domain.RunFailed, nil, fmt.Sprintf("timed out after %s", t.Timeout)
	case ctx.Err() != nil:
		res.Status, res.Output, res.Error = domain.RunCancelled, nil, ctx.Err().Error()
	case err != nil:
		res.Status, res.Output, res.Error = domain.RunFailed, nil, "agent: "+err.Error()

		if errors.Is(err, ports.ErrSandboxLost) {
			lost = scrub.err(fmt.Errorf("agent: %w", err))
		}
	case fin == nil:
		res.Status, res.Error = domain.RunFailed, fmt.Sprintf("worker exited with code %d without a result", st.Code)
		if tail := strings.TrimSpace(st.StderrTail); tail != "" {
			res.Error += ": " + tail
		}
	}

	res.Error, res.Output = scrub.string(res.Error), scrub.json(res.Output)

	e.log.InfoContext(ctx, "sandbox exec", slog.String("run", string(runID)), slog.String("step", "agent"),
		slog.Int("exit", st.Code), slog.Float64("seconds", e.since(started)))

	return res, lost
}

// relay records one event the agent reported, by the agent, for its run. A run without an item
// (a Jonah run) has nowhere to store it, as item events need an item: it is only logged.
func (e *Engine) relay(ctx context.Context, t runTask, runID domain.RunID, ev domain.Event, scrub scrubber) {
	scrub.value(ev.Data)

	if t.item.ID == "" {
		e.log.DebugContext(ctx, "agent event", slog.String("run", string(runID)), slog.String("kind", string(ev.Kind)))
		return
	}

	rec := event(t.item, ev.Kind, ev.At, t.Agent, ev.Data)
	rec.RunID = runID

	if err := e.store.AppendEvents(ctx, rec); err != nil && ctx.Err() == nil {
		e.log.WarnContext(ctx, "record agent event", slog.String("item", string(t.item.ID)), slog.Any("error", err))
	}
}

// failure is the result of a run that could not be carried out: cancelled when its ctx was, so
// a shutdown does not read as a failure.
func failure(ctx context.Context, err error) domain.RunResult {
	if ctx.Err() != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return domain.RunResult{Status: domain.RunCancelled, Error: err.Error()}
	}

	return domain.RunResult{Status: domain.RunFailed, Error: err.Error()}
}

// gatewayToken signs the run's task token: what the gateway serves it (its bundle, its tools)
// and until when. A Jonah review's window is in whole seconds, its end rounded up, so the window
// the gateway serves holds the review's.
func (e *Engine) gatewayToken(ctx context.Context, req domain.RunRequest, runID domain.RunID) (string, error) {
	key, err := e.secrets.Resolve(ctx, e.cfg.GatewayKeyRef)
	if err != nil {
		return "", fmt.Errorf("gateway key: %w", err)
	}

	token, err := domain.SignTaskToken([]byte(key), domain.TaskClaims{
		Kind: req.Bundle, OrgID: req.OrgID, FactoryID: req.FactoryID, VersionID: req.Version, ItemID: req.Item, Station: req.Station,
		Agent: req.Agent, RunID: runID, Connections: req.Connections, Model: req.Model, DeniedCommands: req.DeniedCommands,
		Branch: req.Branch, Base: req.Base, WindowStart: unixSeconds(req.WindowStart, false), WindowEnd: unixSeconds(req.WindowEnd, true),
		Manual: req.Manual, ExpiresAt: e.clock.Now().Add(req.Timeout + e.cfg.LeaseTTL).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("gateway token: %w", err)
	}

	return token, nil
}

// unixSeconds is t in unix seconds, rounded up with up, and 0 for the zero time.
func unixSeconds(t time.Time, up bool) int64 {
	if t.IsZero() {
		return 0
	}

	s := t.Unix()
	if up && t.After(time.Unix(s, 0)) {
		s++
	}

	return s
}

// heartbeat extends the lease every HeartbeatEvery until ctx ends. When the lease is lost it
// stops the task and reports true.
func (e *Engine) heartbeat(ctx context.Context, lease domain.Lease, stopTask context.CancelFunc) bool {
	t := e.clock.NewTicker(e.cfg.HeartbeatEvery)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-t.C():
		}

		now := e.clock.Now()

		_, err := e.store.HeartbeatLease(ctx, lease, now, now.Add(e.cfg.LeaseTTL))

		switch {
		case errors.Is(err, ports.ErrLeaseLost):
			stopTask()
			return true
		case err != nil && ctx.Err() == nil:
			e.log.WarnContext(ctx, "heartbeat lease", slog.String("item", string(lease.ItemID)), slog.Any("error", err))
		}
	}
}

// finishRun records a run's result.
func (e *Engine) finishRun(ctx context.Context, id domain.RunID, res domain.RunResult) {
	ctx, cancel := cleanup(ctx)
	defer cancel()

	if err := e.store.FinishRun(ctx, id, res, e.clock.Now()); err != nil {
		e.log.ErrorContext(ctx, "record run result", slog.String("run", string(id)), slog.Any("error", err))
	}
}
