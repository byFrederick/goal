package engine_test

import (
	"errors"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// landedFollowUps is a worker's first output: a start and a follow-up for triage; it writes none
// after that.
const landedFollowUps = `{"followups":[{"text":"Index the runs","start":{"in":"2d","title":"Index runs"}},"Look at the logs"]}`

// listed is every item of the harness's factory, newest first.
func (h *harness) listed(t *testing.T) []domain.Item {
	t.Helper()

	got, err := h.store.ListItems(inOrg(t), ports.ItemQuery{FactoryID: factoryID, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}

	return got
}

// untilDone steps the harness until it is done, at most n ticks.
func (h *harness) untilDone(t *testing.T, id domain.ItemID, n int) {
	t.Helper()

	for range n {
		if h.item(t, id).State == domain.StateDone {
			return
		}

		h.step(t)
	}

	if got := h.item(t, id); got.State != domain.StateDone {
		t.Fatalf("item is %s after %d ticks; want done", got.State, n)
	}
}

// The tick after an item lands files its start follow-up as an item by the engine and adds the
// other to a new triage item; the follow-ups are filed with them.
func TestEngine_TickFilesFollowUpsWhenWorkLands(t *testing.T) {
	t.Parallel()

	b := &byCall{runs: map[string]int{}, out: map[string][]string{agWorker: {landedFollowUps, `{}`}}}
	h := newHarness(t, withFollowUps(lineOf(agentStation(stBuild, agWorker, 1, 1))), b.agent)
	it := h.submit(t, domain.NewItem{Title: "Add the flag", Priority: 2})

	h.untilDone(t, it.ID, 4)
	h.step(t)

	got := h.followUps(t, it.ID)
	if len(got) != 2 || got[0].State != domain.FollowUpFiled || got[1].State != domain.FollowUpFiled {
		t.Fatalf("follow-ups = %+v; want both filed", got)
	}

	start, triage := h.item(t, got[0].FiledItem), h.item(t, got[1].FiledItem)
	if start.Title != "Index runs" || start.Origin != domain.OriginFollowUp || start.Priority != 1 || start.Generation != 1 || start.Triage {
		t.Errorf("start item = %+v; want Index runs, a follow-up at priority 1, generation 1", start)
	}

	if !triage.Triage || triage.Title != "Follow-ups from "+testFactory+"#1" {
		t.Errorf("triage item = %+v; want the factory's triage item for #1", triage)
	}
}

// A triage item's plan has at most three parts: a fourth is a bad plan, so it never becomes an
// assembly.
func TestEngine_TriagePlanAtMostThreeParts(t *testing.T) {
	t.Parallel()

	four := `{"plan":[{"key":"a","title":"A","brief":"a"},{"key":"b","title":"B","brief":"b"},` +
		`{"key":"c","title":"C","brief":"c"},{"key":"d","title":"D","brief":"d"}]}`
	b := &byCall{runs: map[string]int{}, out: map[string][]string{
		agArchitect: {`{"followups":["Look at the logs"]}`, four}, agWorker: {`{}`},
	}}
	h := newHarness(t, withFollowUps(planLine(true)), b.agent)
	src := h.submit(t, domain.NewItem{})

	h.untilDone(t, src.ID, 4)
	h.step(t)

	got := h.followUps(t, src.ID)
	if len(got) != 1 || got[0].FiledItem == "" {
		t.Fatalf("follow-ups = %+v; want one in a triage item", got)
	}

	tri := got[0].FiledItem

	h.clock.Advance(25 * time.Hour)

	for range 3 {
		h.step(t)
	}

	if it := h.item(t, tri); it.State == domain.StateAssembly || h.parts(t, tri).Total != 0 {
		t.Errorf("triage item is %s with %d parts; want its four-part plan refused", it.State, h.parts(t, tri).Total)
	}

	if len(h.eventsOf(tri, domain.EventContractFailed)) == 0 {
		t.Errorf("triage item events %v; want the plan refused", h.kinds(tri))
	}
}

// A person drops a waiting follow-up and files it again: the item is a follow-up by the person,
// one generation below its source.
func TestFollowUp_FileAndDropByAPerson(t *testing.T) {
	t.Parallel()

	b := &byCall{runs: map[string]int{}, out: map[string][]string{agWorker: {`{"followups":["Look at the logs"]}`, `{}`}}}
	h := newHarness(t, withFollowUps(lineOf(agentStation(stBuild, agWorker, 1, 1))), b.agent)
	src := h.submit(t, domain.NewItem{})

	h.step(t)

	fs := h.followUps(t, src.ID)
	if len(fs) != 1 {
		t.Fatalf("follow-ups = %+v; want one", fs)
	}

	ada := domain.Actor{Kind: domain.ActorUser, ID: "ada", Name: "Ada"}
	req := domain.FileRequest{By: ada, Title: "Read the logs"}

	if _, err := h.e.FileFollowUp(inOrg(t), src.ID, fs[0].ID, req); !errors.Is(err, ports.ErrConflict) {
		t.Errorf("FileFollowUp of a waiting one: err %v; want ErrConflict", err)
	}

	if _, err := h.e.FileFollowUp(inOrg(t), src.ID, "00000000-0000-7000-8000-fffffffffffd", req); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("FileFollowUp of none: err %v; want ErrNotFound", err)
	}

	if f, err := h.e.DropFollowUp(inOrg(t), src.ID, fs[0].ID, ada); err != nil || f.State != domain.FollowUpDropped {
		t.Fatalf("DropFollowUp = %+v, %v; want dropped", f, err)
	}

	it, err := h.e.FileFollowUp(inOrg(t), src.ID, fs[0].ID, req)
	if err != nil || it.Origin != domain.OriginFollowUp || it.CreatedBy != ada.Ref() || it.Generation != 1 || it.Title != "Read the logs" {
		t.Fatalf("FileFollowUp = %+v, %v; want Ada's follow-up at generation 1", it, err)
	}
}
