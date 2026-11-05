package engine_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

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
