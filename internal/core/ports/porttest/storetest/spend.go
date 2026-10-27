package storetest

import (
	"cmp"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

const modelB = "model-b"

// An organization's spend this week, measured on a Wednesday: the runs started from Monday
// 00:00 UTC (one exactly then counts, one a second before does not, a running one counts with
// 0, one started at the window's end does not), split by UTC day and factory, by agent and by
// model; this month and last; the 8 weekly points of cost per merged item; the runs after the
// item was sent back (one started at the very moment counts) and each factory's today and this
// week. Another organization's runs, rework and merged items count for nothing.
func testOrgSpend(f *fixture) {
	t := f.t
	at := func(m time.Month, d, h, mi, s int) time.Time { return time.Date(2026, m, d, h, mi, s, 0, time.UTC) }
	day := func(m time.Month, d int) time.Time { return at(m, d, 0, 0, 0) }
	now := at(10, 28, 12, 0, 0)
	w := domain.SpendWindow{Period: domain.SpendThisWeek, Now: now}

	f1, f2 := f.factory, f.newFactory(t)
	a := f.create(t, domain.NewItem{CreatedAt: day(9, 29)})
	b := f.create(t, domain.NewItem{FactoryID: f2, CreatedAt: day(10, 25)})

	f.spendRun(t, a, agWorker, model, at(9, 30, 10, 0, 0), 4)  // last month
	f.spendRun(t, a, agWorker, model, at(10, 20, 10, 0, 0), 3) // last week
	f.spendRun(t, b, agReviewer, model, at(10, 25, 23, 59, 59), 1.5)
	f.spendRun(t, b, agReviewer, modelB, day(10, 26), 2) // at the window's start
	f.spendRun(t, a, agWorker, model, at(10, 26, 12, 0, 0), 0.5)
	f.spendRun(t, b, agWorker, model, at(10, 28, 11, 0, 0), -1) // still running

	f.move(t, ports.Transition{ItemID: a.ID, From: a.Position(), To: a.Position(), Rework: true, At: day(10, 27)})
	f.spendRun(t, a, agWorker, model, day(10, 27), 1.25) // rework from the moment it was sent back
	f.spendRun(t, a, agWorker, modelB, at(10, 28, 8, 0, 0), 5)
	f.spendRun(t, a, agWorker, model, now, 100) // at the window's end

	f.finish(t, f.create(t, domain.NewItem{CreatedAt: day(10, 20)}), day(10, 21), false)
	f.finish(t, f.create(t, domain.NewItem{FactoryID: f2, CreatedAt: day(10, 26)}), at(10, 27, 9, 0, 0), true)
	f.finish(t, f.create(t, domain.NewItem{CreatedAt: day(10, 26)}), now, false) // merged at the window's end

	// Another organization spends and merges in the same days.
	other := &fixture{Harness: f.Harness, t: t, org: f.NewOrg(t)}
	other.factory = other.NewFactory(t, other.org)
	o := other.create(t, domain.NewItem{CreatedAt: day(9, 29)})
	other.move(t, ports.Transition{ItemID: o.ID, From: o.Position(), To: o.Position(), Rework: true, At: day(9, 30)})
	other.spendRun(t, o, agWorker, model, at(9, 30, 10, 0, 0), 64)
	other.spendRun(t, o, agReviewer, "model-c", at(10, 27, 10, 0, 0), 1000)
	other.finish(t, other.create(t, domain.NewItem{CreatedAt: day(10, 20)}), day(10, 27), false)

	got, err := f.Spend.OrgSpend(f.ctx(t), w)
	if err != nil {
		t.Fatal(err)
	}

	byFactory := func(fs ...domain.FactorySpend) []domain.FactorySpend {
		return slices.SortedFunc(slices.Values(fs), func(x, y domain.FactorySpend) int { return cmp.Compare(x.FactoryID, y.FactoryID) })
	}
	agents := []domain.AgentSpend{
		{FactoryID: f1, Agent: agWorker, USD: 6.75}, {FactoryID: f2, Agent: agReviewer, USD: 2},
		{FactoryID: f2, Agent: agWorker, USD: 0},
	}
	factories := []domain.FactorySpendNow{{FactoryID: f1, TodayUSD: 5, WeekUSD: 6.75}, {FactoryID: f2, WeekUSD: 2}}
	slices.SortFunc(factories, func(x, y domain.FactorySpendNow) int { return cmp.Compare(x.FactoryID, y.FactoryID) })

	want := domain.OrgSpend{
		Period: domain.SpendThisWeek, Since: day(10, 26), Until: now,
		TotalUSD: 8.75, ThisMonthUSD: 13.25, LastMonthUSD: 4,
		Days: []domain.DaySpend{
			{Day: day(10, 26), USD: 2.5, ByFactory: byFactory(domain.FactorySpend{FactoryID: f1, USD: 0.5}, domain.FactorySpend{FactoryID: f2, USD: 2})},
			{Day: day(10, 27), USD: 1.25, ByFactory: byFactory(domain.FactorySpend{FactoryID: f1, USD: 1.25})},
			{Day: day(10, 28), USD: 5, ByFactory: byFactory(domain.FactorySpend{FactoryID: f1, USD: 5}, domain.FactorySpend{FactoryID: f2})},
		},
		ByAgent: agents,
		ByModel: []domain.ModelSpend{{Model: modelB, USD: 7}, {Model: model, USD: 1.75}},
		Merged:  1, CostPerMergedUSD: 8.75,
		Weekly: []domain.WeekCost{
			{WeekStart: day(9, 7)}, {WeekStart: day(9, 14)}, {WeekStart: day(9, 21)},
			{WeekStart: day(9, 28), SpendUSD: 4},
			{WeekStart: day(10, 5)}, {WeekStart: day(10, 12)},
			{WeekStart: day(10, 19), SpendUSD: 4.5, Merged: 1, CostPerMergedUSD: 4.5},
			{WeekStart: day(10, 26), SpendUSD: 8.75, Merged: 1, CostPerMergedUSD: 8.75},
		},
		ReworkUSD: 6.25, ReworkShare: 6.25 / 8.75,
		Factories: factories,
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("OrgSpend =\n  %+v\nwant\n  %+v", got, want)
	}

	theirs, err := f.Spend.OrgSpend(other.ctx(t), w)
	if err != nil {
		t.Fatal(err)
	}

	if theirs.TotalUSD != 1000 || theirs.LastMonthUSD != 64 || theirs.Merged != 1 || theirs.ReworkUSD != 1000 ||
		len(theirs.Factories) != 1 || theirs.Factories[0].FactoryID != other.factory {
		t.Errorf("the other organization's OrgSpend = %+v; want only its own runs and items", theirs)
	}

	if _, err := f.Spend.OrgSpend(t.Context(), w); !errors.Is(err, domain.ErrNoOrg) {
		t.Errorf("OrgSpend without an organization = %v, want ErrNoOrg", err)
	}

	if _, err := f.Spend.OrgSpend(f.ctx(t), domain.SpendWindow{Period: "year", Now: now}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("OrgSpend of an unknown period = %v, want ErrInvalid", err)
	}
}

// spendRun records a run of agent on model started at `at` that cost usd; a negative usd leaves
// it running.
func (f *fixture) spendRun(t *testing.T, it domain.Item, agent, model string, at time.Time, usd float64) {
	t.Helper()

	id, err := f.Store.StartRun(f.ctx(t), ports.NewRun{
		FactoryID: it.FactoryID, ItemID: it.ID, Station: stWork, Agent: agent, Model: model, StartedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}

	if usd < 0 {
		return
	}

	if err := f.Store.FinishRun(f.ctx(t), id, domain.RunResult{Status: domain.RunSucceeded, CostUSD: usd}, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
}
