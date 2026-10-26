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
	keyAPI    = "api"
	keyStore  = "store"
	keyWeb    = "web"
	titleBig  = "big request"
	dataParts = "parts"
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
			PartKey: p.Key, PartPaths: p.Paths, Paths: p.Paths, Type: "feature", Lane: p.Lane, Title: p.Title, Body: p.Brief,
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
		{FactoryID: f.factory, ItemID: it.ID, At: t1, Kind: domain.EventAssembled, Station: stWork, Actor: domain.ActorEngine, Data: map[string]any{dataParts: 3}},
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

	for i, p := range plan() {
		if got := again.Items[i].Paths; !reflect.DeepEqual(got, p.Paths) {
			t.Errorf("part %q paths after a retry = %q; want the plan's %q", p.Key, got, p.Paths)
		}
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

	// Done, it lands where its done parts did, the line's done station, as a finished item does.
	want := f.item(t, it.ID)
	if want.State != domain.StateDone || !want.DoneAt.Equal(t3) || !want.UpdatedAt.Equal(t3) || want.Station != stNext {
		t.Errorf("assembly after its last part = %s at %s, done %v, updated %v; want done at %s at %v", want.State, want.Station, want.DoneAt, want.UpdatedAt, stNext, t3)
	}

	events := f.Events(t, it.ID)
	sameEvents(t, events[len(events)-1:], []domain.Event{{
		FactoryID: f.factory, ItemID: it.ID, At: t3, Kind: domain.EventAssemblyDone, Station: stNext, Actor: domain.ActorEngine,
		Data: map[string]any{"state": string(domain.StateDone), "parts_done": 2, dataParts: 3},
	}})
}

// An assembly whose parts were all cancelled is cancelled where it is, at the station that
// planned it, as a cancelled item is; its assembly_done event says so.
func testAssemblyCancelledWithItsParts(f *fixture) {
	t := f.t

	other, ol := f.planning(t, domain.NewItem{Title: "dropped request"})
	_, dropped := f.assemble(t, assembly(other, ol, plan()[:1]))

	if err := f.finishPart(t, dropped.Items[0], domain.StateCancelled, t1); err != nil {
		t.Fatal(err)
	}

	if got := f.item(t, other.ID); got.State != domain.StateCancelled || got.Station != stWork {
		t.Errorf("assembly whose parts were all cancelled is %s at %s; want cancelled at %s", got.State, got.Station, stWork)
	}

	events := f.Events(t, other.ID)
	sameEvents(t, events[len(events)-1:], []domain.Event{{
		FactoryID: f.factory, ItemID: other.ID, At: t1, Kind: domain.EventAssemblyDone, Station: stWork, Actor: domain.ActorEngine,
		Data: map[string]any{"state": string(domain.StateCancelled), "parts_done": 0, dataParts: 1},
	}})
}

// An open assembly stays at the station that planned it but is no load there: StationLoads
// and StationTimes count its parts where they are, never the assembly.
func testStationLoadsCountNoOpenAssembly(f *fixture) {
	t := f.t

	it, l := f.planning(t, domain.NewItem{Title: titleBig})
	_, parts := f.assemble(t, assembly(it, l, plan()[:1]))
	part := parts.Items[0]

	f.move(t, ports.Transition{ItemID: part.ID, From: part.Position(), To: domain.Position{Station: stNext, State: domain.StateQueued}, At: t1})

	if got := f.item(t, it.ID); got.State != domain.StateAssembly || got.Station != stWork {
		t.Fatalf("assembly is %s at %s; want assembly at %s", got.State, got.Station, stWork)
	}

	loads, err := f.Store.StationLoads(f.ctx(t), f.factory)
	if err != nil {
		t.Fatal(err)
	}

	for _, ld := range loads {
		want := domain.StationLoad{Station: ld.Station}
		if ld.Station == stNext {
			want.Waiting = 1
		}

		if ld != want {
			t.Errorf("load at %s = %+v; want %+v", ld.Station, ld, want)
		}
	}

	times, _, err := f.Flow.StationTimes(f.ctx(t), f.factory, t1, t1.Add(10*time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	for _, st := range times {
		want := 0
		if st.Station == stNext {
			want = 1
		}

		if st.WaitingNow != want {
			t.Errorf("StationTimes at %s waiting now = %d; want %d", st.Station, st.WaitingNow, want)
		}
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
			Data: map[string]any{dataParts: len(parts)},
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

// A cancel cascade closes the assembly as cancelled, without a done time, even when one of its
// parts is done; its assembly_done event says so.
func testCancelCascadeClosesTheAssemblyCancelled(f *fixture) {
	t := f.t

	it, l := f.planning(t, domain.NewItem{Title: titleBig})
	_, parts := f.assemble(t, assembly(it, l, plan()))
	t2 := t1.Add(time.Minute)

	if err := f.finishPart(t, parts.Items[0], domain.StateDone, t1); err != nil {
		t.Fatal(err)
	}

	c := ports.Cascade{Assembly: it.ID, Cancel: true}
	for _, p := range parts.Items[1:] {
		c.Parts = append(c.Parts, ports.Transition{
			ItemID: p.ID, From: p.Position(), To: domain.Position{Station: p.Station, State: domain.StateCancelled}, At: t2,
		})
	}

	if got, err := f.Store.CascadeParts(f.ctx(t), c); err != nil || got.Done != 1 || got.Total != 3 {
		t.Fatalf("CascadeParts cancelling the open parts = %+v, %v; want 1/3", got, err)
	}

	a := f.item(t, it.ID)
	if a.State != domain.StateCancelled || !a.DoneAt.IsZero() || !a.UpdatedAt.Equal(t2) || a.Station != stWork {
		t.Errorf("assembly after a cancel cascade = %s at %s, done %v, updated %v; want cancelled at %s, not done, at %v",
			a.State, a.Station, a.DoneAt, a.UpdatedAt, stWork, t2)
	}

	events := f.Events(t, it.ID)
	sameEvents(t, events[len(events)-1:], []domain.Event{{
		FactoryID: f.factory, ItemID: it.ID, At: t2, Kind: domain.EventAssemblyDone, Station: stWork, Actor: domain.ActorEngine,
		Data: map[string]any{"state": string(domain.StateCancelled), "parts_done": 1, dataParts: 3},
	}})
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

const (
	// keyDocs is the part the AddPart cases add to plan()'s assembly.
	keyDocs = "docs"
	// typeFeature is the type of the AddPart cases' assembly.
	typeFeature = "feature"
	// dataKey and dataActor are keys of the parts' event data.
	dataKey, dataActor = "key", "actor"
)

// addedPart is one part added to the assembly it: docs, needing api and store.
func addedPart(it domain.Item, at time.Time) ports.NewAssemblyPart {
	return ports.NewAssemblyPart{
		Assembly: it.ID,
		Part: domain.NewPart{
			Key: keyDocs, Title: "Docs", Lane: laneBackend, Brief: "the guide", Paths: []string{"docs/guide.md"},
			Needs: []string{keyAPI, keyStore},
		},
		By: domain.LocalUser, At: at,
		Audit: &domain.AuditEntry{
			Actor: domain.LocalUser, Action: domain.AuditItemPartAdded, TargetKind: domain.AuditTargetItem,
			TargetID: string(it.ID), FactoryID: it.FactoryID, Data: map[string]any{"key": keyDocs},
		},
	}
}

// AddPart creates one part of an open assembly as Assemble creates each: the parent's type,
// priority and class, its brief and paths, queued at the assembly's station, with a planned edge
// per need, part_created on the part, part_added on the assembly and the audit entry.
func testAddPart(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	t2 := t1.Add(time.Minute)

	it, l := f.planning(t, domain.NewItem{Title: titleBig, Type: typeFeature, Priority: 4, Class: domain.ClassExpedite})
	assembled, before := f.assemble(t, assembly(it, l, plan()))

	part, parts, err := f.Store.AddPart(ctx, addedPart(assembled, t2))
	if err != nil {
		t.Fatalf("AddPart: %v", err)
	}

	paths := []string{"docs/guide.md"}
	sameItem(t, part, domain.Item{
		ID: part.ID, OrgID: f.org, FactoryID: f.factory, Number: it.Number + 4, ParentID: it.ID,
		PartKey: keyDocs, PartPaths: paths, Paths: paths, Type: typeFeature, Lane: laneBackend, Title: "Docs", Body: "the guide",
		Station: stWork, State: domain.StateQueued, Class: domain.ClassExpedite, Priority: 4, Outputs: map[string]json.RawMessage{},
		CreatedAt: t2, UpdatedAt: t2,
	})
	sameItem(t, f.item(t, part.ID), part)

	if got := keysOf(parts); !reflect.DeepEqual(got, []string{keyAPI, keyStore, keyWeb, keyDocs}) || parts.Total != before.Total+1 {
		t.Fatalf("AddPart parts = %v, %d in all; want api, store, web, docs and %d", got, parts.Total, before.Total+1)
	}

	sameItem(t, parts.Items[3], part)

	if got := f.parts(t, it.ID); !reflect.DeepEqual(got, parts) {
		t.Errorf("Parts =\n  %+v\nwant what AddPart returned\n  %+v", got, parts)
	}

	planned := func(needs domain.ItemID) domain.Edge {
		return domain.Edge{Item: part.ID, Needs: needs, Kind: domain.EdgePlanned, By: domain.LocalUser.Ref(), At: t2}
	}
	sameEdges(t, "docs needs", f.edges(t, part.ID).Needs, []domain.Edge{planned(before.Items[0].ID), planned(before.Items[1].ID)})

	sameEvents(t, f.Events(t, part.ID), []domain.Event{{
		FactoryID: f.factory, ItemID: part.ID, At: t2, Kind: domain.EventPartCreated, Station: stWork, Actor: domain.LocalUser.Name,
		Data: map[string]any{"parent": string(it.ID), dataKey: keyDocs},
	}})

	events := f.Events(t, it.ID)
	sameEvents(t, events[len(events)-1:], []domain.Event{{
		FactoryID: f.factory, ItemID: it.ID, At: t2, Kind: domain.EventPartAdded, Station: stWork, Actor: domain.LocalUser.Name,
		Data: map[string]any{"part": string(part.ID), dataKey: keyDocs, dataActor: domain.LocalUser.Ref()},
	}})

	if got := f.item(t, it.ID); got.State != domain.StateAssembly {
		t.Errorf("assembly after AddPart is %q; want assembly", got.State)
	}

	entries, err := f.Audit.AuditLog(ctx, ports.AuditQuery{Action: domain.AuditItemPartAdded, Limit: 10})
	if err != nil || len(entries) != 1 || entries[0].TargetID != string(it.ID) || entries[0].Data[dataKey] != keyDocs {
		t.Errorf("audit log of %s = %+v, %v; want the one entry of the added part", domain.AuditItemPartAdded, entries, err)
	}
}

// A malformed part, a key the assembly has, an unknown or repeated need, an item that is not an
// assembly, a part, an assembly done and an unknown item are refused, and nothing is written.
func testAddPartRefused(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	t2 := t1.Add(time.Minute)

	it, l := f.planning(t, domain.NewItem{Title: titleBig})
	assembled, parts := f.assemble(t, assembly(it, l, plan()))
	plain := f.create(t, domain.NewItem{Title: "plain"})

	edgesOf := func() [][]domain.Edge {
		var out [][]domain.Edge
		for _, p := range f.parts(t, it.ID).Items {
			out = append(out, f.edges(t, p.ID).Needs)
		}

		return out
	}

	events, edges := len(f.Events(t, it.ID)), edgesOf()

	with := func(edit func(n *ports.NewAssemblyPart)) ports.NewAssemblyPart {
		n := addedPart(assembled, t2)
		edit(&n)

		return n
	}

	refused := []struct {
		name string
		n    ports.NewAssemblyPart
		want error
	}{
		{"no key", with(func(n *ports.NewAssemblyPart) { n.Part.Key = " " }), domain.ErrInvalid},
		{"no title", with(func(n *ports.NewAssemblyPart) { n.Part.Title = "" }), domain.ErrInvalid},
		{"unknown need", with(func(n *ports.NewAssemblyPart) { n.Part.Needs = []string{"db"} }), domain.ErrInvalid},
		{"need twice", with(func(n *ports.NewAssemblyPart) { n.Part.Needs = []string{keyAPI, keyAPI} }), domain.ErrInvalid},
		{"key used", with(func(n *ports.NewAssemblyPart) { n.Part.Key = keyWeb }), ports.ErrConflict},
		{"not an assembly", with(func(n *ports.NewAssemblyPart) { n.Assembly = plain.ID }), domain.ErrInvalid},
		{"a part", with(func(n *ports.NewAssemblyPart) { n.Assembly = parts.Items[0].ID }), domain.ErrInvalid},
		{"unknown item", with(func(n *ports.NewAssemblyPart) { n.Assembly = missingItem }), ports.ErrNotFound},
	}

	for _, r := range refused {
		if _, _, err := f.Store.AddPart(ctx, r.n); !errors.Is(err, r.want) {
			t.Errorf("AddPart with %s = %v; want %v", r.name, err, r.want)
		}
	}

	if got := f.parts(t, it.ID); !reflect.DeepEqual(got, parts) {
		t.Errorf("Parts after refusals =\n  %+v\nwant\n  %+v", got, parts)
	}

	if got := edgesOf(); !reflect.DeepEqual(got, edges) {
		t.Errorf("edges after refusals = %+v; want %+v", got, edges)
	}

	if n := len(f.Events(t, it.ID)); n != events {
		t.Errorf("assembly events after refusals = %d; want %d", n, events)
	}

	if got := f.parts(t, plain.ID); got.Total != 0 {
		t.Errorf("Parts of the plain item = %+v; want none", got)
	}

	f.addPartToDone(t, assembled, parts, t2)
}

// addPartToDone finishes every part of the assembly it and checks that AddPart to it is then
// ErrConflict and writes nothing.
func (f *fixture) addPartToDone(t *testing.T, it domain.Item, parts domain.Parts, t2 time.Time) {
	t.Helper()

	ctx := f.ctx(t)

	for _, p := range parts.Items {
		if err := f.finishPart(t, p, domain.StateDone, t2); err != nil {
			t.Fatal(err)
		}
	}

	done := len(f.Events(t, it.ID))

	if _, _, err := f.Store.AddPart(ctx, addedPart(it, t2)); !errors.Is(err, ports.ErrConflict) {
		t.Errorf("AddPart to a done assembly = %v; want ErrConflict", err)
	}

	if got := f.parts(t, it.ID); got.Total != 3 {
		t.Errorf("Parts of the done assembly = %d; want 3", got.Total)
	}

	if n := len(f.Events(t, it.ID)); n != done {
		t.Errorf("done assembly events after AddPart = %d; want %d", n, done)
	}

	entries, err := f.Audit.AuditLog(ctx, ports.AuditQuery{Action: domain.AuditItemPartAdded, Limit: 10})
	if err != nil || len(entries) != 0 {
		t.Errorf("audit log of %s = %+v, %v; want none", domain.AuditItemPartAdded, entries, err)
	}
}
