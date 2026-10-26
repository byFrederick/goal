package engine_test

import (
	"context"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

// fieldPaths is the output field the tests' definitions map to an item's paths.
const fieldPaths = "touches"

// pathsLine is spec then build, two slots each, with fieldPaths mapped to the item's paths.
func pathsLine() domain.Line {
	l := lineOf(agentStation(stSpec, agArchitect, 2, 2), agentStation(stBuild, agWorker, 2, 2))
	l.Flow.Dependencies.Paths = fieldPaths

	return l
}

// scripted plays the output set for an item at a station, and {} for the rest.
type scripted struct {
	mu  sync.Mutex
	out map[string]string
}

func newScripted() *scripted { return &scripted{out: map[string]string{}} }

func (s *scripted) set(id domain.ItemID, station, output string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.out[string(id)+"@"+station] = output
}

func (s *scripted) agent(_ context.Context, c porttest.ExecCall, _ io.Writer) domain.RunResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	if out, found := s.out[string(c.Sandbox.ItemID)+"@"+c.Sandbox.Station]; found {
		return ok(out)
	}

	return ok(`{}`)
}

// edgesOf returns the edges an item needs, of kind.
func (h *harness) edgesOf(t *testing.T, id domain.ItemID, kind domain.EdgeKind) []domain.Edge {
	t.Helper()

	edges, err := h.store.Edges(inOrg(t), id)
	if err != nil {
		t.Fatal(err)
	}

	return slices.DeleteFunc(edges.Needs, func(e domain.Edge) bool { return e.Kind != kind })
}

// A station that finishes with the mapped field sets the item's paths; a later output without
// it, or with an empty list, keeps them, and a line that maps no field sets none.
func TestEngine_OutputPathsSetItemPaths(t *testing.T) {
	t.Parallel()

	s := newScripted()
	h := newHarness(t, pathsLine(), s.agent)

	a := h.submit(t, domain.NewItem{Title: "a"})
	c := h.submit(t, domain.NewItem{Title: "c"})

	s.set(a.ID, stSpec, `{"touches":["internal/a/**"]}`)
	s.set(c.ID, stSpec, `{"touches":["docs/c/**"]}`)
	s.set(c.ID, stBuild, `{"touches":[]}`)

	h.step(t)

	if got := h.item(t, a.ID); !slices.Equal(got.Paths, []string{"internal/a/**"}) {
		t.Fatalf("a's paths = %v after spec, want [internal/a/**]", got.Paths)
	}

	h.step(t)

	for id, want := range map[domain.ItemID]string{a.ID: "internal/a/**", c.ID: "docs/c/**"} {
		if got := h.item(t, id); got.State != domain.StateDone || !slices.Equal(got.Paths, []string{want}) {
			t.Errorf("item %s %s with paths %v, want done with [%s] kept", id, got.State, got.Paths, want)
		}
	}

	u := newScripted()
	unmapped := newHarness(t, lineOf(agentStation(stSpec, agArchitect, 1, 1)), u.agent)
	it := unmapped.submit(t, domain.NewItem{})
	u.set(it.ID, stSpec, `{"touches":["internal/a/**"]}`)

	unmapped.step(t)

	if got := unmapped.item(t, it.ID); got.Paths != nil {
		t.Errorf("paths = %v on a line that maps no field, want none", got.Paths)
	}
}

// twoOverlapping submits two items whose spec outputs overlap and runs them through spec, so
// both are queued at build with paths.
func twoOverlapping(t *testing.T, l domain.Line) (*harness, domain.Item, domain.Item) {
	t.Helper()

	s := newScripted()
	h := newHarness(t, l, s.agent)

	a := h.submit(t, domain.NewItem{Title: "a"})
	b := h.submit(t, domain.NewItem{Title: "b"})

	s.set(a.ID, stSpec, `{"touches":["internal/a/**"]}`)
	s.set(b.ID, stSpec, `{"touches":["internal/a/b.go","docs/b.md"]}`)

	h.step(t)

	for _, id := range []domain.ItemID{a.ID, b.ID} {
		if got := h.item(t, id); got.Position() != (domain.Position{Station: stBuild, State: domain.StateQueued}) {
			t.Fatalf("item %s at %+v after spec, want queued at %s", id, got.Position(), stBuild)
		}
	}

	return h, a, b
}

// Of two items whose paths overlap, the later one waits for the earlier: it gets an overlap
// edge by the engine, is not claimed while the earlier one is open and is once it is done.
func TestEngine_OverlapHoldsTheLaterItem(t *testing.T) {
	t.Parallel()

	h, a, b := twoOverlapping(t, pathsLine())

	h.step(t)

	if got := h.item(t, a.ID); got.State != domain.StateDone {
		t.Fatalf("a = %s at %s, want done", got.State, got.Station)
	}

	if got := h.item(t, b.ID); got.Position() != (domain.Position{Station: stBuild, State: domain.StateQueued}) {
		t.Fatalf("b at %+v while a was open, want queued at %s", got.Position(), stBuild)
	}

	edges := h.edgesOf(t, b.ID, domain.EdgeOverlap)
	if len(edges) != 1 || edges[0].Needs != a.ID || edges[0].By != domain.EngineActor.Ref() {
		t.Fatalf("b's overlap edges = %+v, want one on a by %s", edges, domain.EngineActor.Ref())
	}

	if got := h.edgesOf(t, a.ID, domain.EdgeOverlap); len(got) != 0 {
		t.Errorf("a's overlap edges = %+v, want none: the earlier item does not wait", got)
	}

	needs, err := h.store.OpenNeeds(inOrg(t), factoryID, []domain.ItemID{b.ID})
	if err != nil {
		t.Fatal(err)
	}

	if len(needs[b.ID]) != 0 {
		t.Errorf("b's open needs = %+v once a is done, want none", needs[b.ID])
	}

	h.step(t)

	if got := h.item(t, b.ID); got.State != domain.StateDone {
		t.Errorf("b = %s at %s once a is done, want done", got.State, got.Station)
	}

	if got := h.edgesOf(t, b.ID, domain.EdgeOverlap); len(got) != 1 {
		t.Errorf("b's overlap edges = %+v after a repeat visit, want still one", got)
	}
}

// The hold writes no edge to a part of the item's own assembly, to the assembly itself or to a
// done or cancelled item.
func TestEngine_OverlapSkipsOwnAssemblyAndClosedItems(t *testing.T) {
	t.Parallel()

	s := newScripted()
	l := pathsLine()
	l.Flow.Assemblies.Materialize = true
	h := newHarness(t, l, s.agent)

	old := h.submit(t, domain.NewItem{Title: "old"})
	s.set(old.ID, stSpec, `{"touches":["db/**"]}`)

	dropped := h.submit(t, domain.NewItem{Title: "dropped"})
	s.set(dropped.ID, stSpec, `{"touches":["db/**"]}`)

	h.step(t)

	if _, err := h.e.Cancel(inOrg(t), dropped.ID, domain.LocalUser, "not needed"); err != nil {
		t.Fatal(err)
	}

	h.step(t)

	if got := h.item(t, old.ID); got.State != domain.StateDone {
		t.Fatalf("old = %s at %s, want done", got.State, got.Station)
	}

	asm := h.submit(t, domain.NewItem{Title: "assembly"})
	s.set(asm.ID, stSpec, `{"touches":["db/**"],"plan":[
		{"key":"one","title":"One","brief":"Add one.","paths":["db/one.sql"]},
		{"key":"two","title":"Two","brief":"Add two.","paths":["db/**"]}
	]}`)

	h.step(t)

	if got := h.item(t, asm.ID); got.State != domain.StateAssembly || !slices.Equal(got.Paths, []string{"db/**"}) {
		t.Fatalf("assembly %s with paths %v, want an assembly with [db/**]", got.State, got.Paths)
	}

	parts := h.parts(t, asm.ID)
	if parts.Total != 2 {
		t.Fatalf("parts = %d, want 2", parts.Total)
	}

	h.step(t)

	for _, p := range parts.Items {
		if got := h.edgesOf(t, p.ID, domain.EdgeOverlap); len(got) != 0 {
			t.Errorf("part %s overlap edges = %+v, want none", p.PartKey, got)
		}

		if got := h.item(t, p.ID); got.Position() != (domain.Position{Station: stBuild, State: domain.StateQueued}) {
			t.Errorf("part %s at %+v, want claimed at spec and queued at %s", p.PartKey, got.Position(), stBuild)
		}
	}
}

// With flow.assemblies.overlap allow, overlapping items run side by side.
func TestEngine_OverlapAllowWritesNoEdge(t *testing.T) {
	t.Parallel()

	l := pathsLine()
	l.Flow.Assemblies.Overlap = domain.OverlapAllow

	h, a, b := twoOverlapping(t, l)

	h.step(t)

	for _, id := range []domain.ItemID{a.ID, b.ID} {
		if got := h.item(t, id); got.State != domain.StateDone {
			t.Errorf("item %s = %s at %s, want done", id, got.State, got.Station)
		}

		if got := h.edgesOf(t, id, domain.EdgeOverlap); len(got) != 0 {
			t.Errorf("item %s overlap edges = %+v, want none", id, got)
		}
	}
}

// An overlap edge that would close a cycle is not written: the later item already needed by
// the earlier one is claimed, and nothing is logged as an error.
func TestEngine_OverlapSkipsAnEdgeThatClosesACycle(t *testing.T) {
	t.Parallel()

	h, a, b := twoOverlapping(t, pathsLine())

	if _, err := h.e.Link(inOrg(t), a.ID, b.ID, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	h.step(t)

	if got := h.item(t, b.ID); got.State != domain.StateDone {
		t.Fatalf("b = %s at %s, want done: an edge on a would close a cycle", got.State, got.Station)
	}

	if got := h.item(t, a.ID); got.State != domain.StateQueued {
		t.Fatalf("a = %s at %s, want queued until b is done", got.State, got.Station)
	}

	if got := h.edgesOf(t, b.ID, domain.EdgeOverlap); len(got) != 0 {
		t.Errorf("b's overlap edges = %+v, want none", got)
	}

	if logs := h.logs.String(); strings.Contains(logs, `"level":"ERROR"`) {
		t.Errorf("logged an error:\n%s", logs)
	}

	h.step(t)

	if got := h.item(t, a.ID); got.State != domain.StateDone {
		t.Errorf("a = %s at %s once b is done, want done", got.State, got.Station)
	}
}
