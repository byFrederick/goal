package api_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// oct is a time on a day of October 2026; the harness's clock reads Oct 23, 09:00 UTC.
func oct(d, hour int) time.Time { return time.Date(2026, 10, d, hour, 0, 0, 0, time.UTC) }

// A range is UTC days, both ends included; compare=previous adds the range of the same length
// just before; targets come from the definition.
func TestAPI_MetricsOverARangeWithThePreviousRange(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	h.finished(t, oct(20, 0), oct(20, 2), false) // 2 h, in the range
	h.finished(t, oct(17, 0), oct(17, 6), true)  // 6 h, reworked, on its first day
	h.finished(t, oct(16, 0), oct(16, 1), false) // 1 h, the previous range's last day
	h.finished(t, oct(10, 0), oct(10, 3), true)  // 3 h, the previous range's first day
	h.finished(t, oct(9, 0), oct(9, 1), false)   // before both

	var m gen.Metrics
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/metrics?from=2026-10-17&to=2026-10-23&compare=previous", "", &m); code != http.StatusOK {
		t.Fatalf("metrics = %d", code)
	}

	if got, want := rangeSummary(m), "2026-10-17..2026-10-23 (7d): 2 done, 0.29 a day, p85 5.40h, rework 0.50"; got != want {
		t.Errorf("metrics = %s, want %s", got, want)
	}

	if len(m.Throughput) != 7 || m.Throughput[0].Done != 1 || m.Throughput[3].Done != 1 {
		t.Errorf("throughput = %+v, want 7 days with Oct 17 and Oct 20 at 1", m.Throughput)
	}

	p := m.Previous
	if p == nil {
		t.Fatal("previous = null, want the range before")
	}

	if got, want := fmt.Sprintf("%s..%s: %d done, %.2f a day, p85 %.2fh, rework %.2f", p.From, p.To, p.ItemsDone, p.ThroughputPerDay, p.LeadTimeHours.P85, p.ReworkRate),
		"2026-10-10..2026-10-16: 2 done, 0.29 a day, p85 2.70h, rework 0.50"; got != want {
		t.Errorf("previous = %s, want %s", got, want)
	}

	if got := targetsSummary(m.Targets); got != "throughput <nil>, lead time 6 h, expedite 60 min" {
		t.Errorf("targets = %s, want lead time 6 h and expedite 60 min from the definition", got)
	}

	var plain gen.Metrics
	if h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/metrics?from=2026-10-17", "", &plain); plain.Previous != nil || plain.Type != nil {
		t.Errorf("metrics without compare = previous %+v type %v, want neither", plain.Previous, plain.Type)
	}
}

// targetsSummary renders targets on one line, <nil> where one is not set.
func targetsSummary(tg gen.Targets) string {
	return fmt.Sprintf("throughput %v, lead time %v h, expedite %v min",
		value(tg.ThroughputPerDay), value(tg.LeadTimeHours), value(tg.ExpediteLeadTimeMinutes))
}

// value is what p points to, or nil.
func value[T any](p *T) any {
	if p == nil {
		return nil
	}

	return *p
}

// Without a range the metrics cover the last 7 days; the window parameter still works.
func TestAPI_MetricsDefaultRange(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for _, query := range []string{"", "?window=7d", "?to=2026-10-23"} {
		var m gen.Metrics
		if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/acme/metrics"+query, "", &m); code != http.StatusOK ||
			m.From.String() != "2026-10-17" || m.To.String() != "2026-10-23" || m.Window != "7d" || len(m.Throughput) != 7 {
			t.Errorf("metrics%s = %d %s..%s %s, want the last 7 days", query, code, m.From, m.To, m.Window)
		}
	}
}

func TestAPI_MetricsRefusesBadRangesAndTypes(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for _, query := range []string{
		"?from=2026-10-20&to=2026-10-19", // ends before it starts
		"?to=2026-10-24",                 // ends after today
		"?from=2026-07-25&to=2026-10-23", // 91 days
		"?window=7d&from=2026-10-20",     // a window and a range
		"?from=Oct-20",                   // not a date
		"?compare=next",                  // not a comparison
		"?type=chore",                    // a type acme does not declare
		"?window=0d",
	} {
		if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/acme/metrics"+query, "", nil); code != http.StatusBadRequest {
			t.Errorf("metrics%s = %d, want 400", query, code)
		}
	}

	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/acme/metrics?from=2026-07-26&type=fix", "", nil); code != http.StatusOK {
		t.Errorf("metrics over 90 days of a declared type = %d, want 200", code)
	}
}

// type keeps one work type's items; long waits are counted at the constraint.
func TestAPI_MetricsByTypeWithLongWaitsAtTheConstraint(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	h.finished(t, oct(22, 0), oct(22, 1), false)
	h.seed(t, stBuild, domain.StateQueued, oct(21, 0)) // waiting at build for 57 h
	h.seed(t, stBuild, domain.StateQueued, oct(23, 8)) // for 1 h

	var m gen.Metrics
	h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/metrics", "", &m)

	if lw := m.LongWaits; lw == nil || lw.Station != stBuild || lw.Items != 1 || lw.ThresholdHours != 24 {
		t.Errorf("long waits = %+v, want 1 item at build over 24 h", lw)
	}

	var other gen.Metrics
	h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/metrics?type=chore", "", &other)

	if other.Type == nil || *other.Type != "chore" || other.ItemsDone != 0 || other.Wip != 0 || other.LongWaits == nil || other.LongWaits.Items != 0 {
		t.Errorf("metrics of chores = %+v, want none of the features", other)
	}

	var idle gen.Metrics
	h.call(t, http.MethodGet, "/v1/orgs/acme/factories/acme/metrics", "", &idle)

	if idle.LongWaits != nil {
		t.Errorf("long waits of a line without a constraint = %+v, want null", idle.LongWaits)
	}
}

func TestAPI_WeeklyThroughput(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	h.finished(t, oct(23, 0), oct(23, 8), false) // this week (Oct 17 to 23)
	h.finished(t, oct(17, 0), oct(17, 1), false) // this week's first day
	h.finished(t, oct(16, 0), oct(16, 1), false) // the week before
	h.finished(t, oct(2, 0), oct(2, 1), false)   // the day before the first of three weeks
	h.finished(t, oct(9, 0), oct(9, 1), false)   // the first week's last day

	var w gen.WeeklyThroughput
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/metrics/weekly?weeks=3", "", &w); code != http.StatusOK {
		t.Fatalf("weekly = %d", code)
	}

	weeks := make([]string, 0, len(w.Weeks))
	for _, wk := range w.Weeks {
		weeks = append(weeks, fmt.Sprintf("%s..%s:%d/%.2f", wk.From, wk.To, wk.Done, wk.PerDay))
	}

	if got, want := strings.Join(weeks, " "), "2026-10-03..2026-10-09:1/0.14 2026-10-10..2026-10-16:1/0.14 2026-10-17..2026-10-23:2/0.29"; got != want {
		t.Errorf("weeks = %s, want %s", got, want)
	}

	if w.To.String() != "2026-10-23" || w.Targets.LeadTimeHours == nil {
		t.Errorf("weekly to %s targets %+v, want today and the definition's targets", w.To, w.Targets)
	}

	var twelve gen.WeeklyThroughput
	if h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/metrics/weekly?to=2026-10-16", "", &twelve); len(twelve.Weeks) != 12 || twelve.Weeks[11].Done != 1 {
		t.Errorf("weekly to Oct 16 = %+v, want 12 weeks, the last with 1 done", twelve.Weeks)
	}

	for _, query := range []string{"?weeks=0", "?weeks=27", "?to=2026-10-24", "?type=chore"} {
		if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/acme/metrics/weekly"+query, "", nil); code != http.StatusBadRequest {
			t.Errorf("weekly%s = %d, want 400", query, code)
		}
	}
}

// rangeSummary renders a range's headline metrics on one line.
func rangeSummary(m gen.Metrics) string {
	p85 := "none"
	if m.LeadTimeHours != nil {
		p85 = fmt.Sprintf("%.2fh", m.LeadTimeHours.P85)
	}

	return fmt.Sprintf("%s..%s (%s): %d done, %.2f a day, p85 %s, rework %.2f",
		m.From, m.To, m.Window, m.ItemsDone, m.ThroughputPerDay, p85, m.ReworkRate)
}

// An assembly's lead time runs from its creation to its last part done; the range reports its
// percentiles and how many were done, the previous range null when none were.
func TestAPI_MetricsAssemblyLeadTime(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	assembly := h.assemble(t, []domain.NewPart{
		{Key: "a", Title: "Contract", Lane: laneAPI},
		{Key: "b", Title: "Store", Lane: laneAPI},
	})

	for i, after := range []time.Duration{time.Hour, 2 * time.Hour} {
		part, err := h.store.ItemByNumber(inOrg(t), lineID, assembly.Number+int64(i)+1)
		if err != nil {
			t.Fatal(err)
		}

		h.clock.Advance(after)
		h.move(t, part, domain.Position{Station: stDone, State: domain.StateDone}, func(tr *ports.Transition) { tr.DoneAt = h.clock.Now() })
	}

	h.clock.Advance(time.Minute) // the range ends now, excluded

	var m gen.Metrics
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/line/metrics?compare=previous", "", &m); code != http.StatusOK {
		t.Fatalf("metrics = %d", code)
	}

	if a := m.AssemblyLeadTimeHours; a == nil || a.Assemblies != 1 || a.P50 != 3 || a.P85 != 3 {
		t.Errorf("assembly lead time = %+v, want 1 assembly at 3 h", a)
	}

	if m.Previous == nil || m.Previous.AssemblyLeadTimeHours != nil {
		t.Errorf("previous = %+v, want a range with no assembly lead time", m.Previous)
	}
}
