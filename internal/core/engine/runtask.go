package engine

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
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
// A try of a past item (ADR 0039 decision 9) holds no lease and works no item either. Its agent
// gets the model's access only: its environment keeps its env, which the worker gives setup alone,
// and loses its files and MCP servers. Once the agent ended, its change against the commit prepare
// checked out is read from the box (domain.PastTryDiffScript) into the result's Diff, and, when it
// succeeded, the station's checks the request names run there (ADR 0039 decision 10), the first
// that fails coming back as the result's FailedCheck. A diff that cannot be read or a check that
// cannot be run is an error, as the try's infrastructure failed.
//
// A grade (a run of kind try on a domain.TaskKindGrade bundle, ADR 0039 decision 10) is recorded
// as a try is, so its cost is Jonah's too. Its token names the try and the result it grades, which
// the gateway serves it; its agent gets the model's access alone, as a Jonah run's does. It reads
// no diff and runs no check: the result carries the worker's answer (domain.ParseGradeAnswer
// reads it) and its cost.
//
// A Jonah run (ADR 0034 decision 1) holds no lease and works no item, so no item event is written
// for it: its sandbox's prepare gets no repository's git login, as it clones none, and its agent
// only the model key. Without pins of its own, it pins the newest snapshot of each of
// its line's knowledge sources scoped to no agent, station, lane or work type.
//
// A check (ADR 0040 decision 19) holds no lease either and works no item: its agent gets its
// environment, tools and MCP servers, its token names its trigger, and it pins no knowledge and
// clones no repository (RunRequest.Check refuses both). The result names
// the run's record (RunResult.RunID) once one was started, with an error too. The line's claims run through the same lifecycle (runOnce),
// with a sandbox their attempts share and their output's checks under the same heartbeat.
func (e *Engine) RunTask(ctx context.Context, req domain.RunRequest) (domain.RunResult, error) {
	if err := req.Check(); err != nil {
		return domain.RunResult{}, err
	}

	req.Timeout = cmp.Or(req.Timeout, e.cfg.TaskTimeout)
	ctx = domain.WithOrg(ctx, req.OrgID)

	if req.Kind == domain.RunKindTry {
		req.Environment.Files, req.Environment.MCP = nil, nil
	}

	// A Jonah review reads the line's knowledge that applies to every run (ADR 0039 decision 4).
	if req.Kind == domain.RunKindJonah && req.Knowledge == nil {
		e.mu.RLock()
		l, ok := e.lines[req.FactoryID]
		e.mu.RUnlock()

		if ok && l.OrgID == req.OrgID {
			pins, err := e.pinSources(ctx, l, domain.RuleTarget{})
			if err != nil {
				return domain.RunResult{}, err
			}

			req.Knowledge = pins
		}
	}

	var b box
	defer e.deleteBox(ctx, e.log.With(slog.String("factory", string(req.FactoryID)), slog.String("kind", string(req.Kind))), &b)

	// A station run's events go on its item, as at its station.
	var it domain.Item
	if req.Item != "" {
		it = domain.Item{ID: req.Item, FactoryID: req.FactoryID, Station: req.Station}
	}

	return e.runTask(ctx, runTask{RunRequest: req, item: it, box: &b})
}

// runTask is one run of the lifecycle: its request and what the caller adds to it.
type runTask struct {
	domain.RunRequest

	// item is the item the run works, whose events record it; zero for a Jonah run.
	item domain.Item
	// box is the sandbox the run opens when it has none, and runs in.
	box *box
	// work is the bundle's brief as the gateway serves it (spec.brief: the item's Work(),
	// internal/gateway/bundle.go); the two must match, as the run's record (RecordRunInput) keeps
	// it as the brief the agent was given. Empty for a run without an item.
	work string
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
		res, err := e.runInBox(taskCtx, t, runID)
		res.RunID = runID

		return res, err
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
	res.RunID = runID

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
		env, err = e.runEnv(ctx, t.RunRequest, runID)
		scrub = newScrubber(append(env.secrets, token)...)
	}

	var line *prepared

	if err == nil {
		prepareEnv := maps.Clone(env.prepare)
		prepareEnv[envGatewayToken] = token
		line, err = e.open(ctx, t, runID, prepareEnv, scrub)
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

	feedback := joinParts(t.Brief, note)
	if feedback != "" {
		agentEnv[envFeedback] = feedback
	}

	e.recordInput(ctx, t, runID, line, scrub.string(joinParts(t.work, feedback)))

	res, lost := e.agent(ctx, t, runID, agentEnv, scrub)

	if lost == nil && t.Kind == domain.RunKindTry && t.Bundle == domain.TaskKindTry {
		lost = e.tryEnded(ctx, t, runID, &res, scrub)
	}

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
	tally := domain.NewKnowledgeTally(t.Knowledge)

	defer e.countReads(ctx, t, runID, tally, scrub)

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
			countTouch(tally, *ev)
			e.relay(ctx, t, runID, *ev, scrub)
		}
	})

	var (
		res  domain.RunResult
		lost error
	)

	if fin != nil {
		res = fin.Result(t.Repository.URL)
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

// tryEnded reads what a try's agent left in its box into res once it ended: its diff unless it
// was cancelled, and, when it succeeded, the first of its checks that fails. An error when either
// could not be read or run.
func (e *Engine) tryEnded(ctx context.Context, t runTask, runID domain.RunID, res *domain.RunResult, scrub scrubber) error {
	if res.Status == domain.RunCancelled {
		return nil
	}

	var err error
	if res.Diff, err = e.tryDiff(ctx, t, runID, scrub); err != nil || res.Status != domain.RunSucceeded {
		return err
	}

	res.FailedCheck, err = e.tryChecks(ctx, t, runID, scrub)

	return err
}

// tryDiffTimeout bounds reading a try's diff from its box.
const tryDiffTimeout = 2 * time.Minute

// tryDiff is a try's change against the commit prepare checked out, as domain.PastTryDiffScript
// prints it in the box: scrubbed of the run's secrets, and an error when the command fails or prints more
// than domain.MaxPastTryDiff bytes.
func (e *Engine) tryDiff(ctx context.Context, t runTask, runID domain.RunID, scrub scrubber) (string, error) {
	p, err := t.box.sandbox.Exec(ctx, ports.Exec{Argv: argvDiff, Env: map[string]string{}, Dir: workspaceDir, Timeout: tryDiffTimeout})
	if err != nil {
		return "", scrub.err(fmt.Errorf("read the try's diff: %w", err))
	}

	out, readErr := io.ReadAll(io.LimitReader(p.Stdout(), domain.MaxPastTryDiff+1))
	_, _ = io.Copy(io.Discard, p.Stdout())

	st, err := p.Wait()

	switch {
	case err != nil:
		return "", scrub.err(fmt.Errorf("read the try's diff: %w", err))
	case readErr != nil:
		return "", fmt.Errorf("read the try's diff: %w", readErr)
	case st.TimedOut || st.Code != 0:
		return "", fmt.Errorf("read the try's diff: git diff exited %d: %s", st.Code, scrub.string(strings.TrimSpace(st.StderrTail)))
	case len(out) > domain.MaxPastTryDiff:
		return "", fmt.Errorf("read the try's diff: it is over %d bytes", domain.MaxPastTryDiff)
	}

	e.log.InfoContext(ctx, "sandbox exec", slog.String("run", string(runID)), slog.String("step", "diff"), slog.Int("bytes", len(out)))

	return scrub.string(string(out)), nil
}

// countTouch adds a tool call the agent reported to the run's knowledge tally.
func countTouch(tally *domain.KnowledgeTally, ev domain.Event) {
	if ev.Kind != domain.EventToolCall {
		return
	}

	tool, _ := ev.Data["tool"].(string)
	p, _ := ev.Data["path"].(string)
	tally.Add(tool, p)
}

// countReadsTimeout bounds counting a run's knowledge reads once the run ended.
const countReadsTimeout = 10 * time.Second

// countReads records the knowledge_read event of a run that read or searched its pinned knowledge
// sources and adds those touches to its factory's rollup for today (ADR 0038 decision 10). It is
// best effort: a failure is logged and never changes the run, and it counts even when the run's
// ctx ended.
func (e *Engine) countReads(ctx context.Context, t runTask, runID domain.RunID, tally *domain.KnowledgeTally, scrub scrubber) {
	sources := tally.Sources()
	if len(sources) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), countReadsTimeout)
	defer cancel()

	e.recordReads(ctx, t, runID, tally, scrub)

	if e.knowledge == nil {
		return
	}

	ids := make(map[string]domain.KnowledgeSourceID, len(sources))

	for _, name := range sources {
		s, err := e.knowledge.Source(ctx, name)
		if err != nil {
			e.log.WarnContext(ctx, "knowledge reads not counted", slog.String("run", string(runID)), slog.String("source", name), slog.Any("error", err))
			continue
		}

		ids[name] = s.ID
	}

	reads := tally.Reads(ids, t.FactoryID, e.clock.Now())
	if len(reads) == 0 {
		return
	}

	if err := e.knowledge.AddReads(ctx, reads); err != nil {
		e.log.WarnContext(ctx, "knowledge reads not counted", slog.String("run", string(runID)), slog.Int("counts", len(reads)), slog.Any("error", err))
	}
}

// recordReads appends the run's knowledge_read event, by its agent, listing what tally touched. A
// run without an item (a Jonah run) records none; a failure is only logged.
func (e *Engine) recordReads(ctx context.Context, t runTask, runID domain.RunID, tally *domain.KnowledgeTally, scrub scrubber) {
	if t.item.ID == "" {
		return
	}

	touched := tally.Touched()
	sources := make([]any, 0, len(touched))

	for _, s := range touched {
		src := map[string]any{"source": s.Source, "reads": anyStrings(s.Reads), "searches": anyStrings(s.Searches)}
		if s.More > 0 {
			src["more"] = s.More
		}

		sources = append(sources, src)
	}

	data := map[string]any{"sources": sources}
	scrub.value(data)

	rec := event(t.item, domain.EventKnowledgeRead, e.clock.Now(), t.Agent, data)
	rec.RunID = runID

	if err := e.store.AppendEvents(ctx, rec); err != nil {
		e.log.WarnContext(ctx, "knowledge read event not recorded", slog.String("run", string(runID)), slog.Any("error", err))
	}
}

// anyStrings is ss as the []any of a decoded JSON list, so scrubber.value reaches each string.
func anyStrings(ss []string) []any {
	out := make([]any, 0, len(ss))
	for _, s := range ss {
		out = append(out, s)
	}

	return out
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
		Knowledge: req.Knowledge, Branch: req.Branch, Base: req.Base, WindowStart: unixSeconds(req.WindowStart, false), WindowEnd: unixSeconds(req.WindowEnd, true),
		Manual: req.Manual, NoEnvironment: req.NoEnvironment, TryOf: req.TryOf, GradeOf: req.GradeOf, Trigger: req.Trigger, ExpiresAt: e.clock.Now().Add(req.Timeout + e.cfg.LeaseTTL).Unix(),
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

// recordInput records what the run was given, for a later try to rerun it (ADR 0039, decision
// 8): brief, joined as the worker's prompt joins the bundle's brief and GOAL_FEEDBACK, and, when
// this run prepared the box, the repositories prepare reported with their heads (ADR 0041
// decision 5); a prepare that reports only the single heads reports them on the task's
// repository. A retry in its claim's box (line nil) records none. A failure is logged and the
// run goes on.
func (e *Engine) recordInput(ctx context.Context, t runTask, runID domain.RunID, line *prepared, brief string) {
	in := domain.RunInput{Brief: brief}
	if line != nil {
		in.Repositories = domain.RunRepositories(line.Repositories, t.Repository.URL,
			domain.WorkerRepository{BaseCommit: line.BaseHead, BranchHead: line.BranchHead})
	}

	if err := e.store.RecordRunInput(ctx, runID, in); err != nil && ctx.Err() == nil {
		e.log.WarnContext(ctx, "record run input", slog.String("run", string(runID)), slog.Any("error", err))
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
