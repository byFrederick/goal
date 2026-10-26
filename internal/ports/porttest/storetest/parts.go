package storetest

import (
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Part keys and the planning item's title the cases use.
const (
	keyAPI   = "api"
	keyStore = "store"
	keyWeb   = "web"
	titleBig = "big request"
)

// plan is three parts: api first, then store and web, web also needing store.
func plan() []domain.NewPart {
	return []domain.NewPart{
		{Key: keyAPI, Title: "API", Lane: laneBackend, Brief: "the contract", Paths: []string{"api/openapi.yaml"}},
		{Key: keyStore, Title: "Store", Brief: "the rows", Paths: []string{"queries/a.sql", "migrations/x.sql"}, Needs: []string{keyAPI}},
		{Key: keyWeb, Title: "Web", Brief: "the page", Needs: []string{keyAPI, keyStore}},
	}
}

// planning claims a new item at the work station: the item whose plan becomes an assembly.
func (f *fixture) planning(t *testing.T, n domain.NewItem) (domain.Item, domain.Lease) {
	t.Helper()

	it := f.create(t, n)

	claimed, l := f.claim(t, work, 0, t1)
	if claimed.ID != it.ID {
		t.Fatalf("claimed %q; want the planning item %q", claimed.Title, it.Title)
	}

	return claimed, l
}

// assembly is the transition that makes a claimed item an assembly at the work station.
func assembly(it domain.Item, l domain.Lease, parts []domain.NewPart) ports.Assembly {
	return ports.Assembly{
		Transition: ports.Transition{
			ItemID: it.ID, From: it.Position(), To: domain.Position{Station: stWork, State: domain.StateAssembly},
			Lease: &l, Outcome: domain.LeaseAdvanced, At: t1,
		},
		Parts: parts,
	}
}

func (f *fixture) assemble(t *testing.T, a ports.Assembly) (domain.Item, domain.Parts) {
	t.Helper()

	parent, parts, err := f.Store.Assemble(f.ctx(t), a)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	return parent, parts
}

func (f *fixture) parts(t *testing.T, id domain.ItemID) domain.Parts {
	t.Helper()

	p, err := f.Store.Parts(f.ctx(t), id)
	if err != nil {
		t.Fatalf("Parts: %v", err)
	}

	return p
}

func keysOf(p domain.Parts) []string {
	out := make([]string, 0, len(p.Items))
	for _, it := range p.Items {
		out = append(out, it.PartKey)
	}

	return out
}

// finishPart moves a waiting part straight to state (done or cancelled).
func (f *fixture) finishPart(t *testing.T, it domain.Item, state domain.ItemState, at time.Time) error {
	t.Helper()

	_, err := f.Store.Transition(f.ctx(t), ports.Transition{
		ItemID: it.ID, From: it.Position(), To: domain.Position{Station: stNext, State: state}, At: at, DoneAt: at,
	})

	return err
}

// Assemble moves the planning item to state assembly, releasing its lease, and creates its
// parts with the parent's type, priority and class, their briefs, keys, paths and planned edges.
// The assembly is never claimed or waiting; its parts are, as their needs allow.
func testAssembleCreatesParts(f *fixture) {
	t := f.t
	ctx := f.ctx(t)

	it, l := f.planning(t, domain.NewItem{Title: titleBig, Type: "feature", Priority: 4, Class: domain.ClassExpedite})

	a := assembly(it, l, plan())
	a.Transition.Events = []domain.Event{{FactoryID: f.factory, ItemID: it.ID, At: t1, Kind: domain.EventMoved, Station: stWork, Actor: agWorker}}

	parent, parts := f.assemble(t, a)

	want := it
	want.State, want.UpdatedAt = domain.StateAssembly, t1
	sameItem(t, parent, want)
	sameItem(t, f.item(t, it.ID), want)

	if ls := f.leases(t, stWork); len(ls) != 0 {
		t.Errorf("Leases after Assemble = %+v; want the planning lease released", ls)
	}

	if got := keysOf(parts); !reflect.DeepEqual(got, []string{keyAPI, keyStore, keyWeb}) || parts.Total != 3 || parts.Done != 0 {
		t.Fatalf("Assemble parts = %v, %d/%d; want api, store, web and 0/3", got, parts.Done, parts.Total)
	}

	for i, p := range plan() {
		got := parts.Items[i]
		w := domain.Item{
			ID: got.ID, OrgID: f.org, FactoryID: f.factory, Number: it.Number + int64(i) + 1, ParentID: it.ID,
			PartKey: p.Key, PartPaths: p.Paths, Type: "feature", Lane: p.Lane, Title: p.Title, Body: p.Brief,
			Station: stWork, State: domain.StateQueued, Class: domain.ClassExpedite, Priority: 4, Outputs: map[string]json.RawMessage{},
			CreatedAt: t1, UpdatedAt: t1,
		}
		sameItem(t, got, w)
		sameEvents(t, f.Events(t, got.ID), []domain.Event{{
			FactoryID: f.factory, ItemID: got.ID, At: t1, Kind: domain.EventPartCreated, Station: stWork, Actor: domain.ActorEngine,
			Data: map[string]any{"parent": string(it.ID), "key": p.Key},
		}})
	}

	if got := f.parts(t, it.ID); !reflect.DeepEqual(got, parts) {
		t.Errorf("Parts =\n  %+v\nwant what Assemble returned\n  %+v", got, parts)
	}

	api, store, web := parts.Items[0], parts.Items[1], parts.Items[2]
	planned := func(item, needs domain.ItemID) domain.Edge {
		return domain.Edge{Item: item, Needs: needs, Kind: domain.EdgePlanned, By: domain.EngineActor.Ref(), At: t1}
	}

	sameEdges(t, "store needs", f.edges(t, store.ID).Needs, []domain.Edge{planned(store.ID, api.ID)})
	sameEdges(t, "web needs", f.edges(t, web.ID).Needs, []domain.Edge{planned(web.ID, api.ID), planned(web.ID, store.ID)})
	sameEdges(t, "api needs", f.edges(t, api.ID).Needs, nil)

	events := f.Events(t, it.ID)
	sameEvents(t, events[len(events)-2:], []domain.Event{
		a.Transition.Events[0],
		{FactoryID: f.factory, ItemID: it.ID, At: t1, Kind: domain.EventAssembled, Station: stWork, Actor: domain.ActorEngine, Data: map[string]any{"parts": 3}},
	})

	for _, s := range f.spans(t, it.ID) {
		if s.LeftAt.IsZero() {
			t.Errorf("assembly has an open span %+v; want none", s)
		}
	}

	if w, err := f.Store.Waiting(ctx, f.factory, stWork, 10); err != nil || !reflect.DeepEqual(titlesOf(w), []string{"API", "Store", "Web"}) {
		t.Errorf("Waiting = %v, %v; want the three parts and not the assembly", titlesOf(w), err)
	}

	if got, _ := f.claim(t, work, 0, t1); got.ID != api.ID {
		t.Fatalf("claimed %q; want API, the only part without open needs", got.Title)
	}

	if _, _, err := f.Store.ClaimNext(ctx, ports.Claim{FactoryID: f.factory, Station: work, Slot: 1, Now: t1, ExpiresAt: t1.Add(ttl)}); !errors.Is(err, ports.ErrNoWork) {
		t.Errorf("ClaimNext with the assembly and held parts = %v; want ErrNoWork", err)
	}
}

// A retry of Assemble for an item that already has its parts returns them and writes nothing.
func testAssembleRetry(f *fixture) {
	t := f.t

	it, l := f.planning(t, domain.NewItem{Title: titleBig})
	a := assembly(it, l, plan())

	_, first := f.assemble(t, a)
	before := len(f.Events(t, it.ID))

	parent, again := f.assemble(t, a)

	if !reflect.DeepEqual(again, first) {
		t.Errorf("Assemble again =\n  %+v\nwant the first parts\n  %+v", again, first)
	}

	if parent.State != domain.StateAssembly {
		t.Errorf("Assemble again parent state = %q; want assembly", parent.State)
	}

	if got := f.parts(t, it.ID); got.Total != 3 {
		t.Errorf("Parts after a retry = %d; want 3", got.Total)
	}

	if n := len(f.Events(t, it.ID)); n != before {
		t.Errorf("parent events after a retry = %d; want %d", n, before)
	}

	if got := f.edges(t, first.Items[2].ID).Needs; len(got) != 2 {
		t.Errorf("web needs after a retry = %+v; want 2 edges", got)
	}
}

// A malformed plan, a transition elsewhere than to state assembly, a lost lease and a plain
// Transition to state assembly are refused, and nothing is written.
func testAssembleRefused(f *fixture) {
	t := f.t
	ctx := f.ctx(t)

	it, l := f.planning(t, domain.NewItem{Title: titleBig})
	before := len(f.Events(t, it.ID))

	with := func(edit func(p []domain.NewPart) []domain.NewPart) []domain.NewPart { return edit(plan()) }

	refused := []struct {
		name  string
		parts []domain.NewPart
		want  error
	}{
		{"no parts", nil, domain.ErrInvalid},
		{"duplicate key", with(func(p []domain.NewPart) []domain.NewPart { p[2].Key = keyAPI; return p }), domain.ErrInvalid},
		{"no key", with(func(p []domain.NewPart) []domain.NewPart { p[0].Key = " "; return p }), domain.ErrInvalid},
		{"no title", with(func(p []domain.NewPart) []domain.NewPart { p[1].Title = ""; return p }), domain.ErrInvalid},
		{"unknown need", with(func(p []domain.NewPart) []domain.NewPart { p[1].Needs = []string{"db"}; return p }), domain.ErrInvalid},
		{"need twice", with(func(p []domain.NewPart) []domain.NewPart { p[1].Needs = []string{keyAPI, keyAPI}; return p }), domain.ErrInvalid},
		{"cycle", with(func(p []domain.NewPart) []domain.NewPart { p[0].Needs = []string{keyWeb}; return p }), domain.ErrCycle},
		{"needs itself", with(func(p []domain.NewPart) []domain.NewPart { p[0].Needs = []string{keyAPI}; return p }), domain.ErrCycle},
	}

	for _, r := range refused {
		if _, _, err := f.Store.Assemble(ctx, assembly(it, l, r.parts)); !errors.Is(err, r.want) {
			t.Errorf("Assemble with %s = %v; want %v", r.name, err, r.want)
		}
	}

	elsewhere := assembly(it, l, plan())
	elsewhere.Transition.To.State = domain.StateQueued

	if _, _, err := f.Store.Assemble(ctx, elsewhere); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("Assemble to state queued = %v; want ErrInvalid", err)
	}

	stale := l
	stale.AcquiredAt = l.AcquiredAt.Add(-time.Second)

	if _, _, err := f.Store.Assemble(ctx, assembly(it, stale, plan())); !errors.Is(err, ports.ErrLeaseLost) {
		t.Errorf("Assemble with a lost lease = %v; want ErrLeaseLost", err)
	}

	if _, err := f.Store.Transition(ctx, assembly(it, l, nil).Transition); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("Transition to state assembly = %v; want ErrInvalid", err)
	}

	sameItem(t, f.item(t, it.ID), it)

	if ls := f.leases(t, stWork); len(ls) != 1 || ls[0] != l {
		t.Errorf("Leases = %+v; want the planning lease still held", ls)
	}

	if got := f.parts(t, it.ID); got.Total != 0 || len(got.Items) != 0 {
		t.Errorf("Parts after refusals = %+v; want none", got)
	}

	if n := len(f.Events(t, it.ID)); n != before {
		t.Errorf("events after refusals = %d; want %d", n, before)
	}
}

// A part never becomes an assembly, and an unknown item has no parts.
func testAssembleRefusesAPart(f *fixture) {
	t := f.t
	ctx := f.ctx(t)

	it, l := f.planning(t, domain.NewItem{Title: titleBig})
	_, parts := f.assemble(t, assembly(it, l, plan()))
	api, apiLease := f.claim(t, work, 1, t1)

	if api.ID != parts.Items[0].ID {
		t.Fatalf("claimed %q; want API", api.Title)
	}

	if _, _, err := f.Store.Assemble(ctx, assembly(api, apiLease, plan())); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("Assemble of a part = %v; want ErrInvalid", err)
	}

	if got := f.parts(t, missingItem); got.Total != 0 {
		t.Errorf("Parts of an unknown item = %+v; want none", got)
	}
}

// The assembly stays open while any part is; the transition that closes its last part closes
// it, done when a part is done.
func testAssemblyClosesWithItsLastPart(f *fixture) {
	t := f.t

	it, l := f.planning(t, domain.NewItem{Title: titleBig})
	_, parts := f.assemble(t, assembly(it, l, plan()))
	t2, t3 := t1.Add(time.Minute), t1.Add(2*time.Minute)

	if err := f.finishPart(t, parts.Items[0], domain.StateDone, t2); err != nil {
		t.Fatal(err)
	}

	if err := f.finishPart(t, parts.Items[1], domain.StateCancelled, t2); err != nil {
		t.Fatal(err)
	}

	if got := f.item(t, it.ID); got.State != domain.StateAssembly {
		t.Fatalf("assembly with an open part is %q; want assembly", got.State)
	}

	if got := f.parts(t, it.ID); got.Done != 1 || got.Total != 3 {
		t.Errorf("Parts = %d/%d; want 1/3", got.Done, got.Total)
	}

	if err := f.finishPart(t, parts.Items[2], domain.StateDone, t3); err != nil {
		t.Fatal(err)
	}

	want := f.item(t, it.ID)
	if want.State != domain.StateDone || !want.DoneAt.Equal(t3) || !want.UpdatedAt.Equal(t3) || want.Station != stWork {
		t.Errorf("assembly after its last part = %s at %s, done %v, updated %v; want done at %s at %v", want.State, want.Station, want.DoneAt, want.UpdatedAt, stWork, t3)
	}

	events := f.Events(t, it.ID)
	sameEvents(t, events[len(events)-1:], []domain.Event{{
		FactoryID: f.factory, ItemID: it.ID, At: t3, Kind: domain.EventAssemblyDone, Station: stWork, Actor: domain.ActorEngine,
		Data: map[string]any{"state": string(domain.StateDone), "parts_done": 2, "parts": 3},
	}})
}

// An assembly whose parts were all cancelled is cancelled.
func testAssemblyCancelledWithItsParts(f *fixture) {
	t := f.t

	other, ol := f.planning(t, domain.NewItem{Title: "dropped request"})
	_, dropped := f.assemble(t, assembly(other, ol, plan()[:1]))

	if err := f.finishPart(t, dropped.Items[0], domain.StateCancelled, t1); err != nil {
		t.Fatal(err)
	}

	if got := f.item(t, other.ID); got.State != domain.StateCancelled {
		t.Errorf("assembly whose parts were all cancelled is %q; want cancelled", got.State)
	}
}

// Of two last parts finishing at once, one closes the assembly, exactly once.
func testAssemblyClosesOnce(f *fixture) {
	t := f.t

	for range 5 {
		race, rl := f.planning(t, domain.NewItem{Title: "race"})
		_, rp := f.assemble(t, assembly(race, rl, []domain.NewPart{{Key: "a", Title: "a"}, {Key: "b", Title: "b"}}))

		var (
			wg   sync.WaitGroup
			errs [2]error
		)

		for i, p := range rp.Items {
			wg.Go(func() { errs[i] = f.finishPart(t, p, domain.StateDone, t1) })
		}

		wg.Wait()

		if err := errors.Join(errs[:]...); err != nil {
			t.Fatalf("concurrent part transitions: %v", err)
		}

		if got := f.item(t, race.ID); got.State != domain.StateDone {
			t.Fatalf("assembly after two concurrent last parts is %q; want done", got.State)
		}

		closes := 0

		for _, e := range f.Events(t, race.ID) {
			if e.Kind == domain.EventAssemblyDone {
				closes++
			}
		}

		if closes != 1 {
			t.Fatalf("assembly_done events = %d; want 1", closes)
		}
	}
}

// pauseAll is the cascade that blocks parts where they are, taking the lease of those in
// progress, with a paused event on each and one on the assembly.
func (f *fixture) pauseAll(assembly domain.ItemID, parts ...domain.Item) ports.Cascade {
	c := ports.Cascade{
		Assembly: assembly,
		Events: []domain.Event{{
			FactoryID: f.factory, ItemID: assembly, At: t1, Kind: domain.EventPaused, Station: stWork, Actor: domain.ActorEngine,
			Data: map[string]any{"parts": len(parts)},
		}},
	}

	for _, p := range parts {
		tr := ports.Transition{
			ItemID: p.ID, From: p.Position(), To: domain.Position{Station: p.Station, State: domain.StateBlocked}, At: t1,
			Events: []domain.Event{{FactoryID: f.factory, ItemID: p.ID, At: t1, Kind: domain.EventPaused, Station: p.Station, Actor: domain.ActorEngine}},
		}
		if p.State == domain.StateInProgress {
			tr.TakeLease, tr.Outcome = true, domain.LeaseTaken
		}

		c.Parts = append(c.Parts, tr)
	}

	return c
}

// CascadeParts moves every part it names, taking the lease of one in progress, appends each
// part's events and the assembly's, and leaves the assembly in state assembly; a cascade that
// closes the last open parts closes the assembly, once.
func testCascadeParts(f *fixture) {
	t := f.t
	ctx := f.ctx(t)

	it, l := f.planning(t, domain.NewItem{Title: titleBig})
	_, parts := f.assemble(t, assembly(it, l, plan()))
	api, _ := f.claim(t, work, 1, t1)
	store, web := parts.Items[1], parts.Items[2]

	c := f.pauseAll(it.ID, web, api, store)

	got, err := f.Store.CascadeParts(ctx, c)
	if err != nil {
		t.Fatalf("CascadeParts: %v", err)
	}

	if !reflect.DeepEqual(got, f.parts(t, it.ID)) || got.Total != 3 || !reflect.DeepEqual(keysOf(got), []string{keyAPI, keyStore, keyWeb}) {
		t.Errorf("CascadeParts returned\n  %+v\nwant the assembly's parts in plan order\n  %+v", got, f.parts(t, it.ID))
	}

	for _, p := range got.Items {
		if p.Position() != (domain.Position{Station: stWork, State: domain.StateBlocked}) || !p.UpdatedAt.Equal(t1) {
			t.Errorf("part %s after the cascade is %s at %s; want blocked at work", p.PartKey, p.State, p.Station)
		}
	}

	if ls := f.leases(t, stWork); len(ls) != 0 {
		t.Errorf("Leases after the cascade = %+v; want the API part's lease taken", ls)
	}

	for i, p := range []domain.Item{web, api, store} {
		events := f.Events(t, p.ID)
		sameEvents(t, events[len(events)-1:], c.Parts[i].Events)
	}

	if a := f.item(t, it.ID); a.State != domain.StateAssembly {
		t.Errorf("assembly after the cascade is %q; want assembly", a.State)
	}

	events := f.Events(t, it.ID)
	sameEvents(t, events[len(events)-1:], c.Events)

	f.cancelClosesTheAssembly(it.ID, got)
}

// cancelClosesTheAssembly cancels every one of parts, the open parts of assembly, in one
// cascade, which closes the assembly as cancelled, once.
func (f *fixture) cancelClosesTheAssembly(assembly domain.ItemID, parts domain.Parts) {
	t := f.t
	cancel := ports.Cascade{Assembly: assembly}

	for _, p := range parts.Items {
		cancel.Parts = append(cancel.Parts, ports.Transition{
			ItemID: p.ID, From: p.Position(), To: domain.Position{Station: stWork, State: domain.StateCancelled}, At: t1,
		})
	}

	if got, err := f.Store.CascadeParts(f.ctx(t), cancel); err != nil || got.Done != 0 || got.Total != 3 {
		t.Fatalf("CascadeParts cancelling every part = %+v, %v; want 0/3", got, err)
	}

	if a := f.item(t, assembly); a.State != domain.StateCancelled {
		t.Errorf("assembly after its parts were cancelled is %q; want cancelled", a.State)
	}

	closes := 0

	for _, e := range f.Events(t, assembly) {
		if e.Kind == domain.EventAssemblyDone {
			closes++
		}
	}

	if closes != 1 {
		t.Errorf("assembly_done events = %d; want 1", closes)
	}
}

// A cascade naming no part, a part of another assembly, the assembly itself or a move to state
// assembly is invalid; one with a stale part, a lost lease or a closed assembly conflicts. None
// writes anything, even when its other parts could move.
func testCascadeRefused(f *fixture) {
	t := f.t
	ctx := f.ctx(t)

	it, l := f.planning(t, domain.NewItem{Title: titleBig})
	_, parts := f.assemble(t, assembly(it, l, plan()))
	api, apiLease := f.claim(t, work, 1, t1)
	store, web := parts.Items[1], parts.Items[2]

	other, ol := f.planning(t, domain.NewItem{Title: "dropped request"})
	_, dropped := f.assemble(t, assembly(other, ol, plan()[:1]))

	if err := f.finishPart(t, dropped.Items[0], domain.StateCancelled, t1); err != nil {
		t.Fatal(err)
	}

	gone := dropped.Items[0]
	gone.Station, gone.State = stNext, domain.StateCancelled

	before := map[domain.ItemID]int{}
	for _, id := range []domain.ItemID{it.ID, api.ID, store.ID, web.ID} {
		before[id] = len(f.Events(t, id))
	}

	edit := func(c ports.Cascade, change func(c *ports.Cascade)) ports.Cascade { change(&c); return c }
	good := f.pauseAll(it.ID, store, web)
	stale := store
	stale.State = domain.StateReady
	lost := apiLease
	lost.AcquiredAt = apiLease.AcquiredAt.Add(-time.Second)

	refused := []struct {
		name string
		c    ports.Cascade
		want error
	}{
		{"no parts", ports.Cascade{Assembly: it.ID}, domain.ErrInvalid},
		{"a part of another assembly", f.pauseAll(it.ID, store, gone), domain.ErrInvalid},
		{"a part twice", f.pauseAll(it.ID, store, store), domain.ErrInvalid},
		{"the assembly itself", f.pauseAll(it.ID, store, f.item(t, it.ID)), domain.ErrInvalid},
		{"a move to state assembly", edit(good, func(c *ports.Cascade) {
			c.Parts = append([]ports.Transition{}, c.Parts...)
			c.Parts[1].To.State = domain.StateAssembly
		}), domain.ErrInvalid},
		{"a stale part", f.pauseAll(it.ID, web, stale), ports.ErrConflict},
		{"a lost lease", edit(f.pauseAll(it.ID, web, api), func(c *ports.Cascade) {
			c.Parts[1].TakeLease, c.Parts[1].Lease, c.Parts[1].Outcome = false, &lost, domain.LeaseAdvanced
		}), ports.ErrLeaseLost},
		{"a closed assembly", ports.Cascade{Assembly: other.ID, Parts: []ports.Transition{{
			ItemID: gone.ID, From: gone.Position(), To: domain.Position{Station: stWork, State: domain.StateQueued}, At: t1,
		}}}, ports.ErrConflict},
	}

	for _, r := range refused {
		if _, err := f.Store.CascadeParts(ctx, r.c); !errors.Is(err, r.want) {
			t.Errorf("CascadeParts with %s = %v; want %v", r.name, err, r.want)
		}
	}

	sameItem(t, f.item(t, api.ID), api)
	sameItem(t, f.item(t, store.ID), store)
	sameItem(t, f.item(t, web.ID), web)

	if a := f.item(t, it.ID); a.State != domain.StateAssembly {
		t.Errorf("assembly after refusals is %q; want assembly", a.State)
	}

	if g := f.item(t, other.ID); g.State != domain.StateCancelled {
		t.Errorf("closed assembly after refusals is %q; want cancelled", g.State)
	}

	if ls := f.leases(t, stWork); len(ls) != 1 || ls[0] != apiLease {
		t.Errorf("Leases after refusals = %+v; want the API part's lease still held", ls)
	}

	for id, n := range before {
		if got := len(f.Events(t, id)); got != n {
			t.Errorf("events of %s after refusals = %d; want %d", id, got, n)
		}
	}
}
