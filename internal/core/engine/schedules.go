package engine

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// scheduleWindow is how far back FireSchedules looks for a schedule's instant: an instant older
// than that (goald was down) is not fired late.
const scheduleWindow = 5 * time.Minute

// scheduleInstant is the field a schedule's event carries: the instant it fired for.
const scheduleInstant = "instant"

// ScheduleTick is what one FireSchedules did: whether it held the schedule lock, the firings of
// the schedule triggers that were due, and how many old deliveries it deleted.
type ScheduleTick struct {
	Ran     bool
	Firings []domain.TriggerFiring
	Pruned  int
}

// FireSchedules fires every loaded line's enabled schedule triggers that are due at now, and
// deletes the webhook deliveries older than domain.DeliveryRetention, under the install's
// schedule lock (ports.Intake.WithScheduleLock, ADR 0026 decision 4): while another goald holds
// it, it does nothing and reports Ran false. A trigger is due for its newest cron instant within
// scheduleWindow before now, and fires as Intake fires a trigger, with that instant in RFC 3339
// UTC as the idempotency key, so each instant creates at most one item whichever instance and
// however many ticks fire it. A trigger that fails does not stop the others.
func (e *Engine) FireSchedules(ctx context.Context, now time.Time) (ScheduleTick, error) {
	if e.intake == nil {
		return ScheduleTick{}, errNoIntake
	}

	var tick ScheduleTick

	ran, err := e.intake.WithScheduleLock(ctx, func(ctx context.Context) error {
		e.mu.RLock()
		lines := slices.SortedFunc(maps.Values(e.lines), func(a, b domain.Line) int { return cmp.Compare(a.FactoryID, b.FactoryID) })
		e.mu.RUnlock()

		var errs []error

		for _, l := range lines {
			for _, t := range l.Triggers {
				f, due, err := e.fireSchedule(ctx, l, t, now)

				switch {
				case err != nil:
					errs = append(errs, fmt.Errorf("schedules: factory %s, trigger %s: %w", l.FactoryID, t.Name, err))
				case due:
					tick.Firings = append(tick.Firings, f)
				}
			}
		}

		n, err := e.intake.PruneDeliveries(ctx, now.Add(-domain.DeliveryRetention))
		if err != nil {
			errs = append(errs, err)
		}

		tick.Pruned = n

		return errors.Join(errs...)
	})
	tick.Ran = ran

	return tick, err
}

// fireSchedule fires t when it is an enabled schedule trigger due at now, and reports whether it
// was.
func (e *Engine) fireSchedule(ctx context.Context, l domain.Line, t domain.Trigger, now time.Time) (domain.TriggerFiring, bool, error) {
	if !t.Enabled || t.On != domain.OnSchedule {
		return domain.TriggerFiring{}, false, nil
	}

	c, err := domain.ParseCron(t.Cron, t.TimeZone)
	if err != nil {
		return domain.TriggerFiring{}, false, err
	}

	at := c.Latest(now.Add(-scheduleWindow), now)
	if at.IsZero() {
		return domain.TriggerFiring{}, false, nil
	}

	key := at.UTC().Format(time.RFC3339)

	f, err := e.fire(ctx, l, t, domain.IntakeEvent{
		FactoryID: l.FactoryID, Source: domain.TriggerSchedule, Key: domain.OnSchedule,
		Fields: map[string]string{scheduleInstant: key}, IdempotencyKey: key,
	})

	return f, err == nil, err
}
