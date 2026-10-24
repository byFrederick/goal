package storetest

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Reads of the flow statistics over windows (ports.FlowStats): metrics by work type, long waits
// and throughput by bucket.

// typeOther is a second work type, filtered out when the suite asks for typeTask.
const typeOther = "other"

// hours is t0's midnight plus n hours: whole hours keep windows readable.
func hours(n int) time.Time { return t0.Truncate(24 * time.Hour).Add(time.Duration(n) * time.Hour) }

// leave blocks a waiting item where it is at `at`, which closes its waiting span.
func (f *fixture) leave(t *testing.T, it domain.Item, at time.Time) {
	t.Helper()

	f.move(t, ports.Transition{
		ItemID: it.ID, From: it.Position(), To: domain.Position{Station: it.Station, State: domain.StateBlocked}, At: at,
	})
}

// Long waits are the items whose span waiting at the station (queued or ready) overlaps the
// window and lasted over a day by its end; every metric keeps only the asked work type.
func testFlowMetricsByTypeWithLongWaits(f *fixture) {
	t := f.t

	since, until := hours(0), hours(72)

	// At the work station: waited 49 h, left in the window; still waiting at the end, 71 h;
	// waited only 2 h; waited 50 h but left before the window; waiting 2 h at the end; ready
	// (not only queued) for 30 h; and a long wait of the other type.
	f.leave(t, f.create(t, domain.NewItem{CreatedAt: since.Add(-48 * time.Hour)}), since.Add(time.Hour))
	f.create(t, domain.NewItem{CreatedAt: hours(1)})
	f.leave(t, f.create(t, domain.NewItem{CreatedAt: since}), hours(2))
	f.leave(t, f.create(t, domain.NewItem{CreatedAt: since.Add(-100 * time.Hour)}), since.Add(-50*time.Hour))
	f.create(t, domain.NewItem{CreatedAt: until.Add(-2 * time.Hour)})
	f.leave(t, f.create(t, domain.NewItem{CreatedAt: hours(10), State: domain.StateReady}), hours(40))
	other := f.create(t, domain.NewItem{CreatedAt: hours(1), Type: typeOther})

	// Waiting long at the next station counts for that station only.
	f.create(t, domain.NewItem{CreatedAt: hours(1), Station: stNext})

	// Done: two items of the asked type (one reworked), one of the other type; spend on both.
	f.finish(t, f.create(t, domain.NewItem{CreatedAt: hours(3)}), hours(5), true)
	f.finish(t, f.create(t, domain.NewItem{CreatedAt: hours(30)}), hours(34), false)
	f.finish(t, f.create(t, domain.NewItem{CreatedAt: hours(3), Type: typeOther}), hours(4), false)

	for _, r := range []struct {
		item domain.ItemID
		cost float64
	}{{other.ID, 2}, {f.create(t, domain.NewItem{CreatedAt: hours(50)}).ID, 0.5}} {
		run := f.startRun(t, r.item, hours(50))
		if err := f.Store.FinishRun(f.ctx(t), run, domain.RunResult{Status: domain.RunSucceeded, CostUSD: r.cost}, hours(51)); err != nil {
			t.Fatal(err)
		}
	}

	q := ports.FlowQuery{FactoryID: f.factory, Since: since, Until: until, Type: typeTask}

	got, err := f.Flow.FlowMetrics(f.ctx(t), q, stWork)
	if err != nil {
		t.Fatal(err)
	}

	for i := range got.DonePerDay {
		got.DonePerDay[i].Day = got.DonePerDay[i].Day.UTC()
	}

	// Open items of the type: the six made at work, the one at next and the run's item.
	want := domain.FlowMetrics{
		DonePerDay: []domain.DayCount{{Day: hours(0), Count: 1}, {Day: hours(24), Count: 1}},
		ItemsDone:  2, Reworked: 1, LeadTimeP50: 3 * time.Hour, LeadTimeP85: 3*time.Hour + 42*time.Minute,
		WIP: 8, SpendUSD: 0.5, LongWaits: 3,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FlowMetrics of %s at work =\n  %+v\nwant\n  %+v", typeTask, got, want)
	}

	q.Type = ""
	f.everyTypeAndOtherStations(t, q)
}

// everyTypeAndOtherStations checks testFlowMetricsByTypeWithLongWaits's history for every type,
// with long waits at the next station and at none.
func (f *fixture) everyTypeAndOtherStations(t *testing.T, q ports.FlowQuery) {
	t.Helper()

	every, err := f.Flow.FlowMetrics(f.ctx(t), q, stWork)
	if err != nil {
		t.Fatal(err)
	}

	if every.ItemsDone != 3 || every.WIP != 9 || every.SpendUSD != 2.5 || every.LongWaits != 4 {
		t.Errorf("FlowMetrics of every type = %+v; want 3 done, 9 open, $2.50 and 4 long waits", every)
	}

	if atNext, err := f.Flow.FlowMetrics(f.ctx(t), q, stNext); err != nil || atNext.LongWaits != 1 {
		t.Errorf("long waits at next = %d, %v; want 1", atNext.LongWaits, err)
	}

	if none, err := f.Flow.FlowMetrics(f.ctx(t), q, ""); err != nil || none.LongWaits != 0 {
		t.Errorf("long waits at no station = %d, %v; want 0", none.LongWaits, err)
	}
}

// DoneByBucket counts done items in equal buckets that end at the query's end, oldest first:
// a bucket holds its start, not its end.
func testDoneByBucket(f *fixture) {
	t := f.t

	until := hours(21 * 24)
	week := 7 * 24 * time.Hour

	for _, d := range []struct {
		at  time.Time
		typ string
	}{
		{until.Add(-3 * week), typeTask},             // the first bucket's start
		{until.Add(-2*week - time.Second), typeTask}, // the first bucket's last second
		{until.Add(-time.Hour), typeTask},            // the last bucket
		{until.Add(-time.Hour), typeOther},
		{until, typeTask},                            // after the last bucket
		{until.Add(-3*week - time.Second), typeTask}, // before the first
	} {
		f.finish(t, f.create(t, domain.NewItem{CreatedAt: d.at.Add(-time.Hour), Type: d.typ}), d.at, false)
	}

	f.finish(t, f.create(t, domain.NewItem{FactoryID: f.newFactory(t), CreatedAt: until.Add(-2 * time.Hour)}), until.Add(-time.Hour), false)

	q := ports.FlowQuery{FactoryID: f.factory, Until: until, Type: typeTask}

	if got, err := f.Flow.DoneByBucket(f.ctx(t), q, week, 3); err != nil || !reflect.DeepEqual(got, []int{2, 0, 1}) {
		t.Errorf("DoneByBucket of %s = %v, %v; want [2 0 1]", typeTask, got, err)
	}

	q.Type = ""

	if got, err := f.Flow.DoneByBucket(f.ctx(t), q, week, 3); err != nil || !reflect.DeepEqual(got, []int{2, 0, 2}) {
		t.Errorf("DoneByBucket of every type = %v, %v; want [2 0 2]", got, err)
	}

	if got, err := f.Flow.DoneByBucket(f.ctx(t), q, 24*time.Hour, 1); err != nil || !reflect.DeepEqual(got, []int{2}) {
		t.Errorf("DoneByBucket of the last day = %v, %v; want [2]", got, err)
	}
}

// The windowed reads need an organization and see only its own items.
func testFlowStatsTenancy(f *fixture) {
	t := f.t

	f.finish(t, f.create(t, domain.NewItem{CreatedAt: hours(1)}), hours(2), false)
	f.create(t, domain.NewItem{CreatedAt: hours(-48)})

	q := ports.FlowQuery{FactoryID: f.factory, Since: hours(0), Until: hours(24)}

	if _, err := f.Flow.FlowMetrics(context.Background(), q, stWork); !errors.Is(err, domain.ErrNoOrg) {
		t.Errorf("FlowMetrics without an organization = %v; want ErrNoOrg", err)
	}

	if _, err := f.Flow.DoneByBucket(context.Background(), q, time.Hour, 24); !errors.Is(err, domain.ErrNoOrg) {
		t.Errorf("DoneByBucket without an organization = %v; want ErrNoOrg", err)
	}

	other := f.otherOrg(t)

	if m, err := other.Flow.FlowMetrics(other.ctx(t), q, stWork); err != nil || !reflect.DeepEqual(m, domain.FlowMetrics{}) {
		t.Errorf("FlowMetrics from another organization = %+v, %v; want zero", m, err)
	}

	if got, err := other.Flow.DoneByBucket(other.ctx(t), q, 24*time.Hour, 1); err != nil || !reflect.DeepEqual(got, []int{0}) {
		t.Errorf("DoneByBucket from another organization = %v, %v; want [0]", got, err)
	}
}
