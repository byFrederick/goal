package engine

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// scheduleWindow is how far back FireSchedules looks for a schedule's instant: an instant older
// than that (goald was down) is not fired late, but recorded in a missed firing.
const scheduleWindow = 5 * time.Minute

// maxMissed is the most instants one missed firing counts; a longer gap is recorded over several
// ticks, each starting where the last ended.
const maxMissed = 100_000

// missedPrefix starts a missed firing's idempotency key, before the gap's first instant.
const missedPrefix = "missed/"

// scheduleInstant is the field a schedule's event carries: the instant it fired for.
const scheduleInstant = "instant"

// ScheduleTick is what one FireSchedules did: whether it held the schedule lock, the firings of
// the schedule triggers (the gaps they missed, then the instants that were due), how many old
// deliveries, ledger ids and counts it deleted, and how many old firings.
type ScheduleTick struct {
	Ran           bool
	Firings       []domain.TriggerFiring
	Pruned        int
	PrunedFirings int
}

// FireSchedules fires every loaded line's enabled schedule triggers that are due at now, and
// deletes the webhook deliveries older than domain.DeliveryRetention and the ledger ids and daily
// counts older than domain.LedgerRetention (ports.Intake.PruneDeliveries), and each loaded
// factory's firings older than domain.FiringRetention whose item is not open
// (ports.Intake.PruneFirings), under the install's schedule lock (ports.Intake.WithScheduleLock,
// ADR 0026 decision 4): while another goald holds it, it does nothing and reports Ran false. A
// trigger is due for its newest cron instant within scheduleWindow before now, and fires as
// Intake fires a trigger, with that instant in RFC 3339 UTC as the idempotency key, so each
// instant creates at most one item whichever instance and however many ticks fire it. The
// instants older than that since the trigger's newest firing are a gap: one missed firing
// records them (missedGap), and none fires late. A trigger that fails does not stop the others.
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
				fs, err := e.fireSchedule(ctx, l, t, now)
				if err != nil {
					errs = append(errs, fmt.Errorf("schedules: factory %s, trigger %s: %w", l.FactoryID, t.Name, err))
				}

				tick.Firings = append(tick.Firings, fs...)
			}

			n, err := e.intake.PruneFirings(domain.WithOrg(ctx, l.OrgID), l.FactoryID, now.Add(-domain.FiringRetention))
			if err != nil {
				errs = append(errs, fmt.Errorf("schedules: factory %s: prune firings: %w", l.FactoryID, err))
			}

			tick.PrunedFirings += n
		}

		n, err := e.intake.PruneDeliveries(ctx, now)
		if err != nil {
			errs = append(errs, err)
		}

		tick.Pruned = n

		return errors.Join(errs...)
	})
	tick.Ran = ran

	return tick, err
}

// fireSchedule records t's missed gap, if it has one, then fires t when it is an enabled
// schedule trigger due at now, and returns what it recorded. A trigger with a check queues one
// (queueCheck) instead of filing an item.
func (e *Engine) fireSchedule(ctx context.Context, l domain.Line, t domain.Trigger, now time.Time) ([]domain.TriggerFiring, error) {
	if !t.Enabled || t.On != domain.OnSchedule {
		return nil, nil
	}

	c, err := domain.ParseCron(t.Cron, t.TimeZone)
	if err != nil {
		return nil, err
	}

	var out []domain.TriggerFiring

	m, missed, err := e.missedGap(ctx, l, t, c, now)
	if err != nil {
		return nil, err
	}

	if missed {
		out = append(out, m)
	}

	at := c.Latest(now.Add(-scheduleWindow), now)
	if at.IsZero() {
		return out, nil
	}

	key := at.UTC().Format(time.RFC3339)

	if t.Check != nil {
		f, err := e.queueCheck(ctx, l, t, key, now)
		if err != nil {
			return out, err
		}

		return append(out, f), nil
	}

	f, err := e.fire(ctx, l, t, domain.IntakeEvent{
		FactoryID: l.FactoryID, Source: domain.TriggerSchedule, Key: domain.OnSchedule,
		Fields: map[string]string{scheduleInstant: key}, IdempotencyKey: key,
	})
	if err != nil {
		return out, err
	}

	return append(out, f), nil
}

// missedGap records, as one missed firing (ADR 0040 decision 16), t's instants after its newest
// firing that are older than scheduleWindow before now: they were never fired, and are not fired
// late. Its key is the gap's first instant and its At the last, so the next tick starts after it
// and a replay records nothing new. A trigger that never fired has no gap. It reports whether it
// recorded one.
func (e *Engine) missedGap(ctx context.Context, l domain.Line, t domain.Trigger, c domain.Cron, now time.Time) (domain.TriggerFiring, bool, error) {
	ctx = domain.WithOrg(ctx, l.OrgID)

	newest, err := e.intake.Firings(ctx, l.FactoryID, t.Name, 1)
	if err != nil || len(newest) == 0 {
		return domain.TriggerFiring{}, false, err
	}

	first, last, n := c.Between(scheduleAnchor(newest[0].Firing), now.Add(-scheduleWindow), maxMissed)
	if n == 0 {
		return domain.TriggerFiring{}, false, nil
	}

	from, to := first.Format(time.RFC3339), last.Format(time.RFC3339)

	instants := "instants"
	if n == 1 {
		instants = "instant"
	}

	f := domain.TriggerFiring{
		FactoryID: l.FactoryID, Trigger: t.Name, Source: domain.TriggerSchedule, EventKey: domain.OnSchedule,
		IdempotencyKey: missedPrefix + from, Outcome: domain.FiringMissed, Missed: n, At: last,
		Reason: fmt.Sprintf("%d %s from %s to %s passed while no goald ran; none was fired late", n, instants, from, to),
	}

	got, fresh, err := e.intake.Fire(ctx, f, nil, domain.FireLimits{})
	if err != nil {
		return domain.TriggerFiring{}, false, fmt.Errorf("record the missed gap: %w", err)
	}

	if fresh {
		e.log.WarnContext(ctx, "trigger missed instants", slog.String("org_id", string(l.OrgID)), slog.String("factory", string(l.FactoryID)),
			slog.String("trigger", t.Name), slog.Int("missed", n), slog.String("from", from), slog.String("to", to))
	}

	return got, fresh, nil
}

// scheduleAnchor is the instant a schedule trigger's newest firing f accounts for: a missed
// firing's last instant, a fired one's instant (its key), or when it fired.
func scheduleAnchor(f domain.TriggerFiring) time.Time {
	if f.Outcome != domain.FiringMissed && f.Source == domain.TriggerSchedule {
		if at, err := time.Parse(time.RFC3339, f.IdempotencyKey); err == nil {
			return at
		}
	}

	return f.At
}
