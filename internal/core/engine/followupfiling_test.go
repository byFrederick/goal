package engine_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// landedFollowUps is a worker's first output: a start and a follow-up for triage; it writes none
// after that.
const landedFollowUps = `{"followups":[{"text":"Index the runs","start":{"in":"2d","title":"Index runs"}},"Look at the logs"]}`

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

// The items filed when work lands take its repositories: the start's item the source's, the
// triage item its sources'. A source joining the open triage item adds the repositories it lacks.
func TestEngine_TickFilesFollowUpsWithTheirSourcesRepositories(t *testing.T) {
	t.Parallel()

	b := &byCall{runs: map[string]int{}, out: map[string][]string{
		agWorker: {landedFollowUps, `{"followups":["Look at the traces"]}`, `{}`},
	}}
	h := newHarness(t, withRepositories(withFollowUps(lineOf(agentStation(stBuild, agWorker, 1, 1)))), b.agent)
	it := h.submitRepos(t, "Add the flag", "acme-labs/web")

	h.untilDone(t, it.ID, 4)
	h.step(t)

	got := h.followUps(t, it.ID)
	if len(got) != 2 || got[0].FiledItem == "" || got[1].FiledItem == "" {
		t.Fatalf("follow-ups = %+v; want both filed", got)
	}

	start, triage := got[0].FiledItem, got[1].FiledItem
	if keys := h.repoKeys(t, start); !slices.Equal(keys, []string{keyWeb}) {
		t.Errorf("start item's repositories = %v; want the source's %v", keys, []string{keyWeb})
	}

	if keys := h.repoKeys(t, triage); !slices.Equal(keys, []string{keyWeb}) {
		t.Errorf("triage item's repositories = %v; want its source's %v", keys, []string{keyWeb})
	}

	second := h.submitRepos(t, "Trace it", "api", "acme-labs/web")

	h.untilDone(t, second.ID, 4)
	h.step(t)

	if fs := h.followUps(t, second.ID); len(fs) != 1 || fs[0].FiledItem != triage {
		t.Fatalf("second follow-ups = %+v; want one joining triage item %s", fs, triage)
	}

	if keys, want := h.repoKeys(t, triage), []string{keyWeb, keyAPI}; !slices.Equal(keys, want) {
		t.Errorf("triage item's repositories after a join = %v; want %v", keys, want)
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

// A follow-up a person files takes the repositories the person names, else its start's, else
// its source's.
func TestFollowUp_FileByAPersonTakesRepositories(t *testing.T) {
	t.Parallel()

	out := `{"followups":["Look at the logs",{"text":"Index","start":{"title":"Index runs","repositories":["acme-labs/docs"]}},"Trace it"]}`
	b := &byCall{runs: map[string]int{}, out: map[string][]string{agWorker: {out, `{}`}}}
	h := newHarness(t, withRepositories(withFollowUps(lineOf(agentStation(stBuild, agWorker, 1, 1)))), b.agent)
	src := h.submitRepos(t, "Add the flag", "acme-labs/web")

	h.step(t)

	fs := h.followUps(t, src.ID)
	if len(fs) != 3 {
		t.Fatalf("follow-ups = %+v; want three", fs)
	}

	ada := domain.Actor{Kind: domain.ActorUser, ID: "ada", Name: "Ada"}

	for _, tt := range []struct {
		f     domain.FollowUp
		named []string
		want  []string
	}{
		{fs[0], nil, []string{keyWeb}},
		{fs[1], nil, []string{keyDocs}},
		{fs[2], []string{"api"}, []string{keyAPI}},
	} {
		if _, err := h.e.DropFollowUp(inOrg(t), src.ID, tt.f.ID, ada); err != nil {
			t.Fatal(err)
		}

		it, err := h.e.FileFollowUp(inOrg(t), src.ID, tt.f.ID, domain.FileRequest{By: ada, Title: "Filed " + tt.f.Text, Repositories: tt.named})
		if err != nil {
			t.Fatal(err)
		}

		if got := h.repoKeys(t, it.ID); !slices.Equal(got, tt.want) {
			t.Errorf("%q named %v: repositories = %v; want %v", tt.f.Text, tt.named, got, tt.want)
		}
	}
}
