package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// maxAttempts is one run plus one retry when the output breaks the station's contract or
// fails a command check.
const maxAttempts = 2

// errCheckFailed means a station's command check ran and exited non-zero.
var errCheckFailed = errors.New("command check failed")

// outputError is an output that failed its checks: a broken contract, an unusable verdict or a
// failed command check. The agent gets one more attempt with the reason.
type outputError struct{ err error }

func (o *outputError) Error() string { return o.err.Error() }
func (o *outputError) Unwrap() error { return o.err }

// claimed is an item this instance holds a lease on at an agent station.
type claimed struct {
	line    domain.Line
	station domain.Station
	item    domain.Item
	lease   domain.Lease
	agent   domain.AgentSpec
}

// work runs the agent on a claimed item until the item moves on, is blocked, is released (at
// shutdown, or cancelled by the weekly budget) or the lease is lost to another instance. The
// claim's runs share one sandbox, deleted when work returns.
func (e *Engine) work(ctx context.Context, l domain.Line, st domain.Station, it domain.Item, lease domain.Lease) {
	c := claimed{line: l, station: st, item: it, lease: lease}
	log := e.log.With(slog.String("factory", string(it.FactoryID)), slog.String("item", string(it.ID)), slog.String("station", st.ID))

	name, err := st.AgentFor(it.Lane)
	if err != nil {
		e.block(ctx, log, c, err.Error())
		return
	}

	if c.agent, err = agentSpec(l, name); err != nil {
		e.block(ctx, log, c, err.Error())
		return
	}

	// The notes people left since the item's last run; a retry adds those left meanwhile.
	notes, err := e.withNotes(ctx, it.ID, nil)
	if err != nil {
		e.block(ctx, log, c, err.Error())
		return
	}

	var b box
	defer e.deleteBox(ctx, log, &b)

	rejection := ""

	for attempt := 1; ; attempt++ {
		res, verdict, err := e.runOnce(ctx, c, &b, feedbackOf(notes, rejection))

		var rejected *outputError

		switch {
		case errors.Is(context.Cause(ctx), errLeaseTaken):
			return // a person moved the item; its run is finished as cancelled
		case errors.Is(err, ports.ErrLeaseLost):
			log.WarnContext(ctx, "lease lost; another instance or a person owns the item now")
			return
		case ctx.Err() != nil:
			e.release(ctx, log, c)
			return
		case errors.As(err, &rejected):
		case err != nil:
			e.block(ctx, log, c, err.Error())
			return
		case res.Status != domain.RunSucceeded:
			e.block(ctx, log, c, fmt.Sprintf("run %s: %s", res.Status, res.Error))
			return
		default:
			e.finish(ctx, log, c, res.Output, verdict)
			return
		}

		if !e.outputFailed(ctx, log, c, attempt, rejected.err) {
			return
		}

		if notes, err = e.withNotes(ctx, it.ID, notes); err != nil {
			log.WarnContext(ctx, "notes left during the run do not reach its retry", slog.Any("error", err))
		}

		rejection = "Your previous attempt was rejected: " + rejected.err.Error() +
			"\nFix that and return output that satisfies the station contract."
	}
}

// outputFailed records an output that failed its checks and reports whether to try again;
// when not, it has blocked the item.
func (e *Engine) outputFailed(ctx context.Context, log *slog.Logger, c claimed, attempt int, err error) bool {
	retryable := errors.Is(err, ports.ErrContractViolation) || errors.Is(err, domain.ErrBadVerdict) ||
		errors.Is(err, errCheckFailed)
	if !retryable {
		e.block(ctx, log, c, "check output: "+err.Error())
		return false
	}

	if !errors.Is(err, errCheckFailed) { // a failed command check has its own check_failed event
		ev := event(c.item, domain.EventContractFailed, e.clock.Now(), domain.ActorEngine,
			map[string]any{"contract": c.station.Output, "error": err.Error(), "attempt": attempt})
		if aerr := e.store.AppendEvents(ctx, ev); aerr != nil {
			log.ErrorContext(ctx, "append contract_failed event", slog.Any("error", aerr))
		}
	}

	if attempt < maxAttempts {
		return true
	}

	e.block(ctx, log, c, fmt.Sprintf("output failed its checks %d times: %v", attempt, err))

	return false
}

// check validates an output against the station's contract and, at a gate, reads its verdict.
func (e *Engine) check(ctx context.Context, c claimed, output json.RawMessage) (*domain.Verdict, error) {
	if c.station.Output != "" {
		if err := e.contracts.Validate(ctx, c.line.VersionID, c.station.Output, output); err != nil {
			return nil, fmt.Errorf("contract %s: %w", c.station.Output, err)
		}
	}

	if c.station.Gate == nil {
		return nil, nil // no verdict outside a gate
	}

	v, err := domain.ParseVerdict(output)
	if err != nil {
		return nil, err
	}

	return &v, nil
}

// runOnce records a run and, while heartbeating the lease, runs it in the claim's sandbox (b,
// created and prepared by the first run), checks its output and runs the station's checks. It
// returns ErrLeaseLost when another instance took the item, an *outputError when the output
// failed its checks, and any other error when the run could not be carried out.
func (e *Engine) runOnce(ctx context.Context, c claimed, b *box, feedback string) (domain.RunResult, *domain.Verdict, error) {
	runID, err := e.store.StartRun(ctx, ports.NewRun{
		FactoryID: c.line.FactoryID, DefinitionVersionID: c.line.VersionID, ItemID: c.item.ID, Station: c.station.ID,
		Agent: c.agent.Name, Model: modelOf(c), StartedAt: e.clock.Now(),
	})
	if err != nil {
		return domain.RunResult{}, nil, fmt.Errorf("record run: %w", err)
	}

	taskCtx, stop := context.WithTimeout(ctx, e.cfg.TaskTimeout)
	defer stop()

	// Heartbeat from before the start: pulling the image, preparing the sandbox and the checks
	// can all outlast the TTL.
	var (
		beats sync.WaitGroup
		lost  bool
	)

	beatCtx, stopBeat := context.WithCancel(taskCtx)

	beats.Go(func() { lost = e.heartbeat(beatCtx, c.lease, stop) })

	res, verdict, err := e.runInBox(taskCtx, c, runID, b, feedback)

	stopBeat()
	beats.Wait()

	if lost {
		return res, nil, ports.ErrLeaseLost
	}

	return res, verdict, err
}

// runInBox opens the claim's sandbox when the run is its first, runs the agent there, checks
// its output and runs the station's checks.
func (e *Engine) runInBox(ctx context.Context, c claimed, runID domain.RunID, b *box, feedback string) (domain.RunResult, *domain.Verdict, error) {
	token, err := e.gatewayToken(ctx, c, runID)

	var (
		env   runEnv
		scrub scrubber
	)

	if err == nil {
		env, err = e.runEnv(ctx, c, runID)
		scrub = newScrubber(append(env.secrets, token)...)
	}

	if err == nil {
		prepareEnv := maps.Clone(env.prepare)
		prepareEnv[envGatewayToken] = token
		err = e.open(ctx, c, runID, b, prepareEnv, scrub)
	}

	if err != nil {
		e.finishRun(ctx, runID, failure(ctx, err))
		return domain.RunResult{}, nil, err
	}

	agentEnv := maps.Clone(env.agent)
	agentEnv[envGatewayToken] = token

	if f := joinParts(feedback, workspaceNote(c, b, env)); f != "" {
		agentEnv[envFeedback] = f
	}

	res := e.agent(ctx, c, runID, b, agentEnv, scrub)
	e.finishRun(ctx, runID, res)

	if res.Status != domain.RunSucceeded {
		return res, nil, nil
	}

	verdict, err := e.check(ctx, c, res.Output)
	if err != nil {
		return res, nil, &outputError{err: err}
	}

	if err := e.runChecks(ctx, c, runID, b, scrub); err != nil {
		return res, nil, err
	}

	return res, verdict, nil
}

// agent runs `goal-agent run` in the claim's sandbox with env, relays the events it reports and
// returns its result. How the command ended (cancelled, timed out, crashed) wins over what the
// worker reported; what reaches the store is scrubbed of the run's secrets.
func (e *Engine) agent(ctx context.Context, c claimed, runID domain.RunID, b *box, env map[string]string, scrub scrubber) domain.RunResult {
	var fin *domain.WorkerFinished

	started := e.clock.Now()

	st, err := execLines(ctx, b.sandbox, ports.Exec{Argv: argvAgent, Env: env}, func(line []byte) {
		ev, f, err := domain.DecodeWorkerLine(line, e.clock.Now())

		switch {
		case errors.Is(err, domain.ErrNotWorkerLine):
		case err != nil:
			e.log.DebugContext(ctx, "ignored worker stdout line", slog.String("run", string(runID)), slog.Any("error", err))
		case f != nil:
			fin = f

			for _, d := range f.Denials(e.clock.Now()) {
				e.relay(ctx, c, runID, d, scrub)
			}
		default:
			e.relay(ctx, c, runID, *ev, scrub)
		}
	})

	var res domain.RunResult
	if fin != nil {
		res = fin.Result()
	}

	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		res.Status, res.Output, res.Error = domain.RunFailed, nil, fmt.Sprintf("timed out after %s", e.cfg.TaskTimeout)
	case ctx.Err() != nil:
		res.Status, res.Output, res.Error = domain.RunCancelled, nil, ctx.Err().Error()
	case err != nil:
		res.Status, res.Output, res.Error = domain.RunFailed, nil, "agent: "+err.Error()
	case fin == nil:
		res.Status, res.Error = domain.RunFailed, fmt.Sprintf("worker exited with code %d without a result", st.Code)
		if tail := strings.TrimSpace(st.StderrTail); tail != "" {
			res.Error += ": " + tail
		}
	}

	res.Error = scrub.string(res.Error)

	e.log.InfoContext(ctx, "sandbox exec", slog.String("run", string(runID)), slog.String("step", "agent"),
		slog.Int("exit", st.Code), slog.Float64("seconds", e.since(started)))

	return res
}

// relay records one event the agent reported, by the agent, for its run.
func (e *Engine) relay(ctx context.Context, c claimed, runID domain.RunID, ev domain.Event, scrub scrubber) {
	scrub.value(ev.Data)

	rec := event(c.item, ev.Kind, ev.At, c.agent.Name, ev.Data)
	rec.RunID = runID

	if err := e.store.AppendEvents(ctx, rec); err != nil && ctx.Err() == nil {
		e.log.WarnContext(ctx, "record agent event", slog.String("item", string(c.item.ID)), slog.Any("error", err))
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

// exitTimedOut is the exit code a check reports when it ran out of time, as timeout(1) does.
const exitTimedOut = 124

// runChecks runs the station's command checks in order in the claim's sandbox, in its workspace,
// with no credential, and stops at the first that fails, which is returned as an *outputError.
// The exit code is the runtime's: nothing the agent left in the sandbox can report a pass. What a
// check prints is scrubbed of the run's secrets, which the agent may have left in a file.
func (e *Engine) runChecks(ctx context.Context, c claimed, runID domain.RunID, b *box, scrub scrubber) error {
	for _, cmd := range c.station.Checks {
		started := e.clock.Now()
		out := &tail{max: maxCheckOutput}

		st, err := execLines(ctx, b.sandbox, ports.Exec{
			Argv: []string{"sh", "-c", cmd}, Env: map[string]string{"CI": "true"}, Dir: workspaceDir, Timeout: e.cfg.CheckTimeout,
		}, out.add)
		if err != nil {
			return fmt.Errorf("run check %q: %w", cmd, err)
		}

		code, output := st.Code, joinParts(out.String(), st.StderrTail)
		if st.TimedOut {
			code, output = exitTimedOut, joinParts(output, fmt.Sprintf("timed out after %s", e.cfg.CheckTimeout))
		}

		output = scrub.string(cut(strings.TrimSpace(output), maxCheckOutput))
		kind := domain.EventCheckPassed
		data := map[string]any{"command": cmd, "exit_code": code, "duration_ms": e.clock.Now().Sub(started).Milliseconds()}

		if code != 0 {
			kind, data["output"] = domain.EventCheckFailed, output
		}

		e.record(ctx, c, runID, kind, data)
		e.log.InfoContext(ctx, "sandbox exec", slog.String("run", string(runID)), slog.String("step", "check"),
			slog.Int("exit", code), slog.Float64("seconds", e.since(started)))

		if code != 0 {
			return &outputError{err: fmt.Errorf("%w: `%s` exited %d; the end of its output:\n%s", errCheckFailed, cmd, code, output)}
		}
	}

	return nil
}

// maxCheckOutput bounds the check output kept in an event and quoted to the agent.
const maxCheckOutput = 4000

// cut keeps the last n bytes of s, where a failing command reports why.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return "…" + strings.ToValidUTF8(s[len(s)-n:], "")
}

// feedbackOf is what goald adds to the brief for one attempt: the notes people left and, on a
// retry, why the previous attempt was rejected.
func feedbackOf(notes []domain.Event, rejection string) string {
	return joinParts(notesSection(notes), rejection)
}

// joinParts joins the non-empty parts with a blank line.
func joinParts(parts ...string) string {
	return strings.Join(slices.DeleteFunc(parts, func(s string) bool { return s == "" }), "\n\n")
}

// workspaceNote tells the agent what its working directory holds, as the sandbox's prepare left
// it, and whether git can push from it; nothing without a repository.
func workspaceNote(c claimed, b *box, env runEnv) string {
	r := c.line.Runtime.Repository
	if r.URL == "" {
		return ""
	}

	state := "a new branch from the default branch"
	if b.existed {
		state = "it already exists: it holds the work of earlier runs of this item"
	}

	host := r.URL
	if u, err := url.Parse(r.URL); err == nil {
		host = u.Host
	}

	return fmt.Sprintf("## Workspace\n\nYour working directory is a clone of %s with branch `%s` checked out (%s). %s",
		r.URL, c.item.Branch(), state, env.pushNote(host))
}

// taskEnv resolves the variables every agent run gets in a version without a model connection
// (Config.TaskEnv).
func (e *Engine) taskEnv(ctx context.Context) (map[string]string, error) {
	env := make(map[string]string, len(e.cfg.TaskEnv)+2)

	for name, ref := range e.cfg.TaskEnv {
		v, err := e.secrets.Resolve(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("task env %s: %w", name, err)
		}

		env[name] = v
	}

	return env, nil
}

// gatewayToken signs the run's task token: what the gateway serves it (its bundle, its tools)
// and until when.
func (e *Engine) gatewayToken(ctx context.Context, c claimed, runID domain.RunID) (string, error) {
	key, err := e.secrets.Resolve(ctx, e.cfg.GatewayKeyRef)
	if err != nil {
		return "", fmt.Errorf("gateway key: %w", err)
	}

	token, err := domain.SignTaskToken([]byte(key), domain.TaskClaims{
		OrgID: c.line.OrgID, FactoryID: c.line.FactoryID, VersionID: c.line.VersionID, ItemID: c.item.ID, Station: c.station.ID, Agent: c.agent.Name, RunID: runID,
		Connections: c.agent.Connections, ExpiresAt: e.clock.Now().Add(e.cfg.TaskTimeout + e.cfg.LeaseTTL).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("gateway token: %w", err)
	}

	return token, nil
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

// finish moves an item whose output passed its checks: through the gate's verdict when the
// station has one, otherwise to the next station. The run was checked against the version it
// started on; where the item goes next is the current line's say, so an item finishing on v1
// after v2 was published moves along v2.
func (e *Engine) finish(ctx context.Context, log *slog.Logger, c claimed, output json.RawMessage, v *domain.Verdict) {
	next := e.lineNow(c.line, c.station.ID)

	t := ports.Transition{
		ItemID: c.item.ID, From: domain.Position{Station: c.station.ID, State: domain.StateInProgress},
		Lease: &c.lease, Outcome: domain.LeaseAdvanced, Lane: domain.LaneFrom(output), OutputStation: c.station.ID,
		Output: output, At: e.clock.Now(),
	}

	var err error

	switch {
	case v != nil && v.Decision == domain.Reject:
		err = e.gateReject(c, next, *v, &t)
	case v != nil:
		t.Events = append(t.Events, event(c.item, domain.EventGateApproved, t.At, c.agent.Name, nil))
		err = e.advance(next, c.item, c.agent.Name, &t)
	default:
		err = e.advance(next, c.item, c.agent.Name, &t)
	}

	if err != nil {
		e.block(ctx, log, c, err.Error())
		return
	}

	e.transition(ctx, log, t)
}

// advance points t at the station after the item's current one.
func (e *Engine) advance(l domain.Line, it domain.Item, actor string, t *ports.Transition) error {
	next, err := l.Next(it.Station)
	if err != nil {
		return err
	}

	pos, events, doneAt := e.arrive(it, next, actor)
	t.To, t.DoneAt = pos, doneAt
	t.Events = append(t.Events, events...)

	return nil
}

// gateReject points t back at the gate's onReject station on line l, or at a human once the
// item has used up the rework allowance of the gate it ran at. The rejection's event keeps the
// verdict's reason and, when the reviewer gave one, its category (the agent statistics group by it).
func (e *Engine) gateReject(c claimed, l domain.Line, v domain.Verdict, t *ports.Transition) error {
	it, gate := c.item, *c.station.Gate
	data := map[string]any{keyReason: v.Reason, "rework_count": it.ReworkCount}

	if v.Category != "" {
		data[keyCategory] = v.Category
	}

	t.Events = append(t.Events, event(it, domain.EventGateRejected, t.At, c.agent.Name, data))

	if !gate.CanRework(it.ReworkCount) {
		t.To, t.Outcome = domain.Position{Station: it.Station, State: domain.StateWaitingHuman}, domain.LeaseEscalated
		t.Events = append(t.Events, event(it, domain.EventWaitingHuman, t.At, domain.ActorEngine,
			map[string]any{keyReason: fmt.Sprintf("rejected after %d reworks (max %d)", it.ReworkCount, gate.MaxRework)}))

		return nil
	}

	target, err := l.ReworkTarget(it.Station)
	if err != nil {
		return err
	}

	pos, events, doneAt := e.arrive(it, target, domain.ActorEngine)
	t.To, t.DoneAt, t.Rework, t.Outcome = pos, doneAt, true, domain.LeaseRejected
	t.Events = append(t.Events, events...)

	return nil
}

// block parks a claimed item as blocked, with the reason in its event, and releases the lease.
func (e *Engine) block(ctx context.Context, log *slog.Logger, c claimed, reason string) {
	now := e.clock.Now()

	e.transition(ctx, log, ports.Transition{
		ItemID: c.item.ID, From: domain.Position{Station: c.station.ID, State: domain.StateInProgress},
		To: domain.Position{Station: c.station.ID, State: domain.StateBlocked}, Lease: &c.lease, Outcome: domain.LeaseBlocked,
		At:     now,
		Events: []domain.Event{event(c.item, domain.EventBlocked, now, domain.ActorEngine, map[string]any{keyReason: reason})},
	})
}

// release returns a claimed item to the queue at its station once ctx has ended: at shutdown,
// or with agent_cancelled when the weekly budget cancelled its run (releaseEvent).
func (e *Engine) release(ctx context.Context, log *slog.Logger, c claimed) {
	now := e.clock.Now()

	e.transition(ctx, log, ports.Transition{
		ItemID: c.item.ID, From: domain.Position{Station: c.station.ID, State: domain.StateInProgress},
		To: domain.Position{Station: c.station.ID, State: domain.StateQueued}, Lease: &c.lease, Outcome: domain.LeaseReleased,
		At:     now,
		Events: []domain.Event{releaseEvent(c.item, context.Cause(ctx), now)},
	})
}

// transition applies t even while ctx is being cancelled; losing the lease or the item to
// another instance is expected and only logged.
func (e *Engine) transition(ctx context.Context, log *slog.Logger, t ports.Transition) {
	ctx, cancel := cleanup(ctx)
	defer cancel()

	_, err := e.store.Transition(ctx, t)

	switch {
	case errors.Is(err, ports.ErrLeaseLost), errors.Is(err, ports.ErrConflict):
		log.WarnContext(ctx, "item changed hands before its transition", slog.String("to", string(t.To.State)), slog.Any("error", err))
	case err != nil:
		log.ErrorContext(ctx, "transition item; the lease will expire and requeue it",
			slog.String("to", string(t.To.State)), slog.Any("error", err))
	default:
		log.InfoContext(ctx, "item moved", slog.String("to_station", t.To.Station), slog.String("to_state", string(t.To.State)))
	}
}

func agentSpec(l domain.Line, name string) (domain.AgentSpec, error) {
	a, ok := l.Agent(name)
	if !ok {
		return domain.AgentSpec{}, fmt.Errorf("agent %q is not defined", name)
	}

	return a, nil
}

func modelOf(c claimed) string {
	if c.agent.Model != "" {
		return c.agent.Model
	}

	return c.line.Runtime.Model
}
