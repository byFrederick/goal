package engine_test

import (
	"context"
	"io"
	"slices"
	"sync"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// fieldFollowUps is the output field the follow-ups tests' lines map.
const fieldFollowUps = "followups"

// withFollowUps is l with fieldFollowUps mapped to follow-ups.
func withFollowUps(l domain.Line) domain.Line {
	l.Flow.Outputs.FollowUps = fieldFollowUps

	return l
}

// followUps reads an item's kept follow-ups.
func (h *harness) followUps(t *testing.T, id domain.ItemID) []domain.FollowUp {
	t.Helper()

	got, err := h.store.FollowUps(inOrg(t), id)
	if err != nil {
		t.Fatal(err)
	}

	return got
}

// texts are the follow-ups' texts, each with the station and agent that wrote it.
func texts(fs []domain.FollowUp) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Station + "/" + f.Agent + ": " + f.Text
	}

	return out
}

// byCall plays each agent's outputs in turn, its last one again after that.
type byCall struct {
	mu   sync.Mutex
	out  map[string][]string
	runs map[string]int
}

func (b *byCall) agent(_ context.Context, c porttest.ExecCall, _ io.Writer) domain.RunResult {
	b.mu.Lock()
	defer b.mu.Unlock()

	outs := b.out[c.Sandbox.Agent]
	n := min(b.runs[c.Sandbox.Agent], len(outs)-1)
	b.runs[c.Sandbox.Agent]++

	return ok(outs[n])
}

// finish keeps the follow-ups of every accepted output, with its run, station and agent: when the
// item advances, when its plan makes it an assembly, and from every run of a rerun, each text once.
// An output that is not accepted, or a line that maps no field, keeps none.
func TestEngine_FinishKeepsFollowUps(t *testing.T) {
	t.Parallel()

	t.Run("advance", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t, withFollowUps(lineOf(agentStation(stBuild, agWorker, 1, 1))),
			byAgent(map[string]string{agWorker: `{"followups":["Add an index",{"text":"Tell the API","for":"acme#3"}]}`}))
		it := h.submit(t, domain.NewItem{})

		h.step(t)

		got := h.followUps(t, it.ID)
		if want := []string{"build/worker: Add an index", "build/worker: Tell the API"}; !slices.Equal(texts(got), want) {
			t.Fatalf("follow-ups = %v, want %v", texts(got), want)
		}

		if got[0].RunID == "" || got[0].RunID != got[1].RunID || got[1].Target != domain.FollowUpNote {
			t.Errorf("follow-ups = %+v, want both of the build run, the second a note", got)
		}
	})

	t.Run("plan", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t, withFollowUps(planLine(true)), byAgent(map[string]string{
			agArchitect: `{"plan":[{"key":"a","title":"A","brief":"a"}],"followups":["Split the store later"]}`, agWorker: `{}`,
		}))
		it := h.submit(t, domain.NewItem{})

		h.step(t)

		if got := h.item(t, it.ID); got.State != domain.StateAssembly {
			t.Fatalf("item is %s, want an assembly", got.State)
		}

		if got, want := texts(h.followUps(t, it.ID)), []string{"spec/architect: Split the store later"}; !slices.Equal(got, want) {
			t.Errorf("follow-ups = %v, want %v", got, want)
		}
	})

	t.Run("rerun", func(t *testing.T) {
		t.Parallel()

		b := &byCall{runs: map[string]int{}, out: map[string][]string{
			agWorker: {`{"followups":["Add an index","Check the docs"]}`, `{"followups":["add  an INDEX","Name the metric"]}`},
			agReviewer: {
				`{"decision":"reject","reason":"no test","followups":["Review the lint rule"]}`,
				`{"decision":"approve","followups":["review the lint rule"]}`,
			},
		}}
		h := newHarness(t, withFollowUps(reviewLine(2)), b.agent)
		it := h.submit(t, domain.NewItem{})

		for range 4 {
			h.step(t)
		}

		if got := h.item(t, it.ID); got.State != domain.StateDone {
			t.Fatalf("item is %s, want done after a rework", got.State)
		}

		want := []string{
			"build/worker: Add an index", "build/worker: Check the docs", "review/reviewer: Review the lint rule",
			"build/worker: Name the metric",
		}
		if got := texts(h.followUps(t, it.ID)); !slices.Equal(got, want) {
			t.Errorf("follow-ups = %v, want %v", got, want)
		}
	})

	t.Run("not accepted or not mapped", func(t *testing.T) {
		t.Parallel()

		failed := newHarness(t, withFollowUps(lineOf(agentStation(stBuild, agWorker, 1, 1))),
			func(context.Context, porttest.ExecCall, io.Writer) domain.RunResult {
				return domain.RunResult{Status: domain.RunFailed, Output: []byte(`{"followups":["Add an index"]}`), Error: "crashed"}
			})
		it := failed.submit(t, domain.NewItem{})

		failed.step(t)

		if got := failed.item(t, it.ID); got.State != domain.StateBlocked || len(failed.followUps(t, it.ID)) != 0 {
			t.Errorf("failed run: item %s with follow-ups %v, want blocked with none", got.State, texts(failed.followUps(t, it.ID)))
		}

		unmapped := newHarness(t, lineOf(agentStation(stBuild, agWorker, 1, 1)),
			byAgent(map[string]string{agWorker: `{"followups":["Add an index"]}`}))
		other := unmapped.submit(t, domain.NewItem{})

		unmapped.step(t)

		if got := unmapped.followUps(t, other.ID); len(got) != 0 {
			t.Errorf("follow-ups on a line that maps no field = %v, want none", texts(got))
		}
	})
}
