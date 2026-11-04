package intaketest

import (
	"errors"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// trimTests are the suite's cases for ADR 0040 decisions 16 and 18: a schedule's missed firing,
// the 90-day prune of firings, and the stored delivery a try reads.
var trimTests = []struct {
	name string
	test func(f *fixture)
}{
	{"Firing_MissedKeepsItsCount", testMissedFiring},
	{"Firing_PruneKeepsOpenItemsAndNewerFirings", testPruneFirings},
	{"Delivery_BySenderIDReadsTheKeptBody", testDeliveryBySender},
}

// A schedule's missed firing is recorded with how many instants it covers, through Fire and read
// back by its key and in the trigger's list; a second one with the same key is a no-op.
func testMissedFiring(f *fixture) {
	t := f.t
	missed := domain.TriggerFiring{
		FactoryID: f.factory, Trigger: nightly, Source: domain.TriggerSchedule, EventKey: domain.OnSchedule,
		IdempotencyKey: "missed/2026-10-01T09:00:00Z", Outcome: domain.FiringMissed, Missed: 3,
		Reason: "3 instants were not fired", At: t0.Add(-time.Hour),
	}

	got, fresh, err := f.in.Fire(f.ctx(), missed, nil, domain.FireLimits{})
	if err != nil || !fresh || got.Outcome != domain.FiringMissed || got.Missed != 3 || got.ItemID != "" {
		t.Fatalf("Fire a missed firing = %+v, %v, %v; want it recorded with 3 instants", got, fresh, err)
	}

	again, fresh, err := f.in.Fire(f.ctx(), missed, nil, domain.FireLimits{})
	if err != nil || fresh || again.ID != got.ID {
		t.Errorf("Fire the same gap again = %+v, %v, %v; want the first, nothing new", again, fresh, err)
	}

	byKey, ok, err := f.in.Firing(f.ctx(), f.factory, nightly, missed.IdempotencyKey)
	if err != nil || !ok || byKey != got {
		t.Errorf("Firing = %+v, %v, %v; want %+v", byKey, ok, err, got)
	}

	list, err := f.in.Firings(f.ctx(), f.factory, nightly, 5)
	if err != nil || len(list) != 1 || list[0].Firing != got {
		t.Errorf("Firings = %+v, %v; want the missed firing", list, err)
	}

	if _, _, err := f.in.RecordFiring(f.ctx(), domain.TriggerFiring{
		FactoryID: f.factory, Trigger: nightly, Source: domain.TriggerSchedule, EventKey: domain.OnSchedule,
		IdempotencyKey: "missed/none", Outcome: domain.FiringMissed, At: t0,
	}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("a missed firing of no instant = %v; want ErrInvalid", err)
	}
}

// PruneFirings deletes the factory's firings from before the cutoff, but for one whose item is
// still open; newer firings, another factory's and another organization's stay.
func testPruneFirings(f *fixture) {
	t := f.t
	cutoff := t0.Add(-domain.FiringRetention)
	old := cutoff.Add(-time.Hour)

	open := f.item(domain.ItemFromGitHub, flaky, old)
	done := f.item(domain.ItemFromGitHub, flaky, old)
	f.close(done, domain.StateDone)

	keptOpen, _ := f.fire(flaky, "old-open", "unit", open.ID, old)
	f.fire(flaky, "old-done", "unit", done.ID, old)
	f.fire(flaky, "old-skipped", "", "", old)
	newer, _ := f.fire(flaky, "newer", "", "", cutoff.Add(time.Minute))

	otherFactory := f.NewFactory(t, f.org)
	if _, _, err := f.in.RecordFiring(f.ctx(), domain.TriggerFiring{
		FactoryID: otherFactory, Trigger: flaky, Source: domain.TriggerGitHub, EventKey: "github.check_run.failed",
		IdempotencyKey: "other-factory", Outcome: domain.FiringSkipped, At: old,
	}); err != nil {
		t.Fatalf("RecordFiring in another factory: %v", err)
	}

	n, err := f.in.PruneFirings(f.ctx(), f.factory, cutoff)
	if err != nil || n != 2 {
		t.Fatalf("PruneFirings = %d, %v; want the done item's and the skipped firing", n, err)
	}

	list, err := f.in.Firings(f.ctx(), f.factory, flaky, 10)
	if err != nil {
		t.Fatal(err)
	}

	ids := make([]string, 0, len(list))
	for _, e := range list {
		ids = append(ids, e.Firing.ID)
	}

	if want := []string{newer.ID, keptOpen.ID}; !slices.Equal(ids, want) {
		t.Errorf("firings left = %v; want the newer one and the open item's %v", ids, want)
	}

	if others, err := f.in.Firings(f.ctx(), otherFactory, flaky, 10); err != nil || len(others) != 1 {
		t.Errorf("another factory's firings = %+v, %v; want its one firing kept", others, err)
	}

	if n, err := f.in.PruneFirings(f.ctx(), f.factory, cutoff); err != nil || n != 0 {
		t.Errorf("a second PruneFirings = %d, %v; want nothing", n, err)
	}
}

// DeliveryBySender reads the webhook's kept delivery by the sender's id, with its body; another
// id, another webhook and another organization read none.
func testDeliveryBySender(f *fixture) {
	t := f.t
	w := f.webhook(orgHook)
	r := f.receive(w.ID, firstDelivery, t0, true)

	got, err := f.in.DeliveryBySender(f.ctx(), w.ID, firstDelivery)
	if err != nil || got.ID != r.Row || got.DeliveryID != firstDelivery || got.Event != pushEvent || string(got.Body) != `{"ref":"main"}` {
		t.Fatalf("DeliveryBySender = %+v, %v; want the kept delivery with its body", got, err)
	}

	other := f.NewOrg(t)
	for name, read := range map[string]func() error{
		"another id": func() error { _, err := f.in.DeliveryBySender(f.ctx(), w.ID, "d-none"); return err },
		"no id":      func() error { _, err := f.in.DeliveryBySender(f.ctx(), w.ID, ""); return err },
		"another organization": func() error {
			_, err := f.in.DeliveryBySender(domain.WithOrg(t.Context(), other), w.ID, firstDelivery)
			return err
		},
	} {
		if err := read(); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("%s: DeliveryBySender = %v; want ErrNotFound", name, err)
		}
	}
}
