package engine

import (
	"cmp"
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
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
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

// checked is what an output that passed its checks says to finish: the gate's verdict, at a
// gate, the plan that makes the item an assembly, if any, and next, the current line it was
// read against, which also decides where the item goes (finish).
type checked struct {
	verdict *domain.Verdict
	plan    []domain.NewPart
	// after are the open items the output says the work must follow (followed).
	after []domain.Item
	next  domain.Line
}

// claimed is an item this instance holds a lease on at an agent station.
type claimed struct {
	line    domain.Line
	station domain.Station
	item    domain.Item
	lease   domain.Lease
	agent   domain.AgentSpec
	// What the organization policy decided at run start (ADR 0024), the same for each of the
	// claim's runs: the model, the commands and tools added to the agent's deny rules, how long
	// one run may take, how long the box may live (0: no policy limit) and whether an approving
	// gate whose action is merge leaves the merge to a person (noMerge).
	model    string
	deny     []string
	timeout  time.Duration
	boxLimit time.Duration
	noMerge  bool
	// branch and base are the runs' branch and the branch it is cut from and its pull request
	// goes into, decided at the claim's start (integrate); both empty for the item's branch on the
	// default branch, as for every item that is not a part on an integration branch.
	branch, base string
}

// webTools are the Claude Code tools of the builtins.web group, which the organization policy's
// Read the web and docs default denies when it is off or ask.
var webTools = []string{"WebFetch", "WebSearch"}

// work runs the agent on a claimed item until the item moves on, is blocked, is released (at
// shutdown, or cancelled by the weekly budget) or the lease is lost to another instance. The
// claim's runs share one sandbox, deleted when work returns; a run whose sandbox was lost
// (ports.ErrSandboxLost) runs again in a new one (retryLost).
func (e *Engine) work(ctx context.Context, l domain.Line, st domain.Station, it domain.Item, lease domain.Lease) {
	c := claimed{line: l, station: st, item: it, lease: lease}
	log := e.log.With(slog.String("factory", string(it.FactoryID)), slog.String("item", string(it.ID)), slog.String("station", st.ID))

	if !e.admitted(ctx, log, &c) {
		return
	}

	// What people said since the item's last run: their notes (a retry adds those left
	// meanwhile), their answers to its agents' questions (ADR 0018) and their decisions on its
	// agents' tool calls (ADR 0031).
	said, err := e.peopleSaid(ctx, it.ID)
	if err != nil {
		e.block(ctx, log, c, err.Error())
		return
	}

	// What the claim's every attempt hears: its assembly, the factory's open work.
	preloaded, err := e.preloaded(ctx, l, st, it)
	if err != nil {
		e.block(ctx, log, c, err.Error())
		return
	}

	var b box
	defer e.deleteBox(ctx, log, &b)

	rejection, attempt, lost := "", 1, 0

	for {
		res, chk, err := e.runOnce(ctx, c, &b, joinParts(preloaded, feedbackOf(said, rejection)))

		if e.cut(ctx, log, c, err) {
			return
		}

		var rejected *outputError

		switch {
		case errors.Is(err, ports.ErrSandboxLost):
			if lost++; !e.retryLost(ctx, log, c, &b, lost, err) {
				return
			}

			continue
		case errors.As(err, &rejected):
		case err != nil:
			e.block(ctx, log, c, err.Error())
			return
		case res.Status != domain.RunSucceeded:
			e.block(ctx, log, c, fmt.Sprintf("run %s: %s", res.Status, res.Error))
			return
		default:
			e.finish(ctx, log, c, res.Output, chk)
			return
		}

		if !e.outputFailed(ctx, log, c, attempt, rejected.err) {
			return
		}

		if said.notes, err = e.withNotes(ctx, it.ID, said.notes); err != nil {
			log.WarnContext(ctx, "notes left during the run do not reach its retry", slog.Any("error", err))
		}

		rejection = "Your previous attempt was rejected: " + rejected.err.Error() +
			"\nFix that and return output that satisfies the station contract."
		attempt++
	}
}

// cut reports whether the claim ended under its run or wait: a person moved the item (its run is
// finished as cancelled), the lease went to another instance (err is ErrLeaseLost), or ctx ended,
// when the item has been released to its queue.
func (e *Engine) cut(ctx context.Context, log *slog.Logger, c claimed, err error) bool {
	switch {
	case errors.Is(context.Cause(ctx), errLeaseTaken):
		return true
	case errors.Is(err, ports.ErrLeaseLost):
		log.WarnContext(ctx, "lease lost; another instance or a person owns the item now")
		return true
	case ctx.Err() != nil:
		e.release(ctx, log, c)
		return true
	}

	return false
}

// retryLost follows the lost-th run of a claim whose sandbox was lost (err, scrubbed): a
// redeploy, a box restart or a network blip, not the item's fault. It deletes the box, so the
// next run creates a new one (the branch keeps what was pushed), and below the line's
// flow.retry.transient.attempts records run_retried and waits TransientBackoff, doubled with
// each loss, while heartbeating the lease. It reports whether to run again; when not, the item is blocked with err, released at shutdown
// or no longer this instance's.
func (e *Engine) retryLost(ctx context.Context, log *slog.Logger, c claimed, b *box, lost int, err error) bool {
	e.deleteBox(ctx, log, b)
	*b = box{}

	limit := c.line.Flow.Retry.Transient()
	if lost >= limit {
		e.block(ctx, log, c, fmt.Sprintf("the sandbox was lost %d times: %v", lost, err))
		return false
	}

	log.WarnContext(ctx, "sandbox lost; running the item again in a new one", slog.Int("attempt", lost), slog.Any("error", err))

	ev := event(c.item, domain.EventRunRetried, e.clock.Now(), domain.ActorEngine,
		map[string]any{keyReason: err.Error(), "attempt": lost, "max": limit})
	if aerr := e.store.AppendEvents(ctx, ev); aerr != nil {
		log.ErrorContext(ctx, "append run_retried event", slog.Any("error", aerr))
	}

	return !e.cut(ctx, log, c, e.backoff(ctx, c.lease, e.cfg.TransientBackoff<<(lost-1)))
}

// backoff waits d on the clock in equal steps of at most HeartbeatEvery and heartbeats the lease
// after each, so the claim outlives the wait. It returns ErrLeaseLost when the lease is lost, or
// ctx's error when ctx ends first.
func (e *Engine) backoff(ctx context.Context, lease domain.Lease, d time.Duration) error {
	steps := max(1, (d+e.cfg.HeartbeatEvery-1)/e.cfg.HeartbeatEvery)

	t := e.clock.NewTicker(d / steps)
	defer t.Stop()

	for range steps {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C():
		}

		now := e.clock.Now()

		_, err := e.store.HeartbeatLease(ctx, lease, now, now.Add(e.cfg.LeaseTTL))

		switch {
		case errors.Is(err, ports.ErrLeaseLost):
			return err
		case err != nil && ctx.Err() == nil:
			e.log.WarnContext(ctx, "heartbeat lease", slog.String("item", string(lease.ItemID)), slog.Any("error", err))
		}
	}

	return ctx.Err()
}

// admitted finds the claim's agent (agentFor), applies the organization policy to it
// (applyPolicy), decides its runs' branch and base (integrate) and reports whether it may run;
// when not, the item is blocked (no agent, the policy could not be read, or the base could not
// be decided) or waits for a person (the policy refuses its model).
func (e *Engine) admitted(ctx context.Context, log *slog.Logger, c *claimed) bool {
	var err error
	if c.agent, err = agentFor(c.line, c.station, c.item.Lane); err != nil {
		e.block(ctx, log, *c, err.Error())
		return false
	}

	refusal, err := e.applyPolicy(ctx, c)
	if err == nil && refusal == "" {
		err = e.integrate(ctx, log, c)
	}

	switch {
	case err != nil:
		e.block(ctx, log, *c, err.Error())
		return false
	case refusal != "":
		e.escalate(ctx, log, *c, refusal)
		return false
	}

	return true
}

// applyPolicy reads the organization policy once for the claim and applies it as a ceiling over
// the version (ADR 0024 decision 3): the run's model is its agent's, its factory's or the
// policy's default; its deny rules get the policy's denied commands; its run and box take no
// longer than the policy's box time limit. It returns why the claim may not run, when its model
// is not one the policy allows, and an error when the policy cannot be read. Without Policies
// only the version decides.
//
// The tool defaults narrow the agent and never widen it, each through the mechanism that already
// decides it:
//   - Read the web and docs, off or ask: the deny rules also get the builtins.web tools
//     (webTools); ask is served as off, as for builtins, since no tool approval is enforced.
//   - Merge a pull request, off: an approving gate whose action is merge does not merge, and the
//     item waits there for a person's merge (noMerge); review_approval is the version's say.
//   - Use credentials, off: the agent's connections list is emptied, so neither its env nor its
//     task token gets any of them; the runtime's model and repository connections still reach
//     the run, which cannot start without them. attached_only is the agent's list.
//   - Write outside the item's branch is not applied: comments, issues, tickets and messages go
//     through connections' CLIs and MCP servers, and no engine or gateway step can ask a person
//     for one, refuse it or allow it.
func (e *Engine) applyPolicy(ctx context.Context, c *claimed) (string, error) {
	c.model, c.timeout = cmp.Or(c.agent.Model, c.line.Runtime.Model), e.cfg.TaskTimeout

	if e.policies == nil {
		return "", nil
	}

	p, err := e.policies.Get(ctx)
	if err != nil {
		return "", fmt.Errorf("read the organization policy: %w", err)
	}

	c.model = cmp.Or(c.model, p.DefaultModel)
	if c.model != "" && !slices.Contains(p.AllowedModels, c.model) {
		return fmt.Sprintf("the organization policy no longer allows model %s, which agent %s runs on: "+
			"choose one of %s for it, or allow it in the policy", c.model, c.agent.Name, strings.Join(p.AllowedModels, ", ")), nil
	}

	c.deny = slices.Clone(p.DeniedCommands)
	if p.ReadWeb == domain.ToolOff || p.ReadWeb == domain.ToolAsk {
		for _, tool := range webTools {
			if !slices.Contains(c.deny, tool) {
				c.deny = append(c.deny, tool)
			}
		}
	}

	c.noMerge = p.MergePullRequest == domain.ToolOff

	if p.UseCredentials == domain.ToolOff {
		c.agent.Connections = nil
	}

	if p.BoxTimeLimitMinutes > 0 {
		c.boxLimit = time.Duration(p.BoxTimeLimitMinutes) * time.Minute
		c.timeout = min(c.timeout, c.boxLimit)
	}

	return "", nil
}

// escalate hands a claimed item that may not run to a person: it waits at its station with the
// reason in its event, and the lease is released.
func (e *Engine) escalate(ctx context.Context, log *slog.Logger, c claimed, reason string) {
	e.park(ctx, log, c, domain.StateWaitingHuman, domain.LeaseEscalated, domain.EventWaitingHuman, reason)
}

// agentFor is the agent that works on an item of lane at st, or why none can: no agent for the
// lane, or a line that does not run on connections alone (domain.Line.Refusal), refused before
// any run, box or connection.
func agentFor(l domain.Line, st domain.Station, lane string) (domain.AgentSpec, error) {
	name, err := st.AgentFor(lane)
	if err != nil {
		return domain.AgentSpec{}, err
	}

	if reason := l.Refusal(); reason != "" {
		return domain.AgentSpec{}, errors.New(reason)
	}

	return agentSpec(l, name)
}

// outputFailed records an output that failed its checks and reports whether to try again;
// when not, it has blocked the item.
func (e *Engine) outputFailed(ctx context.Context, log *slog.Logger, c claimed, attempt int, err error) bool {
	retryable := errors.Is(err, ports.ErrContractViolation) || errors.Is(err, domain.ErrBadVerdict) ||
		errors.Is(err, domain.ErrBadPlan) || errors.Is(err, domain.ErrBadAfter) || errors.Is(err, errCheckFailed)
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

// check validates an output against the station's contract (of the version the run started
// on), at a gate reads its verdict and, when the output is not a rejection, the items it says
// the work follows (followed) and, when the current line materializes plans, its plan. The
// current line is read once here and finish moves the item along the same one, so a plan is
// assembled only by a line that parsed it.
func (e *Engine) check(ctx context.Context, c claimed, output json.RawMessage) (checked, error) {
	if c.station.Output != "" {
		if err := e.contracts.Validate(ctx, c.line.VersionID, c.station.Output, output); err != nil {
			return checked{}, fmt.Errorf("contract %s: %w", c.station.Output, err)
		}
	}

	chk := checked{next: e.lineNow(c.line, c.station.ID)}

	if c.station.Gate != nil {
		v, err := domain.ParseVerdict(output)
		if err != nil {
			return checked{}, err
		}

		chk.verdict = &v
	}

	if chk.verdict != nil && chk.verdict.Decision == domain.Reject {
		return chk, nil
	}

	after, err := e.followed(ctx, c, output, chk.next.Flow.Dependencies.After)
	if err != nil {
		return checked{}, err
	}

	chk.after = after

	if !chk.next.Flow.Assemblies.Materialize {
		return chk, nil
	}

	plan, err := domain.PlanFrom(output)
	if err != nil {
		return checked{}, err
	}

	chk.plan = plan

	return chk, nil
}

// runOnce records a run and, while heartbeating the lease, runs it in the claim's sandbox (b,
// created and prepared by the first run), checks its output and runs the station's checks. It
// returns ErrLeaseLost when another instance took the item, an *outputError when the output
// failed its checks, and any other error when the run could not be carried out.
func (e *Engine) runOnce(ctx context.Context, c claimed, b *box, feedback string) (domain.RunResult, checked, error) {
	runID, err := e.store.StartRun(ctx, ports.NewRun{
		FactoryID: c.line.FactoryID, DefinitionVersionID: c.line.VersionID, ItemID: c.item.ID, Station: c.station.ID,
		Agent: c.agent.Name, Model: c.model, StartedAt: e.clock.Now(),
	})
	if err != nil {
		return domain.RunResult{}, checked{}, fmt.Errorf("record run: %w", err)
	}

	taskCtx, stop := context.WithTimeout(ctx, c.timeout)
	defer stop()

	// Heartbeat from before the start: pulling the image, preparing the sandbox and the checks
	// can all outlast the TTL.
	var (
		beats sync.WaitGroup
		lost  bool
	)

	beatCtx, stopBeat := context.WithCancel(taskCtx)

	beats.Go(func() { lost = e.heartbeat(beatCtx, c.lease, stop) })

	res, chk, err := e.runInBox(taskCtx, c, runID, b, feedback)

	stopBeat()
	beats.Wait()

	if lost {
		return res, checked{}, ports.ErrLeaseLost
	}

	return res, chk, err
}

// runInBox opens the claim's sandbox when the run is its first, runs the agent there, checks
// its output, runs the station's checks and links a single item to the items its output
// follows (follow).
func (e *Engine) runInBox(ctx context.Context, c claimed, runID domain.RunID, b *box, feedback string) (domain.RunResult, checked, error) {
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
		return domain.RunResult{}, checked{}, err
	}

	agentEnv := maps.Clone(env.agent)
	agentEnv[envGatewayToken] = token

	if f := joinParts(feedback, workspaceNote(c, b, env)); f != "" {
		agentEnv[envFeedback] = f
	}

	res, lost := e.agent(ctx, c, runID, b, agentEnv, scrub)
	e.finishRun(ctx, runID, res)

	if lost != nil {
		return res, checked{}, lost
	}

	if res.Status != domain.RunSucceeded {
		return res, checked{}, nil
	}

	chk, err := e.check(ctx, c, res.Output)
	if err != nil {
		return res, checked{}, &outputError{err: err}
	}

	if err := e.runChecks(ctx, c, runID, b, scrub); err != nil {
		return res, checked{}, err
	}

	if err := e.follow(ctx, c, chk); err != nil {
		return res, checked{}, err
	}

	return res, chk, nil
}

// agent runs `goal-agent run` in the claim's sandbox with env, relays the events it reports and
// returns its result. How the command ended (cancelled, timed out, crashed) wins over what the
// worker reported; what reaches the store is scrubbed of the run's secrets. When the sandbox was
// lost on the way the run failed, and the scrubbed error wrapping ports.ErrSandboxLost is returned too.
func (e *Engine) agent(ctx context.Context, c claimed, runID domain.RunID, b *box, env map[string]string, scrub scrubber) (domain.RunResult, error) {
	var fin *domain.WorkerFinished

	started := e.clock.Now()

	st, err := execLines(ctx, b.sandbox, ports.Exec{Argv: argvAgent, Env: env, Timeout: c.timeout}, func(line []byte) {
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

	var (
		res  domain.RunResult
		lost error
	)

	if fin != nil {
		res = fin.Result()
	}

	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded), err == nil && st.TimedOut:
		res.Status, res.Output, res.Error = domain.RunFailed, nil, fmt.Sprintf("timed out after %s", c.timeout)
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
			return fmt.Errorf("run check %q: %w", cmd, scrub.err(err))
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

// preloaded is what every attempt of a claim hears, read once per claim: the assembly a part
// serves, at a station that preloads it, the factory's open work, and what changed on the default
// branch under the item's paths since its base commit (changedSinceNote).
func (e *Engine) preloaded(ctx context.Context, l domain.Line, st domain.Station, it domain.Item) (string, error) {
	assembly, err := e.assemblyNote(ctx, l, it)
	if err != nil {
		return "", err
	}

	inFlight, err := e.inFlightNote(ctx, l, st, it)
	if err != nil {
		return "", err
	}

	return joinParts(assembly, inFlight, e.changedSinceNote(ctx, l, it)), nil
}

// feedbackOf is what goald adds to the brief for one attempt: the notes people left, their
// answers to the agents' questions, their decisions on the agents' tool calls and, on a retry,
// why the previous attempt was rejected.
func feedbackOf(said saidByPeople, rejection string) string {
	return joinParts(notesSection(said.notes), answersSection(said.answers), toolDecisionsSection(said.tools), rejection)
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
	if c.base != "" {
		state = "a new branch from `" + c.base + "`"
	}

	if b.existed {
		state = "it already exists: it holds the work of earlier runs of this item"
	}

	host := r.URL
	if u, err := url.Parse(r.URL); err == nil {
		host = u.Host
	}

	note := fmt.Sprintf("## Workspace\n\nYour working directory is a clone of %s with branch `%s` checked out (%s). %s",
		r.URL, cmp.Or(c.branch, c.item.Branch()), state, env.pushNote(host))
	if c.item.Landing() {
		return note + fmt.Sprintf("\n\nYou are landing the whole assembly: `%s` is its integration branch, which holds "+
			"every part's merged work, and it lands on the default branch (origin/HEAD). First merge the default branch into `%s` "+
			"and resolve any conflicts there; never rebase it. Then, if no pull request from `%s` into the default branch is open, "+
			"open one with `gh pr create --base <default branch> --head %s`, and name it in your output's pull_request. "+
			"When `%s` has no commits beyond the default branch, open none and report `\"pull_requests\": []` instead.",
			c.branch, c.branch, c.branch, c.branch, c.branch)
	}

	if c.base == "" {
		return note
	}

	refresh := cmp.Or(c.line.Flow.Assemblies.Integration.Refresh, domain.RefreshOnLand)

	return note + fmt.Sprintf("\n\nThis part builds on its assembly's integration branch `%s`, not the default branch: "+
		"open your pull request into `%s`. The integration branch takes in the default branch %s (refresh: %s).",
		c.base, c.base, refreshWhen[refresh], refresh)
}

// refreshWhen says when an integration branch takes in the default branch, by its refresh.
var refreshWhen = map[domain.IntegrationRefresh]string{
	domain.RefreshOnLand:   "when the assembly lands",
	domain.RefreshEachPart: "before each part runs",
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
		Connections: c.agent.Connections, Model: c.model, DeniedCommands: c.deny, Branch: c.branch, Base: c.base, ExpiresAt: e.clock.Now().Add(c.timeout + e.cfg.LeaseTTL).Unix(),
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

// finish moves an item whose output passed its checks, with an event for each declared rule the
// output says was broken (violations) and the paths it lists in the field the line maps
// (flow.dependencies.paths), with the base commit it was planned against (baseCommit): through the gate's verdict when the
// station has one, into state assembly when its output has a plan (assemble), otherwise to the
// next station. An approving gate whose action is merge leaves the merge to a person when the
// organization policy lets no agent merge (claimed.noMerge), and goes back to its own queue when
// a pull request is only not mergeable yet (deferMerge). The run was checked against the version it started on; where the item goes
// next is the current line's say (chk.next), so an item finishing on v1 after v2 was published
// moves along v2.
func (e *Engine) finish(ctx context.Context, log *slog.Logger, c claimed, output json.RawMessage, chk checked) {
	next, v := chk.next, chk.verdict

	t := ports.Transition{
		ItemID: c.item.ID, From: domain.Position{Station: c.station.ID, State: domain.StateInProgress},
		Lease: &c.lease, Outcome: domain.LeaseAdvanced, Lane: domain.LaneFrom(output), OutputStation: c.station.ID,
		Paths: domain.PathsFrom(output, c.line.Flow.Dependencies.Paths), BaseCommit: e.baseCommit(ctx, log, c),
		Output: output, At: e.clock.Now(),
	}
	t.Events = e.violations(ctx, log, c, output, t.At)

	var err error

	switch {
	case v != nil && v.Decision == domain.Reject:
		err = e.gateReject(c, next, *v, &t)
	case len(chk.plan) > 0:
		if v != nil { // an approving gate's plan: no merge, the parts make the pull requests
			t.Events = append(t.Events, event(c.item, domain.EventGateApproved, t.At, c.agent.Name, nil))
		}

		e.assemble(ctx, log, c, ports.Assembly{Transition: t, Parts: chk.plan, After: itemIDs(chk.after), AfterBy: agentActor(c)})

		return
	case v != nil && c.noMerge && c.station.Action == domain.ActionMerge:
		t.Events = append(t.Events, event(c.item, domain.EventGateApproved, t.At, c.agent.Name, nil))
		t.To, t.Outcome = domain.Position{Station: c.station.ID, State: domain.StateWaitingHuman}, domain.LeaseEscalated
		t.Events = append(t.Events, event(c.item, domain.EventWaitingHuman, t.At, domain.ActorEngine, map[string]any{
			keyReason: "approved; the organization policy lets no agent merge a pull request, so a person merges it",
		}))
	case v != nil:
		t.Events = append(t.Events, event(c.item, domain.EventGateApproved, t.At, c.agent.Name, nil))
		if err = e.mergeOnApproval(ctx, c, next, &t); err == nil {
			err = e.advance(next, c.item, c.agent.Name, &t)
		} else if e.conflictRework(next, c.item, err, &t) {
			err = nil
		} else {
			err = e.deferMerge(ctx, log, c, err, &t)
		}
	default:
		err = e.advance(next, c.item, c.agent.Name, &t)
	}

	if err != nil {
		e.block(ctx, log, c, err.Error())
		return
	}

	e.transition(ctx, log, t)
}

// violations are the rule_violated events of an accepted output, one per entry of its
// violations that names a rule of the version the run started on. They go in the transition
// that accepts the output, so they are written once with it and never for an output that is
// not. An entry naming no declared rule is logged and not counted.
func (e *Engine) violations(ctx context.Context, log *slog.Logger, c claimed, output json.RawMessage, at time.Time) []domain.Event {
	var events []domain.Event

	for _, v := range domain.ViolationsFrom(output) {
		if !slices.ContainsFunc(c.line.Rules, func(r domain.Rule) bool { return r.Name == v.Rule }) {
			log.WarnContext(ctx, "unknown rule violated", slog.String("rule", cut(v.Rule, maxRuleLog)),
				slog.String("agent", c.agent.Name))

			continue
		}

		events = append(events, event(c.item, domain.EventRuleViolated, at, c.agent.Name, map[string]any{
			keyRule: v.Rule, keyReason: v.Reason, "station": c.station.ID, "agent": c.agent.Name,
		}))
	}

	return events
}

// assemble makes the claimed item an assembly with the parts of its plan, queued at the station
// that planned them; the item stays there, in state assembly. A part cannot plan an assembly
// (assemblies do not nest), so its plan blocks it. A lost acknowledgement is only logged: the
// store's Assemble is idempotent, so the parts exist at most once.
func (e *Engine) assemble(ctx context.Context, log *slog.Logger, c claimed, a ports.Assembly) {
	if c.item.ParentID != "" {
		e.block(ctx, log, c, "a part cannot plan an assembly")
		return
	}

	a.Transition.To = domain.Position{Station: c.station.ID, State: domain.StateAssembly}

	ctx, cancel := cleanup(ctx)
	defer cancel()

	_, parts, err := e.store.Assemble(ctx, a)

	switch {
	case errors.Is(err, ports.ErrLeaseLost), errors.Is(err, ports.ErrConflict):
		log.WarnContext(ctx, "item changed hands before it became an assembly", slog.Any("error", err))
	case errors.Is(err, domain.ErrInvalid), errors.Is(err, domain.ErrCycle):
		e.block(ctx, log, c, "assemble: "+err.Error())
	case err != nil:
		log.ErrorContext(ctx, "assemble item; the lease will expire and requeue it", slog.Any("error", err))
	default:
		log.InfoContext(ctx, "item assembled", slog.String("station", c.station.ID), slog.Int("parts", parts.Total))
	}
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
	e.park(ctx, log, c, domain.StateBlocked, domain.LeaseBlocked, domain.EventBlocked, reason)
}

// park moves a claimed item to state at its station with outcome, recording an engine event of
// kind with the reason, and releases the lease.
func (e *Engine) park(ctx context.Context, log *slog.Logger, c claimed, state domain.ItemState, outcome domain.LeaseOutcome,
	kind domain.EventKind, reason string,
) {
	now := e.clock.Now()

	e.transition(ctx, log, ports.Transition{
		ItemID: c.item.ID, From: domain.Position{Station: c.station.ID, State: domain.StateInProgress},
		To: domain.Position{Station: c.station.ID, State: state}, Lease: &c.lease, Outcome: outcome,
		At:     now,
		Events: []domain.Event{event(c.item, kind, now, domain.ActorEngine, map[string]any{keyReason: reason})},
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
