package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

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
	// fields, so only a schedule's, whose one field is its instant, and a delivery's whose body its
	// webhook still keeps (domain.DeliveryRetention) can be tried.
	ErrEventNotStored = errors.New("the firing's event is not stored: only a schedule's firing, or a delivery's while its webhook keeps it, can be tried")
)

// errNoAudit means the engine was built without an audit log (Deps.Audit).
var errNoAudit = errors.New("engine: no audit log (Deps.Audit)")

// TryTrigger is a dry run of the trigger of the factory's current line, as saved, against one of
// its TryFirings newest firings (ADR 0028 decision 2, ADR 0040 decision 18): it rebuilds the
// firing's event (storedEvent) and evaluates it now, as intake would (evaluate), but creates no
// item, records no firing and changes no count. It writes only its trigger.tried audit entry by
// by, which names the trigger, the firing and the outcome, never the event's text or the brief. A
// replay of the event does not stop it, as it would stop intake.
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

	ev, err := e.storedEvent(ctx, l, f)
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
		slog.String("reason", eval.FiringReason()))

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

// storedEvent is the event f fired on, rebuilt: a schedule's from its instant, as fireSchedule
// made it, and a delivery's from the body its webhook still keeps, as evaluateDelivery made it
// (domain.NewDeliveryEvent). The delivery is read only from a webhook open to l's factory, in the
// organization in ctx. A missed firing fired on nothing, and a delivery that is gone (pruned, or
// received before deliveries kept their sender's id) is ErrEventNotStored.
func (e *Engine) storedEvent(ctx context.Context, l domain.Line, f domain.TriggerFiring) (domain.IntakeEvent, error) {
	switch {
	case f.Outcome == domain.FiringMissed:
		return domain.IntakeEvent{}, ErrEventNotStored
	case f.Source == domain.TriggerSchedule:
		return domain.IntakeEvent{
			FactoryID: l.FactoryID, Source: domain.TriggerSchedule, Key: domain.OnSchedule,
			Fields: map[string]string{scheduleInstant: f.IdempotencyKey}, IdempotencyKey: f.IdempotencyKey,
		}, nil
	}

	webhook, deliveryID, ok := strings.Cut(f.IdempotencyKey, "/")
	if !ok || deliveryID == "" {
		return domain.IntakeEvent{}, ErrEventNotStored
	}

	open, err := e.intake.ListEndpoints(ctx, l.FactoryID)
	if err != nil {
		return domain.IntakeEvent{}, fmt.Errorf("read the factory's webhooks: %w", err)
	}

	i := slices.IndexFunc(open, func(w domain.WebhookEndpoint) bool { return string(w.ID) == webhook })
	if i < 0 {
		return domain.IntakeEvent{}, ErrEventNotStored
	}

	w := open[i]

	d, err := e.intake.DeliveryBySender(ctx, w.ID, deliveryID)

	switch {
	case errors.Is(err, ports.ErrNotFound):
		return domain.IntakeEvent{}, ErrEventNotStored
	case err != nil:
		return domain.IntakeEvent{}, fmt.Errorf("read the delivery: %w", err)
	}

	return domain.NewDeliveryEvent(l.FactoryID, w, d.DeliveryID, d.Event, d.Body, e.eventTable(w.Sender, d.Event)), nil
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
