package storetest

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// repositoriesTests cover items' branches and repositories (ADR 0041 decisions 4 and 6).
var repositoriesTests = []struct {
	name string
	test func(f *fixture)
}{
	{"CreateItem_StoresBranchAndRepositories", testCreateItemStoresBranchAndRepositories},
	{"ItemRepositories_InJoinOrder", testItemRepositoriesInJoinOrder},
	{"ItemRepositories_PartsWrittenWithTheirs", testPartsWrittenWithTheirRepositories},
	{"ItemRepositories_StartFollowUpTakesItsSources", testStartFollowUpTakesItsSourcesRepositories},
	{"ItemRepositories_TriageGainsJoiningSourcesUpToTheCap", testTriageGainsJoiningSourcesRepositories},
}

// The repositories the cases name.
const (
	repoAPI = "github.com/acme/api"
	repoWeb = "github.com/acme/web"
	repoOps = "gitlab.example.com/acme/ops"

	// factoryNameAcme is the factory name the cases file under.
	factoryNameAcme = "acme"
)

func (f *fixture) itemRepositories(t *testing.T, id domain.ItemID) []domain.ItemRepository {
	t.Helper()

	rows, err := f.Store.ItemRepositories(f.ctx(t), id)
	if err != nil {
		t.Fatalf("ItemRepositories: %v", err)
	}

	return rows
}

// A filed item keeps the branch ItemBranch names, with its factory's name at filing, and its
// repositories joined at its creation, in the order given; a part's branch names its assembly's
// factory, and an item filed without a factory name gets goal/<number>-<slug>. Another
// organization sees none of the rows.
func testCreateItemStoresBranchAndRepositories(f *fixture) {
	t := f.t

	asm, l := f.planning(t, domain.NewItem{Title: titleBig, FactoryName: factoryNameAcme})

	_, parts := f.assemble(t, assembly(asm, l, plan()))
	for _, p := range parts.Items {
		if got := f.item(t, p.ID).Branch(); got != domain.ItemBranch(factoryNameAcme, p.Number, p.Title) {
			t.Errorf("part %s branch = %q; want its assembly's factory in it", p.PartKey, got)
		}
	}

	it := f.create(t, domain.NewItem{
		Title: "Add a flag", FactoryName: factoryNameAcme,
		Repositories: []domain.ItemRepository{{Repo: repoWeb, Joined: domain.JoinedFiling}, {Repo: repoAPI, Joined: domain.JoinedFiling}},
	})

	want := domain.ItemBranch(factoryNameAcme, it.Number, "Add a flag")
	if !strings.HasPrefix(want, "goal/acme/") || it.Branch() != want || f.item(t, it.ID).Branch() != want {
		t.Errorf("branch = %q, read back %q; want %q", it.Branch(), f.item(t, it.ID).Branch(), want)
	}

	wantRows := []domain.ItemRepository{
		{Repo: repoWeb, Joined: domain.JoinedFiling, JoinedAt: t0, Ordinal: 0},
		{Repo: repoAPI, Joined: domain.JoinedFiling, JoinedAt: t0, Ordinal: 1},
	}
	if got := f.itemRepositories(t, it.ID); !reflect.DeepEqual(got, wantRows) {
		t.Errorf("repositories =\n%+v\nwant\n%+v", got, wantRows)
	}

	plain := f.create(t, domain.NewItem{Title: "Fix it"})
	if got := f.item(t, plain.ID).Branch(); got != domain.ItemBranch("", plain.Number, "Fix it") {
		t.Errorf("no factory name: branch = %q; want goal/<number>-fix-it", got)
	}

	if got := f.itemRepositories(t, plain.ID); len(got) != 0 {
		t.Errorf("no repositories = %+v; want none", got)
	}

	if got, err := f.Store.ItemRepositories(domain.WithOrg(t.Context(), f.NewOrg(t)), it.ID); err != nil || len(got) != 0 {
		t.Errorf("another organization's ItemRepositories = %+v, %v; want none", got, err)
	}
}

// ItemRepositories reads the rows named at filing first, in the order named, then the rest by
// when they joined and their ordinal; an unknown item has none.
func testItemRepositoriesInJoinOrder(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{Repositories: []domain.ItemRepository{
		{Repo: repoOps, Joined: domain.JoinedDefault},
		{Repo: repoWeb, Joined: domain.JoinedFiling},
		{Repo: repoAPI, Joined: domain.JoinedFiling},
	}})

	var got []string
	for _, r := range f.itemRepositories(t, it.ID) {
		got = append(got, r.Repo)
	}

	if want := []string{repoWeb, repoAPI, repoOps}; !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v; want %v", got, want)
	}

	if first, ok := domain.ItemRepositories(f.itemRepositories(t, it.ID)).First(); !ok || first.Repo != repoWeb {
		t.Errorf("first = %+v, %t; want %s", first, ok, repoWeb)
	}

	if got := f.itemRepositories(t, missingItem); len(got) != 0 {
		t.Errorf("unknown item = %+v; want none", got)
	}
}

// repoKeys are id's repositories as keys, in first-joined order.
func (f *fixture) repoKeys(t *testing.T, id domain.ItemID) []string {
	t.Helper()

	return domain.ItemRepositoryKeys(f.itemRepositories(t, id))
}

// filed are keys joined filing, in order.
func filed(keys ...string) []domain.ItemRepository {
	out := make([]domain.ItemRepository, len(keys))
	for i, k := range keys {
		out[i] = domain.ItemRepository{Repo: k, Joined: domain.JoinedFiling, Ordinal: i}
	}

	return out
}

// repoLineOf is lineOf with entries for acme's every repository on github.com and repoOps.
func repoLineOf(factory domain.FactoryID, name string, from ...domain.FactoryID) domain.Line {
	l := lineOf(factory, name, from...)
	l.Runtime.Repositories = []domain.RepositoryEntry{
		{Name: "acme", URL: "https://github.com/acme/*", Access: "write"},
		{Name: "ops", URL: "https://" + repoOps, Access: "write"},
	}

	return l
}

// Assemble and AddPart write each part's repositories, as the engine resolved them (Filed), joined
// at the part's creation in order; a part given none has none.
func testPartsWrittenWithTheirRepositories(f *fixture) {
	t := f.t

	asm, l := f.planning(t, domain.NewItem{Title: titleBig})

	ps := plan()
	ps[0].Filed = filed(repoWeb, repoAPI)
	ps[1].Filed = filed(repoOps)

	assembled, parts := f.assemble(t, assembly(asm, l, ps))

	want := map[string][]domain.ItemRepository{
		keyAPI:   {{Repo: repoWeb, Joined: domain.JoinedFiling, JoinedAt: t1}, {Repo: repoAPI, Joined: domain.JoinedFiling, JoinedAt: t1, Ordinal: 1}},
		keyStore: {{Repo: repoOps, Joined: domain.JoinedFiling, JoinedAt: t1}},
	}

	for _, p := range parts.Items {
		got, w := f.itemRepositories(t, p.ID), want[p.PartKey]
		if (len(got) != 0 || len(w) != 0) && !reflect.DeepEqual(got, w) {
			t.Errorf("part %s repositories =\n%+v\nwant\n%+v", p.PartKey, got, w)
		}
	}

	added := addedPart(assembled, t1.Add(time.Minute))
	added.Part.Filed = filed(repoAPI)

	part, _, err := f.Store.AddPart(f.ctx(t), added)
	if err != nil {
		t.Fatalf("AddPart: %v", err)
	}

	if got := f.repoKeys(t, part.ID); !reflect.DeepEqual(got, []string{repoAPI}) {
		t.Errorf("added part's repositories = %v; want [%s]", got, repoAPI)
	}
}

// A start follow-up's item takes the start's repositories, else its source's that the target
// line covers: into another factory only those its entries cover.
func testStartFollowUpTakesItsSourcesRepositories(f *fixture) {
	t := f.t

	ops := f.newFactory(t)
	opsLine := repoLineOf(ops, nameOps, f.factory)
	opsLine.Runtime.Repositories = opsLine.Runtime.Repositories[1:]
	lines := []domain.Line{repoLineOf(f.factory, nameDev), opsLine}

	src := f.create(t, domain.NewItem{Title: titleSrc, Repositories: filed(repoWeb, repoOps)})
	f.keepNotes(t, src,
		startFollowUp("inherits", domain.FollowUpPlan{Title: "Inherits"}),
		startFollowUp("names", domain.FollowUpPlan{Title: "Names", Repositories: []string{"acme/api"}}),
		startFollowUp("into ops", domain.FollowUpPlan{Title: "Into ops", Factory: nameOps}),
	)
	f.land(t, src, domain.StateDone, tLanded)

	want := map[string][]string{"inherits": {repoWeb, repoOps}, "names": {repoAPI}, "into ops": {repoOps}}

	for _, fu := range f.sweep(t, lines, 10) {
		if fu.State != domain.FollowUpFiled || fu.Target != domain.FollowUpStart {
			t.Errorf("%s = %+v; want filed as an item", fu.Text, fu)

			continue
		}

		if got := f.repoKeys(t, fu.FiledItem); !reflect.DeepEqual(got, want[fu.Text]) {
			t.Errorf("%s: repositories = %v; want %v", fu.Text, got, want[fu.Text])
		}
	}
}

// A triage item is filed with its sources' repositories; a source joining it adds those it
// lacks, joined filing with the next ordinal, until it has MaxItemRepositories, and the rest are
// left out without an error.
func testTriageGainsJoiningSourcesRepositories(f *fixture) {
	t := f.t
	lines := []domain.Line{repoLineOf(f.factory, nameDev)}

	land := func(title string, repos ...string) {
		src := f.create(t, domain.NewItem{Title: title, Repositories: filed(repos...)})
		f.keepNotes(t, src, triageFollowUp("From "+title))
		f.land(t, src, domain.StateDone, tLanded)
		f.sweep(t, lines, 10)
	}

	land("a", repoWeb)

	tri := f.triageItems(t, f.factory)
	if len(tri) != 1 {
		t.Fatalf("triage items = %+v; want one", tri)
	}

	if got := f.repoKeys(t, tri[0].ID); !reflect.DeepEqual(got, []string{repoWeb}) {
		t.Errorf("triage repositories = %v; want [%s]", got, repoWeb)
	}

	many := []string{repoWeb}
	for i := range domain.MaxItemRepositories - 1 {
		many = append(many, fmt.Sprintf("github.com/acme/r%d", i))
	}

	land("b", many...)
	land("c", repoAPI)

	rows := f.itemRepositories(t, tri[0].ID)
	if got := domain.ItemRepositoryKeys(rows); !reflect.DeepEqual(got, many) {
		t.Errorf("triage repositories after joins = %v; want %v, c's left out", got, many)
	}

	for i, r := range rows {
		if r.Ordinal != i || r.Joined != domain.JoinedFiling || (i > 0 && !r.JoinedAt.Equal(tSweep)) {
			t.Errorf("row %d = %+v; want ordinal %d joined filing, at the sweep after the first", i, r, i)
		}
	}
}
