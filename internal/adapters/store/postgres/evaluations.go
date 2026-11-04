package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// insertEvaluations adds n's evaluations of the delivery row in q's transaction: pending, or
// done and skipped at once with the reason.
func insertEvaluations(ctx context.Context, q *db.Queries, row uuid.UUID, n domain.NewReceipt) error {
	for _, e := range n.Evaluations {
		factory, err := parseID("factory", string(e.FactoryID))
		if err != nil {
			return err
		}

		id, err := newID()
		if err != nil {
			return err
		}

		p := db.InsertDeliveryEvaluationParams{
			ID: id, DeliveryRow: row, FactoryID: factory, Triggers: e.Triggers, State: string(domain.EvaluationPending),
			At: n.ReceivedAt.UTC(),
		}
		if e.Skip != "" {
			p.State, p.Outcome, p.Reason = string(domain.EvaluationDone), new(string(domain.EvaluationSkipped)), e.Skip
		}

		if err := q.InsertDeliveryEvaluation(ctx, p); err != nil {
			return fmt.Errorf("evaluation for factory %s: %w", e.FactoryID, err)
		}
	}

	return nil
}

// DeliveryEvaluations implements ports.Intake.
func (in *Intake) DeliveryEvaluations(ctx context.Context, delivery domain.WebhookDeliveryID) ([]domain.DeliveryEvaluation, error) {
	row, err := parseID("delivery", string(delivery))
	if err != nil {
		return nil, err
	}

	rows, err := fetch(ctx, in.s.read, func(q *db.Queries) ([]db.DeliveryEvaluationsRow, error) {
		return q.DeliveryEvaluations(ctx, row)
	})
	if err != nil {
		return nil, fmt.Errorf("evaluations of delivery %s: %w", delivery, err)
	}

	out := make([]domain.DeliveryEvaluation, 0, len(rows))

	for _, r := range rows {
		e := domain.DeliveryEvaluation{
			ID: r.ID.String(), FactoryID: domain.FactoryID(r.FactoryID.String()), Delivery: delivery,
			WebhookID: domain.WebhookEndpointID(r.EndpointID.String()), DeliveryID: r.DeliveryID, Event: r.Event, Triggers: r.Triggers,
			State: domain.EvaluationState(r.State), Attempts: int(r.Attempts), NextAt: r.NextAttemptAt.UTC(), LeaseHolder: deref(r.LeaseHolder),
			LeaseUntil: utcPtr(r.LeaseUntil), Outcome: domain.EvaluationOutcome(deref(r.Outcome)), Reason: r.Reason, CreatedAt: r.CreatedAt.UTC(),
		}
		if r.ItemID != nil {
			e.ItemID = domain.ItemID(r.ItemID.String())
		}

		out = append(out, e)
	}

	return out, nil
}

// ClaimEvaluations implements ports.Intake.
func (in *Intake) ClaimEvaluations(ctx context.Context, holder string, now time.Time, lease time.Duration, limit int,
) ([]domain.ClaimedEvaluation, error) {
	var rows []db.ClaimDeliveryEvaluationsRow

	err := in.s.inTx(ctx, func(q *db.Queries) error {
		var err error

		rows, err = q.ClaimDeliveryEvaluations(ctx, db.ClaimDeliveryEvaluationsParams{
			Now: now, Max: int32(min(max(limit, 0), maxClaim)), Holder: holder, Until: now.Add(lease), //nolint:gosec // bounded by maxClaim
		})

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("claim delivery evaluations: %w", err)
	}

	out := make([]domain.ClaimedEvaluation, 0, len(rows))

	for _, r := range rows {
		out = append(out, domain.ClaimedEvaluation{
			DeliveryEvaluation: domain.DeliveryEvaluation{
				ID: r.ID.String(), FactoryID: domain.FactoryID(r.FactoryID.String()), Delivery: domain.WebhookDeliveryID(r.DeliveryRow.String()),
				WebhookID: domain.WebhookEndpointID(r.EndpointID.String()), DeliveryID: r.DeliveryID, Event: r.Event, Triggers: r.Triggers,
				State: domain.EvaluationState(r.State), Attempts: int(r.Attempts), NextAt: r.NextAttemptAt.UTC(),
				LeaseHolder: deref(r.LeaseHolder), LeaseUntil: utcPtr(r.LeaseUntil), CreatedAt: r.CreatedAt.UTC(),
			},
			Body: r.Body,
		})
	}

	return out, nil
}

// FinishEvaluation implements ports.Intake.
func (in *Intake) FinishEvaluation(ctx context.Context, f domain.EvaluationFinish) (bool, error) {
	id, err := parseID("evaluation", f.ID)
	if err != nil {
		return false, err
	}

	item, err := optionalID("item", string(f.ItemID))
	if err != nil {
		return false, err
	}

	p := db.FinishDeliveryEvaluationParams{
		ID: id, Holder: f.Holder, Attempts: int32(min(max(f.Attempts, 0), maxInt32)), State: string(f.State), //nolint:gosec // bounded
		Reason: f.Reason, ItemID: item, At: f.At,
	}
	if f.Outcome != "" {
		p.Outcome = new(string(f.Outcome))
	}

	if !f.NextAt.IsZero() {
		p.NextAttemptAt = &f.NextAt
	}

	n, err := fetch(ctx, in.s.inTx, func(q *db.Queries) (int64, error) { return q.FinishDeliveryEvaluation(ctx, p) })
	if err != nil {
		return false, fmt.Errorf("finish delivery evaluation %s: %w", f.ID, ruleError(err, ""))
	}

	return n == 1, nil
}

// Fire implements ports.Intake: the trigger's transaction advisory lock (LockTriggerFiring) is
// taken first, then the counts are read and the item filed with fileItem, the door's helper, in
// the same transaction as the firing.
func (in *Intake) Fire(ctx context.Context, f domain.TriggerFiring, item *domain.NewItem, limits domain.FireLimits) (domain.TriggerFiring, bool, error) {
	if err := f.Check(); err != nil {
		return domain.TriggerFiring{}, false, fmt.Errorf("fire: %w", err)
	}

	factory, err := parseID("factory", string(f.FactoryID))
	if err != nil {
		return domain.TriggerFiring{}, false, err
	}

	id, err := newID()
	if err != nil {
		return domain.TriggerFiring{}, false, err
	}

	var (
		out   domain.TriggerFiring
		fresh bool
	)

	err = in.s.inTx(ctx, func(q *db.Queries) error {
		if err := q.LockTriggerFiring(ctx, db.LockTriggerFiringParams{FactoryID: factory, Trigger: f.Trigger}); err != nil {
			return fmt.Errorf("lock the trigger: %w", err)
		}

		r, err := q.TriggerFiringByKey(ctx, db.TriggerFiringByKeyParams{FactoryID: factory, Trigger: f.Trigger, IdempotencyKey: f.IdempotencyKey})
		if err == nil {
			out = firingFrom(r)

			return nil
		}

		if !notFound(err) {
			return err
		}

		g := f
		if item != nil && g.Outcome == domain.FiringCreated {
			if err := fileFiring(ctx, q, factory, &g, *item, limits); err != nil {
				return err
			}
		}

		itemID, err := optionalID("item", string(g.ItemID))
		if err != nil {
			return err
		}

		if _, err := q.InsertTriggerFiring(ctx, db.InsertTriggerFiringParams{
			ID: id, FactoryID: factory, Trigger: g.Trigger, Source: string(g.Source), EventKey: g.EventKey,
			IdempotencyKey: g.IdempotencyKey, Dedupe: g.Dedupe, Outcome: string(g.Outcome), Reason: g.Reason, ItemID: itemID, At: g.At,
		}); err != nil {
			return err
		}

		out, fresh = g, true
		out.ID, out.At = id.String(), g.At.UTC().Truncate(time.Microsecond)

		return nil
	})
	if isForeignKeyViolation(err) {
		return domain.TriggerFiring{}, false, fmt.Errorf("fire %s: factory %s: %w", f.Trigger, f.FactoryID, ports.ErrNotFound)
	}

	if err != nil {
		return domain.TriggerFiring{}, false, fmt.Errorf("fire %s: %w", f.Trigger, ruleError(err, ""))
	}

	return out, fresh, nil
}

// fileFiring reads the trigger's counts under its lock and, when limits let f create its item,
// files item and sets f's ItemID; otherwise it makes f skipped with limits' reason.
func fileFiring(ctx context.Context, q *db.Queries, factory uuid.UUID, f *domain.TriggerFiring, item domain.NewItem, limits domain.FireLimits) error {
	var c domain.FireCounts

	if f.Dedupe != "" {
		open, err := q.OpenItemByDedupe(ctx, db.OpenItemByDedupeParams{FactoryID: factory, Trigger: f.Trigger, Value: f.Dedupe})

		switch {
		case err == nil:
			c.OpenDedupe = domain.ItemID(open.String())
		case !notFound(err):
			return fmt.Errorf("open item by dedupe: %w", err)
		}
	}

	today, err := q.TriggerCreatedSince(ctx, db.TriggerCreatedSinceParams{FactoryID: factory, Trigger: f.Trigger, Since: f.At.UTC().Truncate(24 * time.Hour)})
	if err != nil {
		return fmt.Errorf("items created today: %w", err)
	}

	hour, err := q.TriggerCreatedSince(ctx, db.TriggerCreatedSinceParams{FactoryID: factory, Trigger: f.Trigger, Since: f.At.Add(-time.Hour)})
	if err != nil {
		return fmt.Errorf("items created in the last hour: %w", err)
	}

	c.Today, c.LastHour = int(today), int(hour)

	if why := limits.Skip(c); why != "" {
		f.Outcome, f.Reason = domain.FiringSkipped, why

		return nil
	}

	row, err := fileItem(ctx, q, filingOf(item, factory))
	if err != nil {
		return err
	}

	f.ItemID = domain.ItemID(row.ID.String())

	return nil
}
