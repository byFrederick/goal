package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"github.com/byFrederick/goal/internal/core/domain"
)

// newHolder is a name for this instance's claims of delivery evaluations, unique enough that two
// instances never share one.
func newHolder() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("engine: holder: %w", err)
	}

	return "goald-" + hex.EncodeToString(b), nil
}

// eventTable is the senders' table for sender's event; empty without one or without Deps.Events.
func (e *Engine) eventTable(sender domain.WebhookSender, event string) domain.EventTable {
	if e.events == nil {
		return domain.EventTable{}
	}

	tb, _ := e.events.EventTable(sender, event)

	return tb
}

// Evaluations are the factories that evaluate a delivery of event with body to the organization's
// webhook w (ADR 0040 decision 13), from the lines already in memory, by factory: one for each
// loaded factory w is open to whose enabled triggers name the delivery and whose opening admits
// its repository (domain.EvaluationFor). A factory without that repository is skipped at once.
// GitHub's ping is evaluated nowhere. The receiver adds them with the delivery's receipt, and a
// delivery none names keeps only its ledger id and count.
func (e *Engine) Evaluations(ctx context.Context, w domain.WebhookEndpoint, event string, body []byte) []domain.NewDeliveryEvaluation {
	if w.Sender == domain.SenderGitHub && event == "ping" {
		return nil
	}

	var (
		out []domain.NewDeliveryEvaluation
		ev  *domain.IntakeEvent
	)

	for _, f := range slices.Sorted(slices.Values(w.Factories())) {
		l, err := e.lineIn(ctx, f)
		if err != nil {
			continue
		}

		if ev == nil {
			d := domain.NewDeliveryEvent("", w, "", event, body, e.eventTable(w.Sender, event))
			ev = &d
		}

		if n, ok := domain.EvaluationFor(l, w, *ev); ok {
			out = append(out, n)
		}
	}

	return out
}

// EvaluateDeliveries evaluates the due delivery evaluations of every organization with loaded
// lines (evaluateDeliveries) and returns how many it finished. Tick runs it; tests call it to
// run one pass.
func (e *Engine) EvaluateDeliveries(ctx context.Context) (int, error) {
	if e.intake == nil {
		return 0, errNoIntake
	}

	e.mu.RLock()
	lines := maps.Clone(e.lines)
	e.mu.RUnlock()

	return e.evaluateDeliveries(ctx, lines)
}

// evaluateDeliveries claims, for each organization with loaded lines, up to SweepLimit of its
// pending evaluations due now, leased to this instance for domain.EvaluationLease
// (ports.Intake.ClaimEvaluations, FOR UPDATE SKIP LOCKED, so instances share them), and
// evaluates each: every trigger of its factory's line that matches the delivery fires
// (Engine.fire), and the row is done with domain.EvaluationResult. A failure goes back to pending
// after domain.EvaluationBackoff and is failed with its error after
// domain.MaxEvaluationAttempts attempts. A row whose lease ran out, because its instance died, is
// claimed again; its firings replay by their idempotency key, so nothing is created twice.
func (e *Engine) evaluateDeliveries(ctx context.Context, lines map[domain.FactoryID]domain.Line) (int, error) {
	orgs := map[domain.OrgID]bool{}
	for _, l := range lines {
		orgs[l.OrgID] = true
	}

	var (
		done int
		errs []error
	)

	for _, org := range slices.Sorted(maps.Keys(orgs)) {
		octx := domain.WithOrg(ctx, org)

		claimed, err := e.intake.ClaimEvaluations(octx, e.holder, e.clock.Now(), domain.EvaluationLease, e.cfg.SweepLimit)
		if err != nil {
			errs = append(errs, fmt.Errorf("evaluate deliveries of organization %s: claim: %w", org, err))
			continue
		}

		for _, c := range claimed {
			firings, err := e.evaluateDelivery(octx, lines, c)
			fin := domain.FinishEvaluation(c, firings, err, e.clock.Now())

			ok, ferr := e.intake.FinishEvaluation(octx, fin)
			if ferr != nil {
				errs = append(errs, fmt.Errorf("evaluate delivery %s for factory %s: finish: %w", c.Delivery, c.FactoryID, ferr))
				continue
			}

			attrs := []any{
				slog.String("org_id", string(org)), slog.String("factory", string(c.FactoryID)), slog.String("evaluation", c.ID),
				slog.String("delivery", string(c.Delivery)), slog.Int("attempt", c.Attempts), slog.String("state", string(fin.State)),
				slog.String("outcome", string(fin.Outcome)), slog.String("item", string(fin.ItemID)),
			}

			switch {
			case !ok:
				e.log.WarnContext(ctx, "delivery evaluation lost its lease", attrs...)
			case err != nil:
				e.log.WarnContext(ctx, "delivery evaluation failed", append(attrs, slog.Any("error", err))...)
			default:
				done++

				e.log.InfoContext(ctx, "delivery evaluated", attrs...)
			}
		}
	}

	return done, errors.Join(errs...)
}

// errFactoryNotLoaded means an evaluation's factory has no line on this instance.
var errFactoryNotLoaded = errors.New("the factory's line is not loaded")

// evaluateDelivery fires every trigger of c's factory that matches c's delivery and returns their
// firings, in declaration order.
func (e *Engine) evaluateDelivery(ctx context.Context, lines map[domain.FactoryID]domain.Line, c domain.ClaimedEvaluation) ([]domain.TriggerFiring, error) {
	l, ok := lines[c.FactoryID]
	if !ok {
		return nil, fmt.Errorf("factory %s: %w", c.FactoryID, errFactoryNotLoaded)
	}

	w, err := e.intake.Endpoint(ctx, c.WebhookID)
	if err != nil {
		return nil, fmt.Errorf("webhook %s: %w", c.WebhookID, err)
	}

	ev := domain.NewDeliveryEvent(l.FactoryID, w, c.DeliveryID, c.Event, c.Body, e.eventTable(w.Sender, c.Event))

	var out []domain.TriggerFiring

	for _, t := range l.Triggers {
		if !t.Matches(ev) {
			continue
		}

		f, err := e.fire(ctx, l, t, ev)
		if err != nil {
			return nil, fmt.Errorf("trigger %s: %w", t.Name, err)
		}

		out = append(out, f)
	}

	return out, nil
}
