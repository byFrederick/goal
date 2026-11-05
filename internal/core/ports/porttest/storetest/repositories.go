package storetest

import (
	"reflect"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
)

// repositoriesTests cover items' branches and repositories (ADR 0041 decisions 4 and 6).
var repositoriesTests = []struct {
	name string
	test func(f *fixture)
}{
	{"CreateItem_StoresBranchAndRepositories", testCreateItemStoresBranchAndRepositories},
	{"ItemRepositories_InJoinOrder", testItemRepositoriesInJoinOrder},
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
