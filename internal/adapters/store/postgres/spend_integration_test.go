//go:build integration

package postgres

import (
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// spendDay is a time on a day of 2026, in UTC.
func spendDay(m time.Month, d, h, mi, sec int) time.Time {
	return time.Date(2026, m, d, h, mi, sec, 0, time.UTC)
}

// Two organizations spend, rework and merge on the same days, the second eight times as much;
// each one's OrgSpend sums only its own runs and items: every total, day, agent, model, week and
// factory row. The session's time zone is far from UTC, and runs a second either side of
// midnight UTC land on their UTC day.
func TestSpend_OrgsDoNotSee(t *testing.T) {
	t.Parallel()

	db := newTestDB(t, OwnerRole)
	migrate(t, db.owner)

	u, err := url.Parse(db.app)
	if err != nil {
		t.Fatal(err)
	}

	q := u.Query()
	q.Set("timezone", "Pacific/Kiritimati") // UTC+14
	u.RawQuery = q.Encode()
	s := New(openPool(t, u.String()))

	w := domain.SpendWindow{Period: domain.SpendThisMonth, Now: spendDay(10, 27, 12, 0, 0)}

	a, fa := seedSpend(t, s, 1)
	b, fb := seedSpend(t, s, 8)

	for _, o := range []struct {
		org     domain.OrgID
		factory domain.FactoryID
		k       float64
	}{{a, fa, 1}, {b, fb, 8}} {
		got, err := s.OrgSpend(domain.WithOrg(t.Context(), o.org), w)
		if err != nil {
			t.Fatal(err)
		}

		checkSpend(t, got, o.factory, o.k)
	}
}

// seedSpend makes an organization with a factory whose item spends k on the last second of
// September, 2k on the first of October and, sent back on October 10, 4k that day; another item
// is merged on October 20.
func seedSpend(t *testing.T, s *Store, k float64) (domain.OrgID, domain.FactoryID) {
	t.Helper()

	org := newOrgID(t, s)
	ctx := domain.WithOrg(t.Context(), org)
	f := saveFactory(ctx, t, s)

	create := func(created time.Time) domain.Item {
		it, err := s.CreateItem(ctx, domain.NewItem{
			FactoryID: f, Station: stWork, State: domain.StateQueued, Class: domain.ClassStandard, Type: typeTask,
			Title: "a spending item", CreatedAt: created,
		})
		if err != nil {
			t.Fatal(err)
		}

		return it
	}

	run := func(it domain.Item, started time.Time, usd float64) {
		rid, err := s.StartRun(ctx, ports.NewRun{
			FactoryID: f, ItemID: it.ID, Station: stWork, Agent: "worker", Model: "model-a", StartedAt: started,
		})
		if err != nil {
			t.Fatal(err)
		}

		if err := s.FinishRun(ctx, rid, domain.RunResult{Status: domain.RunSucceeded, CostUSD: usd}, started.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}

	move := func(tr ports.Transition) {
		if _, err := s.Transition(ctx, tr); err != nil {
			t.Fatal(err)
		}
	}

	it := create(spendDay(9, 29, 0, 0, 0))
	run(it, spendDay(9, 30, 23, 59, 59), k)
	run(it, spendDay(10, 1, 0, 0, 0), 2*k)
	move(ports.Transition{ItemID: it.ID, From: it.Position(), To: it.Position(), Rework: true, At: spendDay(10, 10, 0, 0, 0)})
	run(it, spendDay(10, 10, 0, 0, 0), 4*k)

	done := create(spendDay(10, 19, 0, 0, 0))
	move(ports.Transition{
		ItemID: done.ID, From: done.Position(), To: domain.Position{Station: "done", State: domain.StateDone},
		At: spendDay(10, 20, 0, 0, 0), DoneAt: spendDay(10, 20, 0, 0, 0),
	})

	return org, f
}

// checkSpend checks that got, this month measured on October 27, is seedSpend(k)'s alone.
func checkSpend(t *testing.T, got domain.OrgSpend, factory domain.FactoryID, k float64) {
	t.Helper()

	day := func(d int) time.Time { return spendDay(10, d, 0, 0, 0) }
	now := spendDay(10, 27, 12, 0, 0)
	want := domain.OrgSpend{
		Period: domain.SpendThisMonth, Since: day(1), Until: now, TotalUSD: 6 * k, ThisMonthUSD: 6 * k, LastMonthUSD: k,
		ByAgent: []domain.AgentSpend{{FactoryID: factory, Agent: "worker", USD: 6 * k}},
		ByModel: []domain.ModelSpend{{Model: "model-a", USD: 6 * k}},
		Merged:  1, CostPerMergedUSD: 6 * k, ReworkUSD: 4 * k, ReworkShare: 4 * k / (6 * k),
		Factories: []domain.FactorySpendNow{{FactoryID: factory}},
	}

	for d := 1; d <= 27; d++ {
		want.Days = append(want.Days, domain.DaySpend{Day: day(d)})
	}

	want.Days[0] = domain.DaySpend{Day: day(1), USD: 2 * k, ByFactory: []domain.FactorySpend{{FactoryID: factory, USD: 2 * k}}}
	want.Days[9] = domain.DaySpend{Day: day(10), USD: 4 * k, ByFactory: []domain.FactorySpend{{FactoryID: factory, USD: 4 * k}}}

	for i := range domain.SpendWeeks {
		want.Weekly = append(want.Weekly, domain.WeekCost{WeekStart: spendDay(9, 7+7*i, 0, 0, 0)})
	}

	want.Weekly[3].SpendUSD = 3 * k // September 28: its last second and October 1
	want.Weekly[4].SpendUSD = 4 * k // October 5: the rework of October 10
	want.Weekly[6] = domain.WeekCost{WeekStart: day(19), Merged: 1}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("org ×%v OrgSpend =\n  %+v\nwant only its own runs and items:\n  %+v", k, got, want)
	}
}
