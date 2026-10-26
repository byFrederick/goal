package api_test

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// andonReason is the reason of the andon pull the shell tests open on acme.
const andonReason = "rework rate over 40%"

// pullAndon pulls the andon cord on acme, as the local user, at t0.
func (h *harness) pullAndon(t *testing.T) {
	t.Helper()

	if code := h.call(t, http.MethodPost, acmeAndon, `{"reason":"`+andonReason+`"}`, nil); code != http.StatusCreated {
		t.Fatalf("pull the andon = %d; want 201", code)
	}
}

// waitingAtApprove submits an acme item and moves it to its human station: one decision. The
// submission also loads both factories.
func (h *harness) waitingAtApprove(t *testing.T) gen.Item {
	t.Helper()

	it := h.submit(t)
	if err := h.engine.Tick(inOrg(t)); err != nil {
		t.Fatal(err)
	}

	return it
}

func TestAPI_AttentionCountsEveryDecisionOfTheOrg(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.waitingAtApprove(t)
	h.seed(t, stMerge, domain.StateWaitingHuman, t0)
	h.seed(t, stMerge, domain.StateWaitingHuman, t0)
	h.seed(t, stBuild, domain.StateBlocked, t0)
	h.seed(t, stBuild, domain.StateQueued, t0)
	h.pullAndon(t)

	tests := []struct {
		org  string
		want string
	}{
		{
			org: "acme",
			want: "3 waiting; " + acmeID + ": 1 waiting, 0 blocked, andon " + andonReason + " by user:local at " + t0.Format(time.RFC3339) +
				", 0 connections; " + lineID + ": 2 waiting, 1 blocked, no andon, 0 connections",
		},
		{org: "example-labs", want: "0 waiting"},
	}

	for _, tt := range tests {
		var got gen.OrgAttention
		if code := h.call(t, http.MethodGet, "/v1/orgs/"+tt.org+"/attention", "", &got); code != http.StatusOK || attentionLine(got) != tt.want {
			t.Errorf("attention of %s = %d\n  %s\nwant\n  %s", tt.org, code, attentionLine(got), tt.want)
		}
	}
}

// attentionLine renders an organization's attention as one line, factories in order.
func attentionLine(a gen.OrgAttention) string {
	parts := []string{fmt.Sprintf("%d waiting", a.WaitingOnYou)}

	for _, f := range a.Factories {
		andon := "no andon"
		if p := f.Andon; p != nil {
			andon = fmt.Sprintf("andon %s by %s at %s", p.Reason, p.PulledBy, p.PulledAt.UTC().Format(time.RFC3339))
		}

		parts = append(parts, fmt.Sprintf("%s: %d waiting, %d blocked, %s, %d connections",
			f.FactoryId, f.WaitingOnYou, f.BlockedItems, andon, f.ConnectionsBlocked))
	}

	return strings.Join(parts, "; ")
}

func TestAPI_OverviewSummarizesEachFactory(t *testing.T) {
	t.Parallel()

	h := newHarness(t) // the clock reads 2026-10-23 09:00 UTC
	day := func(d, hour int) time.Time { return time.Date(2026, 10, d, hour, 0, 0, 0, time.UTC) }

	h.waitingAtApprove(t)
	h.pullAndon(t)
	h.finished(t, day(23, 5), day(23, 7), false)
	h.finished(t, day(21, 0), day(21, 4), false)
	h.finished(t, day(14, 0), day(14, 1), false) // the previous 7 days
	h.finished(t, day(9, 0), day(9, 1), false)   // before both
	h.seed(t, stMerge, domain.StateWaitingHuman, t0)

	open := h.seed(t, stBuild, domain.StateQueued, day(22, 0))

	run, err := h.store.StartRun(inOrg(t), ports.NewRun{FactoryID: lineID, ItemID: open.ID, Station: stBuild, Agent: agBackend, StartedAt: day(23, 8)})
	if err != nil {
		t.Fatal(err)
	}

	if err := h.store.FinishRun(inOrg(t), run, domain.RunResult{Status: domain.RunSucceeded, CostUSD: 0.5}, day(23, 8)); err != nil {
		t.Fatal(err)
	}

	var got gen.FactoryOverviewList
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/overview", "", &got); code != http.StatusOK {
		t.Fatalf("overview = %d", code)
	}

	want := []string{
		"7d 2026-10-17..2026-10-23, 2 waiting, $0.50 of 70",
		"acme stopped (" + andonReason + "); 0.00/day vs 0.00, 7 days; constraint none; 1 waiting [feature:1]; $0.00 of none",
		"line running; 0.29/day vs 0.14, 7 days, last 2026-10-23:1; constraint build; 1 waiting [feature:1]; $0.50 of 70",
	}

	if lines := overviewLines(got); strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("overview\n  %s\nwant\n  %s", strings.Join(lines, "\n  "), strings.Join(want, "\n  "))
	}
}

func TestAPI_OverviewWindowsAndBudgets(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.submit(t) // loads both factories; the item is queued, no decision

	tests := []struct {
		path string
		want string
	}{
		{path: "/v1/orgs/acme/overview?window=14d", want: "14d 2026-10-10..2026-10-23, 0 waiting, $0.00 of 140"},
		{path: "/v1/orgs/example-labs/overview", want: "7d 2026-10-17..2026-10-23, 0 waiting, $0.00 of none"},
	}

	for _, tt := range tests {
		var got gen.FactoryOverviewList
		if code := h.call(t, http.MethodGet, tt.path, "", &got); code != http.StatusOK || overviewLines(got)[0] != tt.want {
			t.Errorf("GET %s = %d %v, want %s", tt.path, code, overviewLines(got), tt.want)
		}
	}

	for _, w := range []string{"0d", "91d", "1w"} {
		if code := h.call(t, http.MethodGet, "/v1/orgs/acme/overview?window="+w, "", nil); code != http.StatusBadRequest {
			t.Errorf("window %s = %d, want 400", w, code)
		}
	}
}

// A factory never published is on the Factories home as a draft (the wizard resumes from there),
// with nothing to measure yet.
func TestAPI_OverviewListsFactoriesNeverPublishedAsDrafts(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.submit(t) // loads both factories

	if r := h.request(t, http.MethodPost, factoriesPath, `{"name":"support","start":{"kind":"blank"}}`, nil); r.status != http.StatusCreated {
		t.Fatalf("create = %d %s", r.status, r.body)
	}

	var got gen.FactoryOverviewList
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/overview", "", &got); code != http.StatusOK {
		t.Fatalf("overview = %d", code)
	}

	lines := overviewLines(got)
	if want := "support draft; 0.00/day vs 0.00, 7 days; constraint none; 0 waiting []; $0.00 of none"; !slices.Contains(lines, want) {
		t.Errorf("overview\n  %s\nwant a line %q", strings.Join(lines, "\n  "), want)
	}

	if len(got.Factories) != 3 {
		t.Errorf("overview lists %d factories, want acme, line and the draft", len(got.Factories))
	}
}

// overviewLines renders an overview as lines: the organization's totals, then one per factory.
func overviewLines(l gen.FactoryOverviewList) []string {
	budget := func(usd *float64) string {
		if usd == nil {
			return "none"
		}

		return fmt.Sprint(*usd)
	}

	out := []string{fmt.Sprintf("%s %s..%s, %d waiting, $%.2f of %s", l.Window, l.Since, l.Until, l.WaitingOnYou, l.SpendUsd, budget(l.BudgetUsd))}

	for _, o := range l.Factories {
		health := string(o.Health)
		if o.Andon != nil {
			health += " (" + o.Andon.Reason + ")"
		}

		days := fmt.Sprintf("%d days", len(o.Throughput.Days))
		if last := o.Throughput.Days[len(o.Throughput.Days)-1]; last.Done > 0 {
			days += fmt.Sprintf(", last %s:%d", last.Day, last.Done)
		}

		constraint := "none"
		if o.Constraint != nil {
			constraint = o.Constraint.Station
		}

		types := []string{}
		for _, tc := range o.Waiting.ItemsByType {
			types = append(types, fmt.Sprintf("%s:%d", tc.Type, tc.Count))
		}

		out = append(out, fmt.Sprintf("%s %s; %.2f/day vs %.2f, %s; constraint %s; %d waiting [%s]; $%.2f of %s",
			o.Factory.Name, health, o.Throughput.PerDay, o.Throughput.PreviousPerDay, days, constraint, o.Waiting.OnYou,
			strings.Join(types, " "), o.SpendUsd, budget(o.BudgetUsd)))
	}

	return out
}

func TestAPI_InboxListsDecisionsFirst(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	approve := h.waitingAtApprove(t) // changed at t0
	older := h.seed(t, stMerge, domain.StateWaitingHuman, t0.Add(-2*time.Hour))
	old := h.seed(t, stMerge, domain.StateWaitingHuman, t0.Add(-time.Hour))
	blocked := h.seed(t, stBuild, domain.StateBlocked, t0)

	every := []string{
		"decision:" + string(older.ID) + " line#1 at merge (action merge), since " + t0.Add(-2*time.Hour).Format(time.RFC3339),
		"decision:" + string(old.ID) + " line#2 at merge (action merge), since " + t0.Add(-time.Hour).Format(time.RFC3339),
		"decision:" + approve.Id + " acme#1 at approve, since " + t0.Format(time.RFC3339),
		"blocked:" + string(blocked.ID) + " line#3 at build, kind blocked of factory line, since " + t0.Format(time.RFC3339),
	}

	tests := []struct {
		name, path string
		truncated  bool
		want       []string
	}{
		{name: "decisions first, oldest first", path: "/v1/orgs/acme/inbox", want: every},
		{name: "the most recent, truncated", path: "/v1/orgs/acme/inbox?limit=3", truncated: true, want: every[1:]},
		{name: "one factory", path: "/v1/orgs/acme/inbox?factory=acme", want: every[2:3]},
		{name: "another organization", path: "/v1/orgs/example-labs/inbox", want: []string{}},
	}

	for _, tt := range tests {
		var got gen.Inbox
		if code := h.call(t, http.MethodGet, tt.path, "", &got); code != http.StatusOK || got.Truncated != tt.truncated ||
			strings.Join(entryLines(got), "\n") != strings.Join(tt.want, "\n") {
			t.Errorf("%s: %d truncated %v\n  %s\nwant truncated %v\n  %s", tt.name, code, got.Truncated,
				strings.Join(entryLines(got), "\n  "), tt.truncated, strings.Join(tt.want, "\n  "))
		}
	}
}

// entryLines renders an inbox's entries as lines.
func entryLines(in gen.Inbox) []string {
	out := []string{}

	for _, e := range in.Entries {
		ref := "-"
		if e.Item != nil {
			ref = e.Item.Ref
		}

		s := fmt.Sprintf("%s %s at %s", e.Id, ref, deref(e.Station))
		if e.Action != nil {
			s += " (action " + string(*e.Action) + ")"
		}

		if e.Kind != gen.InboxEntryKindDecision || !strings.HasPrefix(ref, e.Factory.Name+"#") {
			s += fmt.Sprintf(", kind %s of factory %s", e.Kind, e.Factory.Name)
		}

		out = append(out, s+", since "+e.Since.UTC().Format(time.RFC3339))
	}

	return out
}

func TestAPI_InboxRefusesBadQueries(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.submit(t)

	tests := []struct {
		path string
		want int
	}{
		{path: "/v1/orgs/acme/inbox?limit=0", want: http.StatusBadRequest},
		{path: "/v1/orgs/acme/inbox?limit=501", want: http.StatusBadRequest},
		{path: "/v1/orgs/acme/inbox?limit=x", want: http.StatusBadRequest},
		{path: "/v1/orgs/acme/inbox?factory=nope", want: http.StatusNotFound},
		{path: "/v1/orgs/example-labs/inbox?factory=acme", want: http.StatusNotFound},
	}

	for _, tt := range tests {
		if code := h.call(t, http.MethodGet, tt.path, "", nil); code != tt.want {
			t.Errorf("GET %s = %d, want %d", tt.path, code, tt.want)
		}
	}
}

func TestAPI_ShellOfAnUnknownOrgIsNotFound(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for _, path := range []string{"/v1/orgs/nope/attention", "/v1/orgs/nope/overview", "/v1/orgs/nope/inbox"} {
		if code := h.call(t, http.MethodGet, path, "", nil); code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, code)
		}
	}
}
