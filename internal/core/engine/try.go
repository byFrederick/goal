package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// TryFirings is how many of a trigger's newest firings a try may name: the recent events its page
// lists.
const TryFirings = 20

var (
	// ErrTriggerNotFound means the factory's current line has no trigger of the name.
	ErrTriggerNotFound = fmt.Errorf("trigger %w", ports.ErrNotFound)
	// ErrFiringNotFound means the firing is not one of the trigger's TryFirings newest.
	ErrFiringNotFound = fmt.Errorf("firing %w", ports.ErrNotFound)
	// ErrEventNotStored means the firing's event cannot be rebuilt: a firing keeps no event's
	// fields, so only a schedule's, whose one field is its instant, can be tried.
	ErrEventNotStored = errors.New("the firing's event is not stored: only a schedule's firings can be tried")
)

// errNoAudit means the engine was built without an audit log (Deps.Audit).
var errNoAudit = errors.New("engine: no audit log (Deps.Audit)")

// TryTrigger is a dry run of the trigger of the factory's current line against one of its
// TryFirings newest firings (ADR 0028 decision 2): it evaluates the firing's event now, as
// intake would (evaluate), the model check included, but creates no item, records no firing and
// changes no count. It writes only its trigger.tried audit entry by by, which names the trigger,
// the firing and the outcome, never the event's text or the brief, and the connection.used entry
// of a model check's connection. A replay of the event does not stop it, as it would stop intake.
// The factory must belong to the organization in ctx; any other is ports.ErrNotFound.
func (e *Engine) TryTrigger(ctx context.Context, factory domain.FactoryID, trigger, firing string, by domain.Actor) (domain.TriggerTry, error) {
	switch {
	case e.intake == nil:
		return domain.TriggerTry{}, errNoIntake
	case e.audit == nil:
		return domain.TriggerTry{}, errNoAudit
	}

	l, err := e.lineIn(ctx, factory)
	if err != nil {
		return domain.TriggerTry{}, fmt.Errorf("try: %w", err)
	}

	t, ok := lineTrigger(l, trigger)
	if !ok {
		return domain.TriggerTry{}, fmt.Errorf("try: %w: %s is not in the factory's current definition", ErrTriggerNotFound, trigger)
	}

	f, err := e.triedFiring(ctx, l, t, firing)
	if err != nil {
		return domain.TriggerTry{}, err
	}

	ev, err := storedEvent(l, f)
	if err != nil {
		return domain.TriggerTry{}, fmt.Errorf("try %s: firing %s: %w", t.Name, f.ID, err)
	}

	eval, err := e.evaluate(ctx, l, t, ev, e.clock.Now())
	if err != nil {
		return domain.TriggerTry{}, fmt.Errorf("try %s: %w", t.Name, err)
	}

	data := map[string]any{"firing": f.ID, "outcome": eval.Outcome()}
	if s, skipped := eval.Skipped(); skipped {
		data["step"] = s.Step
	}

	if err := e.audit.Append(ctx, domain.AuditEntry{
		Actor: by, Action: domain.AuditTriggerTried, TargetKind: domain.AuditTargetTrigger, TargetID: t.Name, FactoryID: l.FactoryID, Data: data,
	}); err != nil {
		return domain.TriggerTry{}, fmt.Errorf("try %s: audit: %w", t.Name, err)
	}

	e.log.InfoContext(ctx, "trigger tried", slog.String("org_id", string(l.OrgID)), slog.String("factory", string(l.FactoryID)),
		slog.String("trigger", t.Name), slog.String("firing", f.ID), slog.String("outcome", eval.Outcome()),
		slog.String("reason", loggedReason(eval.FiringReason())))

	return domain.TriggerTry{Firing: f, Evaluation: eval}, nil
}

// triedFiring is the firing of t with id among its TryFirings newest.
func (e *Engine) triedFiring(ctx context.Context, l domain.Line, t domain.Trigger, id string) (domain.TriggerFiring, error) {
	entries, err := e.intake.Firings(ctx, l.FactoryID, t.Name, TryFirings)
	if err != nil {
		return domain.TriggerFiring{}, fmt.Errorf("try %s: read its firings: %w", t.Name, err)
	}

	for _, en := range entries {
		if en.Firing.ID == id {
			return en.Firing, nil
		}
	}

	return domain.TriggerFiring{}, fmt.Errorf("try %s: %w: %s is not one of its %d newest", t.Name, ErrFiringNotFound, id, TryFirings)
}

// storedEvent is the event f fired on, rebuilt as fireSchedule made it: only a schedule's can be.
func storedEvent(l domain.Line, f domain.TriggerFiring) (domain.IntakeEvent, error) {
	if f.Source != domain.TriggerSchedule {
		return domain.IntakeEvent{}, ErrEventNotStored
	}

	return domain.IntakeEvent{
		FactoryID: l.FactoryID, Source: domain.TriggerSchedule, Key: domain.OnSchedule,
		Fields: map[string]string{scheduleInstant: f.IdempotencyKey}, IdempotencyKey: f.IdempotencyKey,
	}, nil
}

// lineTrigger is l's trigger named name.
func lineTrigger(l domain.Line, name string) (domain.Trigger, bool) {
	for _, t := range l.Triggers {
		if t.Name == name {
			return t, true
		}
	}

	return domain.Trigger{}, false
}
