package engine_test

import (
	"errors"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/engine"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

func TestEngine_LinkAddsALinkedEdge(t *testing.T) {
	t.Parallel()

	h := newHarness(t, controlLine(), porttest.Succeed(`{}`))
	need, it := h.submit(t, domain.NewItem{}), h.submit(t, domain.NewItem{})

	e, err := h.e.Link(inOrg(t), it.ID, need.ID, human)
	if err != nil {
		t.Fatal(err)
	}

	want := domain.Edge{Item: it.ID, Needs: need.ID, Kind: domain.EdgeLinked, By: human.Ref(), At: h.clock.Now()}
	if e != want {
		t.Errorf("Link = %+v, want %+v", e, want)
	}

	edges, err := h.store.Edges(inOrg(t), it.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(edges.Needs) != 1 || edges.Needs[0] != want {
		t.Errorf("stored needs %+v, want the one linked edge", edges.Needs)
	}

	if added := h.eventsOf(it.ID, domain.EventEdgeAdded); len(added) != 1 || added[0].Actor != human.Name {
		t.Errorf("edge_added events %+v, want one by %s", added, human.Name)
	}
}

func TestEngine_LinkRefusesAClosedItem(t *testing.T) {
	t.Parallel()

	h := newHarness(t, controlLine(), porttest.Succeed(`{}`))
	need := h.submit(t, domain.NewItem{})
	done := h.at(t, h.submit(t, domain.NewItem{}), stDone, domain.StateDone)

	if _, err := h.e.Link(inOrg(t), done.ID, need.ID, human); !errors.Is(err, engine.ErrItemClosed) {
		t.Errorf("Link of a done item: err = %v; want ErrItemClosed", err)
	}

	if err := h.e.Unlink(inOrg(t), done.ID, need.ID, human); !errors.Is(err, engine.ErrItemClosed) {
		t.Errorf("Unlink of a done item: err = %v; want ErrItemClosed", err)
	}

	if edges, err := h.store.Edges(inOrg(t), done.ID); err != nil || len(edges.Needs) != 0 {
		t.Errorf("edges of the done item %+v (%v), want none", edges, err)
	}
}

func TestEngine_UnlinkRemovesTheEdge(t *testing.T) {
	t.Parallel()

	h := newHarness(t, controlLine(), porttest.Succeed(`{}`))
	need, it := h.submit(t, domain.NewItem{}), h.submit(t, domain.NewItem{})

	if _, err := h.e.Link(inOrg(t), it.ID, need.ID, human); err != nil {
		t.Fatal(err)
	}

	if err := h.e.Unlink(inOrg(t), it.ID, need.ID, human); err != nil {
		t.Fatal(err)
	}

	if edges, err := h.store.Edges(inOrg(t), it.ID); err != nil || len(edges.Needs) != 0 {
		t.Errorf("edges after Unlink %+v (%v), want none", edges, err)
	}

	if removed := h.eventsOf(it.ID, domain.EventEdgeRemoved); len(removed) != 1 || removed[0].Actor != human.Name {
		t.Errorf("edge_removed events %+v, want one by %s", removed, human.Name)
	}
}
