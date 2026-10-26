package engine_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

// inFlightLine is planLine with the spec station preloading the factory's open work.
func inFlightLine() domain.Line {
	l := planLine(true)
	l.Stations[1].Context = []domain.StationContext{domain.ContextInFlight}

	return l
}

// move puts it at to with paths, as a person or an earlier station would have.
func (h *harness) move(t *testing.T, it domain.Item, to domain.Position, paths []string) domain.Item {
	t.Helper()

	got, err := h.store.Transition(inOrg(t), ports.Transition{
		ItemID: it.ID, From: h.item(t, it.ID).Position(), To: to, Paths: paths, At: h.clock.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	return got
}

// inFlightOf is the `## In flight` section of the i-th run's feedback, empty when it has none.
func (h *harness) inFlightOf(i int) string {
	f := h.feedback(i)

	_, section, ok := strings.Cut(f, "## In flight")
	if !ok {
		return ""
	}

	section, _, _ = strings.Cut(section, "\n\n## ")

	return "## In flight" + section
}

// A run at a station with context: [inflight] hears the factory's open items and assemblies
// with their paths: never the item itself, its assembly or its assembly's other parts, never a
// closed item, and an assembly with the union of its open parts' paths.
func TestEngine_InFlightSectionListsOpenItems(t *testing.T) {
	t.Parallel()

	h := newHarness(t, inFlightLine(), porttest.Succeed(plan))
	ref := func(n int64) string { return domain.ItemRef{Factory: testFactory, Number: n}.String() }

	other := h.move(t, h.submit(t, domain.NewItem{Title: "Move terraform"}),
		domain.Position{Station: stBuild, State: domain.StateBlocked}, []string{"terraform/**"})
	shipped := h.move(t, h.submit(t, domain.NewItem{Title: "Shipped"}),
		domain.Position{Station: stDone, State: domain.StateDone}, []string{"cmd/**"})
	it := h.submit(t, domain.NewItem{Title: "Plan the export"})
	otherLine := fmt.Sprintf("- %s %q: blocked at %s, paths terraform/**", ref(other.Number), other.Title, stBuild)

	h.step(t) // the item runs spec and plans its assembly

	first := h.inFlightOf(0)
	if !strings.Contains(first, otherLine) {
		t.Errorf("the item's in-flight section lacks %q:\n%s", otherLine, first)
	}

	for _, n := range []int64{it.Number, shipped.Number} {
		if strings.Contains(first, ref(n)+" ") {
			t.Errorf("the item's in-flight section lists %s:\n%s", ref(n), first)
		}
	}

	h.step(t) // store, the part that needs none, runs spec

	parts := map[string]domain.Item{}
	for _, p := range h.parts(t, it.ID).Items {
		parts[p.PartKey] = p
	}

	if got := h.worker.Runs()[1].Sandbox.ItemID; got != parts[keyStore].ID {
		t.Fatalf("second run is of %s, want store's", got)
	}

	second := h.inFlightOf(1)
	if !strings.Contains(second, otherLine) {
		t.Errorf("store's in-flight section lacks %q:\n%s", otherLine, second)
	}

	for _, n := range []int64{it.Number, parts[keyStore].Number, parts["api"].Number, parts["web"].Number} {
		if strings.Contains(second, ref(n)+" ") {
			t.Errorf("store's in-flight section lists %s, of its own assembly:\n%s", ref(n), second)
		}
	}

	h.laterHearsTheAssembly(t, it, parts, otherLine)
}

// laterHearsTheAssembly closes the web part of it, then runs a later item: it hears the
// assembly with its open parts' paths and those parts, the two that change contracts/api/, which
// the later item names, ranked first. web is done: its paths are no longer in flight.
func (h *harness) laterHearsTheAssembly(t *testing.T, it domain.Item, parts map[string]domain.Item, otherLine string) {
	t.Helper()

	ref := func(n int64) string { return domain.ItemRef{Factory: testFactory, Number: n}.String() }

	h.move(t, parts["web"], domain.Position{Station: stDone, State: domain.StateDone}, nil)
	later := h.submit(t, domain.NewItem{Title: "Document contracts/api/"})

	h.step(t)

	if got := h.worker.Runs()[2].Sandbox.ItemID; got != later.ID {
		t.Fatalf("third run is of %s, want the later item's", got)
	}

	third := h.inFlightOf(2)
	want := []string{ // the two that name contracts/api/ first, then the others
		fmt.Sprintf("- %s %q: assembly at %s, paths contracts/api/, db/migrations/", ref(it.Number), it.Title, stSpec),
		fmt.Sprintf("- %s %q: queued at %s, paths contracts/api/", ref(parts["api"].Number), parts["api"].Title, stSpec),
		fmt.Sprintf("- %s %q: blocked at %s, paths db/migrations/", ref(parts[keyStore].Number), parts[keyStore].Title, stSpec),
		otherLine,
	}

	at := make([]int, len(want))
	for i, w := range want {
		if at[i] = strings.Index(third, w); at[i] < 0 {
			t.Errorf("the later item's in-flight section lacks %q:\n%s", w, third)
		}
	}

	if max(at[0], at[1]) > min(at[2], at[3]) {
		t.Errorf("the later item's in-flight section does not rank the entries that name contracts/api/ first:\n%s", third)
	}

	for _, n := range []int64{later.Number, parts["web"].Number} {
		if strings.Contains(third, ref(n)+" ") {
			t.Errorf("the later item's in-flight section lists %s:\n%s", ref(n), third)
		}
	}
}

// A station without context gets no In flight section.
func TestEngine_NoInFlightSectionWithoutContext(t *testing.T) {
	t.Parallel()

	h := newHarness(t, planLine(true), porttest.Succeed(plan))
	h.move(t, h.submit(t, domain.NewItem{Title: "Move the modules"}),
		domain.Position{Station: stBuild, State: domain.StateBlocked}, []string{"terraform/**"})
	h.submit(t, domain.NewItem{Title: "Plan the export"})

	h.step(t)

	if n := len(h.worker.Runs()); n != 1 {
		t.Fatalf("%d runs, want 1", n)
	}

	if f := h.feedback(0); strings.Contains(f, "## In flight") {
		t.Errorf("feedback has an in-flight section:\n%s", f)
	}
}

// listDown is a store whose ListItems fails.
type listDown struct{ ports.Store }

var errListDown = errors.New("items unavailable")

func (listDown) ListItems(context.Context, ports.ItemQuery) ([]domain.Item, error) {
	return nil, errListDown
}

// An item whose station preloads the open work, which cannot be read, is blocked with the
// reason; it never runs without the section.
func TestEngine_InFlightReadErrorBlocks(t *testing.T) {
	t.Parallel()

	h := newHarness(t, inFlightLine(), porttest.Succeed(plan))
	h.e = h.engine(t, listDown{Store: h.store})
	it := h.submit(t, domain.NewItem{Title: "Plan the export"})

	h.step(t)

	if n := len(h.worker.Runs()); n != 0 {
		t.Errorf("%d runs, want none", n)
	}

	ev := h.eventsOf(it.ID, domain.EventBlocked)
	if len(ev) != 1 || !strings.Contains(fmt.Sprint(ev[0].Data["reason"]), errListDown.Error()) {
		t.Errorf("item %s, blocked events %+v; want blocked saying %q", h.item(t, it.ID).State, ev, errListDown)
	}
}
