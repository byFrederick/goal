package engine_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/engine"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

// A part a person adds is queued at the assembly's own station, keyed by its title's slug, with a
// planned edge to each part it needs, part_created and part_added events and an audit entry.
func TestEngine_AddPartQueuesAtTheAssemblysStationWithItsNeeds(t *testing.T) {
	t.Parallel()

	h := newHarness(t, controlLine(), porttest.Succeed(`{}`))
	asm, parts := h.assemblyAtWork(t)

	part, all, err := h.e.AddPart(inOrg(t), asm.ID, domain.LocalUser, domain.NewPart{
		Title: "  Write the Guide!  ", Lane: " docs ", Brief: " how to use it ", Needs: []string{" " + partQueued + " ", partShipped},
	})
	if err != nil {
		t.Fatal(err)
	}

	if part.PartKey != "write-the-guide" || part.Title != "Write the Guide!" || part.Lane != "docs" || part.Body != "how to use it" {
		t.Errorf("part = key %q, title %q, lane %q, body %q; want the trimmed input keyed write-the-guide",
			part.PartKey, part.Title, part.Lane, part.Body)
	}

	if got := h.item(t, part.ID).Position(); got != (domain.Position{Station: stWork, State: domain.StateQueued}) {
		t.Errorf("part at %+v; want queued at the assembly's station %s", got, stWork)
	}

	if all.Total != 5 || all.Items[len(all.Items)-1].ID != part.ID {
		t.Errorf("assembly has %d parts, last %v; want 5, the new one last", all.Total, all.Items[len(all.Items)-1].ID)
	}

	if needs, want := h.plannedNeeds(t, part.ID), []domain.ItemID{parts[partQueued].ID, parts[partShipped].ID}; !slices.Equal(needs, want) {
		t.Errorf("part needs %v; want %v", needs, want)
	}

	h.checkPartAdded(t, asm.ID, part.ID)
}

// checkPartAdded checks the trail of one added part: part_created on it, part_added and one
// audit entry on its assembly.
func (h *harness) checkPartAdded(t *testing.T, asm, part domain.ItemID) {
	t.Helper()

	if len(h.eventsOf(part, domain.EventPartCreated)) != 1 || len(h.eventsOf(asm, domain.EventPartAdded)) != 1 {
		t.Errorf("events: part %v, assembly %v; want part_created and part_added", h.kinds(part), h.kinds(asm))
	}

	if audited := h.audited(t, domain.AuditItemPartAdded); len(audited) != 1 || audited[0].TargetID != string(asm) {
		t.Errorf("audit = %+v; want one %s entry on the assembly", audited, domain.AuditItemPartAdded)
	}
}

// plannedNeeds are the items id needs, oldest edge first; each edge must be planned.
func (h *harness) plannedNeeds(t *testing.T, id domain.ItemID) []domain.ItemID {
	t.Helper()

	edges, err := h.store.Edges(inOrg(t), id)
	if err != nil {
		t.Fatal(err)
	}

	needs := make([]domain.ItemID, 0, len(edges.Needs))
	for _, e := range edges.Needs {
		if e.Kind != domain.EdgePlanned {
			t.Errorf("edge to %s is %s; want planned", e.Needs, e.Kind)
		}

		needs = append(needs, e.Needs)
	}

	return needs
}

// A title whose slug another part has gets -2, then -3; a title without letters or digits is
// keyed part.
func TestEngine_AddPartKeyCollisionGetsASuffix(t *testing.T) {
	t.Parallel()

	h := newHarness(t, controlLine(), porttest.Succeed(`{}`))
	asm, _ := h.assemblyAtWork(t)

	for _, want := range []struct{ title, key string }{
		{"Queued", partQueued + "-2"},
		{"queued!", partQueued + "-3"},
		{"--", "part"},
		{"A title well past the forty-one characters a key can hold", "a-title-well-past-the-forty-one-character"},
		{"A title well past the forty-one characters a key can hold", "a-title-well-past-the-forty-one-charact-2"},
	} {
		part, _, err := h.e.AddPart(inOrg(t), asm.ID, domain.LocalUser, domain.NewPart{Title: want.title, Brief: "b"})
		if err != nil || part.PartKey != want.key {
			t.Errorf("AddPart(%q) keyed %q, %v; want %q", want.title, part.PartKey, err, want.key)
		}
	}
}

// An item that is not an assembly takes no part, and nothing is written.
func TestEngine_AddPartRefusesANonAssembly(t *testing.T) {
	t.Parallel()

	h := newHarness(t, controlLine(), porttest.Succeed(`{}`))
	it := h.submit(t, domain.NewItem{Title: "plain"})

	if _, _, err := h.e.AddPart(inOrg(t), it.ID, domain.LocalUser, domain.NewPart{Title: "t", Brief: "b"}); !errors.Is(err, engine.ErrNotAssembly) {
		t.Errorf("AddPart on a plain item = %v; want ErrNotAssembly", err)
	}

	h.wroteNoPart(t, it.ID)
}

// A done assembly is closed, not "not an assembly".
func TestEngine_AddPartRefusesADoneAssembly(t *testing.T) {
	t.Parallel()

	h := newHarness(t, controlLine(), porttest.Succeed(`{}`))
	asm, _ := h.assemblyAtWork(t)
	asm = h.at(t, h.item(t, asm.ID), stDone, domain.StateDone)

	if _, _, err := h.e.AddPart(inOrg(t), asm.ID, domain.LocalUser, domain.NewPart{Title: "t", Brief: "b"}); !errors.Is(err, engine.ErrItemClosed) {
		t.Errorf("AddPart on a done assembly = %v; want ErrItemClosed", err)
	}

	h.wroteNoPart(t, asm.ID)
}

// An unknown need, an empty title and an empty brief are invalid, and nothing is written.
func TestEngine_AddPartRefusesAnUnknownNeed(t *testing.T) {
	t.Parallel()

	h := newHarness(t, controlLine(), porttest.Succeed(`{}`))
	asm, _ := h.assemblyAtWork(t)

	for name, p := range map[string]domain.NewPart{
		"unknown need": {Title: "t", Brief: "b", Needs: []string{"nope"}},
		"empty title":  {Title: "  ", Brief: "b"},
		"empty brief":  {Title: "t", Brief: " "},
	} {
		if _, _, err := h.e.AddPart(inOrg(t), asm.ID, domain.LocalUser, p); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("AddPart with %s = %v; want domain.ErrInvalid", name, err)
		}
	}

	h.wroteNoPart(t, asm.ID)
}

// wroteNoPart checks that id has no part_added event and the organization no audit entry of one.
func (h *harness) wroteNoPart(t *testing.T, id domain.ItemID) {
	t.Helper()

	if added := h.eventsOf(id, domain.EventPartAdded); len(added) != 0 {
		t.Errorf("part_added events = %+v; want none", added)
	}

	if audited := h.audited(t, domain.AuditItemPartAdded); len(audited) != 0 {
		t.Errorf("audit = %+v; want none", audited)
	}
}
