package domain

import (
	"cmp"
	"fmt"
	"slices"
	"time"
)

// SpendPeriod is the window the organization's spend screen shows.
type SpendPeriod string

// Spend periods. Each starts on a UTC day and ends at the clock time measured.
const (
	// SpendThisWeek starts on Monday at 00:00 UTC.
	SpendThisWeek SpendPeriod = "week"
	// SpendThisMonth starts on the 1st at 00:00 UTC.
	SpendThisMonth SpendPeriod = "month"
	// SpendLast90Days starts at 00:00 UTC 89 days before today, so it holds 90 UTC days.
	SpendLast90Days SpendPeriod = "90d"
)

// SpendWeeks is how many weeks the cost per merged item's series holds, this week last.
const SpendWeeks = 8

// SpendWindow is a spend period measured at a clock time, Now.
type SpendWindow struct {
	Period SpendPeriod
	Now    time.Time
}

// Check refuses an unknown period or a window without a clock time (ErrInvalid).
func (w SpendWindow) Check() error {
	switch w.Period {
	case SpendThisWeek, SpendThisMonth, SpendLast90Days:
	default:
		return fmt.Errorf("spend period %q is not week, month or 90d: %w", w.Period, ErrInvalid)
	}

	if w.Now.IsZero() {
		return fmt.Errorf("spend window without a clock time: %w", ErrInvalid)
	}

	return nil
}

// Bounds is the window [since, until): since is the period's start in UTC, until is Now.
func (w SpendWindow) Bounds() (since, until time.Time) {
	now := w.Now.UTC()

	switch w.Period {
	case SpendThisMonth:
		return monthStart(now), w.Now
	case SpendLast90Days:
		return dayStart(now).AddDate(0, 0, -89), w.Now
	case SpendThisWeek:
		return weekStart(now), w.Now
	default: // Check refuses any other period; measure it as this week.
		return weekStart(now), w.Now
	}
}

// ReadSince is the earliest UTC day OrgSpend needs: the window's start, the start of last month
// or the first week of the weekly series, whichever is first.
func (w SpendWindow) ReadSince() time.Time {
	since, _ := w.Bounds()
	now := w.Now.UTC()

	return minTime(since, lastMonthStart(now), firstSpendWeek(now))
}

func dayStart(t time.Time) time.Time {
	t = t.UTC()

	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// weekStart is the Monday 00:00 UTC of t's week.
func weekStart(t time.Time) time.Time {
	d := dayStart(t)

	return d.AddDate(0, 0, -(int(d.Weekday())+6)%7)
}

func monthStart(t time.Time) time.Time {
	t = t.UTC()

	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func lastMonthStart(t time.Time) time.Time { return monthStart(t).AddDate(0, -1, 0) }

func firstSpendWeek(t time.Time) time.Time { return weekStart(t).AddDate(0, 0, -7*(SpendWeeks-1)) }

func minTime(t time.Time, ts ...time.Time) time.Time {
	for _, u := range ts {
		if u.Before(t) {
			t = u
		}
	}

	return t
}

// FactoryDaySpend is the cost of a factory's agent runs started on one UTC day.
type FactoryDaySpend struct {
	Day       time.Time
	FactoryID FactoryID
	USD       float64
}

// SpendFacts are the sums a store reads for OrgSpend, for the organization in ctx: Days and
// Merged (the items that reached done, still in state done) over [ReadSince, Now), ByAgent,
// ByModel and ReworkUSD over the window. NewOrgSpend lays them out.
type SpendFacts struct {
	Days      []FactoryDaySpend
	Merged    []DayCount
	ByAgent   []AgentSpend
	ByModel   []ModelSpend
	ReworkUSD float64
}

// OrgSpend is what an organization's agent runs cost, for the spend screen. A run counts on the
// day it started, with its cost so far: a running run counts, with 0 until it finishes. Box
// compute time is not recorded, so it is not in here.
type OrgSpend struct {
	Period       SpendPeriod
	Since, Until time.Time
	// TotalUSD is the window's spend.
	TotalUSD float64
	// ThisMonthUSD and LastMonthUSD are the spend of this UTC month so far and of the one before.
	ThisMonthUSD, LastMonthUSD float64
	// Days is every UTC day of the window, oldest first, each split by the factories that spent.
	Days []DaySpend
	// ByAgent is the window's spend by agent, most first; Agent is its name in its factory.
	ByAgent []AgentSpend
	// ByModel is the window's spend by model, most first.
	ByModel []ModelSpend
	// Merged is how many items reached done in the window; CostPerMergedUSD is TotalUSD over it,
	// 0 when none did.
	Merged           int
	CostPerMergedUSD float64
	// Weekly is the cost per merged item of the last SpendWeeks UTC weeks, this week last.
	Weekly []WeekCost
	// ReworkUSD is the window's spend on runs that started after their item was sent back (in a
	// rework round of 1 or more). An item reopened after done keeps its round, so its runs count
	// too. ReworkShare is its share of TotalUSD, 0 when nothing was spent.
	ReworkUSD, ReworkShare float64
	// Factories is each factory that spent since ReadSince with its spend today and this week
	// (UTC), by id, for its budget; a factory not here spent nothing.
	Factories []FactorySpendNow
}

// DaySpend is one UTC day's spend.
type DaySpend struct {
	Day       time.Time
	USD       float64
	ByFactory []FactorySpend
}

// FactorySpend is a factory's share of a spend.
type FactorySpend struct {
	FactoryID FactoryID
	USD       float64
}

// AgentSpend is the spend of an agent of a factory.
type AgentSpend struct {
	FactoryID FactoryID
	Agent     string
	USD       float64
}

// AgentModelSpend is the spend of an agent of a factory on a model.
type AgentModelSpend struct {
	FactoryID FactoryID
	Agent     string
	Model     string
	USD       float64
}

// ModelSpend is the spend on a model.
type ModelSpend struct {
	Model string
	USD   float64
}

// WeekCost is one UTC week's spend, the items merged in it and the cost per merged item (0 when
// none was).
type WeekCost struct {
	WeekStart        time.Time
	SpendUSD         float64
	Merged           int
	CostPerMergedUSD float64
}

// FactorySpendNow is a factory's spend today and this week, in UTC.
type FactorySpendNow struct {
	FactoryID FactoryID
	TodayUSD  float64
	WeekUSD   float64
}

// NewOrgSpend lays out a store's sums for the window w.
func NewOrgSpend(w SpendWindow, f SpendFacts) OrgSpend {
	since, until := w.Bounds()
	l := newSpendLayout(w.Now.UTC(), since)
	l.out.Period, l.out.Since, l.out.Until, l.out.ReworkUSD = w.Period, since, until, f.ReworkUSD

	for _, r := range f.Days {
		l.addSpend(r)
	}

	for _, m := range f.Merged {
		l.addMerged(m)
	}

	return l.finish(f.ByAgent, f.ByModel)
}

// spendLayout is NewOrgSpend's work in progress: the window's days by date, the weekly series
// and each factory's today and this week.
type spendLayout struct {
	out                             OrgSpend
	since, today, week, month, last time.Time
	first                           time.Time
	days                            map[time.Time]int
	factories                       map[FactoryID]*FactorySpendNow
}

func newSpendLayout(now, since time.Time) *spendLayout {
	l := &spendLayout{
		since: since, today: dayStart(now), week: weekStart(now), month: monthStart(now), last: lastMonthStart(now),
		first: firstSpendWeek(now), days: map[time.Time]int{}, factories: map[FactoryID]*FactorySpendNow{},
	}

	for d := since; !d.After(l.today); d = d.AddDate(0, 0, 1) {
		l.days[d] = len(l.out.Days)
		l.out.Days = append(l.out.Days, DaySpend{Day: d})
	}

	l.out.Weekly = make([]WeekCost, SpendWeeks)
	for i := range l.out.Weekly {
		l.out.Weekly[i].WeekStart = l.first.AddDate(0, 0, 7*i)
	}

	return l
}

// weekOf is the index in the weekly series of the week of day d.
func (l *spendLayout) weekOf(d time.Time) (int, bool) {
	i := int(d.Sub(l.first) / (7 * 24 * time.Hour))

	return i, !d.Before(l.first) && i < SpendWeeks
}

func (l *spendLayout) addSpend(r FactoryDaySpend) {
	d := dayStart(r.Day)

	if i, ok := l.days[d]; ok {
		l.out.TotalUSD += r.USD
		l.out.Days[i].USD += r.USD
		l.out.Days[i].ByFactory = append(l.out.Days[i].ByFactory, FactorySpend{FactoryID: r.FactoryID, USD: r.USD})
	}

	switch {
	case !d.Before(l.month):
		l.out.ThisMonthUSD += r.USD
	case !d.Before(l.last):
		l.out.LastMonthUSD += r.USD
	}

	if i, ok := l.weekOf(d); ok {
		l.out.Weekly[i].SpendUSD += r.USD
	}

	fs := l.factories[r.FactoryID]
	if fs == nil {
		fs = &FactorySpendNow{FactoryID: r.FactoryID}
		l.factories[r.FactoryID] = fs
	}

	if !d.Before(l.week) {
		fs.WeekUSD += r.USD
	}

	if d.Equal(l.today) {
		fs.TodayUSD += r.USD
	}
}

func (l *spendLayout) addMerged(m DayCount) {
	d := dayStart(m.Day)
	if !d.Before(l.since) {
		l.out.Merged += m.Count
	}

	if i, ok := l.weekOf(d); ok {
		l.out.Weekly[i].Merged += m.Count
	}
}

// finish sorts what it laid out and divides.
func (l *spendLayout) finish(agents []AgentSpend, models []ModelSpend) OrgSpend {
	out := l.out

	for i := range out.Days {
		slices.SortFunc(out.Days[i].ByFactory, func(a, b FactorySpend) int { return cmp.Compare(a.FactoryID, b.FactoryID) })
	}

	for i, wk := range out.Weekly {
		out.Weekly[i].CostPerMergedUSD = share(wk.SpendUSD, float64(wk.Merged))
	}

	out.CostPerMergedUSD = share(out.TotalUSD, float64(out.Merged))
	out.ReworkShare = share(out.ReworkUSD, out.TotalUSD)

	out.ByAgent = slices.SortedFunc(slices.Values(agents), func(a, b AgentSpend) int {
		return cmp.Or(cmp.Compare(b.USD, a.USD), cmp.Compare(a.FactoryID, b.FactoryID), cmp.Compare(a.Agent, b.Agent))
	})
	out.ByModel = slices.SortedFunc(slices.Values(models), func(a, b ModelSpend) int {
		return cmp.Or(cmp.Compare(b.USD, a.USD), cmp.Compare(a.Model, b.Model))
	})

	for _, fs := range l.factories {
		out.Factories = append(out.Factories, *fs)
	}

	slices.SortFunc(out.Factories, func(a, b FactorySpendNow) int { return cmp.Compare(a.FactoryID, b.FactoryID) })

	return out
}

// share is a over b, 0 when b is 0.
func share(a, b float64) float64 {
	if b == 0 {
		return 0
	}

	return a / b
}
