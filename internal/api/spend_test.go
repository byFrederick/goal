package api_test

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

const orgSpendPath = "/v1/orgs/acme/spend"

// spendRun records a finished run of agent on a new item of factory, started at and costing usd.
func (h *harness) spendRun(t *testing.T, factory domain.FactoryID, agent, model string, at time.Time, usd float64) {
	t.Helper()

	station := stBuild
	if factory == acmeID {
		station = stInbox
	}

	it, err := h.store.CreateItem(inOrg(t), domain.NewItem{
		FactoryID: factory, Type: typeFeature, Title: "spent", Station: station,
		State: domain.StateQueued, Class: domain.ClassStandard, CreatedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}

	run, err := h.store.StartRun(inOrg(t), ports.NewRun{FactoryID: factory, ItemID: it.ID, Station: station, Agent: agent, Model: model, StartedAt: at})
	if err != nil {
		t.Fatal(err)
	}

	if err := h.store.FinishRun(inOrg(t), run, domain.RunResult{Status: domain.RunSucceeded, CostUSD: usd}, at); err != nil {
		t.Fatal(err)
	}
}

// seedSpend records runs on both factories (the clock reads Friday 2026-10-23 09:00 UTC): line
// spends $2 on Thursday, $3 on October 5 and $4 on September 15; acme $1 today. One line item
// merged on Thursday.
func (h *harness) seedSpend(t *testing.T) {
	t.Helper()

	at := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 8, 0, 0, 0, time.UTC) }

	h.spendRun(t, lineID, agBackend, "claude-fable-5-1", at(time.October, 22), 2)
	h.spendRun(t, lineID, agBackend, "claude-fable-5-1", at(time.October, 5), 3)
	h.spendRun(t, lineID, "frontend", "claude-haiku-5-5", at(time.September, 15), 4)
	h.spendRun(t, acmeID, "writer", "claude-fable-5-1", at(time.October, 23), 1)
	h.finished(t, at(time.October, 21), at(time.October, 22), false)
}

// unsetUSD stands for a figure the answer leaves null.
const unsetUSD = "null"

// usdOf renders an optional figure in dollars, or unsetUSD.
func usdOf(v *float64) string {
	if v == nil {
		return unsetUSD
	}

	return fmt.Sprintf("%.2f", *v)
}

// spendLine renders a spend answer's window and totals as one line.
func spendLine(s gen.OrgSpend) string {
	merged := unsetUSD
	if s.Merged != nil {
		merged = strconv.Itoa(*s.Merged)
	}

	return fmt.Sprintf("%s %s..%s complete=%t days=%d total=%.2f month=%s last=%s merged=%s per=%s rework=%s/%s weeks=%d",
		s.Window, s.Since.UTC().Format(time.RFC3339), s.Until.UTC().Format(time.RFC3339), s.Complete, len(s.Days), s.TotalUsd,
		usdOf(s.ThisMonthUsd), usdOf(s.LastMonthUsd), merged, usdOf(s.CostPerMergedUsd), usdOf(s.ReworkUsd), usdOf(s.ReworkShare), len(s.Weekly))
}

// rowsLine renders the per-factory and per-model rows: each day that spent, by agent, by model.
func rowsLine(s gen.OrgSpend) string {
	var days, agents, models []string

	for _, d := range s.Days {
		for _, f := range d.ByFactory {
			days = append(days, fmt.Sprintf("%s %s %.2f", d.Day, f.FactoryId[:4], f.Usd))
		}
	}

	for _, a := range s.ByAgent {
		agents = append(agents, fmt.Sprintf("%s/%s %.2f", a.FactoryId[:4], a.Agent, a.Usd))
	}

	for _, m := range s.ByModel {
		models = append(models, fmt.Sprintf("%s %.2f", m.Model, m.Usd))
	}

	return fmt.Sprintf("days [%s] agents [%s] models [%s]", strings.Join(days, "; "), strings.Join(agents, "; "), strings.Join(models, "; "))
}

// budgetLines renders each budget as one line.
func budgetLines(s gen.OrgSpend) []string {
	out := make([]string, 0, len(s.Budgets))

	for _, b := range s.Budgets {
		var conns []string
		for _, c := range b.ModelConnections {
			conns = append(conns, c.Name+" ("+c.Kind+")")
		}

		out = append(out, fmt.Sprintf("%s daily=%s weekly=%s today=%.2f week=%.2f %s (%s) models [%s]",
			b.Factory.Name, usdOf(b.DailyUsd), usdOf(b.WeeklyUsd), b.TodayUsd, b.WeekUsd, b.State, deref(b.Reason), strings.Join(conns, ", ")))
	}

	return out
}

// Each window has its bounds, days and totals; budgets are each factory's, acme stopped by its
// andon, both paid for by claude.
func TestGetOrgSpend_Windows(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.seedSpend(t)
	h.pullAndon(t)

	if _, err := h.conns.Create(inOrg(t), domain.NewConnection{Name: "claude", Kind: "anthropic", Secrets: map[string]string{"key": "sk-ant-test"}}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct{ query, want string }{
		{"", "week 2026-10-19T00:00:00Z..2026-10-23T09:00:00Z complete=true days=5 total=3.00 month=6.00 last=4.00 merged=1 per=3.00 rework=0.00/0.00 weeks=8"},
		{"?window=week", "week 2026-10-19T00:00:00Z..2026-10-23T09:00:00Z complete=true days=5 total=3.00 month=6.00 last=4.00 merged=1 per=3.00 rework=0.00/0.00 weeks=8"},
		{"?window=month", "month 2026-10-01T00:00:00Z..2026-10-23T09:00:00Z complete=true days=23 total=6.00 month=6.00 last=4.00 merged=1 per=6.00 rework=0.00/0.00 weeks=8"},
		{"?window=90d", "90d 2026-07-26T00:00:00Z..2026-10-23T09:00:00Z complete=true days=90 total=10.00 month=6.00 last=4.00 merged=1 per=10.00 rework=0.00/0.00 weeks=8"},
	} {
		t.Run("window"+c.query, func(t *testing.T) {
			t.Parallel()

			var got gen.OrgSpend
			if code := h.call(t, http.MethodGet, orgSpendPath+c.query, "", &got); code != http.StatusOK {
				t.Fatalf("GET spend%s = %d; want 200", c.query, code)
			}

			if line := spendLine(got); line != c.want {
				t.Errorf("spend%s\n  %s\nwant\n  %s", c.query, line, c.want)
			}

			want := []string{
				"acme daily=null weekly=null today=1.00 week=1.00 stopped (" + andonReason + ") models [claude (anthropic)]",
				"line daily=null weekly=70.00 today=0.00 week=2.00 running () models [claude (anthropic)]",
			}
			if lines := budgetLines(got); strings.Join(lines, "\n") != strings.Join(want, "\n") {
				t.Errorf("budgets\n  %s\nwant\n  %s", strings.Join(lines, "\n  "), strings.Join(want, "\n  "))
			}
		})
	}

	var got gen.OrgSpend
	if code := h.call(t, http.MethodGet, orgSpendPath+"?window=90d", "", &got); code != http.StatusOK {
		t.Fatalf("GET spend 90d = %d", code)
	}

	if rows, want := rowsLine(got), "days [2026-09-15 0b6f 4.00; 2026-10-05 0b6f 3.00; 2026-10-22 0b6f 2.00; 2026-10-23 8d4f 1.00] "+
		"agents [0b6f/backend 5.00; 0b6f/frontend 4.00; 8d4f/writer 1.00] models [claude-fable-5-1 6.00; claude-haiku-5-5 4.00]"; rows != want {
		t.Errorf("90d rows\n  %s\nwant\n  %s", rows, want)
	}

	if res := h.request(t, http.MethodGet, orgSpendPath+"?window=year", "", nil); res.status != http.StatusBadRequest {
		t.Errorf("GET spend?window=year = %d %s; want 400", res.status, res.body)
	}
}

// A weekly budget spent in the last 7×24 hours is over_budget, with the spend and the cap.
func TestGetOrgSpend_OverBudget(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.spendRun(t, lineID, agBackend, "claude-fable-5-1", t0.Add(-6*24*time.Hour), 71) // last week's Saturday: in the rolling window

	var got gen.OrgSpend
	if code := h.call(t, http.MethodGet, orgSpendPath, "", &got); code != http.StatusOK {
		t.Fatalf("GET spend = %d", code)
	}

	want := []string{
		"acme daily=null weekly=null today=0.00 week=0.00 running () models []",
		"line daily=null weekly=70.00 today=0.00 week=71.00 over_budget (spent $71.00 of the $70.00 weekly budget in the last 7 days) models []",
	}
	if lines := budgetLines(got); strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("budgets\n  %s\nwant\n  %s", strings.Join(lines, "\n  "), strings.Join(want, "\n  "))
	}

	if got.TotalUsd != 0 {
		t.Errorf("this week's total = %.2f; want 0, the run was last week", got.TotalUsd)
	}
}

// A member who sees acme alone gets acme's rows and budget only, and no total that line's spend
// is in; the owner sees both.
func TestGetOrgSpend_HidesInvisibleFactories(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)
	h.seedSpend(t)

	viewer := decode[gen.OrgSpend](t, h.as(t, viewerToken, http.MethodGet, orgSpendPath+"?window=90d", ""), http.StatusOK)

	if line, want := spendLine(viewer),
		"90d 2026-07-26T00:00:00Z..2026-10-23T09:00:00Z complete=false days=90 total=1.00 month=null last=null merged=null per=null rework=null/null weeks=0"; line != want {
		t.Errorf("the viewer's spend\n  %s\nwant\n  %s", line, want)
	}

	if rows, want := rowsLine(viewer), "days [2026-10-23 8d4f 1.00] agents [8d4f/writer 1.00] models []"; rows != want {
		t.Errorf("the viewer's rows\n  %s\nwant\n  %s", rows, want)
	}

	var dayTotal float64
	for _, d := range viewer.Days {
		dayTotal += d.Usd
	}

	if dayTotal != 1 {
		t.Errorf("the viewer's days sum to %.2f; want 1.00, acme's alone", dayTotal)
	}

	if lines := budgetLines(viewer); len(lines) != 1 || !strings.HasPrefix(lines[0], "acme ") {
		t.Errorf("the viewer's budgets = %q; want acme's alone", lines)
	}

	owner := decode[gen.OrgSpend](t, h.as(t, ownerToken, http.MethodGet, orgSpendPath+"?window=90d", ""), http.StatusOK)
	if !owner.Complete || owner.TotalUsd != 10 || len(owner.Budgets) != 2 {
		t.Errorf("the owner's spend = %s, %d budgets; want complete, 10.00 and both", spendLine(owner), len(owner.Budgets))
	}
}
