package storetest

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// followUpsTests are the cases of the follow-ups kept with a transition (ADR 0040 decision 6).
var followUpsTests = []struct {
	name string
	test func(f *fixture)
}{
	{"FollowUps_KeptWithTheTransition", testFollowUpsKeptWithTheTransition},
	{"FollowUps_RerunKeepsAndDedupes", testFollowUpsRerunKeepsAndDedupes},
	{"FollowUps_OverLongForGoesToTriage", testFollowUpsOverLongForGoesToTriage},
	{"FollowUps_DeliverNotesIntoAnOpenItem", testDeliverNotesIntoAnOpenItem},
	{"FollowUps_DeliverNotesAcrossFactories", testDeliverNotesAcrossFactories},
	{"FollowUps_DeliverNotesToTriageWithTheReason", testDeliverNotesToTriage},
	{"FollowUps_DeliverNotesCappedADay", testDeliverNotesCapped},
	{"FollowUps_DeliverNotesLeavesUntrustedWaiting", testDeliverNotesLeavesUntrusted},
	{"FollowUps_DeliverNotesBetweenItemsNotingEachOther", testDeliverNotesEachOther},
	{"Touches_ItemNoteIsNotATouch", testTouchesItemNoteIsNotATouch},
}

// followUpsOf are the follow-ups a run at the work station wrote, in output order.
func followUpsOf(run domain.RunID, entries ...domain.FollowUp) []domain.FollowUp {
	out := make([]domain.FollowUp, len(entries))
	for i, e := range entries {
		e.RunID, e.Station, e.Agent = run, stWork, agWorker
		out[i] = e
	}

	return out
}

func triageFollowUp(text string) domain.FollowUp {
	return domain.FollowUp{Text: text, Target: domain.FollowUpTriage, State: domain.FollowUpWaiting}
}

// keptFollowUps reads an item's follow-ups, checks the fields the store sets and clears them, so
// a test compares what was written.
func (f *fixture) keptFollowUps(t *testing.T, it domain.Item) []domain.FollowUp {
	t.Helper()

	got, err := f.Store.FollowUps(f.ctx(t), it.ID)
	if err != nil {
		t.Fatalf("FollowUps: %v", err)
	}

	for i, fu := range got {
		if fu.ID == "" || fu.FactoryID != it.FactoryID || fu.ItemID != it.ID || fu.CreatedAt.IsZero() {
			t.Errorf("follow-up %d = %+v; want an id, the item's factory and item, and a time", i, fu)
		}

		got[i].ID, got[i].FactoryID, got[i].ItemID, got[i].CreatedAt = "", "", "", time.Time{}
	}

	return got
}

// A transition keeps its follow-ups on the item, each target as written; Assemble keeps them
// too. A transition the store refuses keeps none, and another organization sees none.
func testFollowUpsKeptWithTheTransition(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{})
	_, l := f.claim(t, work, 0, t1)
	run := f.startRun(t, it.ID, t1)

	written := followUpsOf(run,
		triageFollowUp("Add an index on runs"),
		domain.FollowUp{
			Text: "The API needs this field", Target: domain.FollowUpNote, State: domain.FollowUpWaiting,
			For: &domain.ItemRef{Factory: "acme", Number: 7},
		},
		domain.FollowUp{
			Text: "Remove the old column", Target: domain.FollowUpStart, State: domain.FollowUpWaiting,
			Start: &domain.FollowUpPlan{
				In: "7d", After: []string{"acme#8"}, Title: "Drop source", Type: "chore", Lane: laneBackend,
				Repositories: []string{"acme/goal"}, Factory: "acme-ops",
			},
		},
		domain.FollowUp{Text: "for a later item", Target: domain.FollowUpTriage, State: domain.FollowUpWaiting, Reason: `for "#12" is not an item ref`},
		domain.FollowUp{Text: "3", State: domain.FollowUpDropped, Reason: domain.ReasonFollowUpNoText},
	)

	stale := next(it, l, t1.Add(time.Minute))
	stale.From.Station, stale.FollowUps = stNext, written

	if _, err := f.Store.Transition(f.ctx(t), stale); err == nil {
		t.Fatal("Transition from the wrong station succeeded; want it refused")
	}

	if got := f.keptFollowUps(t, it); len(got) != 0 {
		t.Fatalf("follow-ups after a refused transition = %+v; want none", got)
	}

	tr := next(it, l, t1.Add(time.Minute))
	tr.FollowUps = written

	if _, err := f.Store.Transition(f.ctx(t), tr); err != nil {
		t.Fatal(err)
	}

	if got := f.keptFollowUps(t, it); !reflect.DeepEqual(got, written) {
		t.Errorf("kept follow-ups = %+v\nwant %+v", got, written)
	}

	other := f.otherOrg(t)
	if got, err := other.Store.FollowUps(other.ctx(t), it.ID); err != nil || len(got) != 0 {
		t.Errorf("another organization's FollowUps = %+v, %v; want none", got, err)
	}

	planned, pl := f.planning(t, domain.NewItem{Title: "planned"})
	planRun := f.startRun(t, planned.ID, t1)
	a := assembly(planned, pl, plan())
	a.Transition.FollowUps = followUpsOf(planRun, triageFollowUp("Split the store"))

	f.assemble(t, a)

	if got := f.keptFollowUps(t, planned); !reflect.DeepEqual(got, a.Transition.FollowUps) {
		t.Errorf("follow-ups kept by Assemble = %+v; want %+v", got, a.Transition.FollowUps)
	}
}

// A rerun keeps the follow-ups the first run wrote and adds its own, each text once per item
// whatever its case and spacing.
func testFollowUpsRerunKeepsAndDedupes(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{})
	_, l := f.claim(t, work, 0, t1)
	first := f.startRun(t, it.ID, t1)

	tr := next(it, l, t1.Add(time.Minute))
	tr.FollowUps = followUpsOf(first, triageFollowUp("Add an index"), triageFollowUp("Check the docs"))

	if _, err := f.Store.Transition(f.ctx(t), tr); err != nil {
		t.Fatal(err)
	}

	second := f.startRun(t, it.ID, t1.Add(2*time.Minute))
	queued := domain.Position{Station: stNext, State: domain.StateQueued}

	if _, err := f.Store.Transition(f.ctx(t), ports.Transition{
		ItemID: it.ID, From: queued, To: queued, At: t1.Add(3 * time.Minute),
		FollowUps: followUpsOf(second, triageFollowUp(" add  AN\tindex "), triageFollowUp("Name the new metric")),
	}); err != nil {
		t.Fatal(err)
	}

	want := append(followUpsOf(first, triageFollowUp("Add an index"), triageFollowUp("Check the docs")),
		followUpsOf(second, triageFollowUp("Name the new metric"))...)
	if got := f.keptFollowUps(t, it); !reflect.DeepEqual(got, want) {
		t.Errorf("follow-ups after a rerun = %+v\nwant %+v", got, want)
	}
}

// An output whose for names a factory longer than the store keeps still passes its transition:
// FollowUpsFrom sends that entry to triage, so an agent's output cannot fail the transaction that
// accepts it.
func testFollowUpsOverLongForGoesToTriage(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{})
	_, l := f.claim(t, work, 0, t1)
	run := f.startRun(t, it.ID, t1)

	out := json.RawMessage(`{"followups":[{"text":"Long ref","for":"` + strings.Repeat("a", 101) + `#3"}]}`)
	parsed := domain.FollowUpsFrom(out, "followups")

	tr := next(it, l, t1.Add(time.Minute))
	tr.FollowUps = followUpsOf(run, parsed...)

	if _, err := f.Store.Transition(f.ctx(t), tr); err != nil {
		t.Fatalf("Transition with an over-long for: %v; want it kept as triage", err)
	}

	got := f.keptFollowUps(t, it)
	if len(got) != 1 || got[0].Target != domain.FollowUpTriage || got[0].For != nil || got[0].Reason == "" {
		t.Errorf("kept follow-ups = %+v; want one for triage with a reason", got)
	}
}

// The names the note tests give the fixture's factories; the store knows factories by id, and
// DeliverNotes resolves a for against the names it is handed.
const (
	nameDev = "dev"
	nameOps = "ops"
)

// noteFor is a waiting note for ref <factory>#<number>.
func noteFor(text, factory string, number int64) domain.FollowUp {
	return domain.FollowUp{
		Text: text, Target: domain.FollowUpNote, State: domain.FollowUpWaiting,
		For: &domain.ItemRef{Factory: factory, Number: number},
	}
}

// keepNotes keeps follow-ups on the queued item src at the work station, with a transition
// that leaves it where it is.
func (f *fixture) keepNotes(t *testing.T, src domain.Item, fs ...domain.FollowUp) {
	t.Helper()

	queued := domain.Position{Station: src.Station, State: domain.StateQueued}

	if _, err := f.Store.Transition(f.ctx(t), ports.Transition{
		ItemID: src.ID, From: queued, To: queued, At: t1, FollowUps: followUpsOf("", fs...),
	}); err != nil {
		t.Fatalf("keep notes: %v", err)
	}
}

// deliver runs DeliverNotes at t1 with the given factories.
func (f *fixture) deliver(t *testing.T, factories []domain.NoteFactory, limit int) []domain.FollowUp {
	t.Helper()

	got, err := f.Store.DeliverNotes(f.ctx(t), factories, t1.Add(time.Minute), limit)
	if err != nil {
		t.Fatalf("DeliverNotes: %v", err)
	}

	return got
}

// itemNotes are an item's item_note events.
func (f *fixture) itemNotes(t *testing.T, id domain.ItemID) []domain.Event {
	t.Helper()

	var out []domain.Event

	for _, e := range f.Events(t, id) {
		if e.Kind == domain.EventItemNote {
			out = append(out, e)
		}
	}

	return out
}

// A note for an open item of the sender's factory becomes an item_note event by the engine on
// it, with the source in its data, and is delivered once; another organization delivers none.
func testDeliverNotesIntoAnOpenItem(f *fixture) {
	t := f.t

	src := f.create(t, domain.NewItem{Title: "source"})
	dst := f.create(t, domain.NewItem{Title: "target"})
	f.keepNotes(t, src, noteFor("The API needs this field", nameDev, dst.Number))

	factories := []domain.NoteFactory{{ID: f.factory, Name: nameDev}}

	other := f.otherOrg(t)
	if got := other.deliver(t, factories, 10); len(got) != 0 {
		t.Errorf("another organization's DeliverNotes = %+v; want none", got)
	}

	got := f.deliver(t, factories, 10)
	if len(got) != 1 || got[0].State != domain.FollowUpDelivered || got[0].Target != domain.FollowUpNote || got[0].Reason != "" {
		t.Fatalf("DeliverNotes = %+v; want the note delivered", got)
	}

	f.expectItemNote(t, src, dst, got[0])

	if kept := f.keptFollowUps(t, src); len(kept) != 1 || kept[0].State != domain.FollowUpDelivered {
		t.Errorf("kept follow-ups = %+v; want the note delivered", kept)
	}

	if again := f.deliver(t, factories, 10); len(again) != 0 {
		t.Errorf("DeliverNotes again = %+v; want nothing left", again)
	}

	if len(f.itemNotes(t, src.ID)) != 0 {
		t.Error("the source item got an item_note; want only the target")
	}
}

// expectItemNote checks the one item_note on dst: by the engine, with the follow-up's source.
func (f *fixture) expectItemNote(t *testing.T, src, dst domain.Item, fu domain.FollowUp) {
	t.Helper()

	notes := f.itemNotes(t, dst.ID)
	if len(notes) != 1 {
		t.Fatalf("item_note events on the target = %+v; want one", notes)
	}

	want := map[string]any{
		"text": "The API needs this field", domain.KeyNoteFollowUp: string(fu.ID),
		domain.KeyNoteFromItem: fmt.Sprintf("%s#%d", nameDev, src.Number), domain.KeyNoteFromItemID: string(src.ID),
		domain.KeyNoteFromFactory: string(f.factory), domain.KeyNoteStation: stWork, domain.KeyNoteAgent: agWorker,
		dataActor: domain.EngineActor.Ref(),
	}

	if n := notes[0]; n.Actor != domain.ActorEngine || n.FactoryID != f.factory || n.Station != stWork || !reflect.DeepEqual(n.Data, want) {
		t.Errorf("item_note = %+v\nwant actor %s, data %v", n, domain.ActorEngine, want)
	}
}

// A note into another factory is delivered when that factory lists the sender in followUpsFrom,
// and named by metadata.name or id.
func testDeliverNotesAcrossFactories(f *fixture) {
	t := f.t

	ops := f.newFactory(t)
	dst := f.create(t, domain.NewItem{FactoryID: ops, Title: "target"})
	src := f.create(t, domain.NewItem{Title: "source"})
	f.keepNotes(t, src, noteFor("by name", nameOps, dst.Number), noteFor("by id", string(ops), dst.Number))

	got := f.deliver(t, []domain.NoteFactory{
		{ID: f.factory, Name: nameDev}, {ID: ops, Name: nameOps, FollowUpsFrom: []domain.FactoryID{f.factory}},
	}, 10)

	if len(got) != 2 || got[0].State != domain.FollowUpDelivered || got[1].State != domain.FollowUpDelivered {
		t.Fatalf("DeliverNotes = %+v; want both delivered", got)
	}

	notes := f.itemNotes(t, dst.ID)
	if len(notes) != 2 || notes[0].FactoryID != ops || notes[0].Data[domain.KeyNoteFromFactory] != string(f.factory) {
		t.Errorf("item_note events on the other factory's item = %+v; want two from %s", notes, f.factory)
	}

	f.expectItemNotesRead(t, dst, notes)
}

// expectItemNotesRead checks that ItemNotes reads want, dst's item_note events, oldest first, and
// limits them; another organization reads none.
func (f *fixture) expectItemNotesRead(t *testing.T, dst domain.Item, want []domain.Event) {
	t.Helper()

	got, err := f.Store.ItemNotes(f.ctx(t), dst.ID, 10)
	if err != nil {
		t.Fatalf("ItemNotes: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("ItemNotes = %+v\nwant %+v", got, want)
	}

	if one, err := f.Store.ItemNotes(f.ctx(t), dst.ID, 1); err != nil || len(one) != 1 || one[0].ID != want[0].ID {
		t.Errorf("ItemNotes limit 1 = %+v, %v; want the oldest", one, err)
	}

	o := f.otherOrg(t)
	if other, err := o.Store.ItemNotes(o.ctx(t), dst.ID, 10); err != nil || len(other) != 0 {
		t.Errorf("another organization's ItemNotes = %+v, %v; want none", other, err)
	}
}

// A note that cannot go where it says goes to triage, still waiting, with the reason and the ref
// it named.
func testDeliverNotesToTriage(f *fixture) {
	t := f.t

	ops := f.newFactory(t)
	opsItem := f.create(t, domain.NewItem{FactoryID: ops})
	done := f.create(t, domain.NewItem{State: domain.StateDone})
	src := f.create(t, domain.NewItem{Title: "source"})

	f.keepNotes(t, src,
		noteFor("unknown factory", "acme", 1),
		noteFor("not accepted", nameOps, opsItem.Number),
		noteFor("unknown item", nameDev, 9999),
		noteFor("done item", nameDev, done.Number),
	)

	got := f.deliver(t, []domain.NoteFactory{{ID: f.factory, Name: nameDev}, {ID: ops, Name: nameOps}}, 10)

	reasons := []string{
		domain.NoteTriageReason(domain.ItemRef{Factory: "acme", Number: 1}, domain.ReasonNoteNoFactory),
		domain.NoteTriageReason(domain.ItemRef{Factory: nameOps, Number: opsItem.Number}, domain.ReasonNoteNotAccepted),
		domain.NoteTriageReason(domain.ItemRef{Factory: nameDev, Number: 9999}, domain.ReasonNoteNoItem),
		domain.NoteTriageReason(domain.ItemRef{Factory: nameDev, Number: done.Number}, domain.ReasonNoteItemClosed),
	}

	if len(got) != len(reasons) {
		t.Fatalf("DeliverNotes = %+v; want %d to triage", got, len(reasons))
	}

	kept := f.keptFollowUps(t, src)
	for i, want := range reasons {
		if k := kept[i]; k.Target != domain.FollowUpTriage || k.State != domain.FollowUpWaiting || k.For != nil || k.Reason != want {
			t.Errorf("follow-up %d = %+v; want waiting for triage with %q", i, k, want)
		}
	}

	if len(f.itemNotes(t, opsItem.ID))+len(f.itemNotes(t, done.ID)) != 0 {
		t.Error("an undeliverable note left an item_note")
	}
}

// A factory takes MaxNotesADay notes from other factories a day; the next goes to triage, while
// a note within the factory is not counted.
func testDeliverNotesCapped(f *fixture) {
	t := f.t

	ops := f.newFactory(t)
	dst := f.create(t, domain.NewItem{FactoryID: ops})
	src := f.create(t, domain.NewItem{})
	inside := f.create(t, domain.NewItem{FactoryID: ops})

	var notes []domain.FollowUp
	for i := range domain.MaxNotesADay + 1 {
		notes = append(notes, noteFor(fmt.Sprintf("note %d", i), nameOps, dst.Number))
	}

	f.keepNotes(t, src, notes...)
	f.keepNotes(t, inside, noteFor("from inside", nameOps, dst.Number))

	got := f.deliver(t, []domain.NoteFactory{
		{ID: f.factory, Name: nameDev}, {ID: ops, Name: nameOps, FollowUpsFrom: []domain.FactoryID{f.factory}},
	}, 100)

	var delivered, capped int

	for _, g := range got {
		switch {
		case g.State == domain.FollowUpDelivered:
			delivered++
		case strings.HasSuffix(g.Reason, domain.ReasonNoteCapped):
			capped++
		}
	}

	if delivered != domain.MaxNotesADay+1 || capped != 1 {
		t.Errorf("DeliverNotes delivered %d and capped %d; want %d delivered (one from inside) and 1 capped",
			delivered, capped, domain.MaxNotesADay+1)
	}
}

// A note whose source item is untrusted is never delivered on its own: it stays waiting
// (decision 21).
func testDeliverNotesLeavesUntrusted(f *fixture) {
	t := f.t

	src := f.create(t, domain.NewItem{Untrusted: true})
	dst := f.create(t, domain.NewItem{})
	f.keepNotes(t, src, noteFor("from a webhook", nameDev, dst.Number))

	if got := f.deliver(t, []domain.NoteFactory{{ID: f.factory, Name: nameDev}}, 10); len(got) != 0 {
		t.Errorf("DeliverNotes = %+v; want the untrusted note left", got)
	}

	if kept := f.keptFollowUps(t, src); len(kept) != 1 || kept[0].State != domain.FollowUpWaiting || kept[0].Target != domain.FollowUpNote {
		t.Errorf("kept follow-ups = %+v; want the note waiting", kept)
	}
}

// Two items noting each other, swept by two instances at once, each get the other's note once:
// a delivery locks only its target, so the two cannot deadlock.
func testDeliverNotesEachOther(f *fixture) {
	t := f.t

	a := f.create(t, domain.NewItem{Title: "a"})
	b := f.create(t, domain.NewItem{Title: "b"})

	for i := range 5 {
		f.keepNotes(t, a, noteFor(fmt.Sprintf("a to b %d", i), nameDev, b.Number))
		f.keepNotes(t, b, noteFor(fmt.Sprintf("b to a %d", i), nameDev, a.Number))
	}

	factories := []domain.NoteFactory{{ID: f.factory, Name: nameDev}}

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		total int
		errs  []error
	)

	for range 4 {
		wg.Go(func() {
			got, err := f.Store.DeliverNotes(f.ctx(t), factories, t1.Add(time.Minute), 100)

			mu.Lock()
			defer mu.Unlock()

			total += len(got)

			errs = append(errs, err)
		})
	}

	wg.Wait()

	for _, err := range errs {
		if err != nil {
			t.Fatalf("concurrent DeliverNotes: %v", err)
		}
	}

	if total != 10 || len(f.itemNotes(t, a.ID)) != 5 || len(f.itemNotes(t, b.ID)) != 5 {
		t.Errorf("concurrent sweeps delivered %d, a has %d and b %d notes; want 10, 5 and 5",
			total, len(f.itemNotes(t, a.ID)), len(f.itemNotes(t, b.ID)))
	}
}
