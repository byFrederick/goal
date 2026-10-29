package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.DefinitionRunner = (*Engine)(nil)

// staleDraft is what goal-agent prepare reports when the gateway answered 409: the draft moved
// past the revision the request's token names.
const staleDraft = "GET /run: 409"

// definitionOutput is the output of goal-agent's definition mode (apps/worker/README.md,
// "Definition mode"); a changed file's content is base64.
type definitionOutput struct {
	Summary string `json:"summary"`
	Changed []struct {
		Path    string          `json:"path"`
		SHA256  domain.BlobHash `json:"sha256"`
		Content []byte          `json:"content"`
	} `json:"changed"`
	Deleted  []string `json:"deleted"`
	Rejected []string `json:"rejected"`
}

// RunDefinition runs a claimed draft request's box (ADR 0021 decision 2) for the organization in
// ctx, on its factory's loaded line: it refuses the request when the line's weekly budget is used
// up, then creates a box with no item and no station, whose run id is the request's, prepares it
// with a draft-request task token (the gateway's bundle gives it the draft's files at t.Revision)
// and runs goal-agent with the env of runtime.modelConnection alone: no repository, no git login,
// no agent connection and no credential listed for an agent. It deletes the box on every path and
// records nothing: the caller applies the result and finishes the request.
func (e *Engine) RunDefinition(ctx context.Context, t ports.DefinitionTask) (ports.DefinitionResult, error) {
	r := t.Request

	l, err := e.lineIn(ctx, r.Factory)
	if err != nil {
		return ports.DefinitionResult{}, fmt.Errorf("draft request %s: %w", r.ID, err)
	}

	b, err := e.weeklyBudget(ctx, l, e.clock.Now())
	if err != nil {
		return ports.DefinitionResult{}, fmt.Errorf("draft request %s: %w", r.ID, err)
	}

	if b.Exhausted {
		return ports.DefinitionResult{}, fmt.Errorf("draft request %s: factory %s: %w", r.ID, r.Factory, ports.ErrBudgetExhausted)
	}

	runID := domain.RunID(r.ID)
	log := e.log.With(slog.String("draft_request", string(r.ID)), slog.String("factory", string(r.Factory)))

	env, err := e.modelEnv(ctx, l, runID)
	if err != nil {
		return ports.DefinitionResult{}, fmt.Errorf("draft request %s: %w", r.ID, err)
	}

	token, err := e.definitionToken(ctx, l, t)
	if err != nil {
		return ports.DefinitionResult{}, fmt.Errorf("draft request %s: %w", r.ID, err)
	}

	scrub := newScrubber(append(env.secrets, token)...)
	started := e.clock.Now()

	sb, err := e.sandboxes.Create(ctx, ports.SandboxSpec{
		OrgID: l.OrgID, FactoryID: l.FactoryID, RunID: runID, Agent: r.Agent, Image: e.cfg.Image, Size: l.Runtime.Size,
		Env: map[string]string{envGatewayURL: e.cfg.GatewayURL}, Deadline: started.Add(e.cfg.TaskTimeout + e.cfg.LeaseTTL),
	})
	if err != nil {
		return ports.DefinitionResult{}, fmt.Errorf("draft request %s: create sandbox: %w", r.ID, scrub.err(err))
	}

	defer e.deleteBox(ctx, log, &box{sandbox: sb})

	if err := e.prepareDefinition(ctx, log, sb, token, scrub); err != nil {
		return ports.DefinitionResult{}, fmt.Errorf("draft request %s: %w", r.ID, err)
	}

	res, err := e.runDefinitionAgent(ctx, log, sb, env.agent, scrub)
	if err != nil {
		return ports.DefinitionResult{}, fmt.Errorf("draft request %s: %w", r.ID, err)
	}

	return res, nil
}

// modelEnv resolves the line's model connection alone for the run, its use audited as
// connection.used, with the credentials its secret fields refer to.
func (e *Engine) modelEnv(ctx context.Context, l domain.Line, run domain.RunID) (runEnv, error) {
	model := l.Runtime.ModelConnection
	if model == "" {
		return runEnv{}, fmt.Errorf("factory %s has no runtime.modelConnection", l.FactoryID)
	}

	// No agent: a credential listed for an agent is not the model connection's.
	conns, opened, err := e.resolve(ctx, []string{model}, domain.CredentialUse{Factory: l.FactoryID, Run: run})
	if err != nil {
		return runEnv{}, err
	}

	env := runEnv{gitHosts: map[string]string{}}
	env.secrets = append(env.secrets, conns[model].secrets...)

	for _, rc := range opened {
		if rc.Value != "" {
			env.secrets = append(env.secrets, rc.Value)
		}
	}

	if err := agentEnv(model, nil, conns, &env); err != nil {
		return runEnv{}, err
	}

	e.log.InfoContext(ctx, "connection used", slog.String("run", string(run)), slog.Any("connections", []string{model}))

	return env, nil
}

// definitionToken signs the request's task token: of kind draft request, for the line's version,
// naming the request and the draft revision it reads, and no item, station or connection.
func (e *Engine) definitionToken(ctx context.Context, l domain.Line, t ports.DefinitionTask) (string, error) {
	key, err := e.secrets.Resolve(ctx, e.cfg.GatewayKeyRef)
	if err != nil {
		return "", fmt.Errorf("gateway key: %w", err)
	}

	token, err := domain.SignTaskToken([]byte(key), domain.TaskClaims{
		Kind: domain.TaskKindDraftRequest, OrgID: l.OrgID, FactoryID: l.FactoryID, VersionID: l.VersionID, Agent: t.Request.Agent,
		RunID: domain.RunID(t.Request.ID), Model: l.Runtime.Model, DraftRequest: t.Request.ID, DraftRevision: t.Revision,
		ExpiresAt: e.clock.Now().Add(e.cfg.TaskTimeout + e.cfg.LeaseTTL).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("gateway token: %w", err)
	}

	return token, nil
}

// prepareDefinition runs goal-agent prepare with the task token alone. A prepare the gateway
// answered 409 wraps ports.ErrConflict: the draft moved past the request's revision.
func (e *Engine) prepareDefinition(ctx context.Context, log *slog.Logger, sb ports.Sandbox, token string, scrub scrubber) error {
	started, prepared := e.clock.Now(), false

	st, err := execLines(ctx, sb, ports.Exec{Argv: argvPrepare, Env: map[string]string{envGatewayToken: token}}, func(l []byte) {
		var p struct {
			Type string `json:"type"`
		}

		if json.Unmarshal(l, &p) == nil && p.Type == "prepared" {
			prepared = true
		}
	})

	log.InfoContext(ctx, "sandbox exec", slog.String("step", "prepare"), slog.Int("exit", st.Code), slog.Float64("seconds", e.since(started)))

	switch why := scrub.string(strings.TrimSpace(st.StderrTail)); {
	case err != nil:
		return fmt.Errorf("prepare the sandbox: %w", scrub.err(err))
	case st.Code != 0 && strings.Contains(why, staleDraft):
		return fmt.Errorf("prepare the sandbox: %s: %w", why, ports.ErrConflict)
	case st.Code != 0 || !prepared:
		return fmt.Errorf("prepare the sandbox: goal-agent prepare exited %d: %s", st.Code, why)
	}

	return nil
}

// runDefinitionAgent runs goal-agent run with env and reads its finished line. A worker that
// finished failed, timed out or gave an output that is no definition result is a failed result;
// a command that could not run, was cancelled or ended without a finished line is an error.
func (e *Engine) runDefinitionAgent(ctx context.Context, log *slog.Logger, sb ports.Sandbox, env map[string]string, scrub scrubber) (ports.DefinitionResult, error) {
	var fin *domain.WorkerFinished

	started := e.clock.Now()

	st, err := execLines(ctx, sb, ports.Exec{Argv: argvAgent, Env: maps.Clone(env), Timeout: e.cfg.TaskTimeout}, func(line []byte) {
		if _, f, err := domain.DecodeWorkerLine(line, e.clock.Now()); err == nil && f != nil {
			fin = f
		}
	})

	log.InfoContext(ctx, "sandbox exec", slog.String("step", "agent"), slog.Int("exit", st.Code), slog.Float64("seconds", e.since(started)))

	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded), err == nil && st.TimedOut:
		return ports.DefinitionResult{Status: domain.RunFailed, Error: fmt.Sprintf("timed out after %s", e.cfg.TaskTimeout)}, nil
	case ctx.Err() != nil:
		return ports.DefinitionResult{}, fmt.Errorf("agent: %w", ctx.Err())
	case err != nil:
		return ports.DefinitionResult{}, fmt.Errorf("agent: %w", scrub.err(err))
	case fin == nil:
		return ports.DefinitionResult{}, fmt.Errorf("agent: worker exited with code %d without a result: %s",
			st.Code, scrub.string(strings.TrimSpace(st.StderrTail)))
	}

	return definitionResult(fin, scrub), nil
}

// definitionResult is the result fin reports, scrubbed of the run's secrets. A changed file that
// holds a secret value fails the result: its content cannot be scrubbed without changing it.
func definitionResult(fin *domain.WorkerFinished, scrub scrubber) ports.DefinitionResult {
	run := fin.Result()
	res := ports.DefinitionResult{Status: run.Status, CostUSD: run.CostUSD, Turns: run.Turns, Error: scrub.string(run.Error)}

	var out definitionOutput

	if len(bytes.TrimSpace(fin.Output)) > 0 && !bytes.Equal(bytes.TrimSpace(fin.Output), []byte("null")) {
		if err := json.Unmarshal(fin.Output, &out); err != nil && res.Status == domain.RunSucceeded {
			res.Status, res.Error = domain.RunFailed, "the worker's output is not a definition result: "+scrub.string(err.Error())
			return res
		}
	}

	res.Summary, res.Deleted, res.Rejected = scrub.string(out.Summary), nonNil(out.Deleted), nonNil(out.Rejected)

	if res.Status != domain.RunSucceeded {
		return res
	}

	for _, c := range out.Changed {
		if scrub.string(string(c.Content)) != string(c.Content) {
			res.Status, res.Error, res.Changed = domain.RunFailed, c.Path+" holds a secret value of the run; nothing is written", nil
			return res
		}

		res.Changed = append(res.Changed, ports.DefinitionFile{Path: c.Path, SHA256: c.SHA256, Content: c.Content})
	}

	return res
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}

	return s
}
