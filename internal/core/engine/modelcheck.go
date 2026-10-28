package engine

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/byFrederick/goal/internal/core/domain"
)

// modelCheckFailed is why a model check that gave no answer skips the event (ADR 0028 decision 3).
const modelCheckFailed = "Model check failed"

// errNoModelCheck means the engine was built without a model check (Deps.ModelCheck).
var errNoModelCheck = errors.New("no model check (Deps.ModelCheck)")

// modelAgrees runs t's only.modelAgrees on ev and returns the model's answer with its reason on
// one line, or no answer and modelCheckFailed when the check gave none. Only a yes passes. It logs
// the check without the event's text, the question, the reason or any connection value.
func (e *Engine) modelAgrees(ctx context.Context, l domain.Line, t domain.Trigger, ev domain.IntakeEvent) (*domain.ModelAnswer, string) {
	m := t.Only.ModelAgrees
	attrs := []any{
		slog.String("org_id", string(l.OrgID)), slog.String("factory", string(l.FactoryID)), slog.String("trigger", t.Name),
		slog.String("event", ev.IdempotencyKey),
	}

	ans, err := e.askModel(ctx, l, t, ev)
	if err != nil {
		e.log.WarnContext(ctx, "trigger model check", append(attrs, slog.String("model", m.Model), slog.String("outcome", "failed"),
			slog.String("error", err.Error()))...)

		return nil, modelCheckFailed
	}

	outcome := "no"
	if ans.Worth {
		outcome = "yes"
	}

	e.log.InfoContext(ctx, "trigger model check", append(attrs,
		slog.String("model", cmp.Or(ans.Model, m.Model)), slog.String("outcome", outcome),
		slog.Int("input_tokens", ans.InputTokens), slog.Int("output_tokens", ans.OutputTokens), slog.Float64("cost_usd", ans.CostUSD))...)

	reason := domain.PlainValue(ans.Reason, domain.MaxModelReason)

	switch {
	case reason != "":
	case ans.Worth:
		reason = "the model answered yes"
	default:
		reason = "the model answered no"
	}

	return &ans, reason
}

// askModel resolves the check's connection (only.modelAgrees.connection, else the runtime's model
// connection), which must be one the factory uses, for t and ev, fills the credentials it refers
// to and asks the model. Its error is scrubbed of the connection's and credentials' values and of
// the event's text.
func (e *Engine) askModel(ctx context.Context, l domain.Line, t domain.Trigger, ev domain.IntakeEvent) (domain.ModelAnswer, error) {
	m := t.Only.ModelAgrees
	name := cmp.Or(m.Connection, l.Runtime.ModelConnection)

	switch {
	case e.modelCheck == nil:
		return domain.ModelAnswer{}, errNoModelCheck
	case name == "":
		return domain.ModelAnswer{}, errors.New("no connection: set only.modelAgrees.connection or runtime.modelConnection")
	case name != l.Runtime.ModelConnection && !slices.Contains(l.Connections, name):
		return domain.ModelAnswer{}, fmt.Errorf("connection %s is not one the factory uses", name)
	}

	resolved, err := e.connections.Resolve(ctx, []string{name}, domain.ConnectionUse{Trigger: t.Name, Event: ev.IdempotencyKey})
	if err != nil {
		return domain.ModelAnswer{}, fmt.Errorf("connection %s: %w", name, err)
	}

	opened, err := e.openCredentials(ctx, resolved, domain.CredentialUse{Factory: l.FactoryID})
	if err != nil {
		return domain.ModelAnswer{}, err
	}

	r := fillRefs(resolved, opened)[0]

	hidden := r.SecretValues()
	for _, rc := range opened {
		hidden = append(hidden, rc.Value)
	}

	for _, v := range ev.Fields {
		hidden = append(hidden, v)
	}

	hidden = append(append(hidden, ev.Paths...), ev.Author)

	if !r.Kind.Spec.Model {
		return domain.ModelAnswer{}, fmt.Errorf("connection %s: its kind %s gives no model access", name, r.Kind.Name)
	}

	ans, err := e.modelCheck.Ask(ctx, r, domain.ModelQuestion{Model: m.Model, Question: m.Question, Event: ev})
	if err != nil {
		return domain.ModelAnswer{}, fmt.Errorf("connection %s: ask: %w", name, newScrubber(hidden...).err(err))
	}

	return ans, nil
}
