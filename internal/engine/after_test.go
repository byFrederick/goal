package engine_test

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

// fieldFollows is the made-up output property a test line maps to flow.dependencies.after.
const fieldFollows = "follows"

// itemOutputs answers each run with the output set for its item, `{}` when none is.
type itemOutputs struct {
	mu  sync.Mutex
	out map[domain.ItemID]string
}

func (o *itemOutputs) set(id domain.ItemID, output string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.out[id] = output
}

func (o *itemOutputs) run(_ context.Context, c porttest.ExecCall, _ io.Writer) domain.RunResult {
	o.mu.Lock()
	defer o.mu.Unlock()

	if out, set := o.out[c.Sandbox.ItemID]; set {
		return ok(out)
	}

	return ok(`{}`)
}

// afterHarness runs spec (two architects) then build, materializing plans, with
// flow.dependencies.after set to field ("" leaves it unset).
func afterHarness(t *testing.T, field string) (*harness, *itemOutputs) {
	t.Helper()

	l := lineOf(agentStation(stSpec, agArchitect, 2, 2), agentStation(stBuild, agWorker, 1, 1))
	l.Flow.Assemblies.Materialize = true
	l.Flow.Dependencies.After = field
	out := &itemOutputs{out: map[domain.ItemID]string{}}

	return newHarness(t, l, out.run), out
}

// follows is an output whose mapped property lists refs, with the rest of the output's fields.
func follows(rest string, refs ...string) string {
	q := make([]string, len(refs))
	for i, r := range refs {
		q[i] = fmt.Sprintf("%q", r)
	}

	return fmt.Sprintf(`{%q:[%s]%s}`, fieldFollows, strings.Join(q, ","), rest)
}

func refOf(it domain.Item) string { return fmt.Sprintf("%s#%d", testFactory, it.Number) }

// linkedNeeds are the linked edges an item needs.
func (h *harness) linkedNeeds(t *testing.T, id domain.ItemID) []domain.Edge {
	t.Helper()

	edges, err := h.store.Edges(inOrg(t), id)
	if err != nil {
		t.Fatal(err)
	}

	var linked []domain.Edge

	for _, e := range edges.Needs {
		if e.Kind == domain.EdgeLinked {
			linked = append(linked, e)
		}
	}

	return linked
}

func architect() domain.Actor {
	return domain.Actor{Kind: domain.ActorAgent, ID: agArchitect, Name: agArchitect}
}

// An item whose output lists another open item in the mapped property gets a linked edge by the
// agent and is not claimed at its next station until that item is done. Unmapped, the same
// output writes no edge.
func TestEngine_AfterHoldsUntilDone(t *testing.T) {
	t.Parallel()

	h, out := afterHarness(t, fieldFollows)
	inFlight := h.submit(t, domain.NewItem{Title: "in flight"})
	follower := h.submit(t, domain.NewItem{Title: "follower"})
	out.set(follower.ID, follows("", refOf(inFlight)))

	h.step(t) // both specified: each queued at build
	out.set(follower.ID, `{}`) // build's output follows nothing

	got := h.linkedNeeds(t, follower.ID)
	if len(got) != 1 || got[0].Needs != inFlight.ID || got[0].By != architect().Ref() {
		t.Fatalf("follower's linked needs = %+v, want one on %s by %s", got, inFlight.ID, architect().Ref())
	}

	h.expectAt(t, follower, stBuild, domain.StateQueued)

	if _, err := h.e.Pause(inOrg(t), inFlight.ID, human, ""); err != nil {
		t.Fatal(err)
	}

	h.step(t)
	h.expectAt(t, follower, stBuild, domain.StateQueued) // held: what it follows is open

	if _, err := h.e.Retry(inOrg(t), inFlight.ID, human); err != nil {
		t.Fatal(err)
	}

	h.step(t)
	h.expectAt(t, inFlight, stDone, domain.StateDone)
	h.step(t)
	h.expectAt(t, follower, stDone, domain.StateDone)

	unmapped, out := afterHarness(t, "")
	ahead := unmapped.submit(t, domain.NewItem{Title: "in flight"})
	it := unmapped.submit(t, domain.NewItem{Title: "follower"})
	out.set(it.ID, follows("", refOf(ahead)))

	unmapped.step(t)

	if got := unmapped.linkedNeeds(t, it.ID); len(got) != 0 {
		t.Errorf("linked needs with dependencies.after unset = %+v, want none", got)
	}

	unmapped.expectAt(t, it, stBuild, domain.StateQueued)
}

// An output with a plan and refs to follow is assembled with a linked edge from every part
// without in-plan needs, and from no other part, to each item it names.
func TestEngine_AfterOnPlanLinksRootParts(t *testing.T) {
	t.Parallel()

	h, out := afterHarness(t, fieldFollows)
	inFlight := h.submit(t, domain.NewItem{Title: "in flight"})
	planner := h.submit(t, domain.NewItem{Title: "planner"})
	out.set(planner.ID, follows(","+strings.TrimSuffix(strings.TrimPrefix(plan, "{"), "}"), refOf(inFlight)))

	h.step(t)

	parts := h.parts(t, planner.ID)
	if parts.Total != 3 {
		t.Fatalf("parts = %d, want 3 (events %v)", parts.Total, h.kinds(planner.ID))
	}

	for _, p := range parts.Items {
		got := h.linkedNeeds(t, p.ID)

		if p.PartKey != keyStore {
			if len(got) != 0 {
				t.Errorf("part %s linked needs = %+v, want none: it needs another part", p.PartKey, got)
			}

			continue
		}

		if len(got) != 1 || got[0].Needs != inFlight.ID || got[0].By != architect().Ref() {
			t.Errorf("root part %s linked needs = %+v, want one on %s by %s", p.PartKey, got, inFlight.ID, architect().Ref())
		}
	}

	if got := h.linkedNeeds(t, planner.ID); len(got) != 0 {
		t.Errorf("assembly's linked needs = %+v, want none: its parts carry them", got)
	}
}

// A ref that is malformed, of another factory, unknown, closed or would close a cycle fails the
// output: contract_failed names the ref, the run is tried again and the item is then blocked,
// and no edge is written.
func TestEngine_AfterBadRefFailsOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// setup prepares the other item, other, and returns the ref the output lists.
		setup func(t *testing.T, h *harness, other, it domain.Item) string
	}{
		{name: "malformed", setup: func(*testing.T, *harness, domain.Item, domain.Item) string { return "acme-api-1" }},
		{name: "another factory", setup: func(_ *testing.T, _ *harness, other, _ domain.Item) string {
			return fmt.Sprintf("acme-web#%d", other.Number)
		}},
		{name: "unknown", setup: func(*testing.T, *harness, domain.Item, domain.Item) string { return testFactory + "#99" }},
		{name: "done", setup: func(t *testing.T, h *harness, other, _ domain.Item) string {
			if _, err := h.e.Move(inOrg(t), other.ID, stBuild, human, ""); err != nil {
				t.Fatal(err)
			}

			h.step(t)
			h.expectAt(t, other, stDone, domain.StateDone)

			return refOf(other)
		}},
		{name: "cancelled", setup: func(t *testing.T, h *harness, other, _ domain.Item) string {
			if _, err := h.e.Cancel(inOrg(t), other.ID, human, ""); err != nil {
				t.Fatal(err)
			}

			return refOf(other)
		}},
		{name: "cycle", setup: func(t *testing.T, h *harness, other, it domain.Item) string {
			if _, err := h.e.Link(inOrg(t), other.ID, it.ID, human); err != nil {
				t.Fatal(err)
			}

			return refOf(other)
		}},
		{name: "itself", setup: func(_ *testing.T, _ *harness, _, it domain.Item) string { return refOf(it) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h, out := afterHarness(t, fieldFollows)
			other := h.submit(t, domain.NewItem{Title: "other"})
			it := h.submit(t, domain.NewItem{Title: "follower"})

			// Paused while setup runs the line, so it runs only with its output set.
			if _, err := h.e.Pause(inOrg(t), it.ID, human, ""); err != nil {
				t.Fatal(err)
			}

			ref := tt.setup(t, h, other, it)
			out.set(it.ID, follows("", ref))

			if _, err := h.e.Retry(inOrg(t), it.ID, human); err != nil {
				t.Fatal(err)
			}

			h.step(t)

			h.expectAt(t, it, stSpec, domain.StateBlocked)

			failed := h.eventsOf(it.ID, domain.EventContractFailed)
			if len(failed) != 2 {
				t.Fatalf("%d contract_failed events, want 2 (events %v)", len(failed), h.kinds(it.ID))
			}

			for _, ev := range failed {
				if msg, _ := ev.Data["error"].(string); !strings.Contains(msg, ref) {
					t.Errorf("contract_failed error %q, want it to name %s", msg, ref)
				}
			}

			if got := h.linkedNeeds(t, it.ID); len(got) != 0 {
				t.Errorf("linked needs after a failed output = %+v, want none", got)
			}
		})
	}
}
