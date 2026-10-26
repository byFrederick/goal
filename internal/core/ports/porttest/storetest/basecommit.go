package storetest

import (
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// A transition with a BaseCommit replaces the item's base commit and an empty one keeps it; the
// item and the store's reads agree.
func testBaseCommitSetByTransition(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{})
	if it.BaseCommit != "" {
		t.Fatalf("new item base commit = %q; want none", it.BaseCommit)
	}

	_, l := f.claim(t, work, 0, t1)

	tr := next(it, l, t1.Add(time.Minute))
	tr.BaseCommit = "c0ffee01"

	moved, err := f.Store.Transition(f.ctx(t), tr)
	if err != nil {
		t.Fatal(err)
	}

	sameBaseCommit(t, "after BaseCommit set", moved, f.item(t, it.ID), "c0ffee01")

	queued := domain.Position{Station: stNext, State: domain.StateQueued}

	kept, err := f.Store.Transition(f.ctx(t), ports.Transition{ItemID: it.ID, From: queued, To: queued, At: t1.Add(2 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}

	sameBaseCommit(t, "after an empty BaseCommit", kept, f.item(t, it.ID), "c0ffee01")
}

// Assemble records the transition's base commit on the assembly and copies it to every part it
// creates, and a part added later gets the assembly's.
func testBaseCommitCopiedToParts(f *fixture) {
	t := f.t

	it, l := f.planning(t, domain.NewItem{Title: titleBig, Type: typeFeature})
	a := assembly(it, l, plan())
	a.Transition.BaseCommit = "c0ffee02"

	assembled, parts := f.assemble(t, a)
	if assembled.BaseCommit != "c0ffee02" || f.item(t, it.ID).BaseCommit != "c0ffee02" {
		t.Errorf("assembly base commit = %q; want c0ffee02", assembled.BaseCommit)
	}

	for _, p := range parts.Items {
		sameBaseCommit(t, "part "+p.PartKey, p, f.item(t, p.ID), "c0ffee02")
	}

	added, _, err := f.Store.AddPart(f.ctx(t), addedPart(assembled, t1.Add(time.Minute)))
	if err != nil {
		t.Fatalf("AddPart: %v", err)
	}

	sameBaseCommit(t, "added part", added, f.item(t, added.ID), "c0ffee02")
}

// MergedItems finds the factory's items by the sha of their merged events: another factory's,
// another kind's and an unknown sha are absent, and the latest event wins a shared sha.
func testMergedItems(f *fixture) {
	t := f.t
	ctx := f.ctx(t)

	a := f.create(t, domain.NewItem{Title: "a"})
	b := f.create(t, domain.NewItem{Title: "b"})
	c := f.create(t, domain.NewItem{Title: "c"})
	other := f.create(t, domain.NewItem{FactoryID: f.newFactory(t), Title: "other"})

	merged := func(it domain.Item, kind domain.EventKind, sha string, at time.Time) domain.Event {
		return domain.Event{
			FactoryID: it.FactoryID, ItemID: it.ID, At: at, Kind: kind, Station: stWork, Actor: domain.ActorEngine,
			Data: map[string]any{"sha": sha, "pull_request": "https://example.com/acme/app/pull/1"},
		}
	}

	if err := f.Store.AppendEvents(ctx,
		merged(a, domain.EventMerged, "sha-a", t1),
		merged(a, domain.EventMerged, "sha-a2", t1),
		merged(b, domain.EventMerged, "sha-shared", t1),
		merged(c, domain.EventMerged, "sha-shared", t1.Add(time.Second)),
		merged(b, domain.EventBlocked, "sha-blocked", t1),
		merged(other, domain.EventMerged, "sha-other", t1),
	); err != nil {
		t.Fatal(err)
	}

	got, err := f.Store.MergedItems(ctx, f.factory, []string{"sha-a", "sha-a2", "sha-shared", "sha-blocked", "sha-other", "sha-none"})
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]domain.ItemID{"sha-a": a.ID, "sha-a2": a.ID, "sha-shared": c.ID}
	if len(got) != len(want) {
		t.Errorf("MergedItems = %d items %v; want %v", len(got), keysOfMerged(got), want)
	}

	for sha, id := range want {
		if got[sha].ID != id {
			t.Errorf("MergedItems[%s] = %q; want %q", sha, got[sha].ID, id)
		}
	}

	if got["sha-a"].Title != "a" || got["sha-a"].Number != a.Number {
		t.Errorf("MergedItems[sha-a] = %+v; want item a whole", got["sha-a"])
	}

	if none, err := f.Store.MergedItems(ctx, f.factory, nil); err != nil || len(none) != 0 {
		t.Errorf("MergedItems of no shas = %v, %v; want none", none, err)
	}
}

func keysOfMerged(m map[string]domain.Item) []string {
	out := make([]string, 0, len(m))
	for sha := range m {
		out = append(out, sha)
	}

	return out
}

// sameBaseCommit checks that the store's answer and the item read back both have want.
func sameBaseCommit(t *testing.T, when string, got, read domain.Item, want string) {
	t.Helper()

	if got.BaseCommit != want || read.BaseCommit != want {
		t.Errorf("%s: base commit %q, read back %q; want %q", when, got.BaseCommit, read.BaseCommit, want)
	}
}
