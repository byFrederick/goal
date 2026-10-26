package engine_test

import (
	"errors"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/engine"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

// assemblyAtWork is an assembly at work of four parts: building (in progress, its lease held),
// queued, stuck (blocked by failed checks) and shipped (done).
func (h *harness) assemblyAtWork(t *testing.T) (domain.Item, map[string]domain.Item) {
	t.Helper()

	it := h.at(t, h.submit(t, domain.NewItem{Title: "big request"}), stWork, domain.StateReady)

	parent, parts, err := h.store.Assemble(inOrg(t), ports.Assembly{
		Transition: ports.Transition{
			ItemID: it.ID, From: it.Position(), To: domain.Position{Station: stWork, State: domain.StateAssembly}, At: h.clock.Now(),
		},
		Parts: []domain.NewPart{{Key: "building", Title: "building"}, {Key: "queued", Title: "queued"}, {Key: "stuck", Title: "stuck"}, {Key: "shipped", Title: "shipped"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	byKey := map[string]domain.Item{}
	for _, p := range parts.Items {
		byKey[p.PartKey] = p
	}

	byKey["stuck"] = h.at(t, byKey["stuck"], stWork, domain.StateBlocked)
	byKey["shipped"] = h.at(t, byKey["shipped"], stDone, domain.StateDone)

	st, _ := h.line.Station(stWork)

	claimed, _, err := h.store.ClaimNext(inOrg(t), ports.Claim{
		FactoryID: factoryID, Station: st, Slot: 0, Now: h.clock.Now(), ExpiresAt: h.clock.Now().Add(ttl),
	})
	if err != nil || claimed.ID != byKey["building"].ID {
		t.Fatalf("claimed %q, %v; want the building part", claimed.Title, err)
	}

	byKey["building"] = claimed

	return parent, byKey
}

// Pausing an assembly blocks each of its open parts that is not blocked yet, where it is, each
// with its own paused event and audit entry naming the assembly, and the lease of one in
// progress taken; the assembly stays an assembly with one paused event. A second pause has
// nothing left to block.
func TestPause_AnAssemblyPausesItsOpenParts(t *testing.T) {
	t.Parallel()

	h := newHarness(t, controlLine(), porttest.Succeed(`{}`))
	a, parts := h.assemblyAtWork(t)

	got, err := h.e.Pause(inOrg(t), a.ID, human, "the vendor is down")
	if err != nil {
		t.Fatal(err)
	}

	if got.ID != a.ID || got.State != domain.StateAssembly || h.item(t, a.ID).State != domain.StateAssembly {
		t.Errorf("Pause of an assembly returned %s in %s; want the assembly, still an assembly", got.ID, got.State)
	}

	want := map[string]domain.Position{
		"building": {Station: stWork, State: domain.StateBlocked}, "queued": {Station: stWork, State: domain.StateBlocked},
		"stuck": {Station: stWork, State: domain.StateBlocked}, "shipped": {Station: stDone, State: domain.StateDone},
	}

	for key, p := range parts {
		if pos := h.item(t, p.ID).Position(); pos != want[key] {
			t.Errorf("part %s after the pause at %+v; want %+v", key, pos, want[key])
		}
	}

	for _, key := range []string{"building", "queued"} {
		paused := h.eventsOf(parts[key].ID, domain.EventPaused)
		if len(paused) != 1 || paused[0].Data["assembly"] != string(a.ID) || paused[0].Data["reason"] != "the vendor is down" || paused[0].Data["actor"] != human.Ref() {
			t.Errorf("paused events of %s = %+v; want one by the person naming the assembly and the reason", key, paused)
		}
	}

	for _, key := range []string{"stuck", "shipped"} {
		if n := len(h.eventsOf(parts[key].ID, domain.EventPaused)); n != 0 {
			t.Errorf("part %s has %d paused events; want none", key, n)
		}
	}

	if n := len(h.eventsOf(parts["building"].ID, domain.EventLeaseTaken)); n != 1 {
		t.Errorf("lease_taken events of the building part = %d; want 1", n)
	}

	if ls, _ := h.store.Leases(inOrg(t), factoryID, stWork); len(ls) != 0 {
		t.Errorf("leases at work after the pause = %+v; want the building part's taken", ls)
	}

	if paused := h.eventsOf(a.ID, domain.EventPaused); len(paused) != 1 || paused[0].Data["parts"] != 2 {
		t.Errorf("paused events of the assembly = %+v; want one with parts 2", paused)
	}

	audited := h.audited(t, domain.AuditItemPaused)
	if len(audited) != 2 {
		t.Fatalf("item.paused audit entries = %+v; want one per paused part", audited)
	}

	for _, e := range audited {
		if e.Data["assembly"] != string(a.ID) || e.Data["reason"] != "the vendor is down" || (e.TargetID != string(parts["building"].ID) && e.TargetID != string(parts["queued"].ID)) {
			t.Errorf("audit entry %+v; want one of a paused part naming the assembly and the reason", e)
		}
	}

	if _, err := h.e.Pause(inOrg(t), a.ID, human, ""); !errors.Is(err, engine.ErrAlreadyBlocked) {
		t.Errorf("Pause of a paused assembly: err = %v; want ErrAlreadyBlocked", err)
	}

	if n := len(h.eventsOf(a.ID, domain.EventPaused)); n != 1 {
		t.Errorf("paused events of the assembly after a second pause = %d; want 1", n)
	}
}

// Retrying an assembly sends each blocked part back to its station's arrival state, whatever
// blocked it, with a retried event each and one on the assembly; done parts stay done and the
// assembly stays an assembly. With no blocked part left there is nothing to retry.
func TestRetry_AnAssemblyResumesItsBlockedParts(t *testing.T) {
	t.Parallel()

	h := newHarness(t, controlLine(), porttest.Succeed(`{}`))
	a, parts := h.assemblyAtWork(t)

	if _, err := h.e.Retry(inOrg(t), a.ID, human); err != nil {
		t.Fatalf("Retry of an assembly with a blocked part: %v", err)
	}

	if pos := h.item(t, parts["stuck"].ID).Position(); pos != (domain.Position{Station: stWork, State: domain.StateQueued}) {
		t.Errorf("stuck part after the retry at %+v; want queued at work", pos)
	}

	if _, err := h.e.Pause(inOrg(t), a.ID, human, ""); err != nil {
		t.Fatal(err)
	}

	got, err := h.e.Retry(inOrg(t), a.ID, human)
	if err != nil {
		t.Fatal(err)
	}

	if got.ID != a.ID || got.State != domain.StateAssembly || h.item(t, a.ID).State != domain.StateAssembly {
		t.Errorf("Retry of an assembly returned %s in %s; want the assembly, still an assembly", got.ID, got.State)
	}

	retried := map[string]int{"building": 1, "queued": 1, "stuck": 2, "shipped": 0}
	for key, p := range parts {
		pos := h.item(t, p.ID).Position()
		if key == "shipped" && pos != (domain.Position{Station: stDone, State: domain.StateDone}) ||
			key != "shipped" && pos != (domain.Position{Station: stWork, State: domain.StateQueued}) {
			t.Errorf("part %s after the retry at %+v; want queued at work (done for shipped)", key, pos)
		}

		events := h.eventsOf(p.ID, domain.EventRetried)
		if len(events) != retried[key] {
			t.Errorf("retried events of %s = %d; want %d", key, len(events), retried[key])
		}

		for _, e := range events {
			if e.Data["assembly"] != string(a.ID) || e.Data["actor"] != human.Ref() {
				t.Errorf("retried event of %s = %+v; want one by the person naming the assembly", key, e)
			}
		}
	}

	if events := h.eventsOf(a.ID, domain.EventRetried); len(events) != 2 || events[1].Data["parts"] != 3 {
		t.Errorf("retried events of the assembly = %+v; want two, the second with parts 3", events)
	}

	if _, err := h.e.Retry(inOrg(t), a.ID, human); !errors.Is(err, engine.ErrNotBlocked) {
		t.Errorf("Retry of an assembly with no blocked part: err = %v; want ErrNotBlocked", err)
	}

	if s := h.item(t, a.ID).State; s != domain.StateAssembly {
		t.Errorf("assembly after the retries is %q; want assembly", s)
	}
}
