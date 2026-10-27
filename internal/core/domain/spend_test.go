package domain

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestSpendWindow_Bounds(t *testing.T) {
	t.Parallel()

	at := func(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 30, 0, 0, time.UTC) }
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

	tests := []struct {
		name   string
		window SpendWindow
		since  time.Time
	}{
		{"a Tuesday's week starts on Monday", SpendWindow{SpendThisWeek, at(2026, 10, 27, 15)}, day(2026, 10, 26)},
		{"a Sunday's week starts six days before", SpendWindow{SpendThisWeek, at(2026, 11, 1, 23)}, day(2026, 10, 26)},
		{"a Monday's week starts that day", SpendWindow{SpendThisWeek, at(2026, 10, 26, 0)}, day(2026, 10, 26)},
		{"a week across the year boundary", SpendWindow{SpendThisWeek, at(2027, 1, 2, 8)}, day(2026, 12, 28)},
		{"this month starts on the 1st", SpendWindow{SpendThisMonth, at(2026, 10, 27, 15)}, day(2026, 10, 1)},
		{"on the 1st the month starts that day", SpendWindow{SpendThisMonth, at(2026, 11, 1, 0)}, day(2026, 11, 1)},
		{"90 days include today", SpendWindow{SpendLast90Days, at(2026, 10, 27, 15)}, day(2026, 7, 30)},
		{"90 days across the year boundary", SpendWindow{SpendLast90Days, at(2027, 1, 2, 8)}, day(2026, 10, 5)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			since, until := tt.window.Bounds()
			if !since.Equal(tt.since) || !until.Equal(tt.window.Now) {
				t.Errorf("Bounds() = %s, %s; want %s, %s", since, until, tt.since, tt.window.Now)
			}
		})
	}

	// Bounds are in UTC whatever the clock's zone: 01:30 on Monday in UTC+3 is Sunday in UTC.
	zone := time.FixedZone("UTC+3", 3*60*60)
	if since, _ := (SpendWindow{SpendThisWeek, time.Date(2026, 10, 26, 1, 30, 0, 0, zone)}).Bounds(); !since.Equal(day(2026, 10, 19)) {
		t.Errorf("week of Monday 01:30 UTC+3 starts %s, want %s", since, day(2026, 10, 19))
	}

	if err := (SpendWindow{Period: "year", Now: at(2026, 10, 27, 15)}).Check(); !errors.Is(err, ErrInvalid) {
		t.Errorf("Check of an unknown period = %v, want ErrInvalid", err)
	}

	if err := (SpendWindow{Period: SpendThisMonth}).Check(); !errors.Is(err, ErrInvalid) {
		t.Errorf("Check without a clock time = %v, want ErrInvalid", err)
	}
}

// NewOrgSpend lays the store's sums out for the screen: every day of the window, the month
// totals, the weekly series and each factory's today and this week, from the same day rows.
func TestNewOrgSpend_Assembles(t *testing.T) {
	t.Parallel()

	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }
	w := SpendWindow{SpendThisWeek, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)} // a Wednesday

	if since := w.ReadSince(); !since.Equal(day(9, 1)) {
		t.Fatalf("ReadSince() = %s, want the start of last month", since)
	}

	got := NewOrgSpend(w, SpendFacts{
		Days: []FactoryDaySpend{
			{Day: day(9, 30), FactoryID: "f1", USD: 4},  // last month
			{Day: day(10, 20), FactoryID: "f1", USD: 3}, // last week
			{Day: day(10, 26), FactoryID: "f2", USD: 2},
			{Day: day(10, 28), FactoryID: "f2", USD: 1},
			{Day: day(10, 28), FactoryID: "f1", USD: 5},
		},
		Merged: []DayCount{{Day: day(10, 20), Count: 1}, {Day: day(10, 27), Count: 2}},
		ByAgent: []AgentSpend{
			{FactoryID: "f2", Agent: "a", USD: 3}, {FactoryID: "f1", Agent: "b", USD: 5},
		},
		ByModel:   []ModelSpend{{Model: "m2", USD: 3}, {Model: "m1", USD: 5}},
		ReworkUSD: 2,
	})

	want := OrgSpend{
		Period: SpendThisWeek, Since: day(10, 26), Until: w.Now,
		TotalUSD: 8, ThisMonthUSD: 11, LastMonthUSD: 4,
		Days: []DaySpend{
			{Day: day(10, 26), USD: 2, ByFactory: []FactorySpend{{FactoryID: "f2", USD: 2}}},
			{Day: day(10, 27)},
			{Day: day(10, 28), USD: 6, ByFactory: []FactorySpend{{FactoryID: "f1", USD: 5}, {FactoryID: "f2", USD: 1}}},
		},
		ByAgent: []AgentSpend{{FactoryID: "f1", Agent: "b", USD: 5}, {FactoryID: "f2", Agent: "a", USD: 3}},
		ByModel: []ModelSpend{{Model: "m1", USD: 5}, {Model: "m2", USD: 3}},
		Merged:  2, CostPerMergedUSD: 4,
		Weekly: []WeekCost{
			{WeekStart: day(9, 7)},
			{WeekStart: day(9, 14)},
			{WeekStart: day(9, 21)},
			{WeekStart: day(9, 28), SpendUSD: 4},
			{WeekStart: day(10, 5)},
			{WeekStart: day(10, 12)},
			{WeekStart: day(10, 19), SpendUSD: 3, Merged: 1, CostPerMergedUSD: 3},
			{WeekStart: day(10, 26), SpendUSD: 8, Merged: 2, CostPerMergedUSD: 4},
		},
		ReworkUSD: 2, ReworkShare: 0.25,
		Factories: []FactorySpendNow{{FactoryID: "f1", TodayUSD: 5, WeekUSD: 5}, {FactoryID: "f2", TodayUSD: 1, WeekUSD: 3}},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("NewOrgSpend =\n  %+v\nwant\n  %+v", got, want)
	}
}

func TestOrgSpend_Ratios(t *testing.T) {
	t.Parallel()

	w := SpendWindow{SpendThisMonth, time.Date(2026, 1, 1, 6, 0, 0, 0, time.UTC)} // the 1st, at a year boundary

	// Nothing spent and nothing merged: every ratio is 0, never NaN.
	empty := NewOrgSpend(w, SpendFacts{})
	if empty.CostPerMergedUSD != 0 || empty.ReworkShare != 0 || len(empty.Weekly) != SpendWeeks || len(empty.Days) != 1 {
		t.Errorf("empty spend = %+v; want 0 ratios, %d weeks and one day", empty, SpendWeeks)
	}

	for _, wk := range empty.Weekly {
		if wk.CostPerMergedUSD != 0 {
			t.Errorf("week %s costs %v per merged item with none merged, want 0", wk.WeekStart, wk.CostPerMergedUSD)
		}
	}

	// Spend without a merged item: no cost per merged item; the rework share is of the total.
	today := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got := NewOrgSpend(w, SpendFacts{Days: []FactoryDaySpend{{Day: today, FactoryID: "f", USD: 8}}, ReworkUSD: 6})

	if got.CostPerMergedUSD != 0 || got.ReworkShare != 0.75 || got.LastMonthUSD != 0 || got.ThisMonthUSD != 8 {
		t.Errorf("spend = %+v; want no cost per merged item, a rework share of 0.75 and 8 this month", got)
	}

	if last := got.Weekly[SpendWeeks-1]; last.SpendUSD != 8 || last.CostPerMergedUSD != 0 {
		t.Errorf("this week = %+v; want 8 spent and no cost per merged item", last)
	}
}
