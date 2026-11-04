package engine_test

import (
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// opsFactory is the second factory of the follow-up notes tests, which accepts follow-ups from
// the harness's.
const (
	opsFactory = domain.FactoryID("5c0d7a52-8b1e-4f43-9d3a-2e6b7c8f9a02")
	opsName    = "acme-ops"
)

// noteHarness is a harness whose factory's worker leaves a note for acme-ops#1, a paused factory
// that accepts follow-ups from it, and another for a factory that does not exist. It returns the
// harness, the source item and the target.
func noteHarness(t *testing.T) (*harness, domain.Item, domain.Item, domain.Line) {
	t.Helper()

	h := newHarness(t, withFollowUps(lineOf(agentStation(stWork, agWorker, 1, 1))), byAgent(map[string]string{
		agWorker: `{"followups":[{"text":"The ops runbook needs the new flag","for":"` + opsName + `#1"},` +
			`{"text":"Tell the docs","for":"nowhere#4"}]}`,
	}))

	ops := lineOf(agentStation(stWork, agWorker, 1, 1))
	ops.FactoryID, ops.Name, ops.Paused = opsFactory, opsName, true
	ops.Flow.FollowUpsFrom = []domain.FactoryID{factoryID}

	if err := h.e.SetLines([]domain.Line{h.line, ops}); err != nil {
		t.Fatal(err)
	}

	target, err := h.e.Submit(inOrg(t), domain.FileRequest{
		FactoryID: opsFactory, Origin: domain.OriginPerson, Type: typeFeature, Title: "Ship the runbook",
	})
	if err != nil {
		t.Fatal(err)
	}

	src := h.submit(t, domain.NewItem{Title: "Add the flag"})

	h.step(t) // the source runs and keeps its follow-ups
	h.step(t) // the next tick delivers them

	return h, src, target, ops
}

// Every tick delivers the waiting notes of each organization with loaded lines, a paused line's
// items too: a note the target factory accepts becomes an item_note by the engine on the item,
// and one for a factory that does not exist goes to triage with the reason.
func TestEngine_TickDeliversFollowUpNotes(t *testing.T) {
	t.Parallel()

	h, src, target, _ := noteHarness(t)

	notes := h.eventsOf(target.ID, domain.EventItemNote)
	if len(notes) != 1 {
		t.Fatalf("item_note events on the target = %+v; want one", notes)
	}

	expectNoteFromWorker(t, notes[0])

	got := h.followUps(t, src.ID)
	if len(got) != 2 || got[0].State != domain.FollowUpDelivered {
		t.Fatalf("follow-ups = %+v; want the first delivered", got)
	}

	if f := got[1]; f.Target != domain.FollowUpTriage || f.State != domain.FollowUpWaiting ||
		!strings.HasSuffix(f.Reason, domain.ReasonNoteNoFactory) || !strings.Contains(f.Reason, "nowhere#4") {
		t.Errorf("follow-up for an unknown factory = %+v; want it waiting for triage with the reason", f)
	}

	h.step(t)

	if again := h.eventsOf(target.ID, domain.EventItemNote); len(again) != 1 {
		t.Errorf("item_note events after another tick = %d; want the note delivered once", len(again))
	}
}

// expectNoteFromWorker checks an item_note is the worker's note from the source item, by the engine.
func expectNoteFromWorker(t *testing.T, n domain.Event) {
	t.Helper()

	if n.Actor != domain.ActorEngine || n.Data["text"] != "The ops runbook needs the new flag" ||
		n.Data[domain.KeyNoteFromItem] != testFactory+"#1" || n.Data[domain.KeyNoteAgent] != agWorker ||
		n.Data[domain.KeyNoteStation] != stWork {
		t.Errorf("item_note = %+v; want the worker's note from %s#1 by the engine", n, testFactory)
	}
}

// A tick with no lines of an organization delivers nothing there.
func TestEngine_TickDeliversNoNotesWithoutLines(t *testing.T) {
	t.Parallel()

	h := newHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)), porttest.Succeed(`{}`))
	if err := h.e.SetLines(nil); err != nil {
		t.Fatal(err)
	}

	h.step(t)
}
