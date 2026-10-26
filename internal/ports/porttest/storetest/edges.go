package storetest

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// addEdge records that item needs needs, planned by the local user at t0.
func (f *fixture) addEdge(t *testing.T, item, needs domain.ItemID) domain.Edge {
	t.Helper()

	e, err := f.Store.AddEdge(f.ctx(t), domain.NewEdge{Item: item, Needs: needs, Kind: domain.EdgePlanned, By: domain.LocalUser, At: t0})
	if err != nil {
		t.Fatalf("AddEdge %s needs %s: %v", item, needs, err)
	}

	return e
}

func (f *fixture) edges(t *testing.T, id domain.ItemID) domain.ItemEdges {
	t.Helper()

	e, err := f.Store.Edges(f.ctx(t), id)
	if err != nil {
		t.Fatalf("Edges: %v", err)
	}

	return e
}

// edgeEvent is the event AddEdge or RemoveEdge appends on the dependent item.
func edgeEvent(kind domain.EventKind, it domain.Item, needs domain.ItemID, edge domain.EdgeKind, at time.Time) domain.Event {
	return domain.Event{
		FactoryID: it.FactoryID, ItemID: it.ID, At: at, Kind: kind, Station: it.Station, Actor: domain.LocalUser.Name,
		Data: map[string]any{"needs": string(needs), "kind": string(edge), "actor": domain.LocalUser.Ref()},
	}
}

func sameEdges(t *testing.T, what string, got, want []domain.Edge) {
	t.Helper()

	for i := range got {
		got[i].At = got[i].At.UTC()
	}

	if len(got) == 0 && len(want) == 0 {
		return
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s =\n  %+v\nwant\n  %+v", what, got, want)
	}
}

// An edge is listed from both ends, its addition and removal are events on the dependent item,
// and malformed edges change nothing.
func testEdgesAddListRemove(f *fixture) {
	t := f.t
	ctx := f.ctx(t)

	a := f.create(t, domain.NewItem{Title: "a"})
	b := f.create(t, domain.NewItem{Title: "b"})
	c := f.create(t, domain.NewItem{Title: "c"})

	ab := f.addEdge(t, b.ID, a.ID)
	want := domain.Edge{Item: b.ID, Needs: a.ID, Kind: domain.EdgePlanned, By: domain.LocalUser.Ref(), At: t0}

	ab.At = ab.At.UTC()
	if ab != want {
		t.Errorf("AddEdge = %+v; want %+v", ab, want)
	}

	ca, err := f.Store.AddEdge(ctx, domain.NewEdge{Item: c.ID, Needs: a.ID, Kind: domain.EdgeLinked, By: domain.LocalUser, At: t1})
	if err != nil {
		t.Fatal(err)
	}

	sameEdges(t, "a needed by", f.edges(t, a.ID).NeededBy, []domain.Edge{want, {Item: c.ID, Needs: a.ID, Kind: domain.EdgeLinked, By: domain.LocalUser.Ref(), At: t1}})
	sameEdges(t, "a needs", f.edges(t, a.ID).Needs, nil)
	sameEdges(t, "b needs", f.edges(t, b.ID).Needs, []domain.Edge{want})

	if ca.Kind != domain.EdgeLinked {
		t.Errorf("AddEdge kind = %q; want linked", ca.Kind)
	}

	refused := []struct {
		name string
		edge domain.NewEdge
		want error
	}{
		{"again", domain.NewEdge{Item: b.ID, Needs: a.ID, Kind: domain.EdgeLinked}, ports.ErrConflict},
		{"itself", domain.NewEdge{Item: a.ID, Needs: a.ID, Kind: domain.EdgeLinked}, domain.ErrInvalid},
		{"unknown kind", domain.NewEdge{Item: a.ID, Needs: c.ID, Kind: "after"}, domain.ErrInvalid},
		{"missing item", domain.NewEdge{Item: missingItem, Needs: a.ID, Kind: domain.EdgeLinked}, ports.ErrNotFound},
		{"missing need", domain.NewEdge{Item: a.ID, Needs: missingItem, Kind: domain.EdgeLinked}, ports.ErrNotFound},
	}

	for _, r := range refused {
		r.edge.By, r.edge.At = domain.LocalUser, t1
		if _, err := f.Store.AddEdge(ctx, r.edge); !errors.Is(err, r.want) {
			t.Errorf("AddEdge %s = %v; want %v", r.name, err, r.want)
		}
	}

	if err := f.Store.RemoveEdge(ctx, b.ID, a.ID, domain.LocalUser, t1); err != nil {
		t.Fatalf("RemoveEdge: %v", err)
	}

	if err := f.Store.RemoveEdge(ctx, b.ID, a.ID, domain.LocalUser, t1); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("RemoveEdge again = %v; want ErrNotFound", err)
	}

	sameEdges(t, "b needs after the removal", f.edges(t, b.ID).Needs, nil)
	sameEdges(t, "a needed by after the removal", f.edges(t, a.ID).NeededBy, []domain.Edge{{Item: c.ID, Needs: a.ID, Kind: domain.EdgeLinked, By: domain.LocalUser.Ref(), At: t1}})
	sameEdges(t, "unknown item", f.edges(t, missingItem).Needs, nil)

	sameEvents(t, f.Events(t, b.ID), []domain.Event{
		{FactoryID: f.factory, ItemID: b.ID, At: t0, Kind: domain.EventCreated, Station: stWork, Actor: domain.ActorEngine},
		edgeEvent(domain.EventEdgeAdded, b, a.ID, domain.EdgePlanned, t0),
		edgeEvent(domain.EventEdgeRemoved, b, a.ID, domain.EdgePlanned, t1),
	})

	if events := f.Events(t, a.ID); len(events) != 1 {
		t.Errorf("events of the needed item = %+v; want only created", events)
	}
}

// No edge may close a cycle, however long, and of two concurrent edges that would close one
// between them only one is added.
func testEdgesCycleRefused(f *fixture) {
	t := f.t
	ctx := f.ctx(t)

	a := f.create(t, domain.NewItem{Title: "a"})
	b := f.create(t, domain.NewItem{Title: "b"})
	c := f.create(t, domain.NewItem{Title: "c"})

	f.addEdge(t, a.ID, b.ID)
	f.addEdge(t, b.ID, c.ID)

	for _, e := range [][2]domain.ItemID{{c.ID, a.ID}, {b.ID, a.ID}, {c.ID, b.ID}} {
		_, err := f.Store.AddEdge(ctx, domain.NewEdge{Item: e[0], Needs: e[1], Kind: domain.EdgeLinked, By: domain.LocalUser, At: t1})
		if !errors.Is(err, domain.ErrCycle) {
			t.Errorf("AddEdge %s needs %s = %v; want ErrCycle", e[0], e[1], err)
		}
	}

	// a needs c through b already; saying so directly is no cycle.
	f.addEdge(t, a.ID, c.ID)

	for range 10 {
		x := f.create(t, domain.NewItem{Title: "x"})
		y := f.create(t, domain.NewItem{Title: "y"})

		var (
			wg    sync.WaitGroup
			errs  [2]error
			pairs = [2][2]domain.ItemID{{x.ID, y.ID}, {y.ID, x.ID}}
		)

		for i, p := range pairs {
			wg.Go(func() {
				_, errs[i] = f.Store.AddEdge(ctx, domain.NewEdge{Item: p[0], Needs: p[1], Kind: domain.EdgeLinked, By: domain.LocalUser, At: t1})
			})
		}

		wg.Wait()

		added := 0

		for _, err := range errs {
			switch {
			case err == nil:
				added++
			case !errors.Is(err, domain.ErrCycle):
				t.Fatalf("concurrent AddEdge = %v; want nil or ErrCycle", err)
			}
		}

		if added != 1 {
			t.Fatalf("concurrent opposite edges: %d added; want exactly 1", added)
		}
	}
}

// An edge joins two items of one factory: an item of another factory is refused, one of another
// organization is not found.
func testEdgesOtherFactoryRefused(f *fixture) {
	t := f.t
	ctx := f.ctx(t)

	a := f.create(t, domain.NewItem{Title: "a"})
	other := f.create(t, domain.NewItem{Title: "other factory", FactoryID: f.newFactory(t)})

	for _, e := range [][2]domain.ItemID{{a.ID, other.ID}, {other.ID, a.ID}} {
		_, err := f.Store.AddEdge(ctx, domain.NewEdge{Item: e[0], Needs: e[1], Kind: domain.EdgeLinked, By: domain.LocalUser, At: t1})
		if !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("AddEdge across factories = %v; want ErrInvalid", err)
		}
	}

	org := f.NewOrg(t)
	octx := domain.WithOrg(t.Context(), org)

	foreign, err := f.Store.CreateItem(octx, domain.NewItem{
		FactoryID: f.NewFactory(t, org), Title: "foreign", Type: typeTask, Station: stWork,
		State: domain.StateQueued, Class: domain.ClassStandard, CreatedAt: t0,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.Store.AddEdge(ctx, domain.NewEdge{Item: a.ID, Needs: foreign.ID, Kind: domain.EdgeLinked, By: domain.LocalUser, At: t1}); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("AddEdge to another organization's item = %v; want ErrNotFound", err)
	}

	if got := f.edges(t, a.ID); len(got.Needs)+len(got.NeededBy) != 0 {
		t.Errorf("edges after refusals = %+v; want none", got)
	}
}

// A claim skips an item while what it needs is open and takes the next in pull order; the item
// is claimed once its need is done, or cancelled. Waiting still lists it.
func testClaimSkipsOpenNeeds(f *fixture) {
	t := f.t
	ctx := f.ctx(t)

	held := f.create(t, domain.NewItem{Title: "held", Class: domain.ClassExpedite})
	heldToo := f.create(t, domain.NewItem{Title: "held too", Priority: 5})
	next := f.create(t, domain.NewItem{Title: "next", CreatedAt: t0.Add(time.Second)})
	need := f.create(t, domain.NewItem{Title: "need", Station: stNext})
	needToo := f.create(t, domain.NewItem{Title: "need too", Station: stNext})

	f.addEdge(t, held.ID, need.ID)
	f.addEdge(t, heldToo.ID, needToo.ID)

	if got, _ := f.claim(t, work, 0, t1); got.ID != next.ID {
		t.Fatalf("claimed %q; want %q, the first item without open needs", got.Title, next.Title)
	}

	if _, _, err := f.Store.ClaimNext(ctx, ports.Claim{FactoryID: f.factory, Station: work, Slot: 1, Now: t1, ExpiresAt: t1.Add(ttl)}); !errors.Is(err, ports.ErrNoWork) {
		t.Fatalf("ClaimNext with only held items = %v; want ErrNoWork", err)
	}

	if w, err := f.Store.Waiting(ctx, f.factory, stWork, 10); err != nil || len(w) != 2 {
		t.Errorf("Waiting = %v, %v; want both held items", titlesOf(w), err)
	}

	finish := func(it domain.Item, state domain.ItemState) {
		t.Helper()

		if _, err := f.Store.Transition(ctx, ports.Transition{
			ItemID: it.ID, From: it.Position(), To: domain.Position{Station: stNext, State: state}, At: t1, DoneAt: t1,
		}); err != nil {
			t.Fatal(err)
		}
	}

	finish(need, domain.StateDone)

	if got, _ := f.claim(t, work, 1, t1); got.ID != held.ID {
		t.Fatalf("claimed %q after its need is done; want %q", got.Title, held.Title)
	}

	finish(needToo, domain.StateCancelled)

	if got, _ := f.claim(t, work, 2, t1); got.ID != heldToo.ID {
		t.Fatalf("claimed %q after its need is cancelled; want %q", got.Title, heldToo.Title)
	}
}

// OpenNeeds reads the open needs of many items at once, oldest edge first, with each needed
// item's number and paths; needs that are done or cancelled, items without open needs, another
// factory's items and ids that are not UUIDs are left out.
func testOpenNeeds(f *fixture) {
	t := f.t
	ctx := f.ctx(t)

	held := f.create(t, domain.NewItem{Title: "held"})
	free := f.create(t, domain.NewItem{Title: "free"})
	planned := f.create(t, domain.NewItem{Title: "planned", Station: stNext})
	overlap := f.create(t, domain.NewItem{Title: "overlap", Station: stNext})
	done := f.create(t, domain.NewItem{Title: "done need", Station: stNext})
	cancelled := f.create(t, domain.NewItem{Title: "cancelled need", Station: stNext})

	queued := domain.Position{Station: stNext, State: domain.StateQueued}
	globs := []string{"db/queries/*.sql", "web/**"}

	if _, err := f.Store.Transition(ctx, ports.Transition{ItemID: overlap.ID, From: queued, To: queued, Paths: globs, At: t1}); err != nil {
		t.Fatal(err)
	}

	add := func(item, needs domain.ItemID, kind domain.EdgeKind, at time.Time) {
		t.Helper()

		if _, err := f.Store.AddEdge(ctx, domain.NewEdge{Item: item, Needs: needs, Kind: kind, By: domain.LocalUser, At: at}); err != nil {
			t.Fatalf("AddEdge %s needs %s: %v", item, needs, err)
		}
	}

	add(held.ID, overlap.ID, domain.EdgeOverlap, t1)
	add(held.ID, planned.ID, domain.EdgePlanned, t0)
	add(held.ID, done.ID, domain.EdgeLinked, t0)
	add(held.ID, cancelled.ID, domain.EdgeLinked, t0)
	add(free.ID, done.ID, domain.EdgePlanned, t0)

	for _, it := range []domain.Item{done, cancelled} {
		state := domain.StateDone
		if it.ID == cancelled.ID {
			state = domain.StateCancelled
		}

		if _, err := f.Store.Transition(ctx, ports.Transition{
			ItemID: it.ID, From: queued, To: domain.Position{Station: stNext, State: state}, At: t1, DoneAt: t1,
		}); err != nil {
			t.Fatal(err)
		}
	}

	otherFactory := f.newFactory(t)
	foreign := f.create(t, domain.NewItem{Title: "other factory", FactoryID: otherFactory})
	foreignNeed := f.create(t, domain.NewItem{Title: "other factory's need", FactoryID: otherFactory})
	add(foreign.ID, foreignNeed.ID, domain.EdgePlanned, t0)

	got, err := f.Store.OpenNeeds(ctx, f.factory, []domain.ItemID{held.ID, free.ID, planned.ID, foreign.ID, "not-a-uuid"})
	if err != nil {
		t.Fatalf("OpenNeeds: %v", err)
	}

	want := map[domain.ItemID][]domain.OpenNeed{held.ID: {
		{Kind: domain.EdgePlanned, Item: planned.ID, Number: planned.Number},
		{Kind: domain.EdgeOverlap, Item: overlap.ID, Number: overlap.Number, Paths: globs},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("OpenNeeds =\n  %+v\nwant\n  %+v", got, want)
	}

	if got, err := f.Store.OpenNeeds(ctx, f.factory, nil); err != nil || len(got) != 0 {
		t.Errorf("OpenNeeds of no ids = %+v, %v; want none", got, err)
	}
}
