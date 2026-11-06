package engine_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// The repositories the filing tests name: an entry of one repository, the default, and two under
// an owner pattern.
const (
	keyAPI  = "github.com/acme/api"
	keyWeb  = "github.com/acme-labs/web"
	keyDocs = "github.com/acme-labs/docs"

	// entryAPI is the keyAPI entry's name.
	entryAPI = "api"
)

// withRepositories is l with runtime.repositories: api, the default, and acme-labs' every
// repository.
func withRepositories(l domain.Line) domain.Line {
	l.Runtime.Repositories = []domain.RepositoryEntry{
		{Name: entryAPI, URL: "https://" + keyAPI + ".git", Access: "write", Default: true},
		{Name: "labs", URL: "https://github.com/acme-labs/*", Access: "write"},
	}

	return l
}

// submitRepos files a person's item naming repos.
func (h *harness) submitRepos(t *testing.T, title string, repos ...string) domain.Item {
	t.Helper()

	it, err := h.e.Submit(inOrg(t), domain.FileRequest{
		FactoryID: factoryID, Origin: domain.OriginPerson, Type: typeFeature, Title: title, Repositories: repos,
	})
	if err != nil {
		t.Fatal(err)
	}

	return it
}

// repoKeys are an item's repositories as keys, in first-joined order.
func (h *harness) repoKeys(t *testing.T, id domain.ItemID) []string {
	t.Helper()

	rows, err := h.store.ItemRepositories(inOrg(t), id)
	if err != nil {
		t.Fatal(err)
	}

	return domain.ItemRepositoryKeys(rows)
}

// A plan entry's repositories become its part's, in plan order; a part whose entry names none
// takes its assembly's, in the assembly's order.
func TestAssemble_PartsTakeTheirRepositories(t *testing.T) {
	t.Parallel()

	out := `{"plan":[
		{"key":"store","title":"Store","brief":"Add the table.","repositories":["acme-labs/docs","acme/api"]},
		{"key":"web","title":"Web","brief":"Show it."}
	]}`
	h := newHarness(t, withRepositories(planLine(true)), porttest.Succeed(out))
	it := h.submitRepos(t, "Big request", "acme-labs/web", entryAPI)

	h.step(t)

	parts := h.parts(t, it.ID)
	if parts.Total != 2 {
		t.Fatalf("parts = %d (events %v); want 2", parts.Total, h.kinds(it.ID))
	}

	want := map[string][]string{keyStore: {keyDocs, keyAPI}, "web": {keyWeb, keyAPI}}
	for _, p := range parts.Items {
		if got := h.repoKeys(t, p.ID); !slices.Equal(got, want[p.PartKey]) {
			t.Errorf("part %s repositories = %v; want %v", p.PartKey, got, want[p.PartKey])
		}
	}
}

// A part added to an assembly takes the assembly's repositories.
func TestEngine_AddPartTakesItsAssemblysRepositories(t *testing.T) {
	t.Parallel()

	h := newHarness(t, withRepositories(planLine(true)), porttest.Succeed(approvedPlan))
	it := h.submitRepos(t, "Big request", "acme-labs/web")

	h.step(t)

	if got := h.item(t, it.ID); got.State != domain.StateAssembly {
		t.Fatalf("item is %s (events %v); want an assembly", got.State, h.kinds(it.ID))
	}

	part, _, err := h.e.AddPart(inOrg(t), it.ID, domain.LocalUser, domain.NewPart{Title: "Write the guide", Brief: "How to use it."})
	if err != nil {
		t.Fatal(err)
	}

	if got, want := h.repoKeys(t, part.ID), []string{keyWeb}; !slices.Equal(got, want) {
		t.Errorf("added part's repositories = %v; want the assembly's %v", got, want)
	}
}

// A plan entry naming a repository no entry covers blocks the assembly with the refusal, which
// quotes the value, and creates no part.
func TestAssemble_RefusedRepositoryBlocks(t *testing.T) {
	t.Parallel()

	out := `{"plan":[{"key":"store","title":"Store","brief":"Add the table.","repositories":["acme/unknown"]}]}`
	h := newHarness(t, withRepositories(planLine(true)), porttest.Succeed(out))
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	if got := h.item(t, it.ID); got.State != domain.StateBlocked || h.parts(t, it.ID).Total != 0 {
		t.Fatalf("item %s with %d parts; want blocked with none", got.State, h.parts(t, it.ID).Total)
	}

	ev := h.eventsOf(it.ID, domain.EventBlocked)
	if len(ev) != 1 || !strings.Contains(fmt.Sprint(ev[0].Data["reason"]), `"acme/unknown"`) {
		t.Errorf("blocked events %+v; want one quoting the repository", ev)
	}
}

// The first run start on an item with no repositories records the default entry's as joined
// default, so its next run starts there even after the line's default entry changes; an item on a
// line without a default entry gets none.
func TestEngine_RunStartKeepsTheDefaultEntrysRepository(t *testing.T) {
	t.Parallel()

	line := withRepositories(lineOf(agentStation(stWork, agWorker, 1, 1), agentStation(stReview, agReviewer, 1, 1)))
	h := newHarness(t, line, porttest.Succeed(`{}`))

	// An item filed with no row, as one filed before repositories or by an old instance.
	it, err := h.store.CreateItem(inOrg(t), domain.NewItem{
		FactoryID: factoryID, Type: typeFeature, Title: "Old item", Station: stWork, State: domain.StateQueued,
		Class: domain.ClassStandard, CreatedAt: t0.Add(-time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}

	h.step(t)

	rows, err := h.store.ItemRepositories(inOrg(t), it.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(rows) != 1 || rows[0].Repo != keyAPI || rows[0].Joined != domain.JoinedDefault || rows[0].Ordinal != 0 {
		t.Fatalf("repositories after the first run start = %+v; want %s joined default", rows, keyAPI)
	}

	moved := withRepositories(lineOf(agentStation(stWork, agWorker, 1, 1), agentStation(stReview, agReviewer, 1, 1)))
	moved.Runtime.Repositories[0].Default = false
	moved.Runtime.Repositories = append(moved.Runtime.Repositories, domain.RepositoryEntry{
		Name: "labs-web", URL: "https://" + keyWeb, Access: "write", Default: true,
	})
	h.setLines(t, moved)

	h.step(t)

	runs := h.worker.Runs()
	if len(runs) != 2 {
		t.Fatalf("%d runs; want one per station", len(runs))
	}

	if feedback := runs[1].Env["GOAL_FEEDBACK"]; !strings.Contains(feedback, "/acme/api at /workspace") {
		t.Errorf("second run's workspace note %q; want %s at /workspace, the recorded repository", feedback, keyAPI)
	}

	if got := h.repoKeys(t, it.ID); !slices.Equal(got, []string{keyAPI}) {
		t.Errorf("repositories after the default moved = %v; want [%s]", got, keyAPI)
	}

	none := lineOf(agentStation(stWork, agWorker, 1, 1))
	h2 := newHarness(t, none, porttest.Succeed(`{}`))
	bare := h2.queuedAt(t, stWork, "No default")

	h2.step(t)

	if got := h2.repoKeys(t, bare.ID); len(got) != 0 {
		t.Errorf("an item on a line without a default entry has %v; want none", got)
	}
}

// fieldRepos is the output field the output repositories tests' lines map.
const fieldRepos = "repos"

// An accepted output's repositories join its item, joined output in the order named after those
// it had, each once; a gate's rejection adds none of those it names.
func TestEngine_OutputRepositoriesJoinItem(t *testing.T) {
	t.Parallel()

	line := withRepositories(reviewLine(3))
	line.Flow.Outputs.Repositories = fieldRepos

	h := newHarness(t, line, byAgent(map[string]string{
		agWorker:   `{"repos":["acme-labs/docs","acme-labs/web","api"]}`,
		agReviewer: `{"decision":"reject","reason":"still wrong","repos":["acme-labs/other"]}`,
	}))
	it := h.submitRepos(t, "Add a flag", "acme-labs/web")

	h.step(t)

	rows, err := h.store.ItemRepositories(inOrg(t), it.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := domain.ItemRepositoryKeys(rows), []string{keyWeb, keyDocs, keyAPI}; !slices.Equal(got, want) {
		t.Fatalf("repositories after build = %v (events %v); want %v", got, h.kinds(it.ID), want)
	}

	for _, r := range rows[1:] {
		if r.Joined != domain.JoinedOutput {
			t.Errorf("%s joined %s; want output", r.Repo, r.Joined)
		}
	}

	h.step(t)

	if len(h.eventsOf(it.ID, domain.EventGateRejected)) == 0 {
		t.Fatalf("events %v; want the gate to reject", h.kinds(it.ID))
	}

	if got := h.repoKeys(t, it.ID); slices.Contains(got, "github.com/acme-labs/other") {
		t.Errorf("repositories after the rejection = %v; want none of the rejection's", got)
	}
}

// An output whose repositories would give its item more than 10 fails its check with the count
// and the cap, is retried like any bad output, and adds none; a name no entry covers fails too.
func TestEngine_OutputRepositoriesPastTheCapFailCheck(t *testing.T) {
	t.Parallel()

	nine := make([]string, domain.MaxItemRepositories-1)
	for i := range nine {
		nine[i] = fmt.Sprintf("acme-labs/r%d", i)
	}

	for _, tt := range []struct{ name, out, reason string }{
		{name: "past the cap", out: `{"repos":["acme-labs/docs","api"]}`, reason: "11 repositories, more than an item's 10"},
		{name: "unknown", out: `{"repos":["acme/unknown"]}`, reason: `"acme/unknown"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			line := withRepositories(lineOf(agentStation(stBuild, agWorker, 1, 1), agentStation(stReview, agReviewer, 1, 1)))
			line.Flow.Outputs.Repositories = fieldRepos

			h := newHarness(t, line, byAgent(map[string]string{agWorker: tt.out, agReviewer: `{}`}))
			it := h.submitRepos(t, "Add a flag", nine...)

			h.step(t)

			failed := h.eventsOf(it.ID, domain.EventContractFailed)
			if len(failed) != 2 {
				t.Fatalf("contract_failed events %d (events %v); want one per attempt", len(failed), h.kinds(it.ID))
			}

			if msg, _ := failed[0].Data["error"].(string); !strings.Contains(msg, tt.reason) {
				t.Errorf("contract_failed error %q; want it to say %s", msg, tt.reason)
			}

			if got := h.item(t, it.ID); got.State != domain.StateBlocked || got.Station != stBuild {
				t.Errorf("item %s at %s; want blocked at %s after its attempts", got.State, got.Station, stBuild)
			}

			if got := h.repoKeys(t, it.ID); len(got) != len(nine) {
				t.Errorf("repositories = %v; want the %d filed", got, len(nine))
			}
		})
	}
}
