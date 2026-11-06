package engine

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
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
	// close is the reason the output closes its item with (domain.CloseFrom); "" when it does not.
	close string
	// repositories are the keys the output adds to its item (repositoriesOf).
	repositories []string
}

// claimed is an item this instance holds a lease on at an agent station.
type claimed struct {
	line    domain.Line
	station domain.Station
	item    domain.Item
	lease   domain.Lease
	agent   domain.AgentSpec
	// env is the agent's environment on a version on secrets (ADR 0037 decision 10); zero on
	// connections.
	env domain.Environment
	// What the organization policy decided at run start (ADR 0024), the same for each of the
	// claim's runs: the model, the commands and tools added to the agent's deny rules, how long
	// one run may take, how long the box may live (0: no policy limit) and whether an approving
	// gate whose action is merge leaves the merge to a person (noMerge), and whether Use secrets
	// is off, giving the agent no env, files or MCP servers (noEnv).
	model    string
	deny     []string
	timeout  time.Duration
	boxLimit time.Duration
	noMerge  bool
	// knowledge are the snapshots the claim's runs read, decided with the policy (pinKnowledge),
	// so a lost box prepared again reads the same ones.
	knowledge []domain.KnowledgePin
	noEnv     bool
	// branch and base are the runs' branch and the branch it is cut from and its pull request
	// goes into, decided at the claim's start (integrate); both empty for the item's branch on the
	// default branch, as for every item that is not a part on an integration branch.
	branch, base string
	// code is what the runs get of the item's repositories and the factory's entries, decided at
	// the claim's start (Line.RunCode).
	code domain.RunCode
	// hostKind is the kind of the code host of a landing root's first repository (firstHostKind),
	// which picks the command its landing note opens a pull request with; "" for GitHub's.
	hostKind domain.CodeHostKind
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
			e.finish(ctx, log, c, res.RunID, res.Output, chk)
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
// (applyPolicy), decides its runs' branch and base (integrate) and code (runCode) and reports whether it may run;
// when not, the item is blocked (no agent, the policy could not be read, or the base could not
// be decided) or waits for a person (the policy refuses its model).
func (e *Engine) admitted(ctx context.Context, log *slog.Logger, c *claimed) bool {
	var err error
	if c.agent, err = agentFor(c.line, c.station, c.item.Lane); err == nil {
		c.env, err = agentEnvironment(c.line, c.agent)
	}

	if err != nil {
		e.block(ctx, log, *c, err.Error())
		return false
	}

	refusal, err := e.applyPolicy(ctx, c)
	if err == nil && refusal == "" {
		c.knowledge, err = e.pinKnowledge(ctx, c.line, c.agent, domain.RuleTarget{
			Agent: c.agent.Name, Station: c.station.ID, Lane: c.item.Lane, WorkType: c.item.Type,
		})
	}

	if err == nil && refusal == "" {
		err = e.integrate(ctx, log, c)
	}

	if err == nil && refusal == "" {
		c.code, err = e.runCode(ctx, c.line, c.agent, c.item.ID)
		c.hostKind = e.firstHostKind(ctx, log, *c)
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
//     item waits there for a person's merge (noMerge); review_approval is the version's say. An
//     untrusted item (ADR 0040 decision 21) takes the same path whatever the policy says.
//   - Use secrets (use_credentials), off: the agent's connections list is emptied, so neither its
//     env nor its task token gets any of them; the runtime's model and repository connections
//     still reach the run, which cannot start without them. On secrets the agent's environment
//     env, files and MCP servers are emptied, and the model key still reaches the run.
//     environment, and its deprecated alias attached_only, give the agent what it names.
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

	c.noEnv = p.UseCredentials == domain.ToolOff
	if c.noEnv {
		c.agent.Connections, c.env.Env, c.env.Files, c.env.MCP = nil, nil, nil, nil
	}

	if p.BoxTimeLimitMinutes > 0 {
		c.boxLimit = time.Duration(p.BoxTimeLimitMinutes) * time.Minute
		c.timeout = min(c.timeout, c.boxLimit)
	}

	return "", nil
}

// pinKnowledge is the newest snapshot of each of the line's knowledge sources that applies to run
// (ADR 0038 decision 7), in the definition's order: none for an agent that may not read files
// (builtins.read off) or without Knowledge, and no pin for a source with no snapshot yet or that
// the organization no longer has. An error when the snapshots cannot be read.
func (e *Engine) pinKnowledge(ctx context.Context, l domain.Line, agent domain.AgentSpec, run domain.RuleTarget) ([]domain.KnowledgePin, error) {
	if !agent.ReadsFiles() {
		return nil, nil
	}

	return e.pinSources(ctx, l, run)
}

// pinSources is the newest snapshot of each of the line's knowledge sources that applies to run,
// as pinKnowledge decides them for any reader; none without Knowledge.
func (e *Engine) pinSources(ctx context.Context, l domain.Line, run domain.RuleTarget) ([]domain.KnowledgePin, error) {
	if e.knowledge == nil {
		return nil, nil
	}

	var pins []domain.KnowledgePin

	for _, k := range domain.ApplyingKnowledge(l.Knowledge, run) {
		snaps, err := e.knowledge.Snapshots(ctx, k.Name)

		switch {
		case errors.Is(err, ports.ErrNotFound):
			continue
		case err != nil:
			return nil, fmt.Errorf("read the snapshots of knowledge source %s: %w", k.Name, err)
		case len(snaps) > 0:
			pins = append(pins, domain.KnowledgePin{Source: k.Name, Snapshot: snaps[0].ID})
		}
	}

	return pins, nil
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
		errors.Is(err, domain.ErrBadPlan) || errors.Is(err, domain.ErrBadAfter) || errors.Is(err, domain.ErrBadClose) ||
		errors.Is(err, domain.ErrBadRepositories) || errors.Is(err, errCheckFailed)
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
// the work follows (followed), its close (closeOf), the repositories it adds to its item
// (repositoriesOf) and, when the current line materializes plans, its plan. The current line is read once here and finish moves the item along the same one, so a
// plan is assembled only by a line that parsed it.
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

	reason, err := closeOf(c, output, chk.next)
	if err != nil {
		return checked{}, err
	}

	chk.close = reason

	if chk.repositories, err = e.repositoriesOf(ctx, c, output); err != nil {
		return checked{}, err
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

	if err := domain.CheckTriagePlan(c.item, plan); err != nil {
		return checked{}, err
	}

	chk.plan = plan

	return chk, nil
}

// closeOf is the close the output gives in the field the line maps (flow.outputs.close), checked
// before any plan is assembled: refused on an item a person filed, on a part, and together with a
// plan when the current line (next) materializes plans. "" when the output closes nothing.
func closeOf(c claimed, output json.RawMessage, next domain.Line) (string, error) {
	reason, err := domain.CloseFrom(output, c.line.Flow.Outputs.Close)
	if err != nil || reason == "" {
		return "", err
	}

	var plan bool

	if next.Flow.Assemblies.Materialize {
		parts, err := domain.PlanFrom(output)
		if err != nil {
			return "", err
		}

		plan = len(parts) > 0
	}

	if err := domain.CheckClose(c.item, plan); err != nil {
		return "", err
	}

	return reason, nil
}

// repositoriesOf are the repositories the output names in the field the line maps
// (flow.outputs.repositories) that its item lacks, resolved against the version the run started
// on: domain.ErrBadRepositories when one is not the factory's, or when they would give the item
// more than domain.MaxItemRepositories with its rows, this run's pushes included (FinishRun joined
// them before the check). None when the output names none.
func (e *Engine) repositoriesOf(ctx context.Context, c claimed, output json.RawMessage) ([]string, error) {
	keys, err := domain.RepositoriesFrom(output, c.line.Flow.Outputs.Repositories, c.line)
	if err != nil || len(keys) == 0 {
		return nil, err
	}

	have, err := e.store.ItemRepositories(ctx, c.item.ID)
	if err != nil {
		return nil, fmt.Errorf("repositories of item %s: %w", c.item.ID, err)
	}

	return domain.OutputRepositories(have, keys)
}

// runOnce runs the claim's next run with the shared run lifecycle (runTask) under the claim's
// lease, in the claim's sandbox (b, created and prepared by the first run), and, when it
// succeeded, checks its output, runs the station's checks and links a single item to the items
// its output follows (follow), all while the lease is heartbeated. It returns ErrLeaseLost when
// another instance took the item, an *outputError when the output failed its checks, and any
// other error when the run could not be carried out.
func (e *Engine) runOnce(ctx context.Context, c claimed, b *box, feedback string) (domain.RunResult, checked, error) {
	var chk checked

	res, err := e.runTask(ctx, runTask{
		RunRequest: c.request(feedback), item: c.item, box: b, work: c.item.Work(),
		// The agent's env gets the connections of its list the factory lists too, as the gateway
		// serves them; its task token names its whole list.
		own:       slices.DeleteFunc(slices.Clone(c.agent.Connections), func(n string) bool { return !slices.Contains(c.line.Connections, n) }),
		workspace: func(b *box, env runEnv) string { return workspaceNote(c, b, env) },
		then: func(ctx context.Context, runID domain.RunID, res domain.RunResult, scrub scrubber) error {
			got, err := e.check(ctx, c, res.Output)
			if err != nil {
				return &outputError{err: err}
			}

			if err := e.runChecks(ctx, c, runID, b, scrub); err != nil {
				return err
			}

			if err := e.follow(ctx, c, got); err != nil {
				return err
			}

			chk = got

			return nil
		},
	})

	return res, chk, err
}

// request is the claim's next run as a RunRequest, with brief added to its bundle's brief. A
// version on secrets gives the run its model key and the agent's environment and no connection,
// and its box the environment's size (medium when it names none) and image.
func (c claimed) request(brief string) domain.RunRequest {
	rt := c.line.Runtime

	r := domain.RunRequest{
		OrgID: c.line.OrgID, FactoryID: c.line.FactoryID, Version: c.line.VersionID, Kind: domain.RunKindStation,
		Station: c.station.ID, Item: c.item.ID, Agent: c.agent.Name, Model: c.model, Brief: brief,
		Repository: rt.Repository, Branch: c.branch, Base: c.base, Code: c.code, ModelConnection: rt.ModelConnection,
		Connections: c.agent.Connections, DeniedCommands: c.deny, Knowledge: c.knowledge, NoEnvironment: c.noEnv, Size: rt.Size, Timeout: c.timeout, BoxLimit: c.boxLimit, Lease: &c.lease,
	}

	if c.line.OnSecrets() {
		r.ModelConnection, r.Connections, r.Repository.Connection = "", nil, ""
		r.ModelKey, r.Environment = rt.ModelKey, c.env
		r.Size, r.Image = cmp.Or(c.env.Size, domain.SandboxMedium), c.env.Image
	}

	return r
}

// exitTimedOut is the exit code a check reports when it ran out of time, as timeout(1) does.
const exitTimedOut = 124

// runChecks runs the station's command checks in order in the claim's sandbox (runCheck), records
// each one's event on the item, and stops at the first that fails, which is returned as an
// *outputError. What a check prints is scrubbed of the run's secrets, which the agent may have
// left in a file.
func (e *Engine) runChecks(ctx context.Context, c claimed, runID domain.RunID, b *box, scrub scrubber) error {
	for _, cmd := range c.station.Checks {
		started := e.clock.Now()

		code, output, err := e.runCheck(ctx, runID, b, cmd, scrub)
		if err != nil {
			return err
		}

		kind := domain.EventCheckPassed
		data := map[string]any{"command": cmd, "exit_code": code, "duration_ms": e.clock.Now().Sub(started).Milliseconds()}

		if code != 0 {
			kind, data["output"] = domain.EventCheckFailed, output
		}

		e.record(ctx, c.item, runID, kind, data)

		if code != 0 {
			return &outputError{err: fmt.Errorf("%w: `%s` exited %d; the end of its output:\n%s", errCheckFailed, cmd, code, output)}
		}
	}

	return nil
}

// tryChecks runs a try's checks (ADR 0039 decision 10) as runChecks runs a station's, in the
// try's box, and returns the first that fails; nil when they all pass. A try works no item, so
// no check event is written. A check that cannot be run is an error.
func (e *Engine) tryChecks(ctx context.Context, t runTask, runID domain.RunID, scrub scrubber) (*domain.FailedCheck, error) {
	for _, cmd := range t.Checks {
		code, output, err := e.runCheck(ctx, runID, t.box, cmd, scrub)
		if err != nil {
			return nil, err
		}

		if code != 0 {
			return &domain.FailedCheck{Command: cmd, ExitCode: code, Output: output}, nil
		}
	}

	return nil, nil
}

// runCheck runs one command check in b, in its workspace, with CI=true and no credential, under
// the check timeout: its exit code (exitTimedOut when it ran out of time) and the end of what it
// printed, scrubbed of the run's secrets. The exit code is the runtime's: nothing the agent left
// in the sandbox can report a pass. An error when the command could not be run.
func (e *Engine) runCheck(ctx context.Context, runID domain.RunID, b *box, cmd string, scrub scrubber) (int, string, error) {
	started := e.clock.Now()
	out := &tail{max: maxCheckOutput}

	st, err := execLines(ctx, b.sandbox, ports.Exec{
		Argv: []string{"sh", "-c", cmd}, Env: map[string]string{"CI": "true"}, Dir: workspaceDir, Timeout: e.cfg.CheckTimeout,
	}, out.add)
	if err != nil {
		return 0, "", fmt.Errorf("run check %q: %w", cmd, scrub.err(err))
	}

	code, output := st.Code, joinParts(out.String(), st.StderrTail)
	if st.TimedOut {
		code, output = exitTimedOut, joinParts(output, fmt.Sprintf("timed out after %s", e.cfg.CheckTimeout))
	}

	e.log.InfoContext(ctx, "sandbox exec", slog.String("run", string(runID)), slog.String("step", "check"),
		slog.Int("exit", code), slog.Float64("seconds", e.since(started)))

	return code, scrub.string(cut(strings.TrimSpace(output), maxCheckOutput)), nil
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
// it, and whether git can push from it, then the item's repositories and how to open another
// (codeNote); nothing for a factory with no code.
func workspaceNote(c claimed, b *box, env runEnv) string {
	r := c.line.Runtime.Repository
	code := codeNote(c)

	if r.URL == "" {
		if code == "" {
			return ""
		}

		return "## Workspace\n\n" + code
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

	note := joinParts(fmt.Sprintf("## Workspace\n\nYour working directory is a clone of %s with branch `%s` checked out (%s). %s",
		r.URL, cmp.Or(c.branch, c.item.Branch()), state, env.pushNote(host)), code)
	if c.item.Landing() && c.agent.Code == domain.CodeRead {
		return note + fmt.Sprintf("\n\nThis is the assembly's integration branch `%s`; it lands on the default branch. "+
			"You only read code: push nothing and open no pull request.", c.branch)
	}

	// A landing root lands only at its landing station; anywhere else (build, after the landing
	// was rejected) it is reworked on the integration branch, so a landing note there would have
	// the agent check the landing instead of fixing what the rejection asks for.
	if c.item.Landing() && c.station.ID != c.item.IntegrationLandAt {
		return note + fmt.Sprintf("\n\nThis is the assembly's integration branch `%s`; it lands on the default branch. "+
			"Fix what the last rejection asks for here, push to `%s` as a plain push, and keep its pull request "+
			"into the default branch open.", c.branch, c.branch)
	}

	if c.item.Landing() {
		return note + fmt.Sprintf("\n\nYou are landing the whole assembly: `%s` is its integration branch, which holds "+
			"every part's merged work, and it lands on the default branch (origin/HEAD). First merge the default branch into `%s` "+
			"and resolve any conflicts there; never rebase it. Then, if no pull request from `%s` into the default branch is open, "+
			"open one with `%s`, and name it in your output's pull_request. "+
			"When `%s` has no commits beyond the default branch, open none and report `\"pull_requests\": []` instead.",
			c.branch, c.branch, c.branch, openCommand(c.hostKind, c.branch), c.branch) + eachRepository(c)
	}

	if c.base == "" {
		return note
	}

	refresh := cmp.Or(c.line.Flow.Assemblies.Integration.Refresh, domain.RefreshOnLand)
	open := fmt.Sprintf("open your pull request into `%s`", c.base)

	if c.agent.Code == domain.CodeRead {
		open = "you only read code, so push nothing and open no pull request"
	}

	return note + fmt.Sprintf("\n\nThis part builds on its assembly's integration branch `%s`, not the default branch: "+
		"%s. The integration branch takes in the default branch %s (refresh: %s).",
		c.base, open, refreshWhen[refresh], refresh)
}

// eachRepository tells a landing with several repositories to land each of them, and is empty
// for one.
func eachRepository(c claimed) string {
	if len(c.code.Clones) < 2 {
		return ""
	}

	return fmt.Sprintf(" Do this in each of the item's repositories where `%s` has commits beyond its default branch, "+
		"and name every pull request in your output's pull_requests.", c.branch)
}

// openCommand is the command that opens a pull request from branch into the default branch on a
// code host of kind (ADR 0041 decision 7): glab on GitLab, gh on GitHub and when kind is unknown.
func openCommand(kind domain.CodeHostKind, branch string) string {
	if kind == domain.CodeHostGitLab {
		return "glab mr create --target-branch <default branch> --source-branch " + branch
	}

	return "gh pr create --base <default branch> --head " + branch
}

// firstHostKind is the kind the organization lists the host of a landing root's first repository
// as: its first clone's, else the line's repository's. "" for any other claim, for one with no
// repository, and when the host is not listed or cannot be read, which is logged: the note then
// names GitHub's command.
func (e *Engine) firstHostKind(ctx context.Context, log *slog.Logger, c claimed) domain.CodeHostKind {
	if !c.item.Landing() {
		return ""
	}

	raw := c.line.Runtime.Repository.URL
	if len(c.code.Clones) > 0 {
		raw = c.code.Clones[0].URL
	}

	if raw == "" {
		return ""
	}

	hosts, err := e.listedCodeHosts(ctx)
	if err != nil {
		log.WarnContext(ctx, "code host kind unknown", slog.Any("error", err))

		return ""
	}

	repo, err := domain.ParseCodeRepository(hosts, raw)
	if err != nil {
		log.WarnContext(ctx, "code host kind unknown", slog.String("repository", raw), slog.Any("error", err))

		return ""
	}

	return repo.Host.Kind
}

// codeNote lists the item's repositories, each with where it is and whether this agent may push
// to it, on the item's branch, and says how to open any other repository the factory's entries
// cover (ADR 0041 decisions 3 and 5); the list is left out for an item with none, and both for a
// line with no entries.
func codeNote(c claimed) string {
	entries := c.line.Runtime.Repositories
	if len(entries) == 0 {
		return ""
	}

	var list string

	if len(c.code.Clones) > 0 {
		var sb strings.Builder

		fmt.Fprintf(&sb, "The item's repositories, each on branch `%s` where a run already pushed it, else on the default branch:",
			cmp.Or(c.branch, c.item.Branch()))

		for _, r := range c.code.Clones {
			access := "read only"
			if r.Write {
				access = "you may push"
			}

			fmt.Fprintf(&sb, "\n- %s at %s: %s", r.URL, r.Path, access)
		}

		list = sb.String()
	}

	urls := make([]string, len(entries))
	for i, e := range entries {
		urls[i] = e.URL
	}

	push := "Push only to a repository whose entry writes."
	if c.agent.Code == domain.CodeRead {
		push = "You only read code: never push to any repository."
	}

	open := fmt.Sprintf("To open another repository the factory's entries cover (%s), clone it with plain git into "+
		"%s/<owner>/<name>, such as `git clone https://<host>/<owner>/<name> %s/<owner>/<name>`. %s",
		strings.Join(urls, ", "), domain.ReposPath, domain.ReposPath, push)

	return joinParts(list, open)
}

// refreshWhen says when an integration branch takes in the default branch, by its refresh.
var refreshWhen = map[domain.IntegrationRefresh]string{
	domain.RefreshOnLand:   "when the assembly lands",
	domain.RefreshEachPart: "before each part runs",
}

// finish moves an item whose output passed its checks, with an event for each declared rule the
// output says was broken (violations) and the paths it lists in the field the line maps
// (flow.dependencies.paths), the follow-ups of run it holds (followUps) and the repositories it adds (chk.repositories), with the base commits it was planned against (baseCommits): through the gate's verdict when the
// station has one, to cancelled at its station when its output closes the item (closeItem), into
// state assembly when its output has a plan (assemble), otherwise to the next station. An approving gate whose action is merge leaves the merge to a person when the
// organization policy lets no agent merge (claimed.noMerge), and goes back to its own queue when
// a pull request is only not mergeable yet (deferMerge). The run was checked against the version it started on; where the item goes
// next is the current line's say (chk.next), so an item finishing on v1 after v2 was published
// moves along v2.
func (e *Engine) finish(ctx context.Context, log *slog.Logger, c claimed, run domain.RunID, output json.RawMessage, chk checked) {
	next, v := chk.next, chk.verdict

	t := ports.Transition{
		ItemID: c.item.ID, From: domain.Position{Station: c.station.ID, State: domain.StateInProgress},
		Lease: &c.lease, Outcome: domain.LeaseAdvanced, Lane: domain.LaneFrom(output), OutputStation: c.station.ID,
		Paths: domain.PathsFrom(output, c.line.Flow.Dependencies.Paths), Output: output, At: e.clock.Now(),
		FollowUps: followUps(c, run, output), Repositories: chk.repositories,
	}
	t.BaseCommit, t.BaseCommits = e.baseCommits(ctx, log, c)
	t.Events = e.violations(ctx, log, c, output, t.At)

	var err error

	switch {
	case v != nil && v.Decision == domain.Reject:
		err = e.gateReject(c, next, *v, &t)
	case chk.close != "":
		closeItem(c, chk.close, &t)
	case len(chk.plan) > 0:
		if v != nil { // an approving gate's plan: no merge, the parts make the pull requests
			t.Events = append(t.Events, event(c.item, domain.EventGateApproved, t.At, c.agent.Name, nil))
		}

		e.assemble(ctx, log, c, ports.Assembly{Transition: t, Parts: chk.plan, After: itemIDs(chk.after), AfterBy: agentActor(c), By: agentActor(c)})

		return
	case v != nil && c.waitsForPerson() && c.station.Action == domain.ActionMerge:
		t.Events = append(t.Events, event(c.item, domain.EventGateApproved, t.At, c.agent.Name, nil))
		t.To, t.Outcome = domain.Position{Station: c.station.ID, State: domain.StateWaitingHuman}, domain.LeaseEscalated
		t.Events = append(t.Events, event(c.item, domain.EventWaitingHuman, t.At, domain.ActorEngine, map[string]any{keyReason: noMergeReason(c.item)}))
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

// closeItem points t at the item's cancellation at its station with reason, its agent the actor
// (ADR 0040 decision 11). The lease ends advanced, as with any accepted output: this is not
// Cancel, which takes a running run's lease.
func closeItem(c claimed, reason string, t *ports.Transition) {
	by := agentActor(c)
	data := map[string]any{keyReason: reason, keyStation: c.station.ID, "agent": c.agent.Name, "closed": true}

	t.To = domain.Position{Station: c.station.ID, State: domain.StateCancelled}
	t.Events = append(t.Events, decision(c.item, domain.EventCancelled, t.At, by, data))
	t.Audit = itemAudit(c.item, t.At, by, domain.AuditItemCancelled, data)
}

// followUps are the follow-ups an accepted output of run holds in the field the line maps
// (flow.outputs.followUps), with the run, station and agent that wrote them. They go in the
// transition that accepts the output, so they are kept once with it and never for an output that
// is not; none when the line maps no field.
func followUps(c claimed, run domain.RunID, output json.RawMessage) []domain.FollowUp {
	fs := domain.FollowUpsFrom(output, c.line.Flow.Outputs.FollowUps)
	for i := range fs {
		fs[i].RunID, fs[i].Station, fs[i].Agent = run, c.station.ID, c.agent.Name
	}

	return fs
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
			keyRule: v.Rule, keyReason: v.Reason, keyStation: c.station.ID, "agent": c.agent.Name,
		}))
	}

	return events
}

// assemble makes the claimed item an assembly with the parts of its plan, queued at the station
// that planned them; the item stays there, in state assembly. A part cannot plan an assembly
// (assemblies do not nest), so its plan blocks it, as does a part domain.File refuses. Each part
// gets the repositories its plan entry names, else the assembly's. A lost acknowledgement is only
// logged: the store's Assemble is idempotent, so the parts exist at most once.
func (e *Engine) assemble(ctx context.Context, log *slog.Logger, c claimed, a ports.Assembly) {
	if c.item.ParentID != "" {
		e.block(ctx, log, c, "a part cannot plan an assembly")
		return
	}

	repos, err := e.repositoryKeys(ctx, c.item.ID)
	if err != nil {
		log.ErrorContext(ctx, "assemble item; the lease will expire and requeue it", slog.Any("error", err))
		return
	}

	parts := make([]domain.NewPart, len(a.Parts))

	for i, p := range a.Parts {
		if parts[i], err = checkPart(c.line, c.item, a.By, p, repos); err != nil {
			e.block(ctx, log, c, "assemble: part "+p.Key+": "+err.Error())
			return
		}
	}

	a.Parts = parts

	a.Transition.To = domain.Position{Station: c.station.ID, State: domain.StateAssembly}

	ctx, cancel := cleanup(ctx)
	defer cancel()

	_, filed, err := e.store.Assemble(ctx, a)

	switch {
	case errors.Is(err, ports.ErrLeaseLost), errors.Is(err, ports.ErrConflict):
		log.WarnContext(ctx, "item changed hands before it became an assembly", slog.Any("error", err))
	case errors.Is(err, domain.ErrInvalid), errors.Is(err, domain.ErrCycle):
		e.block(ctx, log, c, "assemble: "+err.Error())
	case err != nil:
		log.ErrorContext(ctx, "assemble item; the lease will expire and requeue it", slog.Any("error", err))
	default:
		log.InfoContext(ctx, "item assembled", slog.String("station", c.station.ID), slog.Int("parts", filed.Total))
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

// agentEnvironment is the environment a's box gets on a version on secrets: its own, or the
// default; zero on connections. It fails when the version has no file for it.
func agentEnvironment(l domain.Line, a domain.AgentSpec) (domain.Environment, error) {
	if !l.OnSecrets() {
		return domain.Environment{}, nil
	}

	env, ok := l.Environment(a.Environment)
	if !ok {
		return domain.Environment{}, fmt.Errorf("agent %s gets environment %s, which version %d has no environments/%s.yaml for",
			a.Name, cmp.Or(a.Environment, domain.DefaultEnvironment), l.Number, cmp.Or(a.Environment, domain.DefaultEnvironment))
	}

	return env, nil
}

func agentSpec(l domain.Line, name string) (domain.AgentSpec, error) {
	a, ok := l.Agent(name)
	if !ok {
		return domain.AgentSpec{}, fmt.Errorf("agent %q is not defined", name)
	}

	return a, nil
}

// waitsForPerson is whether an approved merge is left to a person: the organization policy lets no
// agent merge, or the item came from outside text (ADR 0040 decision 21).
func (c claimed) waitsForPerson() bool {
	return c.noMerge || c.item.Untrusted
}

// noMergeReason is why an approved merge waits for a person: the item came from outside text
// (ADR 0040 decision 21), or the organization policy lets no agent merge.
func noMergeReason(it domain.Item) string {
	if it.Untrusted {
		return "approved; the item was made from outside text (a webhook delivery), so a person merges it"
	}

	return "approved; the organization policy lets no agent merge a pull request, so a person merges it"
}
