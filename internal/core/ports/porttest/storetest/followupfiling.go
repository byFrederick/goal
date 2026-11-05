package storetest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// followUpFilingTests are the cases of follow-ups filed when work lands, the triage item, and a
// person's File and Drop (ADR 0040 decisions 8, 9, 10 and 21).
var followUpFilingTests = []struct {
	name string
	test func(f *fixture)
}{
	{"FollowUps_FileStartsWhenWorkLands", testFileStartsWhenWorkLands},
	{"FollowUps_FileIntoAnotherFactory", testFileIntoAnotherFactory},
	{"FollowUps_FileCancelledWorkDropped", testFileCancelledWorkDropped},
	{"FollowUps_FileCappedADay", testFileCappedADay},
	{"FollowUps_FileThreeDeep", testFileThreeDeep},
	{"FollowUps_FileDedupesByTitle", testFileDedupesByTitle},
	{"FollowUps_FileUntrustedToTheirOwnTriage", testFileUntrustedToOwnTriage},
	{"FollowUps_FileConcurrentSweeps", testFileConcurrentSweeps},
	{"FollowUps_TriageCollectsUntilClaimed", testTriageCollectsUntilClaimed},
	{"FollowUps_TriageNoneWhenNothingLeft", testTriageNoneWhenNothingLeft},
	{"FollowUps_FileAndDropByAPerson", testFileAndDropByAPerson},
	{"FollowUps_FileInAClosedTriageItem", testFileInAClosedTriageItem},
	{"FollowUps_FiledFromNamesSourcesAndLanding", testFiledFromNamesSourcesAndLanding},
}

// tLanded is when the tests' work lands, and tSweep when the sweep runs.
var (
	tLanded = t0.Add(5 * time.Hour)
	tSweep  = tLanded.Add(time.Hour)
)

// lineOf is a line of factory named name, with the work and next stations, that accepts
// follow-ups from from.
func lineOf(factory domain.FactoryID, name string, from ...domain.FactoryID) domain.Line {
	l := domain.Line{FactoryID: factory, Name: name, Stations: []domain.Station{work, {ID: stNext, Kind: domain.KindAgent, Agent: agWorker}}}
	l.Flow.FollowUpsFrom = from

	return l
}

func startFollowUp(text string, p domain.FollowUpPlan) domain.FollowUp {
	return domain.FollowUp{Text: text, Target: domain.FollowUpStart, State: domain.FollowUpWaiting, Start: &p}
}

// land moves it, queued at the work station, to done at at, or to cancelled.
func (f *fixture) land(t *testing.T, it domain.Item, state domain.ItemState, at time.Time) {
	t.Helper()

	tr := ports.Transition{ItemID: it.ID, From: it.Position(), To: domain.Position{Station: stDone, State: state}, At: at}
	if state == domain.StateDone {
		tr.DoneAt = at
	}

	if _, err := f.Store.Transition(f.ctx(t), tr); err != nil {
		t.Fatalf("finish %s: %v", it.ID, err)
	}
}

// sweep runs FileFollowUps at tSweep with lines.
func (f *fixture) sweep(t *testing.T, lines []domain.Line, limit int) []domain.FollowUp {
	t.Helper()

	got, err := f.Store.FileFollowUps(f.ctx(t), lines, tSweep, limit)
	if err != nil {
		t.Fatalf("FileFollowUps: %v", err)
	}

	return got
}

// items are the factory's items, newest first.
func (f *fixture) items(t *testing.T, factory domain.FactoryID) []domain.Item {
	t.Helper()

	got, err := f.Store.ListItems(f.ctx(t), ports.ItemQuery{FactoryID: factory, Limit: 200})
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}

	return got
}

// triageItems are the factory's triage items, newest first.
func (f *fixture) triageItems(t *testing.T, factory domain.FactoryID) []domain.Item {
	t.Helper()

	var out []domain.Item

	for _, it := range f.items(t, factory) {
		if it.Triage {
			out = append(out, it)
		}
	}

	return out
}

// Nothing is filed before the work lands. Then each start of the item and of its parts becomes
// an item by the engine at the line's first station: origin follow-up, standard class, priority
// one below its source's, generation one above, its start counted from the landing, linked
// after the items it names; the follow-up is filed with it. A second sweep files nothing.
func testFileStartsWhenWorkLands(f *fixture) {
	t := f.t
	lines := []domain.Line{lineOf(f.factory, nameDev)}

	before := f.create(t, domain.NewItem{Title: "before"})
	root := f.create(t, domain.NewItem{Title: "root", Priority: 3, Generation: 1, Class: domain.ClassExpedite})
	part := f.create(t, domain.NewItem{Title: "part", ParentID: root.ID, Priority: 3, Generation: 1})

	f.keepNotes(t, root, startFollowUp("Index the runs", domain.FollowUpPlan{
		In: "2d", Title: "Index runs", Lane: laneBackend, After: []string{fmt.Sprintf("%s#%d", nameDev, before.Number)},
	}))
	f.keepNotes(t, part, startFollowUp("Drop the column", domain.FollowUpPlan{Title: "Drop column"}))

	if got := f.sweep(t, lines, 10); len(got) != 0 {
		t.Fatalf("FileFollowUps before landing = %+v; want none", got)
	}

	f.land(t, part, domain.StateDone, tLanded.Add(-time.Hour))
	f.land(t, root, domain.StateDone, tLanded)

	got := f.sweep(t, lines, 10)
	if len(got) != 2 {
		t.Fatalf("FileFollowUps = %+v; want two filed", got)
	}

	for _, fu := range got {
		if fu.State != domain.FollowUpFiled || fu.FiledItem == "" {
			t.Fatalf("follow-up = %+v; want filed with an item", fu)
		}

		f.expectStartItem(t, f.item(t, fu.FiledItem), before)
	}

	if again := f.sweep(t, lines, 10); len(again) != 0 {
		t.Errorf("second sweep = %+v; want nothing", again)
	}

	if tri := f.triageItems(t, f.factory); len(tri) != 0 {
		t.Errorf("triage items = %+v; want none", tri)
	}
}

// expectStartItem checks it, filed from a start of testFileStartsWhenWorkLands: a standard
// follow-up by the engine at the first station, priority 2 and generation 2, with its start.
func (f *fixture) expectStartItem(t *testing.T, it, before domain.Item) {
	t.Helper()

	if it.Origin != domain.OriginFollowUp || it.CreatedBy != "" || it.Class != domain.ClassStandard || it.Priority != 2 ||
		it.Generation != 2 || it.Station != stWork || it.FactoryID != f.factory || it.Triage {
		t.Errorf("filed item = %+v; want a standard follow-up by the engine at the first station, priority 2, generation 2", it)
	}

	switch it.Title {
	case "Index runs":
		f.expectIndexRuns(t, it, before)
	case "Drop column":
		if !it.StartAt.IsZero() {
			t.Errorf("Drop column starts %v; want now", it.StartAt)
		}
	default:
		t.Errorf("filed %q", it.Title)
	}
}

// expectIndexRuns checks the Index runs item: it starts 2 days after landing, in the backend lane,
// after before.
func (f *fixture) expectIndexRuns(t *testing.T, it, before domain.Item) {
	t.Helper()

	if !it.StartAt.Equal(tLanded.Add(48*time.Hour)) || it.Lane != laneBackend {
		t.Errorf("Index runs starts %v in lane %q; want 2 days after landing, backend", it.StartAt, it.Lane)
	}

	edges, err := f.Store.Edges(f.ctx(t), it.ID)
	if err != nil || len(edges.Needs) != 1 || edges.Needs[0].Needs != before.ID {
		t.Errorf("Index runs needs %+v, %v; want %s", edges.Needs, err, before.ID)
	}
}

// A start into another factory is filed there only when that factory lists the sender in
// followUpsFrom, and only when its after refs are that factory's own items; otherwise it goes to
// the sender's triage with the reason.
func testFileIntoAnotherFactory(f *fixture) {
	t := f.t

	ops, closed := f.newFactory(t), f.newFactory(t)
	lines := []domain.Line{lineOf(f.factory, nameDev), lineOf(ops, nameOps, f.factory), lineOf(closed, "closed")}

	devItem := f.create(t, domain.NewItem{Title: "dev item"})
	src := f.create(t, domain.NewItem{Title: titleSrc})
	f.keepNotes(t, src,
		startFollowUp("into ops", domain.FollowUpPlan{Title: "Ops work", Factory: nameOps}),
		startFollowUp("into closed", domain.FollowUpPlan{Title: "Closed work", Factory: "closed"}),
		startFollowUp("into nowhere", domain.FollowUpPlan{Title: "Nowhere", Factory: "nowhere"}),
		startFollowUp("after another's", domain.FollowUpPlan{
			Title: "Ops after dev", Factory: nameOps, After: []string{fmt.Sprintf("%s#%d", nameDev, devItem.Number)},
		}),
	)
	f.land(t, src, domain.StateDone, tLanded)

	got := f.sweep(t, lines, 10)

	byText := map[string]domain.FollowUp{}
	for _, fu := range got {
		byText[fu.Text] = fu
	}

	if fu := byText["into ops"]; fu.State != domain.FollowUpFiled || f.item(t, fu.FiledItem).FactoryID != ops {
		t.Errorf("into ops = %+v; want filed into ops", fu)
	}

	for text, want := range map[string]string{
		"into closed": domain.ReasonNoteNotAccepted, "into nowhere": domain.ReasonNoteNoFactory, "after another's": "another factory",
	} {
		fu := byText[text]
		if fu.State != domain.FollowUpFiled || fu.Target != domain.FollowUpTriage || fu.Start != nil || !strings.Contains(fu.Reason, want) {
			t.Errorf("%s = %+v; want in triage with a reason naming %q", text, fu, want)

			continue
		}

		if it := f.item(t, fu.FiledItem); !it.Triage || it.FactoryID != f.factory {
			t.Errorf("%s joined %+v; want the sender's triage item", text, it)
		}
	}

	f.expectFiledWhere(t, src)
}

// expectFiledWhere checks that FollowUps reads each filed follow-up of src with the factory and
// number of the item it was filed as or onto, another factory's included.
func (f *fixture) expectFiledWhere(t *testing.T, src domain.Item) {
	t.Helper()

	kept, err := f.Store.FollowUps(f.ctx(t), src.ID)
	if err != nil {
		t.Fatalf("FollowUps: %v", err)
	}

	for _, fu := range kept {
		if fu.State != domain.FollowUpFiled {
			continue
		}

		if it := f.item(t, fu.FiledItem); fu.FiledFactory != it.FactoryID || fu.FiledNumber != it.Number {
			t.Errorf("%s filed at %s#%d; want %s#%d", fu.Text, fu.FiledFactory, fu.FiledNumber, it.FactoryID, it.Number)
		}
	}
}

// A cancelled item's waiting follow-ups, and its parts', are dropped: the work was cancelled.
func testFileCancelledWorkDropped(f *fixture) {
	t := f.t
	lines := []domain.Line{lineOf(f.factory, nameDev)}

	root := f.create(t, domain.NewItem{Title: "root"})
	part := f.create(t, domain.NewItem{Title: "part", ParentID: root.ID})
	f.keepNotes(t, root, startFollowUp("a start", domain.FollowUpPlan{Title: "Start"}), triageFollowUp("a triage"))
	f.keepNotes(t, part, triageFollowUp("a part's"))
	f.land(t, root, domain.StateCancelled, tLanded)

	got := f.sweep(t, lines, 10)
	if len(got) != 3 {
		t.Fatalf("FileFollowUps = %+v; want three dropped", got)
	}

	for _, fu := range got {
		if fu.State != domain.FollowUpDropped || fu.Reason != domain.ReasonFollowUpCancelled {
			t.Errorf("follow-up = %+v; want dropped: %s", fu, domain.ReasonFollowUpCancelled)
		}
	}

	if tri := f.triageItems(t, f.factory); len(tri) != 0 {
		t.Errorf("triage items = %+v; want none", tri)
	}
}

// A factory takes 20 follow-up items a day the engine filed, from every sender; past it a start
// goes to the sender's triage.
func testFileCappedADay(f *fixture) {
	t := f.t

	ops := f.newFactory(t)
	lines := []domain.Line{lineOf(f.factory, nameDev), lineOf(ops, nameOps, f.factory)}

	for i := range domain.MaxFollowUpItemsADay {
		f.create(t, domain.NewItem{FactoryID: ops, Title: fmt.Sprintf("earlier %d", i), Origin: domain.OriginFollowUp, CreatedAt: tSweep.Add(-time.Hour)})
	}

	src := f.create(t, domain.NewItem{Title: titleSrc})
	f.keepNotes(t, src, startFollowUp("one more", domain.FollowUpPlan{Title: "One more", Factory: nameOps}))
	f.land(t, src, domain.StateDone, tLanded)

	got := f.sweep(t, lines, 10)
	if len(got) != 1 || got[0].Target != domain.FollowUpTriage || !strings.Contains(got[0].Reason, domain.ReasonFollowUpCapped) {
		t.Fatalf("FileFollowUps = %+v; want it in triage, capped", got)
	}

	if it := f.item(t, got[0].FiledItem); !it.Triage || it.FactoryID != f.factory {
		t.Errorf("capped follow-up joined %+v; want the sender's triage item", it)
	}
}

// The follow-ups of an item three follow-ups deep are dropped and never filed.
func testFileThreeDeep(f *fixture) {
	t := f.t

	src := f.create(t, domain.NewItem{Title: "deep", Origin: domain.OriginFollowUp, Generation: domain.MaxFollowUpGeneration})
	f.keepNotes(t, src, startFollowUp("deeper", domain.FollowUpPlan{Title: "Deeper"}), triageFollowUp("deeper still"))
	f.land(t, src, domain.StateDone, tLanded)

	got := f.sweep(t, []domain.Line{lineOf(f.factory, nameDev)}, 10)
	if len(got) != 2 {
		t.Fatalf("FileFollowUps = %+v; want two dropped", got)
	}

	for _, fu := range got {
		if fu.State != domain.FollowUpDropped || fu.Reason != domain.ReasonFollowUpThreeDeep {
			t.Errorf("follow-up = %+v; want dropped three deep", fu)
		}
	}
}

// A start whose title matches, trimmed and case folded, an open item already filed from the
// same source is filed onto that item.
func testFileDedupesByTitle(f *fixture) {
	t := f.t

	src := f.create(t, domain.NewItem{Title: titleSrc})
	f.keepNotes(t, src,
		startFollowUp("first", domain.FollowUpPlan{Title: "Index runs"}),
		startFollowUp("second", domain.FollowUpPlan{Title: "  index RUNS "}),
	)
	f.land(t, src, domain.StateDone, tLanded)

	got := f.sweep(t, []domain.Line{lineOf(f.factory, nameDev)}, 10)
	if len(got) != 2 || got[0].FiledItem == "" || got[0].FiledItem != got[1].FiledItem {
		t.Fatalf("FileFollowUps = %+v; want both filed onto one item", got)
	}

	if n := len(f.items(t, f.factory)); n != 2 {
		t.Errorf("factory has %d items; want the source and one follow-up item", n)
	}
}

// Follow-ups of an untrusted item's landed work stay waiting (decision 21).
// The follow-ups of untrusted work, its notes too, are never delivered or filed on their own: when
// it lands they go to a triage item of their own, untrusted and filed paused, which collects
// nothing else; a trusted item's follow-ups still go to the factory's collecting triage item.
func testFileUntrustedToOwnTriage(f *fixture) {
	t := f.t
	lines := []domain.Line{lineOf(f.factory, nameDev)}

	src := f.create(t, domain.NewItem{Title: "hook", Untrusted: true})
	f.keepNotes(t, src, startFollowUp("from a webhook", domain.FollowUpPlan{Title: "Hook work"}),
		triageFollowUp("look at the hook"), noteFor("tell #1", nameDev, 1))

	if got := f.deliver(t, []domain.NoteFactory{domain.NoteFactoryOf(lines[0])}, 10); len(got) != 0 {
		t.Fatalf("DeliverNotes = %+v; want an untrusted item's note left waiting", got)
	}

	f.land(t, src, domain.StateDone, tLanded)

	got := f.sweep(t, lines, 10)
	if len(got) != 3 {
		t.Fatalf("FileFollowUps = %+v; want all three of the untrusted item's follow-ups", got)
	}

	own := got[0].FiledItem
	for _, fu := range got {
		if fu.State != domain.FollowUpFiled || fu.Target != domain.FollowUpTriage || fu.FiledItem != own || fu.For != nil || fu.Start != nil {
			t.Errorf("follow-up = %+v; want it filed for triage onto one item", fu)
		}
	}

	if !strings.Contains(got[0].Reason, "Hook work") || !strings.Contains(got[2].Reason, "dev#1") {
		t.Errorf("reasons %q, %q; want the start's title and the note's ref kept", got[0].Reason, got[2].Reason)
	}

	checkOwnTriage(t, f, lines, own)
}

// checkOwnTriage checks the untrusted item's own triage item, and that a trusted item's follow-up goes elsewhere.
func checkOwnTriage(t *testing.T, f *fixture, lines []domain.Line, own domain.ItemID) {
	t.Helper()

	it := f.item(t, own)
	if !it.Triage || !it.Untrusted || it.State != domain.StateBlocked || it.Origin != domain.OriginFollowUp {
		t.Errorf("own triage item = %+v; want an untrusted triage item, blocked", it)
	}

	var paused []domain.Event

	for _, e := range f.Events(t, own) {
		if e.Kind == domain.EventPaused {
			paused = append(paused, e)
		}
	}

	if len(paused) != 1 || paused[0].Data["reason"] != domain.ReasonUntrustedFollowUps {
		t.Errorf("paused events = %+v; want one with %q", paused, domain.ReasonUntrustedFollowUps)
	}

	trusted := f.create(t, domain.NewItem{Title: "person's"})
	f.keepNotes(t, trusted, triageFollowUp("look at the logs"))
	f.land(t, trusted, domain.StateDone, tLanded)

	later := f.sweep(t, lines, 10)
	if len(later) != 1 || later[0].FiledItem == own || later[0].FiledItem == "" {
		t.Errorf("a trusted item's follow-up = %+v; want it in a triage item other than the untrusted one", later)
	}
}

// Four sweeps at once file each follow-up once and open one triage item.
func testFileConcurrentSweeps(f *fixture) {
	t := f.t
	lines := []domain.Line{lineOf(f.factory, nameDev)}

	for i := range 6 {
		src := f.create(t, domain.NewItem{Title: fmt.Sprintf("src %d", i)})
		f.keepNotes(t, src, startFollowUp(fmt.Sprintf("start %d", i), domain.FollowUpPlan{Title: fmt.Sprintf("Start %d", i)}),
			triageFollowUp(fmt.Sprintf("triage %d", i)))
		f.land(t, src, domain.StateDone, tLanded)
	}

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		total int
		errs  []error
	)

	for range 4 {
		wg.Go(func() {
			got, err := f.Store.FileFollowUps(f.ctx(t), lines, tSweep, 100)

			mu.Lock()
			defer mu.Unlock()

			total += len(got)

			errs = append(errs, err)
		})
	}

	wg.Wait()

	if err := errors.Join(errs...); err != nil {
		t.Fatalf("concurrent FileFollowUps: %v", err)
	}

	if tri := f.triageItems(t, f.factory); total != 12 || len(tri) != 1 {
		t.Errorf("concurrent sweeps took %d and opened %d triage items; want 12 and 1", total, len(tri))
	}

	if n := len(f.items(t, f.factory)); n != 6+6+1 {
		t.Errorf("factory has %d items; want 6 sources, 6 follow-up items and 1 triage item", n)
	}
}

// The triage follow-ups of every item that lands join one triage item until a run claims it:
// filed by the engine with origin follow-up, standard class, its first source's type, starting
// 24 hours after it is filed, a priority one below its sources' lowest, titled by its sources and
// its brief rewritten as follow-ups join. A claimed one collects no more: the next opens another.
func testTriageCollectsUntilClaimed(f *fixture) {
	t := f.t
	lines := []domain.Line{lineOf(f.factory, nameDev)}

	a := f.create(t, domain.NewItem{Title: "a", Priority: 2, Type: "story"})
	f.keepNotes(t, a, triageFollowUp("From a"))
	f.land(t, a, domain.StateDone, tLanded)
	f.sweep(t, lines, 10)

	b := f.create(t, domain.NewItem{Title: "b", Priority: 1})
	f.keepNotes(t, b, triageFollowUp("From b"), triageFollowUp("Also from b"))
	f.land(t, b, domain.StateDone, tLanded)
	f.sweep(t, lines, 10)

	tri := f.triageItems(t, f.factory)
	if len(tri) != 1 {
		t.Fatalf("triage items = %+v; want one", tri)
	}

	it := tri[0]
	want := fmt.Sprintf("Follow-ups from %s#%d and 1 more", nameDev, a.Number)

	if it.Title != want || it.Origin != domain.OriginFollowUp || it.CreatedBy != "" || it.Class != domain.ClassStandard ||
		it.Type != "story" || it.Priority != 0 || it.Generation != 1 || !it.StartAt.Equal(tSweep.Add(24*time.Hour)) || it.Station != stWork {
		t.Errorf("triage item = %+v; want %q, a standard story by the engine, priority 0, generation 1, starting in 24h", it, want)
	}

	expectTriageBrief(t, it.Body)

	f.startRun(t, it.ID, tSweep)

	c := f.create(t, domain.NewItem{Title: "c"})
	f.keepNotes(t, c, triageFollowUp("From c"))
	f.land(t, c, domain.StateDone, tLanded)
	f.sweep(t, lines, 10)

	if tri := f.triageItems(t, f.factory); len(tri) != 2 || tri[0].Title != fmt.Sprintf("Follow-ups from %s#%d", nameDev, c.Number) {
		t.Errorf("triage items after a claim = %+v; want a second one for c", tri)
	}
}

// expectTriageBrief checks the brief of testTriageCollectsUntilClaimed's triage item: every
// follow-up quoted, a's before b's, and the closing.
func expectTriageBrief(t *testing.T, body string) {
	t.Helper()

	for _, s := range []string{"> From a", "> From b", "> Also from b", domain.TriageClosing} {
		if !strings.Contains(body, s) {
			t.Errorf("triage brief lacks %q:\n%s", s, body)
		}
	}

	if i, j := strings.Index(body, "> From a"), strings.Index(body, "> From b"); i > j {
		t.Errorf("triage brief not in source order:\n%s", body)
	}
}

// No triage item is filed when nothing is left for it.
func testTriageNoneWhenNothingLeft(f *fixture) {
	t := f.t

	src := f.create(t, domain.NewItem{Title: titleSrc})
	f.land(t, src, domain.StateDone, tLanded)

	if got := f.sweep(t, []domain.Line{lineOf(f.factory, nameDev)}, 10); len(got) != 0 {
		t.Errorf("FileFollowUps = %+v; want none", got)
	}

	if tri := f.triageItems(t, f.factory); len(tri) != 0 {
		t.Errorf("triage items = %+v; want none", tri)
	}
}

// A person drops a waiting follow-up and files a dropped one; anything else conflicts, and a
// follow-up of another item, or none, is not found.
func testFileAndDropByAPerson(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	ada := adaActor

	src := f.create(t, domain.NewItem{Title: titleSrc})
	other := f.create(t, domain.NewItem{Title: "another"})
	f.keepNotes(t, src, triageFollowUp("drop me"), triageFollowUp("to triage"))

	kept, err := f.Store.FollowUps(ctx, src.ID)
	if err != nil || len(kept) != 2 {
		t.Fatalf("FollowUps = %+v, %v", kept, err)
	}

	drop := kept[0]

	n := personsItem(f)

	_, err = f.Store.DropFollowUp(ctx, other.ID, drop.ID, ada, t1)
	expectErr(t, "DropFollowUp of another item", err, ports.ErrNotFound)

	_, _, err = f.Store.FileFollowUp(ctx, src.ID, drop.ID, n)
	expectErr(t, "FileFollowUp of a waiting one", err, ports.ErrConflict)

	dropped, err := f.Store.DropFollowUp(ctx, src.ID, drop.ID, ada, t1)
	if err != nil || dropped.State != domain.FollowUpDropped || dropped.Reason != domain.DropReason(ada) {
		t.Fatalf("DropFollowUp = %+v, %v; want dropped by grace", dropped, err)
	}

	_, err = f.Store.DropFollowUp(ctx, src.ID, drop.ID, ada, t1)
	expectErr(t, "DropFollowUp twice", err, ports.ErrConflict)

	filed, it, err := f.Store.FileFollowUp(ctx, src.ID, drop.ID, n)
	if err != nil || filed.State != domain.FollowUpFiled || filed.FiledItem != it.ID || it.CreatedBy != ada.Ref() || it.Origin != domain.OriginFollowUp {
		t.Fatalf("FileFollowUp = %+v, %+v, %v; want filed as Grace's follow-up item", filed, it, err)
	}

	_, _, err = f.Store.FileFollowUp(ctx, src.ID, drop.ID, n)
	expectErr(t, "FileFollowUp twice", err, ports.ErrConflict)

	_, _, err = f.Store.FileFollowUp(ctx, src.ID, missingFollowUp, n)
	expectErr(t, "FileFollowUp of none", err, ports.ErrNotFound)
}

// expectErr reports what when err is not want.
func expectErr(t *testing.T, what string, err, want error) {
	t.Helper()

	if !errors.Is(err, want) {
		t.Errorf("%s: err %v; want %v", what, err, want)
	}
}

// A follow-up in a triage item may be filed by a person only once the triage item is done or
// cancelled.
func testFileInAClosedTriageItem(f *fixture) {
	t := f.t
	ctx := f.ctx(t)

	src := f.create(t, domain.NewItem{Title: titleSrc})
	f.keepNotes(t, src, triageFollowUp("to triage"))
	f.land(t, src, domain.StateDone, tLanded)

	got := f.sweep(t, []domain.Line{lineOf(f.factory, nameDev)}, 10)
	if len(got) != 1 {
		t.Fatalf("FileFollowUps = %+v; want one in triage", got)
	}

	n := personsItem(f)

	if _, _, err := f.Store.FileFollowUp(ctx, src.ID, got[0].ID, n); !errors.Is(err, ports.ErrConflict) {
		t.Errorf("FileFollowUp in an open triage item: err %v; want ErrConflict", err)
	}

	triage := f.triageItems(t, f.factory)[0]
	f.land(t, triage, domain.StateCancelled, tSweep)

	if filed, _, err := f.Store.FileFollowUp(ctx, src.ID, got[0].ID, n); err != nil || filed.State != domain.FollowUpFiled || filed.FiledItem == triage.ID {
		t.Errorf("FileFollowUp in a closed triage item = %+v, %v; want filed as a new item", filed, err)
	}
}

// personsItem is the item Grace files for a follow-up.
func personsItem(f *fixture) domain.NewItem {
	return domain.NewItem{
		FactoryID: f.factory, Title: "Filed by Grace", Type: typeTask, Station: stWork, State: domain.StateQueued,
		Class: domain.ClassStandard, Origin: domain.OriginFollowUp, CreatedBy: adaActor, Generation: 1, CreatedAt: t1,
	}
}

// missingFollowUp names no follow-up.
const missingFollowUp domain.FollowUpID = "00000000-0000-7000-8000-fffffffffffd"

// titleSrc is the tests' source items' title.
const titleSrc = "src"

// adaActor is the person who drops and files follow-ups.
var adaActor = domain.Actor{Kind: domain.ActorUser, ID: "grace", Name: "Grace"}

// FiledFrom reads the follow-ups an item was filed from: a part's names the part and its
// assembly, whose landing filed it; one filed into another factory names its source there. An
// item filed by no follow-up has none, and another organization reads none.
func testFiledFromNamesSourcesAndLanding(f *fixture) {
	t := f.t

	ops := f.newFactory(t)
	lines := []domain.Line{lineOf(f.factory, nameDev), lineOf(ops, nameOps, f.factory)}

	root := f.create(t, domain.NewItem{Title: "root"})
	part := f.create(t, domain.NewItem{Title: "part", ParentID: root.ID})
	src := f.create(t, domain.NewItem{Title: titleSrc})

	f.keepNotes(t, part, startFollowUp("Drop the column", domain.FollowUpPlan{Title: "Drop column"}))
	f.keepNotes(t, src, startFollowUp("into ops", domain.FollowUpPlan{Title: "Ops work", Factory: nameOps}))
	f.land(t, part, domain.StateDone, tLanded.Add(-time.Hour))
	f.land(t, root, domain.StateDone, tLanded)
	f.land(t, src, domain.StateDone, tLanded)

	if got := f.sweep(t, lines, 10); len(got) != 2 {
		t.Fatalf("FileFollowUps = %+v; want two filed", got)
	}

	fromPart, fromSrc := f.filedAs(t, part), f.filedAs(t, src)

	if got := f.filedFrom(t, f.ctx(t), fromPart.FiledItem); len(got) != 1 || got[0] != (domain.FiledFrom{
		FollowUp: fromPart.ID, Source: part.ID, SourceFactory: f.factory, SourceNumber: part.Number, Station: stWork, Agent: agWorker,
		Landed: root.ID, LandedFactory: f.factory, LandedNumber: root.Number,
	}) {
		t.Errorf("FiledFrom of the part's follow-up item = %+v; want the part, landed with its assembly %s", got, root.ID)
	}

	if it := f.item(t, fromSrc.FiledItem); it.FactoryID != ops {
		t.Fatalf("into ops filed in %s; want ops", it.FactoryID)
	}

	if got := f.filedFrom(t, f.ctx(t), fromSrc.FiledItem); len(got) != 1 || got[0] != (domain.FiledFrom{
		FollowUp: fromSrc.ID, Source: src.ID, SourceFactory: f.factory, SourceNumber: src.Number, Station: stWork, Agent: agWorker,
		Landed: src.ID, LandedFactory: f.factory, LandedNumber: src.Number,
	}) {
		t.Errorf("FiledFrom of the item filed into ops = %+v; want its source in dev, landed itself", got)
	}

	for what, id := range map[string]domain.ItemID{"a person's item": src.ID, "an unknown item": "not-an-item"} {
		if got := f.filedFrom(t, f.ctx(t), id); len(got) != 0 {
			t.Errorf("FiledFrom of %s = %+v; want none", what, got)
		}
	}

	if got := f.filedFrom(t, f.otherOrg(t).ctx(t), fromPart.FiledItem); len(got) != 0 {
		t.Errorf("another organization's FiledFrom = %+v; want none", got)
	}
}

// filedAs is src's one follow-up, filed.
func (f *fixture) filedAs(t *testing.T, src domain.Item) domain.FollowUp {
	t.Helper()

	kept, err := f.Store.FollowUps(f.ctx(t), src.ID)
	if err != nil || len(kept) != 1 || kept[0].State != domain.FollowUpFiled {
		t.Fatalf("FollowUps of %s = %+v, %v; want one filed", src.Title, kept, err)
	}

	return kept[0]
}

// filedFrom reads FiledFrom of id in ctx.
func (f *fixture) filedFrom(t *testing.T, ctx context.Context, id domain.ItemID) []domain.FiledFrom {
	t.Helper()

	got, err := f.Store.FiledFrom(ctx, id)
	if err != nil {
		t.Fatalf("FiledFrom: %v", err)
	}

	return got
}
