package engine_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/engine"
)

// everyQuarter is a schedule trigger that fires every 15 minutes at the work station.
func everyQuarter() domain.Trigger {
	return domain.Trigger{
		Name: "quarterly-sweep", Enabled: true, On: domain.OnSchedule, Cron: "*/15 * * * *",
		Create: domain.TriggerCreate{Type: typeFeature, Station: stWork, Title: "Sweep for {{instant}}"},
	}
}

func TestSchedules_FireOncePerInstantAcrossInstances(t *testing.T) {
	t.Parallel()

	h := intakeHarness(t, everyQuarter())
	other := h.engine(t, h.store) // a second goald on the same database

	// 09:02, two minutes after the 09:00 instant: both instances tick three times at once.
	fireConcurrently(t, t0.Add(2*time.Minute), h.e, other)

	if n := h.items(t); n != 1 {
		t.Fatalf("%d items after six ticks of two instances; want 1", n)
	}

	f, ok, err := h.intake.Firing(inOrg(t), factoryID, "quarterly-sweep", "2026-10-23T09:00:00Z")
	if err != nil || !ok {
		t.Fatalf("Firing of the 09:00 instant = %v, %v; want it recorded", ok, err)
	}

	if it := h.item(t, f.ItemID); it.Title != `Sweep for "2026-10-23T09:00:00Z"` || it.Source != domain.ItemFromSchedule || f.Source != domain.TriggerSchedule {
		t.Errorf("item %q from %s, firing from %s; want the schedule's", it.Title, it.Source, f.Source)
	}

	// The next instant fires once more; a tick with no instant in its window fires nothing.
	got, err := other.FireSchedules(t.Context(), t0.Add(16*time.Minute))
	if err != nil || !got.Ran || len(got.Firings) != 1 || got.Firings[0].IdempotencyKey != "2026-10-23T09:15:00Z" {
		t.Fatalf("FireSchedules at 09:16 = %+v, %v; want the 09:15 instant", got, err)
	}

	if got, err := h.e.FireSchedules(t.Context(), t0.Add(25*time.Minute)); err != nil || len(got.Firings) != 0 {
		t.Errorf("FireSchedules at 09:25 = %+v, %v; want nothing due", got, err)
	}
}

// fireConcurrently runs three FireSchedules at now on each engine, all at once.
func fireConcurrently(t *testing.T, now time.Time, engines ...*engine.Engine) {
	t.Helper()

	var wg sync.WaitGroup

	for _, e := range engines {
		wg.Go(func() {
			for range 3 {
				if _, err := e.FireSchedules(t.Context(), now); err != nil {
					t.Error(err)
				}
			}
		})
	}

	wg.Wait()
}

func TestSchedules_SkippedWhileAnotherHoldsTheLock(t *testing.T) {
	t.Parallel()

	h := intakeHarness(t, everyQuarter())

	ran, err := h.intake.WithScheduleLock(t.Context(), func(ctx context.Context) error {
		got, err := h.e.FireSchedules(ctx, t0)
		if err != nil || got.Ran || len(got.Firings) != 0 {
			t.Errorf("FireSchedules while the lock is held = %+v, %v; want it skipped", got, err)
		}

		return nil
	})
	if err != nil || !ran {
		t.Fatalf("WithScheduleLock = %v, %v", ran, err)
	}

	if n := h.items(t); n != 0 {
		t.Errorf("%d items; want none", n)
	}
}

func TestSchedules_PruneOldDeliveries(t *testing.T) {
	t.Parallel()

	h, w := deliveryHarness(t)
	ctx := inOrg(t)

	for _, at := range []time.Time{t0.Add(-31 * 24 * time.Hour), t0.Add(-29 * 24 * time.Hour)} {
		if _, err := h.intake.RecordDelivery(ctx, domain.NewWebhookDelivery{
			EndpointID: w.ID, ReceivedAt: at, Outcome: domain.DeliveryAccepted,
		}); err != nil {
			t.Fatal(err)
		}
	}

	got, err := h.e.FireSchedules(t.Context(), t0)
	if err != nil || got.Pruned != 1 {
		t.Errorf("FireSchedules = %+v, %v; want one delivery pruned", got, err)
	}
}

// A schedule that misses instants by more than five minutes, because no goald ran, records one
// missed firing for the gap with how many instants it covers, creates nothing for them and never
// records the same gap twice (ADR 0040 decision 16).
func TestSchedules_MissedGapRecordedOnceAndNotFiredLate(t *testing.T) {
	t.Parallel()

	h := intakeHarness(t, everyQuarter())

	if _, err := h.e.FireSchedules(t.Context(), t0.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}

	// Down from 09:02 to 10:07: 09:15, 09:30, 09:45 and 10:00 passed by more than five minutes.
	got, err := h.e.FireSchedules(t.Context(), t0.Add(67*time.Minute))
	if err != nil || len(got.Firings) != 1 {
		t.Fatalf("FireSchedules at 10:07 = %+v, %v; want one missed firing", got, err)
	}

	checkMissedGap(t, h, got.Firings[0])

	for _, at := range []time.Duration{68 * time.Minute, 69 * time.Minute} {
		if got, err := h.e.FireSchedules(t.Context(), t0.Add(at)); err != nil || len(got.Firings) != 0 {
			t.Errorf("FireSchedules %s after 09:00 = %+v, %v; want the gap recorded once", at, got, err)
		}
	}

	// Running again, the next instant fires as usual.
	got, err = h.e.FireSchedules(t.Context(), t0.Add(76*time.Minute))
	if err != nil || len(got.Firings) != 1 || got.Firings[0].Outcome != domain.FiringCreated || got.Firings[0].IdempotencyKey != "2026-10-23T10:15:00Z" {
		t.Errorf("FireSchedules at 10:16 = %+v, %v; want the 10:15 instant created", got, err)
	}
}

// checkMissedGap checks m is the gap of 09:15 to 10:00 and that none of it was fired late.
func checkMissedGap(t *testing.T, h *harness, m domain.TriggerFiring) {
	t.Helper()

	if m.Outcome != domain.FiringMissed || m.Missed != 4 || m.ItemID != "" || m.IdempotencyKey != "missed/2026-10-23T09:15:00Z" ||
		!m.At.Equal(t0.Add(time.Hour)) || !strings.Contains(m.Reason, "4 instants") {
		t.Errorf("missed firing = %+v; want 4 instants from 09:15, at 10:00", m)
	}

	if n := h.items(t); n != 1 {
		t.Errorf("%d items; want only the 09:00 instant's, none fired late", n)
	}
}

// A trigger that never fired has no gap to record: its first tick fires only the instant in its
// window, as a new trigger does.
func TestSchedules_MissedGapNeedsAnEarlierFiring(t *testing.T) {
	t.Parallel()

	h := intakeHarness(t, everyQuarter())

	got, err := h.e.FireSchedules(t.Context(), t0.Add(67*time.Minute))
	if err != nil || len(got.Firings) != 0 {
		t.Fatalf("FireSchedules = %+v, %v; want nothing recorded", got, err)
	}

	if list, err := h.intake.Firings(inOrg(t), factoryID, "quarterly-sweep", 10); err != nil || len(list) != 0 {
		t.Errorf("firings = %+v, %v; want none", list, err)
	}
}

// Each tick prunes every loaded factory's firings older than domain.FiringRetention, but for one
// whose item is still open.
func TestSchedules_PruneOldFirings(t *testing.T) {
	t.Parallel()

	h := intakeHarness(t)
	ctx := inOrg(t)
	old := t0.Add(-domain.FiringRetention - time.Hour)

	for _, key := range []string{"old-1", "old-2"} {
		if _, _, err := h.intake.RecordFiring(ctx, domain.TriggerFiring{
			FactoryID: factoryID, Trigger: "alerts", Source: domain.TriggerWebhook, EventKey: "alerts", IdempotencyKey: key,
			Outcome: domain.FiringSkipped, At: old,
		}); err != nil {
			t.Fatal(err)
		}
	}

	got, err := h.e.FireSchedules(t.Context(), t0)
	if err != nil || got.PrunedFirings != 2 {
		t.Errorf("FireSchedules = %+v, %v; want two firings pruned", got, err)
	}
}
