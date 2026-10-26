package api

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Flow metrics (tag work) cover ranges of whole UTC days (decision 7 of the 2.2 review): from
// and to both included, at most maxRangeDays, never ending after today.

const (
	day          = 24 * time.Hour
	week         = 7 * day
	maxRangeDays = 90
	// defaultRangeDays is the range metrics cover when the request names none.
	defaultRangeDays = 7
	defaultWeeks     = 12
	maxWeeks         = 26
)

// window is the metrics parameter of the CLI: the last N UTC days, today included.
var window = regexp.MustCompile(`^([1-9][0-9]?)d$`)

// dayRange is a range of whole UTC days, from and to (midnights) both included.
type dayRange struct {
	from, to time.Time
}

// lastDays is the range of n days that ends on today.
func lastDays(n int, now time.Time) dayRange {
	today := now.UTC().Truncate(day)

	return dayRange{from: today.Add(-time.Duration(n-1) * day), to: today}
}

func (r dayRange) days() int { return int(r.to.Sub(r.from)/day) + 1 }

// name is the range's length as the window parameter writes it, as in 7d.
func (r dayRange) name() string { return strconv.Itoa(r.days()) + "d" }

// until is the end of the range: its last day's end, or now when that has not come yet.
func (r dayRange) until(now time.Time) time.Time {
	if end := r.to.Add(day); end.Before(now) {
		return end
	}

	return now
}

// before is the range of the same length that ends the day before r starts.
func (r dayRange) before() dayRange {
	n := time.Duration(r.days()) * day

	return dayRange{from: r.from.Add(-n), to: r.to.Add(-n)}
}

// query is the flow query of factory over r as of now.
func (r dayRange) query(factory domain.FactoryID, typ string, now time.Time) ports.FlowQuery {
	return ports.FlowQuery{FactoryID: factory, Since: r.from, Until: r.until(now), Type: typ}
}

// windowOf checks a window parameter: the last N days, 7 when absent.
func windowOf(param *string, now time.Time) (dayRange, error) {
	if param == nil {
		return lastDays(defaultRangeDays, now), nil
	}

	m := window.FindStringSubmatch(*param)
	if m == nil {
		return dayRange{}, fmt.Errorf("%w: window %q is not 1d to %dd", errInvalid, *param, maxRangeDays)
	}

	n, _ := strconv.Atoi(m[1])
	if n > maxRangeDays {
		return dayRange{}, fmt.Errorf("%w: window %q is not 1d to %dd", errInvalid, *param, maxRangeDays)
	}

	return lastDays(n, now), nil
}

// rangeOf checks a request's range: a window, or from and to (to defaults to today, from to 6
// days before to), never both.
func rangeOf(win *string, from, to *openapi_types.Date, now time.Time) (dayRange, error) {
	if win != nil && (from != nil || to != nil) {
		return dayRange{}, fmt.Errorf("%w: give a window or a range (from, to), not both", errInvalid)
	}

	if win != nil || (from == nil && to == nil) {
		return windowOf(win, now)
	}

	today := now.UTC().Truncate(day)
	r := dayRange{to: today}

	if to != nil {
		r.to = to.Time.UTC().Truncate(day)
	}

	r.from = r.to.Add(-(defaultRangeDays - 1) * day)
	if from != nil {
		r.from = from.Time.UTC().Truncate(day)
	}

	switch {
	case r.to.After(today):
		return dayRange{}, fmt.Errorf("%w: to %s is after today", errInvalid, r.to.Format(time.DateOnly))
	case r.from.After(r.to):
		return dayRange{}, fmt.Errorf("%w: from is after to", errInvalid)
	case r.days() > maxRangeDays:
		return dayRange{}, fmt.Errorf("%w: a range is at most %d days", errInvalid, maxRangeDays)
	}

	return r, nil
}

// workTypeOf checks a type parameter against the factory's work types ("" for every type); a
// factory that declares none takes any.
func workTypeOf(f *config.Factory, param *string) (string, error) {
	typ := deref(param)
	if types := f.Spec.Board.WorkTypes; typ != "" && len(types) > 0 && !slices.Contains(types, typ) {
		return "", fmt.Errorf("%w: factory %s has no work type %q", errInvalid, f.Metadata.Name, typ)
	}

	return typ, nil
}

func (s *server) GetMetrics(ctx context.Context, req gen.GetMetricsRequestObject) (gen.GetMetricsResponseObject, error) {
	p := req.Params
	now := s.clock.Now().UTC()

	r, err := rangeOf(p.Window, p.From, p.To, now)
	if err != nil {
		return nil, err
	}

	if p.Compare != nil && *p.Compare != gen.Previous {
		return nil, fmt.Errorf("%w: compare is previous or absent", errInvalid)
	}

	f, err := s.factory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	typ, err := workTypeOf(f, p.Type)
	if err != nil {
		return nil, err
	}

	l := f.Line()

	drum, err := s.drum(ctx, l, now)
	if err != nil {
		return nil, err
	}

	fm, err := s.items.FlowMetrics(ctx, r.query(l.FactoryID, typ, now), drum)
	if err != nil {
		return nil, err
	}

	out := gen.GetMetrics200JSONResponse(metricsOf(r, fm))
	out.Type, out.Targets = optional(typ), targetsOf(l.Flow.Targets)

	if drum != "" {
		out.LongWaits = &struct {
			Items          int     `json:"items"`
			Station        string  `json:"station"`
			ThresholdHours float64 `json:"threshold_hours"`
		}{Items: fm.LongWaits, Station: drum, ThresholdHours: domain.LongWait.Hours()}
	}

	if p.Compare != nil {
		before := r.before()

		prev, err := s.items.FlowMetrics(ctx, before.query(l.FactoryID, typ, now), "")
		if err != nil {
			return nil, err
		}

		out.Previous = new(summaryOf(before, prev))
	}

	return out, nil
}

// drum is the station the line's flow is paced by now (drumNow), where long waits are counted;
// "" when there is none.
func (s *server) drum(ctx context.Context, l domain.Line, now time.Time) (string, error) {
	if _, ok := l.Station(l.Flow.Constraint); ok {
		return l.Flow.Constraint, nil
	}

	loads, err := s.stationLoads(ctx, l.FactoryID)
	if err != nil {
		return "", err
	}

	return s.drumNow(ctx, l, loads, now)
}

func (s *server) GetWeeklyThroughput(ctx context.Context, req gen.GetWeeklyThroughputRequestObject) (gen.GetWeeklyThroughputResponseObject, error) {
	p := req.Params
	today := s.clock.Now().UTC().Truncate(day)

	to := today
	if p.To != nil {
		to = p.To.Time.UTC().Truncate(day)
	}

	weeks := defaultWeeks
	if p.Weeks != nil {
		weeks = *p.Weeks
	}

	if to.After(today) || weeks < 1 || weeks > maxWeeks {
		return nil, fmt.Errorf("%w: to must not be after today and weeks must be in [1, %d]", errInvalid, maxWeeks)
	}

	f, err := s.factory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	typ, err := workTypeOf(f, p.Type)
	if err != nil {
		return nil, err
	}

	l := f.Line()

	done, err := s.items.DoneByBucket(ctx, ports.FlowQuery{FactoryID: l.FactoryID, Until: to.Add(day), Type: typ}, week, weeks)
	if err != nil {
		return nil, err
	}

	out := gen.GetWeeklyThroughput200JSONResponse{
		To: openapi_types.Date{Time: to}, Type: optional(typ), Targets: targetsOf(l.Flow.Targets),
	}

	first := to.Add(day - time.Duration(weeks)*week)
	for i, n := range done {
		from := first.Add(time.Duration(i) * week)
		out.Weeks = append(out.Weeks, struct {
			Done   int                `json:"done"`
			From   openapi_types.Date `json:"from"`
			PerDay float64            `json:"per_day"`
			To     openapi_types.Date `json:"to"`
		}{Done: n, From: openapi_types.Date{Time: from}, PerDay: float64(n) / 7, To: openapi_types.Date{Time: from.Add(week - day)}})
	}

	return out, nil
}

// targetsOf is the contract's view of a line's targets: null where none is set.
func targetsOf(t domain.Targets) gen.Targets {
	var out gen.Targets

	if t.ThroughputPerDay > 0 {
		out.ThroughputPerDay = &t.ThroughputPerDay
	}

	if t.LeadTime > 0 {
		out.LeadTimeHours = new(t.LeadTime.Hours())
	}

	if t.ExpediteLeadTime > 0 {
		out.ExpediteLeadTimeMinutes = new(int(t.ExpediteLeadTime / time.Minute))
	}

	return out
}

// donePerDay pads fm's items done per day to one entry for every day of r.
func donePerDay(r dayRange, fm domain.FlowMetrics) []struct {
	Day  openapi_types.Date `json:"day"`
	Done int                `json:"done"`
} {
	done := make(map[time.Time]int, len(fm.DonePerDay))
	for _, d := range fm.DonePerDay {
		done[d.Day.UTC()] = d.Count
	}

	out := make([]struct {
		Day  openapi_types.Date `json:"day"`
		Done int                `json:"done"`
	}, 0, r.days())

	for i := range r.days() {
		d := r.from.Add(time.Duration(i) * day)
		out = append(out, struct {
			Day  openapi_types.Date `json:"day"`
			Done int                `json:"done"`
		}{Day: openapi_types.Date{Time: d}, Done: done[d]})
	}

	return out
}

// leadTimeOf is the lead-time percentiles in hours, or nil when nothing was done.
func leadTimeOf(fm domain.FlowMetrics) *struct {
	P50 float64 `json:"p50"`
	P85 float64 `json:"p85"`
} {
	if fm.ItemsDone == 0 {
		return nil
	}

	return &struct {
		P50 float64 `json:"p50"`
		P85 float64 `json:"p85"`
	}{P50: fm.LeadTimeP50.Hours(), P85: fm.LeadTimeP85.Hours()}
}

// assemblyLeadTimeOf is the assembly lead-time percentiles in hours with how many assemblies
// were done, or nil when none were.
func assemblyLeadTimeOf(fm domain.FlowMetrics) *struct {
	Assemblies int     `json:"assemblies"`
	P50        float64 `json:"p50"`
	P85        float64 `json:"p85"`
} {
	if fm.AssembliesDone == 0 {
		return nil
	}

	return &struct {
		Assemblies int     `json:"assemblies"`
		P50        float64 `json:"p50"`
		P85        float64 `json:"p85"`
	}{Assemblies: fm.AssembliesDone, P50: fm.AssemblyLeadTimeP50.Hours(), P85: fm.AssemblyLeadTimeP85.Hours()}
}

// reworkRate is the share of the items done that were sent back at least once.
func reworkRate(fm domain.FlowMetrics) float64 {
	if fm.ItemsDone == 0 {
		return 0
	}

	return float64(fm.Reworked) / float64(fm.ItemsDone)
}

func metricsOf(r dayRange, fm domain.FlowMetrics) gen.Metrics {
	return gen.Metrics{
		Window: r.name(), From: openapi_types.Date{Time: r.from}, To: openapi_types.Date{Time: r.to},
		Wip: fm.WIP, SpendUsd: fm.SpendUSD, ItemsDone: fm.ItemsDone, Throughput: donePerDay(r, fm),
		ThroughputPerDay: float64(fm.ItemsDone) / float64(r.days()), LeadTimeHours: leadTimeOf(fm), ReworkRate: reworkRate(fm),
		AssemblyLeadTimeHours: assemblyLeadTimeOf(fm),
	}
}

func summaryOf(r dayRange, fm domain.FlowMetrics) gen.MetricsSummary {
	return gen.MetricsSummary{
		From: openapi_types.Date{Time: r.from}, To: openapi_types.Date{Time: r.to}, ItemsDone: fm.ItemsDone,
		ThroughputPerDay: float64(fm.ItemsDone) / float64(r.days()), LeadTimeHours: leadTimeOf(fm), ReworkRate: reworkRate(fm),
		AssemblyLeadTimeHours: assemblyLeadTimeOf(fm),
	}
}
