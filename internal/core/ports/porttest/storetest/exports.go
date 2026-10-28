package storetest

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// errStop is what an export's callback returns to stop it early.
var errStop = errors.New("stop")

// EachOrgRun hands over every run OrgRuns would list, in its order, whatever the limit and the
// cursor say; more than max is ErrTooMany before any run; the callback's error stops it.
func testEachOrgRun(f *fixture) {
	t := f.t

	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	since, until := t0, t0.Add(24*time.Hour)
	second := f.newFactory(t)
	done := domain.RunResult{Status: domain.RunSucceeded, CostUSD: 0.25}

	login := f.create(t, domain.NewItem{Title: `Fix "login", again`, CreatedAt: t0})
	billing := f.create(t, domain.NewItem{FactoryID: second, Title: "Billing", CreatedAt: t0})

	first := f.orgRun(t, login, agWorker, model, at(1), done)
	tieA := f.orgRun(t, login, agBackend, model, at(2), done)
	tieB := f.orgRun(t, billing, agWorker, model, at(2), domain.RunResult{Status: domain.RunRunning})
	last := f.orgRun(t, billing, agWorker, model, at(3), done)
	f.orgRun(t, login, agWorker, model, until, done) // outside the window

	other := f.otherOrg(t)
	other.orgRun(t, other.create(t, domain.NewItem{Title: "Theirs", CreatedAt: t0}), agWorker, model, at(1), done)

	tie := []domain.RunID{tieA, tieB}
	slices.Sort(tie)
	slices.Reverse(tie)

	each := func(q ports.RunQuery, maxRows int) ([]domain.ListedRun, error) {
		t.Helper()

		q.Since, q.Until = since, until
		if q.Factories == nil {
			q.Factories = []domain.FactoryID{f.factory, second, other.factory}
		}

		out := []domain.ListedRun{}
		err := f.Store.EachOrgRun(f.ctx(t), q, maxRows, func(r domain.ListedRun) error {
			out = append(out, r)

			return nil
		})

		return out, err
	}

	for _, tt := range []struct {
		name string
		q    ports.RunQuery
		want []domain.RunID
	}{
		{"every run, newest first", ports.RunQuery{}, []domain.RunID{last, tie[0], tie[1], first}},
		{
			"limit and cursor are not used",
			ports.RunQuery{Limit: 1, After: &ports.RunCursor{StartedAt: at(1), ID: first}},
			[]domain.RunID{last, tie[0], tie[1], first},
		},
		{"filtered like OrgRuns", ports.RunQuery{Text: "LOGIN", Status: domain.RunSucceeded}, []domain.RunID{tieA, first}},
		{"one factory", ports.RunQuery{Factories: []domain.FactoryID{second}}, []domain.RunID{last, tieB}},
		{"no factory", ports.RunQuery{Factories: []domain.FactoryID{}}, []domain.RunID{}},
	} {
		got, err := each(tt.q, 4)
		if err != nil {
			t.Fatalf("EachOrgRun %s: %v", tt.name, err)
		}

		if ids := listedIDs(got); !slices.Equal(ids, tt.want) {
			t.Errorf("EachOrgRun %s = %v; want %v", tt.name, ids, tt.want)
		}
	}

	got, _ := each(ports.RunQuery{Text: "Login"}, 4)
	sameListedRun(t, got, domain.ListedRun{ItemNumber: login.Number, ItemTitle: login.Title, Run: domain.Run{
		ID: first, FactoryID: f.factory, ItemID: login.ID, Station: stWork, Agent: agWorker, Model: model,
		Result: done, StartedAt: at(1), EndedAt: at(2),
	}})

	if got, err := each(ports.RunQuery{}, 3); !errors.Is(err, ports.ErrTooMany) || len(got) != 0 {
		t.Errorf("EachOrgRun of 4 runs, at most 3 = %d runs, %v; want none and ErrTooMany", len(got), err)
	}

	calls := 0

	err := f.Store.EachOrgRun(f.ctx(t), ports.RunQuery{Factories: []domain.FactoryID{f.factory, second}, Since: since, Until: until}, 4,
		func(domain.ListedRun) error {
			calls++

			return errStop
		})
	if !errors.Is(err, errStop) || calls != 1 {
		t.Errorf("EachOrgRun stopped by its callback = %v after %d calls; want errStop after 1", err, calls)
	}

	err = f.Store.EachOrgRun(t.Context(), ports.RunQuery{Factories: []domain.FactoryID{f.factory}, Since: since, Until: until}, 4,
		func(domain.ListedRun) error { return nil })
	expect(t, "EachOrgRun without an organization", err, domain.ErrNoOrg)
}

// EachAgentModelSpend sums the window's runs of the factories asked for by factory, agent and
// model, ordered by factory id, agent and model; more than max rows is ErrTooMany before any.
func testEachAgentModelSpend(f *fixture) {
	t := f.t
	at := func(d, h int) time.Time { return time.Date(2026, 10, d, h, 0, 0, 0, time.UTC) }
	now := at(28, 12)
	w := domain.SpendWindow{Period: domain.SpendThisWeek, Now: now} // from Monday the 26th

	f1, f2, f3 := f.factory, f.newFactory(t), f.newFactory(t)
	a := f.create(t, domain.NewItem{CreatedAt: at(20, 0)})
	b := f.create(t, domain.NewItem{FactoryID: f2, CreatedAt: at(20, 0)})
	c := f.create(t, domain.NewItem{FactoryID: f3, CreatedAt: at(20, 0)})

	f.spendRun(t, a, agWorker, model, at(26, 0), 1)   // at the window's start
	f.spendRun(t, a, agWorker, model, at(27, 9), 0.5) // the same row
	f.spendRun(t, a, agWorker, modelB, at(27, 9), 2)  // another model
	f.spendRun(t, a, agBackend, model, at(27, 10), 4) // another agent
	f.spendRun(t, b, agWorker, model, at(28, 11), -1) // still running: 0
	f.spendRun(t, a, agWorker, model, at(25, 23), 8)  // before the window
	f.spendRun(t, a, agWorker, model, now, 16)        // at the window's end
	f.spendRun(t, c, agWorker, model, at(27, 9), 32)  // a factory not asked for
	other := f.otherOrg(t)
	other.spendRun(t, other.create(t, domain.NewItem{CreatedAt: at(20, 0)}), agWorker, model, at(27, 9), 64)

	rows := func(factories []domain.FactoryID, maxRows int) ([]string, error) {
		t.Helper()

		out := []string{}
		err := f.Spend.EachAgentModelSpend(f.ctx(t), w, factories, maxRows, func(r domain.AgentModelSpend) error {
			name := map[domain.FactoryID]string{f1: "f1", f2: "f2", other.factory: "theirs"}[r.FactoryID]
			out = append(out, fmt.Sprintf("%s/%s/%s=%.2f", name, r.Agent, r.Model, r.USD))

			return nil
		})

		return out, err
	}

	ofF1 := []string{"f1/" + agBackend + "/" + model + "=4.00", "f1/" + agWorker + "/" + model + "=1.50", "f1/" + agWorker + "/" + modelB + "=2.00"}
	slices.Sort(ofF1)

	want := append([]string{}, ofF1...)
	if f2 < f1 {
		want = append([]string{"f2/" + agWorker + "/" + model + "=0.00"}, want...)
	} else {
		want = append(want, "f2/"+agWorker+"/"+model+"=0.00")
	}

	got, err := rows([]domain.FactoryID{f1, f2, other.factory}, 4)
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("EachAgentModelSpend = %v, %v;\nwant %v", got, err, want)
	}

	if got, err := rows([]domain.FactoryID{}, 4); err != nil || len(got) != 0 {
		t.Errorf("EachAgentModelSpend of no factory = %v, %v; want nothing", got, err)
	}

	if got, err := rows([]domain.FactoryID{f1, f2}, 3); !errors.Is(err, ports.ErrTooMany) || len(got) != 0 {
		t.Errorf("EachAgentModelSpend of 4 rows, at most 3 = %v, %v; want none and ErrTooMany", got, err)
	}

	err = f.Spend.EachAgentModelSpend(f.ctx(t), w, []domain.FactoryID{f1}, 4, func(domain.AgentModelSpend) error { return errStop })
	expect(t, "EachAgentModelSpend stopped by its callback", err, errStop)

	err = f.Spend.EachAgentModelSpend(f.ctx(t), domain.SpendWindow{Period: "year", Now: now}, []domain.FactoryID{f1}, 4,
		func(domain.AgentModelSpend) error { return nil })
	expect(t, "EachAgentModelSpend of an unknown period", err, domain.ErrInvalid)

	err = f.Spend.EachAgentModelSpend(t.Context(), w, []domain.FactoryID{f1}, 4, func(domain.AgentModelSpend) error { return nil })
	expect(t, "EachAgentModelSpend without an organization", err, domain.ErrNoOrg)
}
