package storetest

import (
	"cmp"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// orgRun records a run of agent with model on it, started at at and finished with res unless
// res is running.
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

	done := func(s domain.RunStatus, usd float64) domain.RunResult { return domain.RunResult{Status: s, CostUSD: usd} }

	edge := f.orgRun(t, refactor, agWorker, model, since, done(domain.RunSucceeded, 1))
	loginOK := f.orgRun(t, login, agWorker, model, at(1), done(domain.RunSucceeded, 0.25))
	loginFailed := f.orgRun(t, login, agBackend, "model-b", at(2), done(domain.RunFailed, 0.5))
	running := f.orgRun(t, billing, agWorker, model, at(2), done(domain.RunRunning, 0))
	cancelled := f.orgRun(t, refactor, agWorker, model, at(3), done(domain.RunCancelled, 0.125))
	if login.Number != 1 || billing.Number != 1 {
		t.Fatalf("login and billing are numbers %d and %d; want 1 in each factory", login.Number, billing.Number)
	}

	// Outside the window, or in a factory not asked for, or in another organization.
	f.orgRun(t, login, agWorker, model, since.Add(-time.Microsecond), done(domain.RunSucceeded, 8))
	f.orgRun(t, login, agWorker, model, until, done(domain.RunSucceeded, 8))
	f.orgRun(t, outside, agWorker, model, at(4), done(domain.RunSucceeded, 8))

	other := f.otherOrg(t)
	otherItem := other.create(t, domain.NewItem{Title: "Fix the Login page", CreatedAt: t0})
	other.orgRun(t, otherItem, agWorker, model, at(4), done(domain.RunSucceeded, 8))

	// The two runs that started at the same time are ordered by id, descending (PostgreSQL
	// orders uuids as their canonical text).
	tie := []domain.RunID{loginFailed, running}
	slices.Sort(tie)
	slices.Reverse(tie)

	asked := []domain.FactoryID{f.factory, second, other.factory}
	list := func(q ports.RunQuery) ([]domain.ListedRun, domain.RunSummary) {
		t.Helper()

		q.Since, q.Until = since, until
		if q.Factories == nil {
			q.Factories = asked
		}

		if q.Limit == 0 {
			q.Limit = 20
		}

		runs, sum, err := f.Store.OrgRuns(f.ctx(t), q)
		if err != nil {
			t.Fatal(err)
		}

		return runs, sum
	}
	ids := func(runs []domain.ListedRun) []domain.RunID {
		out := []domain.RunID{}
		for _, r := range runs {
			out = append(out, r.ID)
		}

		return out
	}

	all := []domain.RunID{cancelled, tie[0], tie[1], loginOK, edge}

	runs, sum := list(ports.RunQuery{})
	if got := ids(runs); !slices.Equal(got, all) {
		t.Errorf("OrgRuns = %v; want %v, newest first", got, all)
	}

	for _, r := range runs {
		if r.ID != loginFailed {
			continue
		}

		want := domain.ListedRun{ItemNumber: login.Number, ItemTitle: login.Title, Run: domain.Run{
			ID: loginFailed, FactoryID: f.factory, ItemID: login.ID, Station: stWork, Agent: agBackend, Model: "model-b",
			Result: domain.RunResult{Status: domain.RunFailed, CostUSD: 0.5}, StartedAt: at(2), EndedAt: at(3),
		}}
		if r.ItemNumber != want.ItemNumber || r.ItemTitle != want.ItemTitle || r.FactoryID != want.FactoryID ||
			r.ItemID != want.ItemID || r.Agent != want.Agent || r.Model != want.Model || r.Result.Status != want.Result.Status ||
			r.Result.CostUSD != want.Result.CostUSD || !r.StartedAt.Equal(want.StartedAt) || !r.EndedAt.Equal(want.EndedAt) {
			t.Errorf("OrgRuns' failed run = %+v; want %+v", r, want)
		}
	}

	wantSum := domain.RunSummary{Total: 5, Running: 1, Failed: 1, Finished: 3, SpendUSD: 1.875}
	wantByFactory := []domain.FactorySpend{{FactoryID: f.factory, SpendUSD: 1.875}, {FactoryID: second, SpendUSD: 0}}
	slices.SortFunc(wantByFactory, func(a, b domain.FactorySpend) int { return cmp.Compare(a.FactoryID, b.FactoryID) })
	sameSummary(t, "every run", sum, wantSum, wantByFactory)

	// Following each page's last run reaches every run once.
	var paged []domain.RunID

	q := ports.RunQuery{Limit: 2}
	for range 4 {
		page, _ := list(q)
		if len(page) == 0 {
			break
		}

		paged = append(paged, ids(page)...)
		last := page[len(page)-1]
		q.After = &ports.RunCursor{StartedAt: last.StartedAt, ID: last.ID}
	}

	if !slices.Equal(paged, all) {
		t.Errorf("OrgRuns paged by 2 = %v; want %v", paged, all)
	}

	tests := []struct {
		name string
		q    ports.RunQuery
		want []domain.RunID
	}{
		{"by status", ports.RunQuery{Status: domain.RunFailed}, []domain.RunID{loginFailed}},
		{"title, case-insensitive", ports.RunQuery{Text: "LOGIN"}, []domain.RunID{loginFailed, loginOK}},
		{"agent", ports.RunQuery{Text: "Backend-Eng"}, []domain.RunID{loginFailed}},
		{"model", ports.RunQuery{Text: "model-b"}, []domain.RunID{loginFailed}},
		{"% matches literally", ports.RunQuery{Text: "50%"}, []domain.RunID{running}},
		{"_ matches literally", ports.RunQuery{Text: "model_b"}, []domain.RunID{}},
		{"the item's number", ports.RunQuery{Text: "#9999", Number: refactor.Number}, []domain.RunID{cancelled, edge}},
		{"a number in every factory", ports.RunQuery{Text: "#1", Number: 1}, []domain.RunID{tie[0], tie[1], loginOK}},
		{"a number in one factory", ports.RunQuery{Text: "x#1", Number: 1, NumberOf: second}, []domain.RunID{running}},
		{"text and status", ports.RunQuery{Text: "login", Status: domain.RunSucceeded}, []domain.RunID{loginOK}},
		{"one factory", ports.RunQuery{Factories: []domain.FactoryID{second}}, []domain.RunID{running}},
		{"no factory", ports.RunQuery{Factories: []domain.FactoryID{}}, []domain.RunID{}},
		{"limited", ports.RunQuery{Limit: 1}, []domain.RunID{cancelled}},
		{"after a cursor", ports.RunQuery{After: &ports.RunCursor{StartedAt: at(2), ID: tie[0]}}, []domain.RunID{tie[1], loginOK, edge}},
	}

	for _, tt := range tests {
		runs, sum := list(tt.q)
		if got := ids(runs); !slices.Equal(got, tt.want) {
			t.Errorf("OrgRuns %s = %v; want %v", tt.name, got, tt.want)
		}

		// The summary ignores the status, the text and the cursor; only the factories bound it.
		switch {
		case tt.q.Factories == nil:
			sameSummary(t, tt.name, sum, wantSum, wantByFactory)
		case len(tt.q.Factories) == 0:
			sameSummary(t, tt.name, sum, domain.RunSummary{}, nil)
		default:
			sameSummary(t, tt.name, sum, domain.RunSummary{Total: 1, Running: 1}, []domain.FactorySpend{{FactoryID: second}})
		}
	}
}

func sameSummary(t *testing.T, what string, got, want domain.RunSummary, byFactory []domain.FactorySpend) {
	t.Helper()

	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

	ok := got.Total == want.Total && got.Running == want.Running && got.Failed == want.Failed &&
		got.Finished == want.Finished && near(got.SpendUSD, want.SpendUSD) && len(got.ByFactory) == len(byFactory)
	for i := 0; ok && i < len(byFactory); i++ {
		ok = got.ByFactory[i].FactoryID == byFactory[i].FactoryID && near(got.ByFactory[i].SpendUSD, byFactory[i].SpendUSD)
	}

	if !ok {
		t.Errorf("OrgRuns' summary (%s) = %+v; want %+v with %+v", what, got, want, byFactory)
	}
}
