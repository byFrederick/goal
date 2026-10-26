package engine_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/engine"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

const notesHeading = "## Notes from people"

func TestNote_RecordsANoteEvent(t *testing.T) {
	t.Parallel()

	h := newHarness(t, humanLine(), porttest.Succeed(`{}`))
	it := h.submit(t, domain.NewItem{})

	ev, err := h.e.Note(inOrg(t), it.ID, human, "  Also check the connection pool.  ")
	if err != nil {
		t.Fatal(err)
	}

	notes := h.eventsOf(it.ID, domain.EventNote)
	if len(notes) != 1 || notes[0].ID != ev.ID || ev.ID == 0 {
		t.Fatalf("note events %+v, returned %+v; want the one note with its id", notes, ev)
	}

	got := notes[0]
	if got.Actor != human.Name || got.Data["actor"] != human.Ref() || got.Data["text"] != "Also check the connection pool." ||
		got.Station != it.Station || got.RunID != "" {
		t.Errorf("note event %+v, want the trimmed text by the local user at %s", got, it.Station)
	}
}

// feedback is what goald added to the brief of the item's i-th agent run (GOAL_FEEDBACK).
func (h *harness) feedback(i int) string { return h.worker.Runs()[i].Env["GOAL_FEEDBACK"] }

// A note reaches the next run, once, after its brief: the run after that has moved on.
func TestNote_TheNextRunCarriesItOnce(t *testing.T) {
	t.Parallel()

	h := newHarness(t, humanLine(), porttest.Succeed(`{}`))
	it := h.submit(t, domain.NewItem{Title: "Write the spec", Body: "For the lease sweep."})

	for _, text := range []string{"Also check the connection pool.", "Keep it under a page.\nNo tables."} {
		if _, err := h.e.Note(inOrg(t), it.ID, human, text); err != nil {
			t.Fatal(err)
		}
	}

	h.step(t) // spec runs, then waits at approve

	first := h.feedback(0)

	if want := notesHeading + "\n\n"; !strings.HasPrefix(first, want) {
		t.Fatalf("feedback %q, want the notes section", first)
	}

	for _, s := range []string{"Note from Local user: Also check the connection pool.", "Note from Local user: Keep it under a page.\n  No tables."} {
		if strings.Count(first, s) != 1 {
			t.Errorf("feedback %q, want %q once", first, s)
		}
	}

	if _, err := h.e.Reject(inOrg(t), it.ID, human, "split it in two"); err != nil {
		t.Fatal(err)
	}

	if _, err := h.e.Note(inOrg(t), it.ID, human, "Split by station."); err != nil {
		t.Fatal(err)
	}

	h.step(t)

	second := h.feedback(1)
	if strings.Contains(second, "connection pool") || strings.Count(second, "Note from Local user: Split by station.") != 1 {
		t.Errorf("second feedback %q, want only the note left since the first run", second)
	}

	if _, err := h.e.Reject(inOrg(t), it.ID, human, "again"); err != nil {
		t.Fatal(err)
	}

	h.step(t)

	if third := h.feedback(2); strings.Contains(third, notesHeading) {
		t.Errorf("third feedback %q, want no notes section: nobody left one", third)
	}
}

// A note left while a run works reaches its retry, with the notes the run already had.
func TestNote_ARetryKeepsTheNotesAndAddsNewOnes(t *testing.T) {
	t.Parallel()

	work := agentStation(stWork, agWorker, 1, 1)
	work.Output = contractResult

	var (
		h     *harness
		calls atomic.Int32
	)

	h = newHarness(t, lineOf(work), func(_ context.Context, run porttest.ExecCall, _ io.Writer) domain.RunResult {
		if calls.Add(1) == 1 {
			if _, err := h.e.Note(domain.WithOrg(context.Background(), testOrg), run.Sandbox.ItemID, human, "Use the staging data."); err != nil {
				t.Error(err)
			}

			return ok(`{}`) // breaks the contract: one more attempt
		}

		return ok(`{"answer":1}`)
	})
	h.contracts.Require(v1, contractResult, "answer")
	it := h.submit(t, domain.NewItem{Title: "count"})

	if _, err := h.e.Note(inOrg(t), it.ID, human, "Count twice."); err != nil {
		t.Fatal(err)
	}

	h.step(t)

	if n := len(h.worker.Runs()); n != 2 {
		t.Fatalf("%d agent runs, want 2 (events %v)", n, h.kinds(it.ID))
	}

	if first := h.feedback(0); !strings.Contains(first, "Count twice.") || strings.Contains(first, "staging") {
		t.Errorf("first feedback %q, want only the note left before it", first)
	}

	retry := h.feedback(1)
	for _, s := range []string{"Note from Local user: Count twice.", "Note from Local user: Use the staging data.", `missing property "answer"`} {
		if strings.Count(retry, s) != 1 {
			t.Errorf("retry feedback %q, want %q once", retry, s)
		}
	}
}

func TestNote_Refusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t, humanLine(), porttest.Succeed(`{}`))
	done := h.submit(t, domain.NewItem{})

	h.step(t) // spec, then approve

	if got, err := h.e.Approve(inOrg(t), done.ID, human); err != nil || got.State != domain.StateDone {
		t.Fatalf("approve = %+v, %v; want done", got, err)
	}

	open := h.submit(t, domain.NewItem{})

	tests := []struct {
		name string
		item domain.ItemID
		text string
		want error
	}{
		{name: "blank", item: open.ID, text: " \n ", want: engine.ErrBadNote},
		{name: "too long", item: open.ID, text: strings.Repeat("é", engine.MaxNoteRunes+1), want: engine.ErrBadNote},
		{name: "done item", item: done.ID, text: "late", want: engine.ErrItemClosed},
		{name: "unknown item", item: "00000000-0000-7000-8000-ffffffffffff", text: "hi", want: ports.ErrNotFound},
	}

	for _, tt := range tests {
		if _, err := h.e.Note(inOrg(t), tt.item, human, tt.text); !errors.Is(err, tt.want) {
			t.Errorf("%s: err = %v, want %v", tt.name, err, tt.want)
		}
	}

	if n := len(h.eventsOf(done.ID, domain.EventNote)) + len(h.eventsOf(open.ID, domain.EventNote)); n != 0 {
		t.Errorf("%d note events after refusals, want none", n)
	}

	if _, err := h.e.Note(inOrg(t), open.ID, human, strings.Repeat("é", engine.MaxNoteRunes)); err != nil {
		t.Errorf("a note of exactly %d characters: %v", engine.MaxNoteRunes, err)
	}
}
