package engine_test

import (
	"errors"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/engine"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

// The parts of assemblyAtWork, by key.
const (
	partBuilding = "building"
	partQueued   = "queued"
	partStuck    = "stuck"
	partShipped  = "shipped"
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
		Parts: []domain.NewPart{{Key: partBuilding, Title: partBuilding}, {Key: partQueued, Title: partQueued}, {Key: partStuck, Title: partStuck}, {Key: partShipped, Title: partShipped}},
	})
	if err != nil {
		t.Fatal(err)
	}

	byKey := map[string]domain.Item{}
	for _, p := range parts.Items {
		byKey[p.PartKey] = p
	}

	byKey[partStuck] = h.at(t, byKey[partStuck], stWork, domain.StateBlocked)
	byKey[partShipped] = h.at(t, byKey[partShipped], stDone, domain.StateDone)

	st, _ := h.line.Station(stWork)

	claimed, _, err := h.store.ClaimNext(inOrg(t), ports.Claim{
		FactoryID: factoryID, Station: st, Slot: 0, Now: h.clock.Now(), ExpiresAt: h.clock.Now().Add(ttl),
	})
	if err != nil || claimed.ID != byKey[partBuilding].ID {
		t.Fatalf("claimed %q, %v; want the building part", claimed.Title, err)
	}

	byKey[partBuilding] = claimed

	return parent, byKey
}

// atPositions checks that each of parts is where want says.
func (h *harness) atPositions(t *testing.T, after string, parts map[string]domain.Item, want map[string]domain.Position) {
	t.Helper()

	for key, p := range parts {
		if pos := h.item(t, p.ID).Position(); pos != want[key] {
			t.Errorf("part %s after the %s at %+v; want %+v", key, after, pos, want[key])
		}
	}
}

// Pausing an assembly blocks each of its open parts that is not blocked yet, where it is, each
// with its own paused event and audit entry naming the assembly, and the lease of one in
// progress taken; the assembly stays an assembly with one paused event. A second pause has
// nothing left to block.
func TestPause_AnAssemblyPausesItsOpenParts(t *testing.T) {
	t.Parallel()

	h := newHarness(t, controlLine(), porttest.Succeed(`{}`))
	a, parts := h.assemblyAtWork(t)

	got, err := h.e.Pause(inOrg(t), a.ID, human, reasonVendor)
	if err != nil {
		t.Fatal(err)
	}

	if got.ID != a.ID || got.State != domain.StateAssembly || h.item(t, a.ID).State != domain.StateAssembly {
		t.Errorf("Pause of an assembly returned %s in %s; want the assembly, still an assembly", got.ID, got.State)
	}

	blocked := domain.Position{Station: stWork, State: domain.StateBlocked}
	h.atPositions(t, "pause", parts, map[string]domain.Position{
		partBuilding: blocked, partQueued: blocked, partStuck: blocked, partShipped: {Station: stDone, State: domain.StateDone},
	})
	h.pausedParts(t, a, parts)

	if n := len(h.eventsOf(parts[partBuilding].ID, domain.EventLeaseTaken)); n != 1 {
		t.Errorf("lease_taken events of the building part = %d; want 1", n)
	}

	if ls, _ := h.store.Leases(inOrg(t), factoryID, stWork); len(ls) != 0 {
		t.Errorf("leases at work after the pause = %+v; want the building part's taken", ls)
	}

	if _, err := h.e.Pause(inOrg(t), a.ID, human, ""); !errors.Is(err, engine.ErrAlreadyBlocked) {
		t.Errorf("Pause of a paused assembly: err = %v; want ErrAlreadyBlocked", err)
	}

	if paused := h.eventsOf(a.ID, domain.EventPaused); len(paused) != 1 || paused[0].Data["parts"] != 2 {
		t.Errorf("paused events of the assembly = %+v; want one, with parts 2", paused)
	}
}

// reasonVendor is the reason TestPause_AnAssemblyPausesItsOpenParts pauses with.
const reasonVendor = "the vendor is down"

// pausedParts checks the paused events of assembly a's parts and their audit entries: one each
// for building and queued, naming the assembly and the reason, none for the others.
func (h *harness) pausedParts(t *testing.T, a domain.Item, parts map[string]domain.Item) {
	t.Helper()

	for key, p := range parts {
		paused := h.eventsOf(p.ID, domain.EventPaused)
		if key == partStuck || key == partShipped {
			if len(paused) != 0 {
				t.Errorf("part %s has paused events %+v; want none", key, paused)
			}

			continue
		}

		if len(paused) != 1 || paused[0].Data["assembly"] != string(a.ID) || paused[0].Data["reason"] != reasonVendor || paused[0].Data["actor"] != human.Ref() {
			t.Errorf("paused events of %s = %+v; want one by the person naming the assembly and the reason", key, paused)
		}
	}

	h.pausedAudit(t, a, parts)
}

// pausedAudit checks the item.paused audit entries: one per paused part, building and queued,
// naming the assembly and the reason.
func (h *harness) pausedAudit(t *testing.T, a domain.Item, parts map[string]domain.Item) {
	t.Helper()

	audited := h.audited(t, domain.AuditItemPaused)
	if len(audited) != 2 {
		t.Fatalf("item.paused audit entries = %+v; want one per paused part", audited)
	}

	for _, e := range audited {
		ofPart := e.TargetID == string(parts[partBuilding].ID) || e.TargetID == string(parts[partQueued].ID)
		if !ofPart || e.Data["assembly"] != string(a.ID) || e.Data["reason"] != reasonVendor {
			t.Errorf("audit entry %+v; want one of a paused part naming the assembly and the reason", e)
		}
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

	if pos := h.item(t, parts[partStuck].ID).Position(); pos != (domain.Position{Station: stWork, State: domain.StateQueued}) {
		t.Errorf("stuck part after the retry at %+v; want queued at work", pos)
	}

	if _, err := h.e.Pause(inOrg(t), a.ID, human, ""); err != nil {
		t.Fatal(err)
	}

	got, err := h.e.Retry(inOrg(t), a.ID, human)
	if err != nil {
		t.Fatal(err)
	}

	if got.ID != a.ID || got.State != domain.StateAssembly {
		t.Errorf("Retry of an assembly returned %s in %s; want the assembly, still an assembly", got.ID, got.State)
	}

	queued := domain.Position{Station: stWork, State: domain.StateQueued}
	h.atPositions(t, "retry", parts, map[string]domain.Position{
		partBuilding: queued, partQueued: queued, partStuck: queued, partShipped: {Station: stDone, State: domain.StateDone},
	})
	h.retriedParts(t, a, parts, map[string]int{partBuilding: 1, partQueued: 1, partStuck: 2, partShipped: 0})

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

// retriedParts checks that each of assembly a's parts has want's number of retried events, each
// by the person and naming the assembly.
func (h *harness) retriedParts(t *testing.T, a domain.Item, parts map[string]domain.Item, want map[string]int) {
	t.Helper()

	for key, p := range parts {
		events := h.eventsOf(p.ID, domain.EventRetried)
		if len(events) != want[key] {
			t.Errorf("retried events of %s = %d; want %d", key, len(events), want[key])
		}

		for _, e := range events {
			if e.Data["assembly"] != string(a.ID) || e.Data["actor"] != human.Ref() {
				t.Errorf("retried event of %s = %+v; want one by the person naming the assembly", key, e)
			}
		}
	}
}
