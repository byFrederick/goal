package engine_test

import (
	"context"
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
	now := t0.Add(2 * time.Minute)  // 09:02, two minutes after the 09:00 instant

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		ticks []bool
	)

	for _, e := range []*engine.Engine{h.e, other} {
		wg.Go(func() {
			for range 3 {
				got, err := e.FireSchedules(t.Context(), now)
				if err != nil {
					t.Error(err)
				}

				mu.Lock()
				ticks = append(ticks, got.Ran)
				mu.Unlock()
			}
		})
	}

	wg.Wait()

	if n := h.items(t); n != 1 {
		t.Fatalf("%d items after six ticks of two instances; want 1", n)
	}

	f, ok, err := h.intake.Firing(inOrg(t), factoryID, "quarterly-sweep", "2026-10-23T09:00:00Z")
	if err != nil || !ok {
		t.Fatalf("Firing of the 09:00 instant = %v, %v; want it recorded", ok, err)
	}

	it, err := h.store.Item(inOrg(t), f.ItemID)
	if err != nil {
		t.Fatal(err)
	}

	if it.Title != "Sweep for 2026-10-23T09:00:00Z" || it.Source != domain.ItemFromSchedule || f.Source != domain.TriggerSchedule {
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

	h := intakeHarness(t)
	ctx := inOrg(t)

	s, err := h.intake.CreateEndpoint(ctx, domain.NewWebhookEndpoint{FactoryID: factoryID, Name: "alerts", Kind: domain.WebhookGeneric})
	if err != nil {
		t.Fatal(err)
	}

	for _, at := range []time.Time{t0.Add(-31 * 24 * time.Hour), t0.Add(-29 * 24 * time.Hour)} {
		if _, err := h.intake.RecordDelivery(ctx, domain.NewWebhookDelivery{
			EndpointID: s.Endpoint.ID, ReceivedAt: at, Outcome: domain.DeliveryAccepted,
		}); err != nil {
			t.Fatal(err)
		}
	}

	got, err := h.e.FireSchedules(t.Context(), t0)
	if err != nil || got.Pruned != 1 {
		t.Errorf("FireSchedules = %+v, %v; want one delivery pruned", got, err)
	}
}
