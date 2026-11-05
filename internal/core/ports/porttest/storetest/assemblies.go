package storetest

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// An assembly comes with all its parts in number order whatever their state; a child created
// without a part key is not a part, and an item without parts and another factory's assembly are
// absent.
func testAssembliesPartsInNumberOrder(f *fixture) {
	t := f.t

	it, l := f.planning(t, domain.NewItem{Title: titleBig})
	asm, planned := f.assemble(t, assembly(it, l, plan()))
	p1, p2, p3 := planned.Items[0], planned.Items[1], planned.Items[2]

	if err := f.finishPart(t, p1, domain.StateDone, t1); err != nil {
		t.Fatalf("finish part one: %v", err)
	}

	f.move(t, ports.Transition{ItemID: p2.ID, From: p2.Position(), To: domain.Position{Station: stNext, State: domain.StateReady}, At: t1})
	f.create(t, domain.NewItem{ParentID: asm.ID, Title: "a child that is not a part"})

	plain := f.create(t, domain.NewItem{Title: "a plain item"})

	other := f.newFactory(t)
	away := f.create(t, domain.NewItem{FactoryID: other, Title: "another factory's assembly"})
	f.create(t, domain.NewItem{FactoryID: other, ParentID: away.ID, Title: "its part"})

	got, err := f.Store.Assemblies(f.ctx(t), f.factory, []domain.ItemID{asm.ID, plain.ID, away.ID, missingItem})
	if err != nil {
		t.Fatalf("Assemblies: %v", err)
	}

	want := map[domain.ItemID]domain.Assembly{asm.ID: {ID: asm.ID, Number: asm.Number, Title: asm.Title, Parts: []domain.Part{
		{ID: p1.ID, Number: p1.Number, Station: stNext, State: domain.StateDone},
		{ID: p2.ID, Number: p2.Number, Station: stNext, State: domain.StateReady},
		{ID: p3.ID, Number: p3.Number, Station: stWork, State: domain.StateQueued},
	}}}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("Assemblies =\n  %+v\nwant\n  %+v", got, want)
	}

	if none, err := f.Store.Assemblies(f.ctx(t), f.factory, nil); err != nil || len(none) != 0 {
		t.Errorf("Assemblies of no ids = %v, %v; want none", none, err)
	}
}

// The landing station and integration branch the integration-branch cases use.
const (
	stLand     = "land"
	branchIntg = "goal/330-integration"
)

// setBranch sets an assembly's integration branch to branchIntg, landing at stLand.
func (f *fixture) setBranch(t *testing.T, id domain.ItemID) {
	t.Helper()

	if set, err := f.Store.SetIntegrationBranch(f.ctx(t), id, branchIntg, stLand, t1); err != nil || !set {
		t.Fatalf("SetIntegrationBranch = %v, %v; want set", set, err)
	}
}

// sameBranch checks that item id has the integration branch branchIntg and lands at stLand.
func (f *fixture) sameBranch(t *testing.T, when string, id domain.ItemID) {
	t.Helper()

	if it := f.item(t, id); it.IntegrationBranch != branchIntg || it.IntegrationLandAt != stLand {
		t.Errorf("%s: integration branch %q landing at %q; want %q at %q", when, it.IntegrationBranch, it.IntegrationLandAt, branchIntg, stLand)
	}
}

// An integration branch is set once: a second call with other values changes neither, and both
// stay after the assembly lands and closes. An empty value is invalid and an unknown item not
// found.
func testIntegrationBranchSetOnceAndSticky(f *fixture) {
	t := f.t

	it, l := f.planning(t, domain.NewItem{Title: titleBig})
	asm, parts := f.assemble(t, assembly(it, l, plan()[:1]))

	if asm.IntegrationBranch != "" || asm.IntegrationLandAt != "" {
		t.Fatalf("new assembly integration branch %q at %q; want none", asm.IntegrationBranch, asm.IntegrationLandAt)
	}

	if _, err := f.Store.SetIntegrationBranch(f.ctx(t), asm.ID, "", stLand, t1); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("SetIntegrationBranch without a branch: %v; want ErrInvalid", err)
	}

	if _, err := f.Store.SetIntegrationBranch(f.ctx(t), asm.ID, branchIntg, "", t1); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("SetIntegrationBranch without a station: %v; want ErrInvalid", err)
	}

	f.setBranch(t, asm.ID)
	f.sameBranch(t, "after the first call", asm.ID)

	if set, err := f.Store.SetIntegrationBranch(f.ctx(t), asm.ID, "goal/other", stNext, t1); err != nil || set {
		t.Errorf("second SetIntegrationBranch = %v, %v; want not set", set, err)
	}

	f.sameBranch(t, "after a second call", asm.ID)

	if _, err := f.Store.SetIntegrationBranch(f.ctx(t), missingItem, branchIntg, stLand, t1); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("SetIntegrationBranch of an unknown item: %v; want ErrNotFound", err)
	}

	if err := f.finishPart(t, parts.Items[0], domain.StateDone, t1); err != nil {
		t.Fatal(err)
	}

	f.sameBranch(t, "after it landed", asm.ID)

	landed := f.item(t, asm.ID)
	f.move(t, ports.Transition{ItemID: asm.ID, From: landed.Position(), To: domain.Position{Station: stLand, State: domain.StateDone}, At: t1, DoneAt: t1})
	f.sameBranch(t, "after it closed", asm.ID)
}

// An assembly with an integration branch whose last part closes with a part done is not done:
// it moves to its landing station, queued, with one assembly_landing event, and is claimed
// there like any item.
func testIntegrationBranchAssemblyLandsAtItsStation(f *fixture) {
	t := f.t

	it, l := f.planning(t, domain.NewItem{Title: titleBig})
	_, parts := f.assemble(t, assembly(it, l, plan()))
	f.setBranch(t, it.ID)

	t2, t3 := t1.Add(time.Minute), t1.Add(2*time.Minute)
	for i, st := range []domain.ItemState{domain.StateDone, domain.StateCancelled, domain.StateDone} {
		if err := f.finishPart(t, parts.Items[i], st, []time.Time{t1, t2, t3}[i]); err != nil {
			t.Fatalf("finish part %d: %v", i, err)
		}
	}

	got := f.item(t, it.ID)
	if got.State != domain.StateQueued || got.Station != stLand || !got.DoneAt.IsZero() || !got.UpdatedAt.Equal(t3) {
		t.Errorf("assembly after its last part = %s at %s, done %v, updated %v; want queued at %s, not done, at %v",
			got.State, got.Station, got.DoneAt, got.UpdatedAt, stLand, t3)
	}

	events := f.Events(t, it.ID)
	sameEvents(t, events[len(events)-1:], []domain.Event{{
		FactoryID: f.factory, ItemID: it.ID, At: t3, Kind: domain.EventAssemblyLanding, Station: stLand, Actor: domain.ActorEngine,
		Data: map[string]any{dataPartsDone: 2, dataParts: 3, "branch": branchIntg},
	}})

	for _, e := range events {
		if e.Kind == domain.EventAssemblyDone {
			t.Errorf("landing assembly has an assembly_done event: %+v", e)
		}
	}

	land := domain.Station{ID: stLand, Kind: domain.KindAgent, Agent: agWorker, Agents: 1}
	if claimed, _ := f.claim(t, land, 0, t3); claimed.ID != it.ID || claimed.State != domain.StateInProgress {
		t.Errorf("claimed %q %s at the landing station; want the assembly in progress", claimed.Title, claimed.State)
	}
}

// An assembly with an integration branch closes as one without: cancelled at its own station
// when every part was cancelled or a person cancelled it, with assembly_done.
func testIntegrationBranchNoDonePartOrCancelClosesAsToday(f *fixture) {
	t := f.t

	// Every part cancelled. Its one part leaves no part queued at the work station, so the next
	// planning item is the one claimed there.
	dropped, dl := f.planning(t, domain.NewItem{Title: "dropped request"})
	_, dparts := f.assemble(t, assembly(dropped, dl, plan()[:1]))
	f.setBranch(t, dropped.ID)

	if err := f.finishPart(t, dparts.Items[0], domain.StateCancelled, t1); err != nil {
		t.Fatal(err)
	}

	if got := f.item(t, dropped.ID); got.State != domain.StateCancelled || got.Station != stWork {
		t.Errorf("assembly whose parts were all cancelled is %s at %s; want cancelled at %s", got.State, got.Station, stWork)
	}

	events := f.Events(t, dropped.ID)
	sameEvents(t, events[len(events)-1:], []domain.Event{{
		FactoryID: f.factory, ItemID: dropped.ID, At: t1, Kind: domain.EventAssemblyDone, Station: stWork, Actor: domain.ActorEngine,
		Data: map[string]any{dataState: string(domain.StateCancelled), dataPartsDone: 0, dataParts: 1},
	}})

	// A person's cancel, with a part already done.
	it, l := f.planning(t, domain.NewItem{Title: titleBig})
	_, parts := f.assemble(t, assembly(it, l, plan()))
	f.setBranch(t, it.ID)

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

	if _, err := f.Store.CascadeParts(f.ctx(t), c); err != nil {
		t.Fatalf("CascadeParts cancelling the open parts: %v", err)
	}

	if a := f.item(t, it.ID); a.State != domain.StateCancelled || !a.DoneAt.IsZero() || a.Station != stWork {
		t.Errorf("assembly after a cancel cascade = %s at %s, done %v; want cancelled at %s, not done", a.State, a.Station, a.DoneAt, stWork)
	}

	events = f.Events(t, it.ID)
	sameEvents(t, events[len(events)-1:], []domain.Event{{
		FactoryID: f.factory, ItemID: it.ID, At: t2, Kind: domain.EventAssemblyDone, Station: stWork, Actor: domain.ActorEngine,
		Data: map[string]any{dataState: string(domain.StateCancelled), dataPartsDone: 1, dataParts: 3},
	}})
}

// integrationRows are id's repositories as key, how they joined, ordinal and integration branch.
func (f *fixture) integrationRows(t *testing.T, id domain.ItemID) []string {
	t.Helper()

	var out []string
	for _, r := range f.itemRepositories(t, id) {
		out = append(out, fmt.Sprintf("%s %s #%d %s", r.Repo, r.Joined, r.Ordinal, r.IntegrationBranch))
	}

	return out
}

// An assembly's integration branch is kept per repository (ADR 0041 decision 5): a repository it
// lacks joins filing with the next ordinal and the branch, one it has gets the branch, and a
// second call with another branch changes no row that has one. An empty branch is invalid and an
// unknown item not found.
func testIntegrationBranchPerRepositorySetOnce(f *fixture) {
	t := f.t

	it, l := f.planning(t, domain.NewItem{Title: titleBig, Repositories: filed(repoAPI)})
	asm, _ := f.assemble(t, assembly(it, l, plan()[:1]))

	if left, err := f.Store.JoinIntegrationRepositories(f.ctx(t), asm.ID, []string{repoAPI, repoWeb}, branchIntg, t1); err != nil || len(left) != 0 {
		t.Fatalf("JoinIntegrationRepositories = %v, %v; want none left", left, err)
	}

	want := []string{repoAPI + " filing #0 " + branchIntg, repoWeb + " filing #1 " + branchIntg}
	if got := f.integrationRows(t, asm.ID); !reflect.DeepEqual(got, want) {
		t.Errorf("after the first call rows = %v; want %v", got, want)
	}

	if rows := f.itemRepositories(t, asm.ID); !rows[1].JoinedAt.Equal(t1) {
		t.Errorf("%s joined at %v; want %v", repoWeb, rows[1].JoinedAt, t1)
	}

	t2 := t1.Add(time.Minute)
	if _, err := f.Store.JoinIntegrationRepositories(f.ctx(t), asm.ID, []string{repoWeb, repoAPI}, "goal/other", t2); err != nil {
		t.Fatalf("second JoinIntegrationRepositories: %v", err)
	}

	if got := f.integrationRows(t, asm.ID); !reflect.DeepEqual(got, want) {
		t.Errorf("after a second call with another branch rows = %v; want %v", got, want)
	}

	if _, err := f.Store.JoinIntegrationRepositories(f.ctx(t), asm.ID, []string{repoAPI}, "", t2); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("JoinIntegrationRepositories without a branch: %v; want ErrInvalid", err)
	}

	if _, err := f.Store.JoinIntegrationRepositories(f.ctx(t), missingItem, []string{repoAPI}, branchIntg, t2); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("JoinIntegrationRepositories of an unknown item: %v; want ErrNotFound", err)
	}
}

// Repositories past domain.MaxItemRepositories get no row and are returned as left out.
func testIntegrationBranchPerRepositoryUpToTheCap(f *fixture) {
	t := f.t

	it, l := f.planning(t, domain.NewItem{Title: titleBig, Repositories: filed(repoAPI)})
	asm, _ := f.assemble(t, assembly(it, l, plan()[:1]))

	more := []string{repoWeb, repoOps}
	for i := range domain.MaxItemRepositories - 2 {
		more = append(more, fmt.Sprintf("github.com/acme/r%d", i))
	}

	left, err := f.Store.JoinIntegrationRepositories(f.ctx(t), asm.ID, more, branchIntg, t1)
	if err != nil {
		t.Fatalf("JoinIntegrationRepositories past the cap: %v", err)
	}

	if last := more[len(more)-1]; !reflect.DeepEqual(left, []string{last}) {
		t.Errorf("left out %v; want [%s]", left, last)
	}

	got := f.integrationRows(t, asm.ID)
	if len(got) != domain.MaxItemRepositories || got[2] != repoOps+" filing #2 "+branchIntg {
		t.Errorf("rows past the cap = %v; want %d, %s third", got, domain.MaxItemRepositories, repoOps)
	}
}

// When an assembly with an integration branch lands, it has a row with that branch for every
// repository of its done parts, in plan order, and none for a repository only a cancelled part
// had; one it already had gets the branch.
func testIntegrationBranchLandingTakesThePartsRepositories(f *fixture) {
	t := f.t

	it, l := f.planning(t, domain.NewItem{Title: titleBig, Repositories: filed(repoAPI)})

	ps := plan()
	ps[0].Filed = filed(repoWeb)
	ps[1].Filed = filed(repoOps)
	ps[2].Filed = filed(repoAPI, repoWeb)

	_, parts := f.assemble(t, assembly(it, l, ps))
	f.setBranch(t, it.ID)

	t2, t3 := t1.Add(time.Minute), t1.Add(2*time.Minute)
	for i, st := range []domain.ItemState{domain.StateDone, domain.StateCancelled, domain.StateDone} {
		if err := f.finishPart(t, parts.Items[i], st, []time.Time{t1, t2, t3}[i]); err != nil {
			t.Fatalf("finish part %d: %v", i, err)
		}
	}

	if got := f.item(t, it.ID); got.State != domain.StateQueued || got.Station != stLand {
		t.Fatalf("assembly after its last part = %s at %s; want queued at %s", got.State, got.Station, stLand)
	}

	want := []string{repoAPI + " filing #0 " + branchIntg, repoWeb + " filing #1 " + branchIntg}
	if got := f.integrationRows(t, it.ID); !reflect.DeepEqual(got, want) {
		t.Errorf("landing assembly's rows = %v; want %v", got, want)
	}

	if rows := f.itemRepositories(t, it.ID); !rows[1].JoinedAt.Equal(t3) {
		t.Errorf("%s joined at %v; want the landing's %v", repoWeb, rows[1].JoinedAt, t3)
	}
}
