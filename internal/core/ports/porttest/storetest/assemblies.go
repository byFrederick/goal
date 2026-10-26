package storetest

import (
	"reflect"

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
