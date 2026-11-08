package storetest

import (
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// note is a person's note on it at t.
func note(it domain.Item, text string, at time.Time) domain.Event {
	return domain.Event{
		FactoryID: it.FactoryID, ItemID: it.ID, At: at, Kind: domain.EventNote, Station: it.Station,
		Actor: domain.LocalUser.Name, Data: map[string]any{"text": text, "actor": domain.LocalUser.Ref()},
	}
}

func (f *fixture) notes(t *testing.T, id domain.ItemID) []domain.Event {
	t.Helper()

	notes, err := f.Store.NotesSinceLastRun(f.ctx(t), id)
	if err != nil {
		t.Fatal(err)
	}

	return notes
}

func (f *fixture) appendNote(t *testing.T, e domain.Event) domain.Event {
	t.Helper()

	got, err := f.Store.AppendNote(f.ctx(t), e, domain.LocalUser)
	if err != nil {
		t.Fatalf("AppendNote: %v", err)
	}

	return got
}

// agentStarted appends run's agent_started event, as goald records the worker's started line.
func (f *fixture) agentStarted(t *testing.T, item domain.ItemID, run domain.RunID, at time.Time) {
	t.Helper()

	if err := f.Store.AppendEvents(f.ctx(t), domain.Event{
		FactoryID: f.factory, ItemID: item, RunID: run, At: at, Kind: domain.EventAgentStarted, Actor: agWorker,
	}); err != nil {
		t.Fatalf("append agent_started: %v", err)
	}
}

// Notes are appended to open items only, with their id, and read back from the item's latest run
// whose agent started on: what the next run's brief carries.
func testNotes(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{})
	first := note(it, "Also check the pool.", t0.Add(time.Minute))

	got := f.appendNote(t, first)
	if events := f.Events(t, it.ID); got.ID == 0 || events[len(events)-1].ID != got.ID {
		t.Fatalf("AppendNote = %+v; want the item's latest event with its id (events %+v)", got, events)
	}

	sameEvents(t, []domain.Event{got}, []domain.Event{first})
	sameEvents(t, f.notes(t, it.ID), []domain.Event{first})

	// A run starts and fails before its agent starts: its agent read no brief, so the note stays.
	f.claim(t, work, 0, t1)
	f.startRun(t, it.ID, t1)
	sameEvents(t, f.notes(t, it.ID), []domain.Event{first})

	// The next run's agent starts: the notes before that run were its brief's.
	run := f.startRun(t, it.ID, t1)
	f.agentStarted(t, it.ID, run, t1)

	if notes := f.notes(t, it.ID); len(notes) != 0 {
		t.Errorf("notes after a run's agent started = %+v; want none", notes)
	}

	second := note(it, "Split it.", t1.Add(time.Minute))
	f.appendNote(t, second)

	if err := f.Store.AppendEvents(f.ctx(t), domain.Event{
		FactoryID: f.factory, ItemID: it.ID, At: t1.Add(2 * time.Minute), Kind: domain.EventGateApproved, Actor: agWorker,
	}); err != nil {
		t.Fatal(err)
	}

	sameEvents(t, f.notes(t, it.ID), []domain.Event{second})

	if notes, err := f.Store.NotesSinceLastRun(f.ctx(t), missingItem); err != nil || len(notes) != 0 {
		t.Errorf("NotesSinceLastRun of an unknown item = %+v, %v; want none", notes, err)
	}

	_, err := f.Store.AppendNote(f.ctx(t), note(domain.Item{ID: missingItem, FactoryID: f.factory}, "x", t1), domain.LocalUser)
	expect(t, "AppendNote to an unknown item", err, ports.ErrNotFound)
}

func testNotesOnClosedItems(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{})
	f.finish(t, it, t1, false)
	before := len(f.Events(t, it.ID))

	_, err := f.Store.AppendNote(f.ctx(t), note(it, "late", t1.Add(time.Minute)), domain.LocalUser)
	expect(t, "AppendNote to a done item", err, ports.ErrConflict)

	if after := len(f.Events(t, it.ID)); after != before {
		t.Errorf("%d events after a refused note, want %d", after, before)
	}
}
