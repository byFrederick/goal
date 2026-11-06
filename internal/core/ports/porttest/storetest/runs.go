package storetest

import (
	"cmp"
	"encoding/json"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// orgRun records a run of agent with model on it, started at the given time and finished with
// res unless res is running.
func (f *fixture) orgRun(t *testing.T, it domain.Item, agent, runModel string, at time.Time, res domain.RunResult) domain.RunID {
	t.Helper()

	id, err := f.Store.StartRun(f.ctx(t), ports.NewRun{
		FactoryID: it.FactoryID, ItemID: it.ID, Station: stWork, Agent: agent, Model: runModel, StartedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}

	if res.Status != domain.RunRunning {
		if err := f.Store.FinishRun(f.ctx(t), id, res, at.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}

	return id
}

func testOrgRuns(f *fixture) {
	t := f.t

	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	since, until := t0, t0.Add(24*time.Hour)
	second, third := f.newFactory(t), f.newFactory(t)

	login := f.create(t, domain.NewItem{Title: "Fix the Login page", CreatedAt: t0})
	refactor := f.create(t, domain.NewItem{Title: "Refactor", CreatedAt: t0})
	billing := f.create(t, domain.NewItem{FactoryID: second, Title: "Billing at 50%", CreatedAt: t0})
	outside := f.create(t, domain.NewItem{FactoryID: third, Title: "Fix the Login page too", CreatedAt: t0})

	if login.Number != 1 || billing.Number != 1 {
		t.Fatalf("login and billing are numbers %d and %d; want 1 in each factory", login.Number, billing.Number)
	}

	done := func(s domain.RunStatus, usd float64) domain.RunResult {
		return domain.RunResult{Status: s, CostUSD: usd}
	}
	edge := f.orgRun(t, refactor, agWorker, model, since, done(domain.RunSucceeded, 1))
	loginOK := f.orgRun(t, login, agWorker, model, at(1), done(domain.RunSucceeded, 0.25))
	loginFailed := f.orgRun(t, login, agBackend, "model-b", at(2), done(domain.RunFailed, 0.5))
	running := f.orgRun(t, billing, agWorker, model, at(2), done(domain.RunRunning, 0))
	cancelled := f.orgRun(t, refactor, agWorker, model, at(3), done(domain.RunCancelled, 0.125))

	// Outside the window, or in a factory not asked for, or in another organization.
	f.orgRun(t, login, agWorker, model, since.Add(-time.Microsecond), done(domain.RunSucceeded, 8))
	f.orgRun(t, login, agWorker, model, until, done(domain.RunSucceeded, 8))
	f.orgRun(t, outside, agWorker, model, at(4), done(domain.RunSucceeded, 8))

	other := f.otherOrg(t)
	other.orgRun(t, other.create(t, domain.NewItem{Title: "Fix the Login page", CreatedAt: t0}), agWorker, model, at(4), done(domain.RunSucceeded, 8))

	// The two runs that started at the same time are ordered by id, descending (PostgreSQL
	// orders uuids as their canonical text).
	tie := []domain.RunID{loginFailed, running}
	slices.Sort(tie)
	slices.Reverse(tie)

	list := func(q ports.RunQuery) ([]domain.ListedRun, domain.RunSummary) {
		t.Helper()

		q.Since, q.Until = since, until
		if q.Factories == nil {
			q.Factories = []domain.FactoryID{f.factory, second, other.factory}
		}

		runs, sum, err := f.Store.OrgRuns(f.ctx(t), q)
		if err != nil {
			t.Fatal(err)
		}

		return runs, sum
	}

	all := []domain.RunID{cancelled, tie[0], tie[1], loginOK, edge}
	byFactory := []domain.FactorySpend{{FactoryID: f.factory, USD: 1.875}, {FactoryID: second, USD: 0}}
	slices.SortFunc(byFactory, func(a, b domain.FactorySpend) int { return cmp.Compare(a.FactoryID, b.FactoryID) })
	// The summary ignores the status, the text and the cursor; only the factories bound it.
	every := domain.RunSummary{Total: 5, Running: 1, Failed: 1, Finished: 3, SpendUSD: 1.875, ByFactory: byFactory}
	ofSecond := domain.RunSummary{Total: 1, Running: 1, ByFactory: []domain.FactorySpend{{FactoryID: second}}}

	tests := []struct {
		name string
		q    ports.RunQuery
		want []domain.RunID
		sum  domain.RunSummary
	}{
		{"every run, newest first", ports.RunQuery{}, all, every},
		{"by status", ports.RunQuery{Status: domain.RunFailed}, []domain.RunID{loginFailed}, every},
		{"title, case-insensitive", ports.RunQuery{Text: "LOGIN"}, []domain.RunID{loginFailed, loginOK}, every},
		{"agent", ports.RunQuery{Text: "Backend-Eng"}, []domain.RunID{loginFailed}, every},
		{"model", ports.RunQuery{Text: "model-b"}, []domain.RunID{loginFailed}, every},
		{"% matches literally", ports.RunQuery{Text: "50%"}, []domain.RunID{running}, every},
		{"_ matches literally", ports.RunQuery{Text: "model_b"}, []domain.RunID{}, every},
		{"the item's number", ports.RunQuery{Text: "#9999", Number: refactor.Number}, []domain.RunID{cancelled, edge}, every},
		{"a number in every factory", ports.RunQuery{Text: "#1", Number: 1}, []domain.RunID{tie[0], tie[1], loginOK}, every},
		{"a number in one factory", ports.RunQuery{Text: "x#1", Number: 1, NumberOf: second}, []domain.RunID{running}, every},
		{"text and status", ports.RunQuery{Text: "login", Status: domain.RunSucceeded}, []domain.RunID{loginOK}, every},
		{"one factory", ports.RunQuery{Factories: []domain.FactoryID{second}}, []domain.RunID{running}, ofSecond},
		{"no factory", ports.RunQuery{Factories: []domain.FactoryID{}}, []domain.RunID{}, domain.RunSummary{}},
		{"limited", ports.RunQuery{Limit: 1}, []domain.RunID{cancelled}, every},
		{"after a cursor", ports.RunQuery{After: &ports.RunCursor{StartedAt: at(2), ID: tie[0]}}, []domain.RunID{tie[1], loginOK, edge}, every},
	}

	for _, tt := range tests {
		if tt.q.Limit == 0 {
			tt.q.Limit = 20
		}

		runs, sum := list(tt.q)
		if got := listedIDs(runs); !slices.Equal(got, tt.want) {
			t.Errorf("OrgRuns %s = %v; want %v", tt.name, got, tt.want)
		}

		sameSummary(t, tt.name, sum, tt.sum)
	}

	runs, _ := list(ports.RunQuery{Limit: 20})
	sameListedRun(t, runs, domain.ListedRun{ItemNumber: login.Number, ItemTitle: login.Title, Run: domain.Run{
		ID: loginFailed, FactoryID: f.factory, ItemID: login.ID, Station: stWork, Agent: agBackend, Model: "model-b",
		Result: domain.RunResult{Status: domain.RunFailed, CostUSD: 0.5}, StartedAt: at(2), EndedAt: at(3),
	}})

	// Following each page's last run reaches every run once.
	if paged := pageOrgRuns(list, 2); !slices.Equal(paged, all) {
		t.Errorf("OrgRuns paged by 2 = %v; want %v", paged, all)
	}
}

// pageOrgRuns lists every run limit at a time, each page after the last run of the one before.
func pageOrgRuns(list func(ports.RunQuery) ([]domain.ListedRun, domain.RunSummary), limit int) []domain.RunID {
	var out []domain.RunID

	q := ports.RunQuery{Limit: limit}

	for range 10 {
		page, _ := list(q)
		if len(page) == 0 {
			break
		}

		out = append(out, listedIDs(page)...)
		last := page[len(page)-1]
		q.After = &ports.RunCursor{StartedAt: last.StartedAt, ID: last.ID}
	}

	return out
}

func listedIDs(runs []domain.ListedRun) []domain.RunID {
	out := []domain.RunID{}
	for _, r := range runs {
		out = append(out, r.ID)
	}

	return out
}

// sameListedRun checks the run of runs with want's id against want.
func sameListedRun(t *testing.T, runs []domain.ListedRun, want domain.ListedRun) {
	t.Helper()

	i := slices.IndexFunc(runs, func(r domain.ListedRun) bool { return r.ID == want.ID })
	if i < 0 {
		t.Fatalf("OrgRuns has no run %s", want.ID)
	}

	got := runs[i]
	sameRuns(t, []domain.Run{got.Run}, []domain.Run{want.Run})

	if got.ItemNumber != want.ItemNumber || got.ItemTitle != want.ItemTitle {
		t.Errorf("OrgRuns' run %s is of item %d %q; want %d %q", want.ID, got.ItemNumber, got.ItemTitle, want.ItemNumber, want.ItemTitle)
	}
}

func sameSummary(t *testing.T, what string, got, want domain.RunSummary) {
	t.Helper()

	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

	ok := got.Total == want.Total && got.Running == want.Running && got.Failed == want.Failed &&
		got.Finished == want.Finished && near(got.SpendUSD, want.SpendUSD) && len(got.ByFactory) == len(want.ByFactory)
	for i := 0; ok && i < len(want.ByFactory); i++ {
		ok = got.ByFactory[i].FactoryID == want.ByFactory[i].FactoryID && near(got.ByFactory[i].USD, want.ByFactory[i].USD)
	}

	if !ok {
		t.Errorf("OrgRuns' summary (%s) = %+v; want %+v", what, got, want)
	}
}

// agJonah is the agent name of the suite's Jonah runs.
const agJonah = "jonah"

// testRunsJonahWithoutItem: a Jonah run works no item. It starts and finishes with its cost, in
// its organization only, and writes no item event, which PostgreSQL would refuse without an item
// (item_events.item_id is NOT NULL); a station run still needs an item that exists.
func testRunsJonahWithoutItem(f *fixture) {
	t := f.t
	t2 := t1.Add(time.Minute)

	id, err := f.Store.StartRun(f.ctx(t), ports.NewRun{
		FactoryID: f.factory, Agent: agJonah, Model: model, StartedAt: t1, Kind: domain.RunKindJonah,
	})
	if err != nil {
		t.Fatalf("StartRun of a jonah run without an item: %v", err)
	}

	res := domain.RunResult{Status: domain.RunSucceeded, CostUSD: 0.5, Turns: 4, SessionID: "s-1"}
	if err := f.Store.FinishRun(f.ctx(t), id, res, t2); err != nil {
		t.Fatalf("FinishRun of a jonah run without an item: %v", err)
	}

	got, err := f.Store.Run(f.ctx(t), id)
	want := domain.Run{ID: id, FactoryID: f.factory, Agent: agJonah, Model: model, Result: res, StartedAt: t1, EndedAt: t2}

	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	sameRuns(t, []domain.Run{got}, []domain.Run{want})

	if s, err := f.Jonah.JonahSpendSince(f.ctx(t), f.factory, t1); err != nil || s.JonahUSD != 0.5 {
		t.Errorf("JonahSpendSince = %+v, %v; want 0.5 of Jonah's, the jonah run's cost", s, err)
	}

	other := f.otherOrg(t)

	_, err = f.Store.Run(other.ctx(t), id)
	expect(t, "Run of another org's jonah run", err, ports.ErrNotFound)

	_, err = f.Store.StartRun(other.ctx(t), ports.NewRun{FactoryID: f.factory, Agent: agJonah, StartedAt: t1, Kind: domain.RunKindJonah})
	expect(t, "StartRun of a jonah run in another org's factory", err, ports.ErrNotFound)

	for _, item := range []domain.ItemID{"", missingItem} {
		for _, kind := range []domain.RunKind{"", domain.RunKindStation} {
			_, err := f.Store.StartRun(f.ctx(t), ports.NewRun{
				FactoryID: f.factory, ItemID: item, Station: stWork, Agent: agWorker, Model: model, StartedAt: t1, Kind: kind,
			})
			expect(t, "StartRun of a station run of item "+string(item), err, ports.ErrNotFound)
		}
	}
}

// The repositories testRunRecords' runs record.
const (
	repoApp  = "github.com/acme/app"
	repoSame = "github.com/acme/same"
	repoDocs = "github.com/acme/docs"
)

// testRunRecords: a run keeps what it was given (RecordRunInput) and its own output and pushed
// heads (FinishRun), one row per repository (ADR 0041 decision 5), and Run and Runs return them;
// a run wrote a repository when it said so or pushed a commit other than its branch head. Empty
// values read back empty, and another organization can record nothing on the run (ADR 0039,
// decision 8).
func testRunRecords(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{})
	t2 := t1.Add(time.Minute)
	a, b, c, d, e := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40), strings.Repeat("d", 40), strings.Repeat("e", 40)
	in := domain.RunInput{Brief: "Add a button\n\nThe reviewer asked for tests.", Repositories: []domain.RunRepository{
		{Repo: repoApp, BaseCommit: b, BranchHead: a},
		{Repo: repoSame, BaseCommit: b, BranchHead: e},
	}}
	res := domain.RunResult{
		Status: domain.RunSucceeded, Output: json.RawMessage(`{"lane":"backend","n":1}`), CostUSD: 0.5,
		Repositories: []domain.RunRepository{{Repo: repoApp, PushedHead: c}, {Repo: repoSame, PushedHead: e}, {Repo: repoDocs, PushedHead: d}},
	}

	first := f.recordedRun(t, it.ID, t1, in, res)

	got, err := f.Store.Run(f.ctx(t), first)
	if err != nil {
		t.Fatal(err)
	}

	rows := []domain.RunRepository{
		{Repo: repoApp, BaseCommit: b, BranchHead: a, PushedHead: c, Wrote: true},
		{Repo: repoSame, BaseCommit: b, BranchHead: e, PushedHead: e},
		{Repo: repoDocs, PushedHead: d, Wrote: true},
	}
	if want := (domain.RunInput{Brief: in.Brief, Repositories: rows}); !reflect.DeepEqual(got.Input, want) {
		t.Errorf("Run input = %+v; want %+v", got.Input, want)
	}

	if !reflect.DeepEqual(got.Result.Repositories, rows) {
		t.Errorf("Run result's repositories = %+v; want %+v", got.Result.Repositories, rows)
	}

	if g, w := normalJSON(t, got.Result.Output), normalJSON(t, res.Output); g != w {
		t.Errorf("Run output = %s; want %s", g, w)
	}

	// A retry in the claim's box: its own brief, no heads; a failed run, no output.
	retryIn := domain.RunInput{Brief: "Add a button"}
	f.recordedRun(t, it.ID, t2, retryIn, domain.RunResult{Status: domain.RunFailed, Error: "no"})

	runs, err := f.Store.Runs(f.ctx(t), it.ID, 10)
	if err != nil || len(runs) != 2 {
		t.Fatalf("Runs = %+v, %v; want two", runs, err)
	}

	if r := runs[1]; !reflect.DeepEqual(r.Input, retryIn) || r.Result.Repositories != nil || r.Result.Output != nil {
		t.Errorf("Runs' retry = input %+v, repositories %+v, output %s; want only its brief", r.Input, r.Result.Repositories, r.Result.Output)
	}

	if !reflect.DeepEqual(runs[0].Input.Repositories, rows) {
		t.Errorf("Runs' first run repositories = %+v; want %+v", runs[0].Input.Repositories, rows)
	}

	f.expectNoRunRecords(t, it.ID, in)
}

// testRunUsage: a run records the box size it started with, and the CPU seconds and peak memory
// it finished with; a run whose worker reported no counters reads back with no values, not zero.
func testRunUsage(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{})
	t2 := t1.Add(time.Minute)

	start := func(at time.Time, size domain.SandboxSize) domain.RunID {
		id, err := f.Store.StartRun(f.ctx(t), ports.NewRun{
			FactoryID: f.factory, ItemID: it.ID, Station: stWork, Agent: agWorker, Model: model, StartedAt: at, Size: size,
		})
		if err != nil {
			t.Fatalf("StartRun: %v", err)
		}

		return id
	}

	cpu, peak := 12.5, int64(536870912)
	counted := start(t1, domain.SandboxLarge)

	res := domain.RunResult{Status: domain.RunSucceeded, Output: json.RawMessage(`{}`), CPUSeconds: &cpu, PeakMemoryBytes: &peak}
	if err := f.Store.FinishRun(f.ctx(t), counted, res, t1); err != nil {
		t.Fatal(err)
	}

	bare := start(t2, domain.SandboxSmall)
	if err := f.Store.FinishRun(f.ctx(t), bare, domain.RunResult{Status: domain.RunFailed, Error: "no"}, t2); err != nil {
		t.Fatal(err)
	}

	got, err := f.Store.Run(f.ctx(t), counted)
	if err != nil {
		t.Fatal(err)
	}

	if got.BoxSize != domain.SandboxLarge || got.Result.CPUSeconds == nil || *got.Result.CPUSeconds != cpu ||
		got.Result.PeakMemoryBytes == nil || *got.Result.PeakMemoryBytes != peak {
		t.Errorf("Run = box %q, cpu %v, peak %v; want large, %v, %d", got.BoxSize, got.Result.CPUSeconds, got.Result.PeakMemoryBytes, cpu, peak)
	}

	got, err = f.Store.Run(f.ctx(t), bare)
	if err != nil {
		t.Fatal(err)
	}

	if got.BoxSize != domain.SandboxSmall || got.Result.CPUSeconds != nil || got.Result.PeakMemoryBytes != nil {
		t.Errorf("Run without counters = box %q, cpu %v, peak %v; want small, nil, nil", got.BoxSize, got.Result.CPUSeconds, got.Result.PeakMemoryBytes)
	}

	runs, err := f.Store.Runs(f.ctx(t), it.ID, 10)
	if err != nil || len(runs) != 2 {
		t.Fatalf("Runs = %+v, %v; want two", runs, err)
	}

	if r := runs[0]; r.BoxSize != domain.SandboxLarge || r.Result.CPUSeconds == nil || r.Result.PeakMemoryBytes == nil {
		t.Errorf("Runs' first = box %q, cpu %v, peak %v; want its size and usage", r.BoxSize, r.Result.CPUSeconds, r.Result.PeakMemoryBytes)
	}

	listed, _, err := f.Store.OrgRuns(f.ctx(t), ports.RunQuery{Factories: []domain.FactoryID{f.factory}, Since: t1, Until: t2.Add(time.Second), Limit: 10})
	if err != nil || len(listed) != 2 {
		t.Fatalf("OrgRuns = %+v, %v; want two", listed, err)
	}

	if r := listed[1]; r.ID != counted || r.BoxSize != domain.SandboxLarge || r.Result.CPUSeconds == nil || r.Result.PeakMemoryBytes == nil {
		t.Errorf("OrgRuns' oldest = %s, box %q, cpu %v, peak %v; want %s with its size and usage", r.ID, r.BoxSize, r.Result.CPUSeconds, r.Result.PeakMemoryBytes, counted)
	}

	// A caller that names no size records none.
	got, err = f.Store.Run(f.ctx(t), f.startRun(t, it.ID, t2.Add(time.Second)))
	if err != nil || got.BoxSize != "" {
		t.Errorf("Run without a size = box %q, %v; want none", got.BoxSize, err)
	}
}

// recordedRun starts a run of item at at, records in and finishes it with res.
func (f *fixture) recordedRun(t *testing.T, item domain.ItemID, at time.Time, in domain.RunInput, res domain.RunResult) domain.RunID {
	t.Helper()

	id := f.startRun(t, item, at)
	if err := f.Store.RecordRunInput(f.ctx(t), id, in); err != nil {
		t.Fatalf("RecordRunInput: %v", err)
	}

	if err := f.Store.FinishRun(f.ctx(t), id, res, at); err != nil {
		t.Fatal(err)
	}

	return id
}

// expectNoRunRecords: a run that records nothing reads back empty, an unknown run and another
// organization's record nothing.
func (f *fixture) expectNoRunRecords(t *testing.T, item domain.ItemID, in domain.RunInput) {
	t.Helper()

	bare := f.startRun(t, item, t1)
	if got, err := f.Store.Run(f.ctx(t), bare); err != nil || !reflect.DeepEqual(got.Input, domain.RunInput{}) || got.Result.Repositories != nil {
		t.Errorf("Run without records = %+v, %v; want no input and no repositories", got, err)
	}

	err := f.Store.RecordRunInput(f.ctx(t), missingRun, in)
	expect(t, "RecordRunInput of an unknown run", err, ports.ErrNotFound)

	err = f.Store.RecordRunInput(f.otherOrg(t).ctx(t), bare, in)
	expect(t, "RecordRunInput of another org's run", err, ports.ErrNotFound)

	if got, err := f.Store.Run(f.ctx(t), bare); err != nil || !reflect.DeepEqual(got.Input, domain.RunInput{}) {
		t.Errorf("Run after another org's RecordRunInput = %+v, %v; want no input", got.Input, err)
	}
}
