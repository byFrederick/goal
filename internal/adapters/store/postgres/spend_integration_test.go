//go:build integration

package postgres

import (
	"net/url"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

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

	at := func(m time.Month, d, h, mi, sec int) time.Time { return time.Date(2026, m, d, h, mi, sec, 0, time.UTC) }
	day := func(m time.Month, d int) time.Time { return at(m, d, 0, 0, 0) }
	w := domain.SpendWindow{Period: domain.SpendThisMonth, Now: at(10, 27, 12, 0, 0)}

	seed := func(k float64) (domain.OrgID, domain.FactoryID) {
		id := newOrgID(t, s)
		ctx := domain.WithOrg(t.Context(), id)
		f := saveFactory(ctx, t, s)

		create := func(created time.Time) domain.Item {
			it, err := s.CreateItem(ctx, domain.NewItem{
				FactoryID: f, Station: "work", State: domain.StateQueued, Class: domain.ClassStandard, Type: "task",
				Title: "an item", CreatedAt: created,
			})
			if err != nil {
				t.Fatal(err)
			}

			return it
		}

		run := func(it domain.Item, started time.Time, usd float64) {
			rid, err := s.StartRun(ctx, ports.NewRun{
				FactoryID: f, ItemID: it.ID, Station: "work", Agent: "worker", Model: "model-a", StartedAt: started,
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

		it := create(day(9, 29))
		run(it, at(9, 30, 23, 59, 59), 1*k) // last month
		run(it, day(10, 1), 2*k)            // this month's first second
		move(ports.Transition{ItemID: it.ID, From: it.Position(), To: it.Position(), Rework: true, At: day(10, 10)})
		run(it, day(10, 10), 4*k) // rework

		done := create(day(10, 19))
		move(ports.Transition{
			ItemID: done.ID, From: done.Position(), To: domain.Position{Station: "done", State: domain.StateDone},
			At: day(10, 20), DoneAt: day(10, 20),
		})

		return id, f
	}

	a, fa := seed(1)
	b, fb := seed(8)

	for _, o := range []struct {
		id      domain.OrgID
		factory domain.FactoryID
		k       float64
	}{{a, fa, 1}, {b, fb, 8}} {
		got, err := s.OrgSpend(domain.WithOrg(t.Context(), o.id), w)
		if err != nil {
			t.Fatal(err)
		}

		k := o.k
		if got.TotalUSD != 6*k || got.ThisMonthUSD != 6*k || got.LastMonthUSD != k || got.Merged != 1 ||
			got.CostPerMergedUSD != 6*k || got.ReworkUSD != 4*k || got.ReworkShare != 4.0/6 {
			t.Errorf("org ×%v totals = %+v; want only its own runs and items", k, got)
		}

		if len(got.Days) != 27 || !got.Days[0].Day.Equal(day(10, 1)) || got.Days[0].USD != 2*k ||
			len(got.Days[0].ByFactory) != 1 || got.Days[0].ByFactory[0] != (domain.FactorySpend{FactoryID: o.factory, USD: 2 * k}) ||
			got.Days[9].USD != 4*k || len(got.Days[9].ByFactory) != 1 {
			t.Errorf("org ×%v days = %+v; want Oct 1 to 27 with its own factory only", k, got.Days)
		}

		if len(got.ByAgent) != 1 || got.ByAgent[0] != (domain.AgentSpend{FactoryID: o.factory, Agent: "worker", USD: 6 * k}) ||
			len(got.ByModel) != 1 || got.ByModel[0] != (domain.ModelSpend{Model: "model-a", USD: 6 * k}) {
			t.Errorf("org ×%v by agent %+v, by model %+v; want its own runs only", k, got.ByAgent, got.ByModel)
		}

		if len(got.Factories) != 1 || got.Factories[0] != (domain.FactorySpendNow{FactoryID: o.factory}) {
			t.Errorf("org ×%v factories = %+v; want its own, nothing today or this week", k, got.Factories)
		}

		var weekly, merged float64
		for _, wk := range got.Weekly {
			weekly += wk.SpendUSD
			merged += float64(wk.Merged)
		}

		if weekly != 7*k || merged != 1 {
			t.Errorf("org ×%v weekly = %+v; want %v spent and 1 merged over the 8 weeks", k, got.Weekly, 7*k)
		}
	}
}
