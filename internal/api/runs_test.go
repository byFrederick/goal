package api_test

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// runModel is the model of the runs the tests record.
const runModel = "claude-sonnet-5-5"

// startRun records a run of the backend agent on it at the build station, on version.
func (h *harness) startRun(t *testing.T, it domain.Item, version domain.DefinitionVersionID, at time.Time) domain.RunID {
	t.Helper()

	id, err := h.store.StartRun(inOrg(t), ports.NewRun{
		FactoryID: lineID, DefinitionVersionID: version, ItemID: it.ID, Station: stBuild, Agent: agBackend,
		Model: runModel, StartedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}

	return id
}

// lineV1 is the line factory's published version 1.
func (h *harness) lineV1(t *testing.T) domain.DefinitionVersion {
	t.Helper()

	v, err := h.defs.VersionByNumber(inOrg(t), lineID, 1)
	if err != nil {
		t.Fatal(err)
	}

	return v
}

func TestAPI_RunsOfAnItem(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	v1 := h.lineV1(t)
	h.seed(t, stBuild, domain.StateQueued, t0)
	it := h.claim(t, 0)

	first := h.startRun(t, it, v1.ID, t0.Add(time.Minute))
	ended := t0.Add(5 * time.Minute)
	res := domain.RunResult{
		Status: domain.RunFailed, CostUSD: 1.25, Turns: 14, InputTokens: 900, OutputTokens: 300, Error: "output failed its checks",
	}

	if err := h.store.FinishRun(inOrg(t), first, res, ended); err != nil {
		t.Fatal(err)
	}

	second := h.startRun(t, it, "", t0.Add(6*time.Minute)) // a run from before versions, still running
	version, versionID, failure := v1.Number, string(v1.ID), res.Error
	wantFirst := gen.Run{
		Id: string(first), ItemId: string(it.ID), Station: stBuild, Agent: agBackend, Model: runModel,
		Status: gen.RunStatus(domain.RunFailed), Turns: 14, InputTokens: 900, OutputTokens: 300, CostUsd: 1.25,
		Error: &failure, StartedAt: t0.Add(time.Minute), EndedAt: &ended, DefinitionVersionId: &versionID,
		DefinitionVersion: &version,
	}
	wantSecond := gen.Run{
		Id: string(second), ItemId: string(it.ID), Station: stBuild, Agent: agBackend, Model: runModel,
		Status: gen.RunStatus(domain.RunRunning), StartedAt: t0.Add(6 * time.Minute),
	}

	var list gen.RunList
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/line%231/runs", "", &list); code != http.StatusOK {
		t.Fatalf("list runs = %d", code)
	}

	if got, want := asJSON(t, list), asJSON(t, gen.RunList{Runs: []gen.Run{wantFirst, wantSecond}}); got != want {
		t.Errorf("runs =\n  %s\nwant oldest first\n  %s", got, want)
	}

	h.seam.steps()

	var run gen.Run
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/runs/"+string(first), "", &run); code != http.StatusOK ||
		asJSON(t, run) != asJSON(t, wantFirst) {
		t.Errorf("get run = %d %s, want %s", code, asJSON(t, run), asJSON(t, wantFirst))
	}

	if steps := h.seam.steps(); !slices.ContainsFunc(steps, func(s step) bool {
		return s.access != nil && s.access.Operation == "getRun" && s.access.Factory == lineID
	}) {
		t.Errorf("get run: seam steps %+v, want the run's factory authorized", steps)
	}

	for _, path := range []string{
		"/v1/orgs/acme/runs/00000000-0000-7000-8000-ffffffffffff", // no such run
		"/v1/orgs/acme/runs/not-a-run",
		"/v1/orgs/example-labs/runs/" + string(first), // a run of another organization
		"/v1/orgs/example-labs/items/" + string(it.ID) + "/runs",
		"/v1/orgs/acme/items/line%2399/runs",
	} {
		if code := h.call(t, http.MethodGet, path, "", nil); code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, code)
		}
	}
}

func TestAPI_RunsOfAnItemAreCappedAt200(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	v1 := h.lineV1(t)
	it := h.seed(t, stBuild, domain.StateQueued, t0)

	for i := range 201 {
		h.startRun(t, it, v1.ID, t0.Add(time.Duration(i)*time.Second))
	}

	var list gen.RunList
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/line%231/runs", "", &list); code != http.StatusOK {
		t.Fatalf("list runs = %d", code)
	}

	if !list.Truncated || len(list.Runs) != 200 || !list.Runs[0].StartedAt.Equal(t0) {
		t.Errorf("runs: %d, truncated %v; want the oldest 200 and truncated", len(list.Runs), list.Truncated)
	}
}

// asJSON encodes v, so contract values compare by what a client reads.
func asJSON(t *testing.T, v any) string {
	t.Helper()

	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

const orgRunsPath = "/v1/orgs/acme/runs"

// orgRun records a run of agent on it, started at the given time and finished with res unless
// res is running.
func (h *harness) orgRun(t *testing.T, it domain.Item, agent string, at time.Time, res domain.RunResult) domain.RunID {
	t.Helper()

	id, err := h.store.StartRun(inOrg(t), ports.NewRun{
		FactoryID: it.FactoryID, ItemID: it.ID, Station: it.Station, Agent: agent, Model: runModel, StartedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}

	if res.Status != domain.RunRunning {
		if err := h.store.FinishRun(inOrg(t), id, res, at.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}

	return id
}

// seedGuide creates an item titled title on acme.
func (h *harness) seedGuide(t *testing.T, title string) domain.Item {
	t.Helper()

	it, err := h.store.CreateItem(inOrg(t), domain.NewItem{
		FactoryID: acmeID, Type: typeFeature, Title: title, Station: stInbox,
		State: domain.StateQueued, Class: domain.ClassStandard, CreatedAt: t0,
	})
	if err != nil {
		t.Fatal(err)
	}

	return it
}

// runIDs are the ids of a page's runs, in order.
func runIDs(list gen.OrgRunList) []domain.RunID {
	out := []domain.RunID{}
	for _, r := range list.Runs {
		out = append(out, domain.RunID(r.Id))
	}

	return out
}

// sameRunSummary checks a run list's summary, which cents of float sums leave exact here.
func sameRunSummary(t *testing.T, got, want gen.RunSummary) {
	t.Helper()

	if asJSON(t, got) != asJSON(t, want) {
		t.Errorf("summary =\n  %s\nwant\n  %s", asJSON(t, got), asJSON(t, want))
	}
}

// spendOf renders a summary's spend by factory as "name=usd" pairs.
func spendOf(s gen.RunSummary) []string {
	out := []string{}
	for _, f := range s.SpendByFactory {
		out = append(out, fmt.Sprintf("%s=%.4f", f.Name, f.SpendUsd))
	}

	return out
}

func TestAPI_ListRuns(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	build := h.seedTitled(t, "Fix the backend build")
	rename := h.seedTitled(t, "Rename the flag")
	guide := h.seedGuide(t, "Write the user guide")

	ok := h.orgRun(t, build, agBackend, t0.Add(time.Minute), domain.RunResult{Status: domain.RunSucceeded, CostUSD: 0.5, Turns: 3})
	failed := h.orgRun(t, build, agBackend, t0.Add(2*time.Minute), domain.RunResult{Status: domain.RunFailed, CostUSD: 0.25, Error: "checks failed"})
	running := h.orgRun(t, guide, evalAgent, t0.Add(3*time.Minute), domain.RunResult{Status: domain.RunRunning})
	// The day before the clock's day (t0 is 09:00 UTC).
	yesterday := h.orgRun(t, rename, agBackend, t0.Add(-10*time.Hour), domain.RunResult{Status: domain.RunCancelled, CostUSD: 1})

	list := decode[gen.OrgRunList](t, h.request(t, http.MethodGet, orgRunsPath, "", nil), http.StatusOK)
	if got, want := runIDs(list), []domain.RunID{running, failed, ok}; !slices.Equal(got, want) {
		t.Errorf("runs of today = %v; want %v, newest first", got, want)
	}

	if list.NextCursor != nil {
		t.Errorf("next_cursor = %q; want null on the last page", *list.NextCursor)
	}

	sameRunSummary(t, list.Summary, gen.RunSummary{
		Total: 3, Running: 1, Failed: 1, Finished: 1, SpendUsd: 0.75,
		SpendByFactory: []gen.FactorySpend{{Id: lineID, Name: lineSlug, SpendUsd: 0.75}, {Id: acmeID, Name: acmeSlug}},
	})

	failure := "checks failed"
	ended := t0.Add(3 * time.Minute)
	row := list.Runs[1]
	want := gen.OrgRun{
		Id: string(failed), ItemId: string(build.ID), Station: stBuild, Agent: agBackend, Model: runModel,
		Status: gen.RunStatus(domain.RunFailed), CostUsd: 0.25, Error: &failure, StartedAt: t0.Add(2 * time.Minute), EndedAt: &ended,
		Factory: gen.FactoryHit{Id: lineID, Name: lineSlug, Title: row.Factory.Title},
		Item:    gen.RunItem{Id: string(build.ID), Ref: lineItemRef, Title: "Fix the backend build"},
	}

	if got := asJSON(t, row); got != asJSON(t, want) {
		t.Errorf("the failed run =\n  %s\nwant\n  %s", got, asJSON(t, want))
	}

	if row.Factory.Title == "" || strings.Contains(asJSON(t, row), "slot") {
		t.Errorf("the failed run = %s; want a factory title and no slot", asJSON(t, row))
	}

	since := url.QueryEscape(t0.Add(-24 * time.Hour).Format(time.RFC3339))

	for _, c := range []struct {
		name, query string
		want        []domain.RunID
		total       int
	}{
		{"by status", "status=failed", []domain.RunID{failed}, 3},
		{"by agent", "q=WRITER", []domain.RunID{running}, 3},
		{"by title", "q=backend+build", []domain.RunID{failed, ok}, 3},
		{"by ref", "q=line%231", []domain.RunID{failed, ok}, 3},
		{"by number", "q=%232&since=" + since, []domain.RunID{yesterday}, 4},
		{"since", "since=" + since, []domain.RunID{running, failed, ok, yesterday}, 4},
		{"until, excluded", "until=" + url.QueryEscape(t0.Add(2*time.Minute).Format(time.RFC3339)), []domain.RunID{ok}, 1},
		{"one factory by name", "factory=acme", []domain.RunID{running}, 1},
		{"one factory by id", "factory=" + lineID, []domain.RunID{failed, ok}, 2},
	} {
		got := decode[gen.OrgRunList](t, h.request(t, http.MethodGet, orgRunsPath+"?"+c.query, "", nil), http.StatusOK)
		if ids := runIDs(got); !slices.Equal(ids, c.want) || got.Summary.Total != c.total {
			t.Errorf("%s: runs %v, summary total %d; want %v and %d", c.name, ids, got.Summary.Total, c.want, c.total)
		}
	}
}

func TestAPI_ListRunsPagesByCursor(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	it := h.seedTitled(t, "Fix the backend build")

	var want []domain.RunID
	// Two runs share each start, so the cursor must order by id too.
	for i := range 5 {
		want = append(want, h.orgRun(t, it, agBackend, t0.Add(time.Duration(i/2)*time.Minute), domain.RunResult{Status: domain.RunSucceeded}))
	}

	slices.SortFunc(want, func(a, b domain.RunID) int {
		ra, _ := h.store.Run(inOrg(t), a)
		rb, _ := h.store.Run(inOrg(t), b)

		return cmp.Or(rb.StartedAt.Compare(ra.StartedAt), cmp.Compare(b, a))
	})

	var (
		got   []domain.RunID
		pages int
	)

	for path := orgRunsPath + "?limit=2"; ; pages++ {
		list := decode[gen.OrgRunList](t, h.request(t, http.MethodGet, path, "", nil), http.StatusOK)
		got = append(got, runIDs(list)...)

		if list.Summary.Total != 5 {
			t.Errorf("page %d: summary total %d; want 5 on every page", pages, list.Summary.Total)
		}

		if list.NextCursor == nil {
			break
		}

		if pages > 5 {
			t.Fatal("next_cursor never ends")
		}

		path = orgRunsPath + "?limit=2&cursor=" + url.QueryEscape(*list.NextCursor)
	}

	if !slices.Equal(got, want) || pages != 2 {
		t.Errorf("paged by 2: %v in %d pages; want %v in 3", got, pages+1, want)
	}
}

func TestAPI_ListRunsRefusesBadParameters(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for _, c := range []struct {
		query string
		want  int
	}{
		{"limit=0", http.StatusBadRequest},
		{"limit=201", http.StatusBadRequest},
		{"limit=200", http.StatusOK},
		{"cursor=not-a-cursor", http.StatusBadRequest},
		{"since=" + url.QueryEscape(t0.Format(time.RFC3339)) + "&until=" + url.QueryEscape(t0.Format(time.RFC3339)), http.StatusBadRequest},
		{"q=" + strings.Repeat("x", 201), http.StatusBadRequest},
		{"factory=nope", http.StatusNotFound},
	} {
		if res := h.request(t, http.MethodGet, orgRunsPath+"?"+c.query, "", nil); res.status != c.want {
			t.Errorf("GET ?%s = %d %s; want %d", c.query, res.status, res.body, c.want)
		}
	}
}

// A viewer of acme alone sees neither line's runs nor its spend; its owner sees both.
func TestAPI_ListRunsHidesFactoriesTheCallerCannotRead(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)
	h.orgRun(t, h.seedTitled(t, "Fix the backend build"), agBackend, t0.Add(time.Minute), domain.RunResult{Status: domain.RunSucceeded, CostUSD: 2})
	guide := h.orgRun(t, h.seedGuide(t, "Write the user guide"), evalAgent, t0.Add(2*time.Minute), domain.RunResult{Status: domain.RunFailed, CostUSD: 1})

	viewer := decode[gen.OrgRunList](t, h.as(t, viewerToken, http.MethodGet, orgRunsPath, ""), http.StatusOK)
	if got := runIDs(viewer); !slices.Equal(got, []domain.RunID{guide}) || viewer.Summary.Total != 1 ||
		viewer.Summary.SpendUsd != 1 || !slices.Equal(spendOf(viewer.Summary), []string{"acme=1.0000"}) {
		t.Errorf("the viewer sees %v and %+v; want acme's run and spend alone", got, viewer.Summary)
	}

	if res := h.as(t, viewerToken, http.MethodGet, orgRunsPath+"?factory=line", ""); res.status != http.StatusNotFound {
		t.Errorf("the viewer naming line = %d; want 404, as for an unknown factory", res.status)
	}

	owner := decode[gen.OrgRunList](t, h.as(t, ownerToken, http.MethodGet, orgRunsPath, ""), http.StatusOK)
	if len(owner.Runs) != 2 || owner.Summary.Total != 2 || !slices.Equal(spendOf(owner.Summary), []string{"line=2.0000", "acme=1.0000"}) {
		t.Errorf("the owner sees %v and %+v; want both factories", runIDs(owner), owner.Summary)
	}
}
