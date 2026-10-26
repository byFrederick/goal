package engine_test

import (
	"context"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
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

// Made-up stations of progressLine.
const (
	stDraft   = "draft"
	stForge   = "forge"
	stInspect = "inspect"
)

// progressLine is inbox → draft → forge → inspect → done, two slots per agent station, with
// fieldPaths mapped to the item's paths.
func progressLine() domain.Line {
	l := lineOf(agentStation(stDraft, agWorker, 2, 2), agentStation(stForge, agBackend, 2, 2),
		agentStation(stInspect, agReviewer, 2, 2))
	l.Flow.Dependencies.Paths = fieldPaths

	return l
}

// placed puts an item queued at station with paths, as earlier stations would have.
func (h *harness) placed(t *testing.T, it domain.Item, station string, paths ...string) domain.Item {
	t.Helper()

	got, err := h.store.Transition(inOrg(t), ports.Transition{
		ItemID: it.ID, From: it.Position(), To: domain.Position{Station: station, State: domain.StateQueued},
		At: h.clock.Now(), Paths: paths,
	})
	if err != nil {
		t.Fatal(err)
	}

	return got
}

// expectQueued fails the test unless the item is queued at station.
func (h *harness) expectQueued(t *testing.T, id domain.ItemID, station string) {
	t.Helper()

	if got := h.item(t, id); got.Position() != (domain.Position{Station: station, State: domain.StateQueued}) {
		t.Errorf("item %d at %+v, want queued at %s", got.Number, got.Position(), station)
	}
}

// expectOverlapNeeds fails the test unless the item's overlap edges, all by the engine, need
// exactly want.
func (h *harness) expectOverlapNeeds(t *testing.T, it domain.Item, want ...domain.ItemID) {
	t.Helper()

	var got []domain.ItemID

	for _, e := range h.edgesOf(t, it.ID, domain.EdgeOverlap) {
		if e.By != domain.EngineActor.Ref() {
			t.Errorf("item %d's overlap edge on %s by %s, want by the engine", it.Number, e.Needs, e.By)
		}

		got = append(got, e.Needs)
	}

	if !slices.Equal(got, want) {
		t.Errorf("item %d's overlap needs = %v, want %v", it.Number, got, want)
	}
}

// twoPlaced places two items with overlapping paths, the first at station a and the second at
// station b, and runs one step.
func twoPlaced(t *testing.T, a, b string) (*harness, domain.Item, domain.Item) {
	t.Helper()

	h := newHarness(t, progressLine(), newScripted().agent)

	first := h.placed(t, h.submit(t, domain.NewItem{Title: "lower"}), a, "internal/x/**")
	second := h.placed(t, h.submit(t, domain.NewItem{Title: "higher"}), b, "internal/x/y.go")

	h.step(t)

	return h, first, second
}

// A started item further along the line is not held by a lower-numbered item that has not
// started: the queued item waits for the started one instead.
func TestEngine_OverlapStartedItemGoesFirst(t *testing.T) {
	t.Parallel()

	h, queued, started := twoPlaced(t, stDraft, stForge)

	h.expectOverlapNeeds(t, started)
	h.expectQueued(t, started.ID, stInspect)
	h.expectOverlapNeeds(t, queued, started.ID)
	h.expectQueued(t, queued.ID, stDraft)
}

// Of two items that have not started, the higher-numbered one waits for the lower.
func TestEngine_OverlapQueuedItemsKeepNumberOrder(t *testing.T) {
	t.Parallel()

	h, a, b := twoPlaced(t, stDraft, stDraft)

	h.expectOverlapNeeds(t, a)
	h.expectQueued(t, a.ID, stForge)
	h.expectOverlapNeeds(t, b, a.ID)
	h.expectQueued(t, b.ID, stDraft)
}

// An overlap edge whose reason no longer holds is removed on the next turn, with the reason in
// edge_removed's data, and its item is claimed; a linked edge stays.
func TestEngine_OverlapRemovesAStaleEdge(t *testing.T) {
	t.Parallel()

	h := newHarness(t, progressLine(), newScripted().agent)

	a := h.placed(t, h.submit(t, domain.NewItem{Title: "a"}), stDraft, "internal/x/**")
	b := h.placed(t, h.submit(t, domain.NewItem{Title: "b"}), stForge, "internal/x/y.go")
	c := h.placed(t, h.submit(t, domain.NewItem{Title: "c"}), stDraft, "docs/**")
	d := h.placed(t, h.submit(t, domain.NewItem{Title: "d"}), stDraft, "api/**")
	e := h.placed(t, h.submit(t, domain.NewItem{Title: "e"}), stInspect, "web/**")

	// b, already at forge, was held by a, which has not started; d's paths no longer overlap c's.
	for _, edge := range [][2]domain.ItemID{{b.ID, a.ID}, {d.ID, c.ID}} {
		if _, err := h.store.AddEdge(inOrg(t), domain.NewEdge{
			Item: edge[0], Needs: edge[1], Kind: domain.EdgeOverlap, By: domain.EngineActor, At: h.clock.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := h.e.Link(inOrg(t), e.ID, a.ID, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	h.step(t)

	for _, it := range []domain.Item{b, d} {
		h.expectOverlapNeeds(t, it)

		removed := h.eventsOf(it.ID, domain.EventEdgeRemoved)
		if len(removed) != 1 || removed[0].Actor != domain.EngineActor.Name {
			t.Fatalf("item %d's edge_removed events = %+v, want one by the engine", it.Number, removed)
		}

		if reason, _ := removed[0].Data["reason"].(string); reason == "" {
			t.Errorf("item %d's edge_removed data = %v, want a reason", it.Number, removed[0].Data)
		}
	}

	h.expectQueued(t, b.ID, stInspect)
	h.expectQueued(t, d.ID, stForge)

	if got := h.edgesOf(t, e.ID, domain.EdgeLinked); len(got) != 1 || got[0].Needs != a.ID {
		t.Errorf("e's linked edges = %+v, want the one on a kept", got)
	}

	h.expectQueued(t, e.ID, stInspect)
}
