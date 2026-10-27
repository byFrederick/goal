package engine_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/engine"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

const answersHeading = "## Answers from people"

// asking is an item whose run at work is working: the agent is about to ask.
func (h *harness) asking(t *testing.T) (domain.Item, domain.RunID) {
	t.Helper()

	it := h.at(t, h.submit(t, domain.NewItem{}), stWork, domain.StateQueued)

	if err := h.e.Tick(inOrg(t)); err != nil {
		t.Fatal(err)
	}

	if err := h.clock.WaitTickers(bounded(t), 1); err != nil { // the run heartbeats
		t.Fatal(err)
	}

	runs := h.runs(t, testOrg, it.ID)
	if len(runs) != 1 {
		t.Fatalf("runs %+v; want the one run working", runs)
	}

	return h.item(t, it.ID), runs[0].ID
}

// ask has the item's running agent ask a person, the way the gateway passes its question on.
func (h *harness) ask(t *testing.T, e *engine.Engine, it domain.Item, run domain.RunID, choices ...string) domain.Item {
	t.Helper()

	got, err := e.Ask(inOrg(t), it.ID, run, agWorker, "Organization or factory spend?", choices)
	if err != nil {
		t.Fatal(err)
	}

	return got
}

// Asking ends the run under the lease outcome asked, whichever instance the gateway reached, and
// leaves the item waiting for a person at its station with the question open.
func TestAsk_RecordsTheQuestionAndEndsTheRun(t *testing.T) {
	t.Parallel()

	for _, other := range []bool{false, true} {
		name := "by the instance running it"
		if other {
			name = "told through Interrupt"
		}

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, controlLine(), porttest.Block(nil, `{}`))
			it, run := h.asking(t)

			asker := h.e
			if other {
				asker = h.engine(t, h.store)
			}

			got := h.ask(t, asker, it, run, "Organization", "Factory")

			if other {
				h.e.Interrupt(ports.ItemNotice{
					OrgID: testOrg, FactoryID: factoryID, ItemID: it.ID, Kind: domain.EventLeaseTaken, Station: stWork,
					At: h.clock.Now(),
				})
			}

			h.wait(t, h.e) // returns only once the run was cancelled

			want := domain.Position{Station: stWork, State: domain.StateWaitingHuman}
			if got.Position() != want || h.item(t, it.ID).Position() != want {
				t.Errorf("item at %+v; want %+v", got.Position(), want)
			}

			if _, held := h.store.Lease(it.ID); held {
				t.Error("the item still holds a lease")
			}

			h.expectOutcomes(t, it.ID, domain.LeaseAsked)

			if runs := h.runs(t, testOrg, it.ID); len(runs) != 1 || runs[0].Result.Status != domain.RunCancelled {
				t.Errorf("runs %+v; want the one run cancelled", runs)
			}

			h.expectAsked(t, it.ID, run)
		})
	}
}

// expectAsked checks the item's one question_asked event, left open, and the events around it.
func (h *harness) expectAsked(t *testing.T, id domain.ItemID, run domain.RunID) {
	t.Helper()

	asked := h.eventsOf(id, domain.EventQuestionAsked)
	if len(asked) != 1 {
		t.Fatalf("events %v; want one question_asked", h.kinds(id))
	}

	q := domain.QuestionOf(asked[0])
	if q.Text != "Organization or factory spend?" || strings.Join(q.Choices, ",") != "Organization,Factory" ||
		q.Agent != agWorker || q.RunID != run || q.Station != stWork || asked[0].RunID != run {
		t.Errorf("question %+v (event %+v); want the agent's question with its run", q, asked[0])
	}

	if open, ok := domain.OpenQuestion(h.store.Events(id)); !ok || open.EventID != asked[0].ID {
		t.Errorf("open question %+v, %v; want the one asked", open, ok)
	}

	// One lease_taken and waiting_human; the ended run moves the item no further.
	for k, want := range map[domain.EventKind]int{
		domain.EventLeaseTaken: 1, domain.EventWaitingHuman: 1,
		domain.EventReleased: 0, domain.EventAgentCancelled: 0, domain.EventBlocked: 0,
	} {
		if n := len(h.eventsOf(id, k)); n != want {
			t.Errorf("events %v; want %d %s", h.kinds(id), want, k)
		}
	}
}

func TestAsk_Refusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t, controlLine(), porttest.Block(nil, `{}`))
	it, run := h.asking(t)

	for _, tt := range []struct {
		name     string
		question string
		choices  []string
	}{
		{name: "blank question", question: "  "},
		{name: "question too long", question: strings.Repeat("q", engine.MaxQuestionRunes+1)},
		{name: "too many choices", question: "Which?", choices: []string{"a", "b", "c", "d", "e"}},
		{name: "blank choice", question: "Which?", choices: []string{"a", " "}},
		{name: "choice too long", question: "Which?", choices: []string{strings.Repeat("c", engine.MaxChoiceRunes+1)}},
	} {
		if _, err := h.e.Ask(inOrg(t), it.ID, run, agWorker, tt.question, tt.choices); !errors.Is(err, engine.ErrBadQuestion) {
			t.Errorf("%s: Ask = %v; want ErrBadQuestion", tt.name, err)
		}
	}

	if n := len(h.eventsOf(it.ID, domain.EventQuestionAsked)); n != 0 {
		t.Fatalf("events %v after refused questions; want none asked", h.kinds(it.ID))
	}

	longest := strings.Repeat("q", engine.MaxQuestionRunes)
	if _, err := h.e.Ask(inOrg(t), it.ID, run, agWorker, longest, []string{"a", "b", "c", strings.Repeat("c", engine.MaxChoiceRunes)}); err != nil {
		t.Fatalf("Ask at the limits = %v; want it recorded", err)
	}

	h.wait(t, h.e)

	before := len(h.store.Events(it.ID))
	if _, err := h.e.Ask(inOrg(t), it.ID, run, agWorker, "Again?", nil); !errors.Is(err, engine.ErrQuestionOpen) {
		t.Errorf("a second Ask = %v; want ErrQuestionOpen", err)
	}

	if after := len(h.store.Events(it.ID)); after != before {
		t.Errorf("events %v; the refused second question must record nothing", h.kinds(it.ID))
	}

	queued := h.submit(t, domain.NewItem{})
	if _, err := h.e.Ask(inOrg(t), queued.ID, run, agWorker, "Which?", nil); !errors.Is(err, engine.ErrNotInProgress) {
		t.Errorf("Ask on a queued item = %v; want ErrNotInProgress", err)
	}
}

// An answer puts the item back in its station's queue without counting a rework, and records the
// picked choice's text as the answer.
func TestAnswer_ResumesTheItemWithoutRework(t *testing.T) {
	t.Parallel()

	h := newHarness(t, controlLine(), porttest.Block(nil, `{}`))
	it, run := h.asking(t)
	h.ask(t, h.e, it, run, "Organization", "Factory")
	h.wait(t, h.e)

	second := 1

	got, err := h.e.Answer(inOrg(t), it.ID, human, "", &second)
	if err != nil {
		t.Fatal(err)
	}

	if want := (domain.Position{Station: stWork, State: domain.StateQueued}); got.Position() != want || got.ReworkCount != 0 {
		t.Errorf("item at %+v rework %d; want %+v without rework", got.Position(), got.ReworkCount, want)
	}

	answered := h.eventsOf(it.ID, domain.EventQuestionAnswered)
	if len(answered) != 1 {
		t.Fatalf("events %v; want one question_answered", h.kinds(it.ID))
	}

	a := answered[0]
	if a.Data[domain.KeyAnswer] != "Factory" || a.Data[domain.KeyChoiceIndex] != 1 || a.Actor != human.Name ||
		a.Data["actor"] != human.Ref() {
		t.Errorf("answer event %+v; want the picked choice's text and index by the person", a)
	}

	if _, ok := domain.OpenQuestion(h.store.Events(it.ID)); ok {
		t.Error("the question is still open after its answer")
	}

	checkAnswerAudit(t, h, it.ID, "map[choice_index:1 length:7]")
}

// checkAnswerAudit checks that the item has one item.answered entry by the person, with data
// (printed) holding the answer's length and choice index, never its text.
func checkAnswerAudit(t *testing.T, h *harness, id domain.ItemID, data string) {
	t.Helper()

	audited := h.audited(t, domain.AuditItemAnswered)
	if len(audited) != 1 || audited[0].TargetID != string(id) || audited[0].Actor.Ref() != human.Ref() ||
		fmt.Sprint(audited[0].Data) != data {
		t.Errorf("audit %+v; want one item.answered with %s, never the answer's text", audited, data)
	}
}

func TestAnswer_Refusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t, controlLine(), porttest.Block(nil, `{}`))
	it, run := h.asking(t)
	h.ask(t, h.e, it, run, "Organization", "Factory")
	h.wait(t, h.e)

	if _, err := h.e.Answer(inOrg(t), h.submit(t, domain.NewItem{}).ID, human, "yes", nil); !errors.Is(err, engine.ErrNoOpenQuestion) {
		t.Errorf("Answer with no question = %v; want ErrNoOpenQuestion", err)
	}

	outside, negative := 2, -1

	for _, tt := range []struct {
		name   string
		answer string
		choice *int
	}{
		{name: "choice past the choices", choice: &outside},
		{name: "negative choice", choice: &negative},
		{name: "blank answer", answer: " \n"},
		{name: "answer too long", answer: strings.Repeat("a", engine.MaxAnswerRunes+1)},
	} {
		if _, err := h.e.Answer(inOrg(t), it.ID, human, tt.answer, tt.choice); !errors.Is(err, engine.ErrBadAnswer) {
			t.Errorf("%s: Answer = %v; want ErrBadAnswer", tt.name, err)
		}
	}

	if got := h.item(t, it.ID); got.State != domain.StateWaitingHuman || len(h.eventsOf(it.ID, domain.EventQuestionAnswered)) != 0 {
		t.Errorf("item %+v events %v; refused answers change nothing", got.Position(), h.kinds(it.ID))
	}

	done := h.at(t, h.submit(t, domain.NewItem{}), stDone, domain.StateDone)
	if _, err := h.e.Answer(inOrg(t), done.ID, human, "yes", nil); !errors.Is(err, engine.ErrItemClosed) {
		t.Errorf("Answer on a done item = %v; want ErrItemClosed", err)
	}

	if _, err := h.e.Cancel(inOrg(t), it.ID, human, ""); err != nil {
		t.Fatal(err)
	}

	if _, err := h.e.Answer(inOrg(t), it.ID, human, "yes", nil); !errors.Is(err, engine.ErrItemClosed) {
		t.Errorf("Answer on a cancelled item = %v; want ErrItemClosed", err)
	}

	if a := h.audited(t, domain.AuditItemAnswered); len(a) != 0 {
		t.Errorf("audit %+v; refused answers audit nothing", a)
	}
}

// The item waits for an answer, not an approval.
func TestApprove_RefusedWhileAQuestionIsOpen(t *testing.T) {
	t.Parallel()

	h := newHarness(t, controlLine(), porttest.Block(nil, `{}`))
	it, run := h.asking(t)
	h.ask(t, h.e, it, run)
	h.wait(t, h.e)

	if _, err := h.e.Approve(inOrg(t), it.ID, human); !errors.Is(err, engine.ErrQuestionOpen) {
		t.Errorf("Approve = %v; want ErrQuestionOpen", err)
	}

	if got := h.item(t, it.ID); got.Position() != (domain.Position{Station: stWork, State: domain.StateWaitingHuman}) {
		t.Errorf("item at %+v; want it still waiting for the answer", got.Position())
	}
}

// Move, Pause, Reject and Cancel withdraw the open question: nothing is left to answer.
func TestQuestion_WithdrawnByAPersonsControl(t *testing.T) {
	t.Parallel()

	for name, act := range map[string]control{
		"move": func(e *engine.Engine, ctx context.Context, id domain.ItemID) (domain.Item, error) {
			return e.Move(ctx, id, stApprove, human, "")
		},
		"pause": func(e *engine.Engine, ctx context.Context, id domain.ItemID) (domain.Item, error) {
			return e.Pause(ctx, id, human, "")
		},
		"send back": func(e *engine.Engine, ctx context.Context, id domain.ItemID) (domain.Item, error) {
			return e.Reject(ctx, id, human, "wrong question")
		},
		"cancel": func(e *engine.Engine, ctx context.Context, id domain.ItemID) (domain.Item, error) {
			return e.Cancel(ctx, id, human, "")
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, controlLine(), porttest.Block(nil, `{}`))
			it, run := h.asking(t)
			h.ask(t, h.e, it, run)
			h.wait(t, h.e)

			if _, err := act(h.e, inOrg(t), it.ID); err != nil {
				t.Fatal(err)
			}

			if _, ok := domain.OpenQuestion(h.store.Events(it.ID)); ok {
				t.Errorf("the question is still open after %s", name)
			}

			// A cancelled item is closed, which Answer checks first.
			want := engine.ErrNoOpenQuestion
			if name == "cancel" {
				want = engine.ErrItemClosed
			}

			if _, err := h.e.Answer(inOrg(t), it.ID, human, "too late", nil); !errors.Is(err, want) {
				t.Errorf("Answer after %s = %v; want %v", name, err, want)
			}

			if n := len(h.eventsOf(it.ID, domain.EventQuestionAnswered)); n != 0 {
				t.Errorf("events %v; a withdrawn question records no answer", h.kinds(it.ID))
			}
		})
	}
}

// The run after the answer reads the question and its answer after its brief, next to the
// notes, and the run after that does not.
func TestAnswer_TheNextRunCarriesTheAnswer(t *testing.T) {
	t.Parallel()

	var h *harness

	// The item's first run asks through the engine, as the gateway passes the question on, and
	// ends when that takes its lease; the runs after it succeed.
	h = newHarness(t, controlLine(), func(ctx context.Context, run porttest.ExecCall, _ io.Writer) domain.RunResult {
		ctx = domain.WithOrg(ctx, testOrg)

		runs, err := h.store.Runs(ctx, run.Sandbox.ItemID, 10)
		if err != nil || len(runs) != 1 {
			return ok(`{}`)
		}

		if _, err := h.e.Ask(ctx, run.Sandbox.ItemID, runs[0].ID, agWorker, "Organization or factory spend?",
			[]string{"Organization", "Factory"}); err != nil {
			t.Error(err)
		}

		<-ctx.Done()

		return domain.RunResult{Status: domain.RunCancelled, Error: ctx.Err().Error()}
	})
	it := h.at(t, h.submit(t, domain.NewItem{}), stWork, domain.StateQueued)

	h.step(t) // the run that asks

	if got := h.item(t, it.ID); got.State != domain.StateWaitingHuman {
		t.Fatalf("item at %+v (events %v); want it waiting for the answer", got.Position(), h.kinds(it.ID))
	}

	if _, err := h.e.Note(inOrg(t), it.ID, human, "Keep it small."); err != nil {
		t.Fatal(err)
	}

	if _, err := h.e.Answer(inOrg(t), it.ID, human, "  Factory spend,\nper week.  ", nil); err != nil {
		t.Fatal(err)
	}

	h.step(t) // the run after the answer, at work

	next := h.feedback(1)
	for _, s := range []string{
		notesHeading, answersHeading,
		"- Question from " + agWorker + ": Organization or factory spend?\n  Answer from Local user: Factory spend,\n  per week.",
	} {
		if strings.Count(next, s) != 1 {
			t.Errorf("feedback %q; want %q once", next, s)
		}
	}

	if strings.Index(next, notesHeading) > strings.Index(next, answersHeading) {
		t.Errorf("feedback %q; want the answers after the notes", next)
	}

	if _, err := h.e.Reject(inOrg(t), it.ID, human, "again"); err != nil {
		t.Fatal(err)
	}

	h.step(t)

	if n := len(h.worker.Runs()); n != 3 {
		t.Fatalf("%d runs; want the one after the rejection too (events %v)", n, h.kinds(it.ID))
	}

	if third := h.feedback(2); strings.Contains(third, answersHeading) {
		t.Errorf("third feedback %q; want no answers: they reached the run before", third)
	}
}
