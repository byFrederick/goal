package storetest

import (
	"reflect"

	"github.com/byFrederick/goal/internal/domain"
)

// An assembly comes with all its parts in number order whatever their state; an item without
// parts and another factory's assembly are absent.
func testAssembliesPartsInNumberOrder(f *fixture) {
	t := f.t

	asm := f.create(t, domain.NewItem{Title: "the assembly"})
	p1 := f.create(t, domain.NewItem{ParentID: asm.ID, Title: "part one", State: domain.StateDone, Station: stDone})
	plain := f.create(t, domain.NewItem{Title: "a plain item"})
	p2 := f.create(t, domain.NewItem{ParentID: asm.ID, Title: "part two", State: domain.StateReady, Station: stNext})
	p3 := f.create(t, domain.NewItem{ParentID: asm.ID, Title: "part three"})

	other := f.newFactory(t)
	away := f.create(t, domain.NewItem{FactoryID: other, Title: "another factory's assembly"})
	f.create(t, domain.NewItem{FactoryID: other, ParentID: away.ID, Title: "its part"})

	got, err := f.Store.Assemblies(f.ctx(t), f.factory, []domain.ItemID{asm.ID, plain.ID, away.ID, missingItem})
	if err != nil {
		t.Fatalf("Assemblies: %v", err)
	}

	want := map[domain.ItemID]domain.Assembly{asm.ID: {ID: asm.ID, Number: asm.Number, Title: asm.Title, Parts: []domain.Part{
		{ID: p1.ID, Number: p1.Number, Station: stDone, State: domain.StateDone},
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
