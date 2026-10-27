package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// errNoIntake means the engine was built without intake state (Deps.Intake).
var errNoIntake = errors.New("engine: no intake state (Deps.Intake)")

// The steps a skipped firing's reason starts with, in the order they run (ADR 0026 decision 1).
const (
	stepFailures = "only.failuresInARow"
	stepDedupe   = "only.dedupe"
	stepPerDay   = "limits.perDay"
	stepBudget   = "limits.pauseOnBudget"
)

// Intake hands an event to the triggers of its factory's current line (ADR 0026) and returns
// one firing for each trigger it reached, in declaration order: every enabled trigger whose on:
// and filter match it (domain.Trigger.Matches). A trigger it does not reach records nothing. For
// each one reached, the checks run in order (only.failuresInARow, only.dedupe, limits.perDay,
// limits.pauseOnBudget); the first that fails records a skipped firing whose reason starts with
// its step, and when all pass the trigger creates its item and records a created firing with it.
// Replaying an event with the same idempotency key returns the firing recorded first and creates
// nothing. The factory must belong to the organization in ctx; any other is ports.ErrNotFound.
// A trigger that fails does not stop the others: their firings are returned with the errors
// joined.
func (e *Engine) Intake(ctx context.Context, ev domain.IntakeEvent) ([]domain.TriggerFiring, error) {
	if e.intake == nil {
		return nil, errNoIntake
	}

	if ev.IdempotencyKey == "" {
		return nil, fmt.Errorf("intake: an event needs an idempotency key: %w", domain.ErrInvalid)
	}

	l, err := e.lineIn(ctx, ev.FactoryID)
	if err != nil {
		return nil, fmt.Errorf("intake: %w", err)
	}

	out := []domain.TriggerFiring{}

	var errs []error

	for _, t := range l.Triggers {
		if !t.Matches(ev) {
			continue
		}

		f, err := e.fire(ctx, l, t, ev)
		if err != nil {
			errs = append(errs, fmt.Errorf("intake: trigger %s: %w", t.Name, err))
			continue
		}

		out = append(out, f)
	}

	return out, errors.Join(errs...)
}

// fire runs one trigger on an event that reached it and records the firing, or returns the one
// already recorded for the event's idempotency key.
func (e *Engine) fire(ctx context.Context, l domain.Line, t domain.Trigger, ev domain.IntakeEvent) (domain.TriggerFiring, error) {
	ctx = domain.WithOrg(ctx, l.OrgID)

	if f, ok, err := e.intake.Firing(ctx, l.FactoryID, t.Name, ev.IdempotencyKey); err != nil || ok {
		return f, err
	}

	now := e.clock.Now()
	f := domain.TriggerFiring{
		FactoryID: l.FactoryID, Trigger: t.Name, Source: ev.Source, EventKey: t.On, IdempotencyKey: ev.IdempotencyKey,
		Dedupe: t.DedupeValue(ev), At: now,
	}

	reason, err := e.triggerChecks(ctx, l, t, f.Dedupe, now)
	if err != nil {
		return domain.TriggerFiring{}, err
	}

	if reason != "" {
		f.Outcome, f.Reason = domain.FiringSkipped, reason
	} else {
		it, err := e.createFromTrigger(ctx, l, t, ev, now)
		if err != nil {
			return domain.TriggerFiring{}, err
		}

		f.Outcome, f.ItemID = domain.FiringCreated, it.ID
	}

	got, fresh, err := e.intake.RecordFiring(ctx, f)
	if err != nil {
		return domain.TriggerFiring{}, fmt.Errorf("record the firing: %w", err)
	}

	attrs := []any{
		slog.String("org_id", string(l.OrgID)), slog.String("factory", string(l.FactoryID)), slog.String("trigger", t.Name),
		slog.String("outcome", string(got.Outcome)), slog.String("reason", got.Reason), slog.String("item", string(got.ItemID)),
	}

	if !fresh && f.ItemID != "" {
		// Another delivery of the same event recorded its firing first: the item this one created
		// has no firing (the risk the engine part's pull request names).
		e.log.WarnContext(ctx, "trigger firing raced", append(attrs, slog.String("orphan_item", string(f.ItemID)))...)
	} else {
		e.log.InfoContext(ctx, "trigger fired", attrs...)
	}

	return got, nil
}

// triggerChecks runs t's only and limits steps in order and returns why the first that fails
// skips the event, "" when all pass. Reasons hold no event text.
func (e *Engine) triggerChecks(ctx context.Context, l domain.Line, t domain.Trigger, dedupe string, now time.Time) (string, error) {
	if reason, err := e.onlyChecks(ctx, l, t, dedupe); reason != "" || err != nil {
		return reason, err
	}

	return e.limitChecks(ctx, l, t, now)
}

// onlyChecks runs only.failuresInARow, then only.dedupe.
func (e *Engine) onlyChecks(ctx context.Context, l domain.Line, t domain.Trigger, dedupe string) (string, error) {
	if n := t.Only.FailuresInARow; n > 1 {
		streak, err := e.intake.FailureStreak(ctx, l.FactoryID, t.Name, dedupe)
		if err != nil {
			return "", err
		}

		if streak+1 < n {
			return fmt.Sprintf("%s: %d of the %d failures in a row needed", stepFailures, streak+1, n), nil
		}
	}

	if t.Only.Dedupe != "" && dedupe != "" {
		id, open, err := e.intake.OpenItemByDedupe(ctx, l.FactoryID, t.Name, dedupe)
		if err != nil {
			return "", err
		}

		if open {
			return fmt.Sprintf("%s: item %s is open for the same %s", stepDedupe, id, t.Only.Dedupe), nil
		}
	}

	return "", nil
}

// limitChecks runs limits.perDay, then limits.pauseOnBudget.
func (e *Engine) limitChecks(ctx context.Context, l domain.Line, t domain.Trigger, now time.Time) (string, error) {
	if n := t.Limits.PerDay; n > 0 {
		created, err := e.intake.CreatedSince(ctx, l.FactoryID, t.Name, now.UTC().Truncate(24*time.Hour))
		if err != nil {
			return "", err
		}

		if created >= n {
			return fmt.Sprintf("%s: %d items created today (UTC), the limit is %d", stepPerDay, created, n), nil
		}
	}

	if t.Limits.PauseOnBudget {
		// The weekly budget: no daily one exists (ADR 0026 decision 1).
		b, err := e.weeklyBudget(ctx, l, now)
		if err != nil {
			return "", err
		}

		if b.Exhausted {
			return fmt.Sprintf("%s: the weekly budget is spent ($%.2f of $%.2f)", stepBudget, b.SpentUSD, b.WeeklyUSD), nil
		}
	}

	return "", nil
}

// createFromTrigger creates t's item for ev at create.station in its arrival state, of
// create.type and create.lane, expedite when create.expedite says so (else as the line classes
// the type). Its title is create.title rendered with plain values (the trigger's name when that
// is empty), its body create.brief rendered with quoted values and then the event's fields in a
// block marked as untrusted data (ADR 0026 decision 5), so domain.Item.Work carries both.
func (e *Engine) createFromTrigger(ctx context.Context, l domain.Line, t domain.Trigger, ev domain.IntakeEvent, now time.Time) (domain.Item, error) {
	st, ok := l.Station(t.Create.Station)
	if !ok {
		return domain.Item{}, fmt.Errorf("create.station %q is not on the line", t.Create.Station)
	}

	class := l.ClassFor(t.Create.Type)
	if t.Create.Expedite {
		class = domain.ClassExpedite
	}

	title := strings.TrimSpace(domain.RenderTemplate(t.Create.Title, ev.Fields, false))
	if title == "" {
		title = domain.Humanize(t.Name)
	}

	body := domain.UntrustedEventBlock(ev)
	if brief := strings.TrimSpace(domain.RenderTemplate(t.Create.Brief, ev.Fields, true)); brief != "" {
		body = brief + "\n\n" + body
	}

	it, err := e.store.CreateItem(ctx, domain.NewItem{
		FactoryID: l.FactoryID, Type: t.Create.Type, Lane: t.Create.Lane, Title: title, Body: body,
		Station: st.ID, State: st.Arrival(), Class: class, CreatedAt: now, Source: itemSource(ev.Source), Trigger: t.Name,
	})
	if err != nil {
		return domain.Item{}, fmt.Errorf("create the item: %w", err)
	}

	return it, nil
}

// itemSource is where an item a trigger created from an event of source s came from.
func itemSource(s domain.TriggerSource) domain.ItemSource {
	switch s {
	case domain.TriggerSchedule:
		return domain.ItemFromSchedule
	case domain.TriggerGitHub:
		return domain.ItemFromGitHub
	case domain.TriggerWebhook:
		return domain.ItemFromWebhook
	}

	return ""
}
