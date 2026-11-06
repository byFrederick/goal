package storetest

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// repositoriesTests cover items' branches and repositories (ADR 0041 decisions 4, 5 and 6).
var repositoriesTests = []struct {
	name string
	test func(f *fixture)
}{
	{"CreateItem_StoresBranchAndRepositories", testCreateItemStoresBranchAndRepositories},
	{"ItemRepositories_InJoinOrder", testItemRepositoriesInJoinOrder},
	{"ItemsRepositories_OnePageAtOnce", testItemsRepositoriesOnePageAtOnce},
	{"ItemRepositories_PartsWrittenWithTheirs", testPartsWrittenWithTheirRepositories},
	{"ItemRepositories_StartFollowUpTakesItsSources", testStartFollowUpTakesItsSourcesRepositories},
	{"ItemRepositories_TriageGainsJoiningSourcesUpToTheCap", testTriageGainsJoiningSourcesRepositories},
	{"OpenItemRepositories_OnlyOpenItems", testOpenItemRepositoriesOnlyOpenItems},
	{"ItemRepositories_JoinAppendsUpToTheCap", testJoinItemRepositoriesAppendsUpToTheCap},
	{"ItemRepositories_PushedJoinAtFinish", testPushedRepositoriesJoinAtFinish},
	{"ItemRepositories_ReadOnlyStaysOnRun", testReadOnlyRepositoriesStayOnRun},
	{"ItemRepositories_PushPastCapStaysOnRun", testPushPastCapStaysOnRun},
	{"IntegrationBranch_PerRepositorySetOnce", testIntegrationBranchPerRepositorySetOnce},
	{"IntegrationBranch_PerRepositoryUpToTheCap", testIntegrationBranchPerRepositoryUpToTheCap},
	{"IntegrationBranch_LandingTakesThePartsRepositories", testIntegrationBranchLandingTakesThePartsRepositories},
	{"Transition_JoinsOutputRepositories", testTransitionJoinsOutputRepositories},
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

// ItemsRepositories reads a page of items' repositories in one call, each item's in first-joined
// order as ItemRepositories reads them; an item without rows, an unknown id and an id that is not
// a UUID are left out, and another organization reads none of them.
func testItemsRepositoriesOnePageAtOnce(f *fixture) {
	t := f.t

	two := f.create(t, domain.NewItem{Repositories: []domain.ItemRepository{
		{Repo: repoOps, Joined: domain.JoinedDefault},
		{Repo: repoWeb, Joined: domain.JoinedFiling},
	}})
	one := f.create(t, domain.NewItem{Repositories: filed(repoAPI)})
	none := f.create(t, domain.NewItem{})
	ids := []domain.ItemID{two.ID, one.ID, none.ID, missingItem, "not-a-uuid"}

	got, err := f.Store.ItemsRepositories(f.ctx(t), ids)
	if err != nil {
		t.Fatalf("ItemsRepositories: %v", err)
	}

	keys := map[domain.ItemID][]string{}
	for id, rows := range got {
		keys[id] = domain.ItemRepositoryKeys(rows)
	}

	want := map[domain.ItemID][]string{two.ID: {repoWeb, repoOps}, one.ID: {repoAPI}}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("ItemsRepositories = %v; want %v", keys, want)
	}

	if !reflect.DeepEqual(got[two.ID], f.itemRepositories(t, two.ID)) {
		t.Errorf("ItemsRepositories of %s = %+v; want what ItemRepositories reads, %+v", two.ID, got[two.ID], f.itemRepositories(t, two.ID))
	}

	o := f.otherOrg(t)
	if other, err := o.Store.ItemsRepositories(o.ctx(t), ids); err != nil || len(other) != 0 {
		t.Errorf("another organization's ItemsRepositories = %v, %v; want none", other, err)
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
		{Name: "all", URL: "https://github.com/acme/*", Access: "write"},
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

// OpenItemRepositories lists the repositories of the factory's open items, one use per item and
// repository, by repository then item number: not a done or cancelled item's, another factory's
// or another organization's.
func testOpenItemRepositoriesOnlyOpenItems(f *fixture) {
	t := f.t

	filed := func(repos ...string) []domain.ItemRepository {
		out := make([]domain.ItemRepository, 0, len(repos))
		for _, r := range repos {
			out = append(out, domain.ItemRepository{Repo: r, Joined: domain.JoinedFiling})
		}

		return out
	}

	a := f.create(t, domain.NewItem{Repositories: filed(repoWeb, repoAPI)})
	b := f.create(t, domain.NewItem{Repositories: filed(repoAPI)})
	f.land(t, f.create(t, domain.NewItem{Repositories: filed(repoOps)}), domain.StateDone, t1)
	f.cancel(t, f.create(t, domain.NewItem{Repositories: filed(repoOps)}), t1)
	f.create(t, domain.NewItem{FactoryID: f.newFactory(t), Repositories: filed(repoOps)})
	f.create(t, domain.NewItem{})

	got, err := f.Store.OpenItemRepositories(f.ctx(t), f.factory)
	if err != nil {
		t.Fatalf("OpenItemRepositories: %v", err)
	}

	want := []domain.RepositoryUse{
		{Repo: repoAPI, Item: a.ID, Number: a.Number},
		{Repo: repoAPI, Item: b.ID, Number: b.Number},
		{Repo: repoWeb, Item: a.ID, Number: a.Number},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("uses =\n%+v\nwant\n%+v", got, want)
	}

	if got, err := f.Store.OpenItemRepositories(domain.WithOrg(t.Context(), f.NewOrg(t)), f.factory); err != nil || len(got) != 0 {
		t.Errorf("another organization's OpenItemRepositories = %+v, %v; want none", got, err)
	}
}

// JoinItemRepositories appends the keys an item lacks, joined as given with the next ordinals,
// until it has MaxItemRepositories, and leaves the rest out without an error; an unknown item
// and another organization's are ErrNotFound.
func testJoinItemRepositoriesAppendsUpToTheCap(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{Title: "Old item"})
	join := func(keys ...string) {
		t.Helper()

		if err := f.Store.JoinItemRepositories(f.ctx(t), it.ID, domain.JoinedDefault, keys, t1); err != nil {
			t.Fatalf("JoinItemRepositories: %v", err)
		}
	}

	join(repoWeb)
	join(repoWeb, repoAPI)

	want := []domain.ItemRepository{
		{Repo: repoWeb, Joined: domain.JoinedDefault, JoinedAt: t1},
		{Repo: repoAPI, Joined: domain.JoinedDefault, JoinedAt: t1, Ordinal: 1},
	}
	if got := f.itemRepositories(t, it.ID); !reflect.DeepEqual(got, want) {
		t.Errorf("repositories =\n%+v\nwant\n%+v", got, want)
	}

	many := make([]string, 0, domain.MaxItemRepositories)
	for i := range domain.MaxItemRepositories {
		many = append(many, fmt.Sprintf("github.com/acme/r%d", i))
	}

	join(many...)

	if got := f.itemRepositories(t, it.ID); len(got) != domain.MaxItemRepositories || got[2].Repo != many[0] {
		t.Errorf("after joining past the cap = %v; want %d, the first two kept", domain.ItemRepositoryKeys(got), domain.MaxItemRepositories)
	}

	err := f.Store.JoinItemRepositories(f.ctx(t), missingItem, domain.JoinedDefault, []string{repoWeb}, t1)
	expect(t, "JoinItemRepositories of an unknown item", err, ports.ErrNotFound)

	err = f.Store.JoinItemRepositories(f.otherOrg(t).ctx(t), it.ID, domain.JoinedDefault, []string{repoOps}, t1)
	expect(t, "JoinItemRepositories of another org's item", err, ports.ErrNotFound)
}

// pushedRun starts a run of item at t1 that checked out in's repositories and finishes it at
// endedAt with pushed.
func (f *fixture) pushedRun(t *testing.T, item domain.ItemID, in, pushed []domain.RunRepository, endedAt time.Time) domain.Run {
	t.Helper()

	id := f.startRun(t, item, t1)
	if err := f.Store.RecordRunInput(f.ctx(t), id, domain.RunInput{Repositories: in}); err != nil {
		t.Fatalf("RecordRunInput: %v", err)
	}

	if err := f.Store.FinishRun(f.ctx(t), id, domain.RunResult{Status: domain.RunSucceeded, Repositories: pushed}, endedAt); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}

	run, err := f.Store.Run(f.ctx(t), id)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	return run
}

// runRepos are the repositories of a run's rows, in the order recorded.
func runRepos(rs []domain.RunRepository) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Repo
	}

	return out
}

// A run that pushes a change to a second repository adds it to its item as joined push, at the
// run's end, with the next ordinal; the item's first repository stays first.
func testPushedRepositoriesJoinAtFinish(f *fixture) {
	t := f.t
	ended := t1.Add(time.Hour)

	it := f.create(t, domain.NewItem{Title: "Push", Repositories: filed(repoAPI)})
	f.pushedRun(t, it.ID,
		[]domain.RunRepository{{Repo: repoAPI, BranchHead: "a1"}, {Repo: repoWeb, BranchHead: "w1"}},
		[]domain.RunRepository{{Repo: repoAPI, PushedHead: "a2"}, {Repo: repoWeb, PushedHead: "w2"}}, ended)

	rows := f.itemRepositories(t, it.ID)

	want := []domain.ItemRepository{
		{Repo: repoAPI, Joined: domain.JoinedFiling, JoinedAt: t0, Ordinal: 0},
		{Repo: repoWeb, Joined: domain.JoinedPush, JoinedAt: ended, Ordinal: 1},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("repositories =\n%+v\nwant\n%+v", rows, want)
	}

	if first, ok := domain.ItemRepositories(rows).First(); !ok || first.Repo != repoAPI {
		t.Errorf("first = %+v, %t; want %s", first, ok, repoAPI)
	}
}

// A repository the run fetched and did not write, one it pushed its branch head back to, and a
// migrated row without a repository join no item; the run keeps their rows.
func testReadOnlyRepositoriesStayOnRun(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{Title: "Read", Repositories: filed(repoAPI)})
	run := f.pushedRun(t, it.ID,
		[]domain.RunRepository{{Repo: repoAPI, BranchHead: "a1"}, {Repo: repoWeb, BranchHead: "w1"}, {Repo: repoOps, BranchHead: "o1"}},
		[]domain.RunRepository{{Repo: repoWeb, PushedHead: "w1"}, {Repo: repoOps}, {PushedHead: "x1", Wrote: true}}, t1.Add(time.Hour))

	if got := f.repoKeys(t, it.ID); !reflect.DeepEqual(got, []string{repoAPI}) {
		t.Errorf("repositories = %v; want only [%s]", got, repoAPI)
	}

	if got, want := runRepos(run.Input.Repositories), []string{repoAPI, repoWeb, repoOps, ""}; !reflect.DeepEqual(got, want) {
		t.Errorf("run's repositories = %q; want %q", got, want)
	}
}

// An item that has MaxItemRepositories gains none at finish, and the run keeps its row for the
// one past the cap; a Jonah run, which works no item, joins nothing and finishes.
func testPushPastCapStaysOnRun(f *fixture) {
	t := f.t

	many := make([]string, 0, domain.MaxItemRepositories)
	for i := range domain.MaxItemRepositories {
		many = append(many, fmt.Sprintf("github.com/acme/r%d", i))
	}

	it := f.create(t, domain.NewItem{Title: "Full", Repositories: filed(many...)})
	run := f.pushedRun(t, it.ID, []domain.RunRepository{{Repo: repoOps, BranchHead: "o1"}},
		[]domain.RunRepository{{Repo: repoOps, PushedHead: "o2"}}, t1.Add(time.Hour))

	if got := f.repoKeys(t, it.ID); !reflect.DeepEqual(got, many) {
		t.Errorf("repositories = %v; want the %d filed", got, domain.MaxItemRepositories)
	}

	if !slices.ContainsFunc(run.Result.Repositories, func(r domain.RunRepository) bool { return r.Repo == repoOps && r.Wrote }) {
		t.Errorf("run's pushed repositories = %+v; want %s, wrote", run.Result.Repositories, repoOps)
	}

	jonah, err := f.Store.StartRun(f.ctx(t), ports.NewRun{FactoryID: f.factory, Agent: agJonah, StartedAt: t1, Kind: domain.RunKindJonah})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	res := domain.RunResult{Status: domain.RunSucceeded, Repositories: []domain.RunRepository{{Repo: repoOps, PushedHead: "o3", Wrote: true}}}
	if err := f.Store.FinishRun(f.ctx(t), jonah, res, t1.Add(time.Hour)); err != nil {
		t.Errorf("FinishRun of a Jonah run: %v", err)
	}
}

// A transition's repositories join the item, joined output at its At with the next ordinals, in
// the transition's transaction: those the item lacks, once each. A transition that fails its
// guard joins none.
func testTransitionJoinsOutputRepositories(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{Repositories: filed(repoAPI)})
	queued := domain.Position{Station: it.Station, State: domain.StateQueued}

	tr := ports.Transition{ItemID: it.ID, From: queued, To: queued, At: t1, Repositories: []string{repoAPI, repoWeb, repoOps, repoWeb}}
	if _, err := f.Store.Transition(f.ctx(t), tr); err != nil {
		t.Fatalf("Transition: %v", err)
	}

	want := []domain.ItemRepository{
		{Repo: repoAPI, Joined: domain.JoinedFiling, JoinedAt: it.CreatedAt},
		{Repo: repoWeb, Joined: domain.JoinedOutput, JoinedAt: t1, Ordinal: 1},
		{Repo: repoOps, Joined: domain.JoinedOutput, JoinedAt: t1, Ordinal: 2},
	}
	if got := f.itemRepositories(t, it.ID); !reflect.DeepEqual(got, want) {
		t.Errorf("repositories =\n%+v\nwant\n%+v", got, want)
	}

	stale := ports.Transition{
		ItemID: it.ID, From: domain.Position{Station: it.Station, State: domain.StateInProgress}, To: queued, At: t1,
		Repositories: []string{"github.com/acme/docs"},
	}
	_, err := f.Store.Transition(f.ctx(t), stale)
	expect(t, "Transition from where the item is not", err, ports.ErrConflict)

	if got := f.repoKeys(t, it.ID); len(got) != len(want) {
		t.Errorf("after a refused transition = %v; want %d", got, len(want))
	}
}
