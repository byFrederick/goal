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

// Intake hands an event to the triggers of its factory's current line (ADR 0026) and returns
// one firing for each trigger it reached, in declaration order: every enabled trigger whose on:
// and filter match it (domain.Trigger.Matches). A trigger it does not reach records nothing. For
// each one reached, the checks run in order (only.failuresInARow, only.dedupe, only.modelAgrees,
// limits.perDay, limits.pauseOnBudget); the first that fails records a skipped firing whose reason starts with
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

	eval, err := e.evaluate(ctx, l, t, ev, now)
	if err != nil {
		return domain.TriggerFiring{}, err
	}

	if _, skipped := eval.Skipped(); skipped {
		f.Outcome, f.Reason = domain.FiringSkipped, eval.FiringReason()
	} else {
		it, err := e.createFromTrigger(ctx, l, t, ev, *eval.Item, now)
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
		slog.String("outcome", string(got.Outcome)), slog.String("reason", loggedReason(got.Reason)), slog.String("item", string(got.ItemID)),
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

// evaluate runs t on ev at now as intake does, and as a try does (ADR 0028 decision 1): When,
// only.failuresInARow, only.dedupe, only.modelAgrees (last of only, so the model is asked only
// about events that would otherwise go on), Create (it renders the item, creating nothing), then
// limits.perDay and limits.pauseOnBudget. It stops at the first step that skips the event. It only
// reads, but for the model check's connection.used entry. Reasons hold no event text, but for the
// model's reason and a dedupe skip's item id.
func (e *Engine) evaluate(ctx context.Context, l domain.Line, t domain.Trigger, ev domain.IntakeEvent, now time.Time) (domain.TriggerEvaluation, error) {
	out := domain.NewTriggerEvaluation(t)

	reason, ok := whenReached(t, ev)
	if !ok {
		out.Skip(domain.StepWhen, reason)

		return out, nil
	}

	out.Pass(domain.StepWhen, reason)

	if skipped, err := e.onlyChecks(ctx, l, t, t.DedupeValue(ev), &out); skipped || err != nil {
		return out, err
	}

	if t.Only.ModelAgrees != nil {
		ans, reason := e.modelAgrees(ctx, l, t, ev)
		out.Model = ans

		if ans == nil || !ans.Worth {
			out.Skip(domain.StepModel, reason)

			return out, nil
		}

		out.Pass(domain.StepModel, reason)
	}

	it, err := renderItem(l, t, ev)
	if err != nil {
		return out, err
	}

	out.Item = &it
	out.Pass(domain.StepCreate, "")

	return out, e.limitChecks(ctx, l, t, now, &out)
}

// whenReached reports whether ev reaches t, with why: an enabled schedule trigger is reached by its
// schedule's events, any other by the events domain.Trigger.Matches.
func whenReached(t domain.Trigger, ev domain.IntakeEvent) (string, bool) {
	switch {
	case !t.Enabled:
		return "the trigger is disabled", false
	case t.On == domain.OnSchedule && ev.Source == domain.TriggerSchedule:
		return "the trigger's schedule fired", true
	case t.Matches(ev):
		return "the event matches on: and every filter", true
	}

	return "the event does not match on: or a filter", false
}

// onlyChecks runs only.failuresInARow, then only.dedupe, into out, and reports whether one skipped
// the event.
func (e *Engine) onlyChecks(ctx context.Context, l domain.Line, t domain.Trigger, dedupe string, out *domain.TriggerEvaluation) (bool, error) {
	if n := t.Only.FailuresInARow; n > 0 {
		streak := 0

		if n > 1 {
			var err error
			if streak, err = e.intake.FailureStreak(ctx, l.FactoryID, t.Name, dedupe); err != nil {
				return false, err
			}
		}

		if streak+1 < n {
			out.Skip(domain.StepFailures, fmt.Sprintf("%d of the %d failures in a row needed", streak+1, n))

			return true, nil
		}

		out.Pass(domain.StepFailures, fmt.Sprintf("%d of the %d failures in a row", streak+1, n))
	}

	if t.Only.Dedupe != "" {
		if dedupe == "" {
			out.Pass(domain.StepDedupe, "the event has no "+t.Only.Dedupe)

			return false, nil
		}

		id, open, err := e.intake.OpenItemByDedupe(ctx, l.FactoryID, t.Name, dedupe)
		if err != nil {
			return false, err
		}

		if open {
			out.Skip(domain.StepDedupe, fmt.Sprintf("item %s is open for the same %s", id, t.Only.Dedupe))

			return true, nil
		}

		out.Pass(domain.StepDedupe, "no item is open for the same "+t.Only.Dedupe)
	}

	return false, nil
}

// loggedReason is a firing's reason as the log holds it: a model check's without the model's
// words, which the event shaped.
func loggedReason(reason string) string {
	if strings.HasPrefix(reason, domain.StepModel+": ") {
		return domain.StepModel
	}

	return reason
}

// limitChecks runs limits.perDay, then limits.pauseOnBudget, into out.
func (e *Engine) limitChecks(ctx context.Context, l domain.Line, t domain.Trigger, now time.Time, out *domain.TriggerEvaluation) error {
	if n := t.Limits.PerDay; n > 0 {
		created, err := e.intake.CreatedSince(ctx, l.FactoryID, t.Name, now.UTC().Truncate(24*time.Hour))
		if err != nil {
			return err
		}

		if created >= n {
			out.Skip(domain.StepPerDay, fmt.Sprintf("%d items created today (UTC), the limit is %d", created, n))

			return nil
		}

		out.Pass(domain.StepPerDay, fmt.Sprintf("%d of the %d items a day created today (UTC)", created, n))
	}

	if t.Limits.PauseOnBudget {
		// The weekly budget: no daily one exists (ADR 0026 decision 1).
		b, err := e.weeklyBudget(ctx, l, now)
		if err != nil {
			return err
		}

		switch {
		case b.Exhausted:
			out.Skip(domain.StepBudget, fmt.Sprintf("the weekly budget is spent ($%.2f of $%.2f)", b.SpentUSD, b.WeeklyUSD))
		case !l.Runtime.HasWeeklyBudget():
			out.Pass(domain.StepBudget, "the factory has no weekly budget")
		default:
			out.Pass(domain.StepBudget, fmt.Sprintf("$%.2f of the weekly $%.2f spent", b.SpentUSD, b.WeeklyUSD))
		}
	}

	return nil
}

// renderItem is t's item for ev: at create.station in its arrival state, of create.type and
// create.lane, expedite when create.expedite says so (else as the line classes the type). Its
// title is create.title rendered with plain values (the trigger's name when that is empty), its
// body create.brief rendered with quoted values and then the event's fields in a block marked as
// untrusted data (ADR 0026 decision 5), so domain.Item.Work carries both.
func renderItem(l domain.Line, t domain.Trigger, ev domain.IntakeEvent) (domain.TriggerItem, error) {
	st, ok := l.Station(t.Create.Station)
	if !ok {
		return domain.TriggerItem{}, fmt.Errorf("create.station %q is not on the line", t.Create.Station)
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

	return domain.TriggerItem{
		Type: t.Create.Type, Station: st.ID, State: st.Arrival(), Lane: t.Create.Lane, Class: class, Title: title, Body: body,
	}, nil
}

// createFromTrigger creates the item it, rendered for ev, from t.
func (e *Engine) createFromTrigger(ctx context.Context, l domain.Line, t domain.Trigger, ev domain.IntakeEvent, it domain.TriggerItem, now time.Time) (domain.Item, error) {
	created, err := e.store.CreateItem(ctx, domain.NewItem{
		FactoryID: l.FactoryID, Type: it.Type, Lane: it.Lane, Title: it.Title, Body: it.Body,
		Station: it.Station, State: it.State, Class: it.Class, CreatedAt: now, Source: itemSource(ev.Source), Trigger: t.Name,
	})
	if err != nil {
		return domain.Item{}, fmt.Errorf("create the item: %w", err)
	}

	return created, nil
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
