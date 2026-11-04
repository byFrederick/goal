package storetest

import (
	"reflect"
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
