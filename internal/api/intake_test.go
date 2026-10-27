package api_test

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api"
	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

const (
	intakePath  = "/v1/orgs/acme/factories/acme/intake"
	flakyPath   = intakePath + "/triggers/flaky-test-on-main"
	flakyName   = "flaky-test-on-main"
	nightlyName = "nightly-audit"
	deployName  = "deploy-failed"
	typeFix     = "fix"
)

// intakeItem creates an item of acme from source (and trigger, when set) at the instant given.
func (h *harness) intakeItem(t *testing.T, source domain.ItemSource, trigger string, at time.Time) domain.Item {
	t.Helper()

	it, err := h.store.CreateItem(inOrg(t), domain.NewItem{
		FactoryID: acmeID, Type: typeFix, Title: "from " + string(source), Station: stInbox, State: domain.StateQueued,
		Class: domain.ClassStandard, CreatedAt: at, Source: source, Trigger: trigger,
	})
	if err != nil {
		t.Fatal(err)
	}

	return it
}

// fire records a firing of trigger at the instant given for the check dedupe: created with item when it is set,
// skipped with reason otherwise.
func (h *harness) fire(t *testing.T, trigger, dedupe string, item domain.ItemID, reason string, at time.Time) {
	t.Helper()

	out := domain.FiringSkipped
	if item != "" {
		out = domain.FiringCreated
	}

	_, _, err := h.intake.RecordFiring(inOrg(t), domain.TriggerFiring{
		FactoryID: acmeID, Trigger: trigger, Source: domain.TriggerGitHub, EventKey: "github.check_run.failed",
		IdempotencyKey: fmt.Sprintf("%s-%d", dedupe, at.UnixNano()), Dedupe: dedupe, Outcome: out, Reason: reason, ItemID: item, At: at,
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The sources count acme's 48 newest items by default, every source answered, over the days the
// oldest and newest of them were created; the triggers are the definition's, in its order.
func TestIntake_SourceTally(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for range 2 { // older than the newest 48
		h.intakeItem(t, domain.ItemFromAPI, "", oct(10, 0))
	}

	start := oct(20, 0)
	sources := []struct {
		src domain.ItemSource
		n   int
	}{{domain.ItemFromPeople, 20}, {domain.ItemFromCLI, 10}, {domain.ItemFromAPI, 5}, {domain.ItemFromSchedule, 6}, {domain.ItemFromWebhook, 3}, {domain.ItemFromGitHub, 4}}

	i := 0

	for _, s := range sources {
		for range s.n {
			h.intakeItem(t, s.src, "", start.Add(time.Duration(i)*time.Hour))
			i++
		}
	}

	var in gen.Intake
	if code := h.call(t, http.MethodGet, intakePath, "", &in); code != http.StatusOK {
		t.Fatalf("intake = %d", code)
	}

	got := fmt.Sprintf("%d items %s to %s: %+v", in.Items, in.From, in.To, in.Sources)
	if want := "48 items 2026-10-20 to 2026-10-21: {Api:5 Cli:10 Github:4 People:20 Schedule:6 Webhook:3}"; got != want {
		t.Errorf("intake = %s, want %s", got, want)
	}

	checkIntakeTriggers(t, in.Triggers)

	var five gen.Intake
	if h.call(t, http.MethodGet, intakePath+"?items=5", "", &five); five.Items != 5 || five.Sources.Github != 4 || five.Sources.Webhook != 1 {
		t.Errorf("intake of 5 items = %+v, want the newest 5: 4 from GitHub, 1 from a webhook", five)
	}

	for _, q := range []string{"?items=0", "?items=501"} {
		if code := h.call(t, http.MethodGet, intakePath+q, "", nil); code != http.StatusBadRequest {
			t.Errorf("intake%s = %d, want 400", q, code)
		}
	}
}

// checkIntakeTriggers checks acme's triggers as Intake shows them, in definition order.
func checkIntakeTriggers(t *testing.T, triggers []gen.IntakeTrigger) {
	t.Helper()

	var got []string

	for _, tr := range triggers {
		lane := "-"
		if tr.Create.Lane != nil {
			lane = *tr.Create.Lane
		}

		got = append(got, fmt.Sprintf("%s: %s on %s, when %q, enabled %v, creates %s at %s lane %s expedite %v, problem %v",
			tr.Name, tr.Source, tr.On, tr.When, tr.Enabled, tr.Create.Type, tr.Create.Station, lane, tr.Create.Expedite, tr.Problem != nil))
	}

	want := []string{
		`nightly-audit: schedule on schedule, when "0 6 * * MON-FRI, Europe/Berlin", enabled true, creates feature at inbox lane - expedite false, problem false`,
		`flaky-test-on-main: github on github.check_run.failed, when "github.check_run.failed", enabled true, creates fix at inbox lane - expedite true, problem true`,
		`deploy-failed: webhook on webhook.ci-hooks.deploy, when "webhook.ci-hooks.deploy", enabled false, creates fix at inbox lane - expedite false, problem true`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("triggers =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// A factory without items has no window, and zero for every source.
func TestIntake_SourceTallyEmpty(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	var in gen.Intake
	if code := h.call(t, http.MethodGet, intakePath, "", &in); code != http.StatusOK || in.Items != 0 || in.From != nil || in.To != nil ||
		in.Sources.People != 0 || len(in.Triggers) != 3 {
		t.Errorf("intake = %d %+v, want no items, no window and the 3 triggers", code, in)
	}

	for _, tr := range in.Triggers {
		if !slices.Equal(tr.Days, make([]int, 7)) {
			t.Errorf("%s days = %v, want 7 zeros", tr.Name, tr.Days)
		}
	}
}

// A trigger shows the items it created on each of the last 7 UTC days, this week's items, merges
// and spend, and its events newest first: created, merged or skipped with the reason.
func TestIntake_TriggerStats(t *testing.T) {
	t.Parallel()

	h := newHarness(t) // now is Oct 23, 09:00 UTC

	old := h.intakeItem(t, domain.ItemFromGitHub, flakyName, oct(16, 8)) // before the week and the 7 days
	h.fire(t, flakyName, "TestOld", old.ID, "", oct(16, 8))

	c := h.intakeItem(t, domain.ItemFromGitHub, flakyName, oct(21, 10))
	h.fire(t, flakyName, "TestOrgRLS", c.ID, "", oct(21, 10))
	h.fire(t, flakyName, "TestLeaseExpiry", "", "only.failuresInARow: failed once", oct(22, 12))

	a := h.intakeItem(t, domain.ItemFromGitHub, flakyName, oct(23, 1))
	h.fire(t, flakyName, "TestBoardStream", a.ID, "", oct(23, 1))

	b := h.intakeItem(t, domain.ItemFromGitHub, flakyName, oct(23, 8))
	h.fire(t, flakyName, "TestLeaseExpiry", b.ID, "", oct(23, 8))

	nightly := h.intakeItem(t, domain.ItemFromSchedule, nightlyName, oct(23, 4))
	h.fire(t, nightlyName, "", nightly.ID, "", oct(23, 4))

	h.mergedAndSpent(t, a, b, 1.5)

	var in gen.Intake
	h.call(t, http.MethodGet, intakePath, "", &in)

	days := map[string][]int{}
	for _, tr := range in.Triggers {
		days[tr.Name] = tr.Days
	}

	// Oct 17 to Oct 23: one on the 21st, two on the 23rd; the skipped event creates nothing.
	if want := []int{0, 0, 0, 0, 1, 0, 2}; !slices.Equal(days[flakyName], want) {
		t.Errorf("flaky days = %v, want %v", days[flakyName], want)
	}

	if want := []int{0, 0, 0, 0, 0, 0, 1}; !slices.Equal(days[nightlyName], want) {
		t.Errorf("nightly days = %v, want %v", days[nightlyName], want)
	}

	var d gen.IntakeTriggerDetail
	if code := h.call(t, http.MethodGet, flakyPath, "", &d); code != http.StatusOK {
		t.Fatalf("trigger = %d", code)
	}

	if d.Trigger.Name != flakyName || !slices.Equal(d.Trigger.Days, days[flakyName]) {
		t.Errorf("trigger = %+v, want flaky as the intake shows it", d.Trigger)
	}

	if w := d.Week; w.Created != 3 || w.Merged != 1 || w.SpendUsd != 1.5 {
		t.Errorf("week = %+v, want 3 created, 1 merged, $1.50", w)
	}

	events := eventLines(d.Events)

	want := []string{
		fmt.Sprintf("TestLeaseExpiry Oct 23 08:00 created #%d", b.Number),
		fmt.Sprintf("TestBoardStream Oct 23 01:00 merged #%d", a.Number),
		"TestLeaseExpiry Oct 22 12:00 skipped: only.failuresInARow: failed once",
		fmt.Sprintf("TestOrgRLS Oct 21 10:00 created #%d", c.Number),
		fmt.Sprintf("TestOld Oct 16 08:00 created #%d", old.Number),
	}
	if !slices.Equal(events, want) {
		t.Errorf("events =\n%q\nwant\n%q", events, want)
	}
}

// A GitHub or webhook trigger whose endpoint the factory does not have misses its connection; one
// whose endpoint refused deliveries in the last 7 days says how many.
func TestIntake_TriggerProblems(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	problems := func() map[string]string {
		t.Helper()

		var in gen.Intake
		h.call(t, http.MethodGet, intakePath, "", &in)

		out := map[string]string{}

		for _, tr := range in.Triggers {
			if tr.Problem != nil {
				out[tr.Name] = string(tr.Problem.Kind) + ": " + tr.Problem.Message
			}
		}

		return out
	}

	got := problems()
	if want := map[string]string{
		flakyName:  "missing_connection: The factory has no GitHub webhook endpoint",
		deployName: "missing_connection: The factory has no webhook endpoint named ci-hooks",
	}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("problems without endpoints = %v, want %v", got, want)
	}

	endpoint := func(name string, kind domain.WebhookKind) domain.WebhookEndpointID {
		t.Helper()

		s, err := h.intake.CreateEndpoint(inOrg(t), domain.NewWebhookEndpoint{FactoryID: acmeID, Name: name, Kind: kind})
		if err != nil {
			t.Fatal(err)
		}

		return s.Endpoint.ID
	}

	gh := endpoint("github-app", domain.WebhookGitHub)
	endpoint("ci-hooks", domain.WebhookGeneric)

	for _, at := range []time.Time{oct(10, 0), oct(20, 0), oct(23, 8)} { // the first is older than 7 days
		if _, err := h.intake.RecordDelivery(inOrg(t), domain.NewWebhookDelivery{
			EndpointID: gh, ReceivedAt: at, Outcome: domain.DeliveryRefused, Reason: "signature did not match",
		}); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := h.intake.RecordDelivery(inOrg(t), domain.NewWebhookDelivery{EndpointID: gh, ReceivedAt: oct(23, 8), Outcome: domain.DeliveryAccepted}); err != nil {
		t.Fatal(err)
	}

	got = problems()
	if want := map[string]string{flakyName: "refused_deliveries: 2 deliveries refused in the last 7 days"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("problems with endpoints = %v, want %v", got, want)
	}

	var d gen.IntakeTriggerDetail
	if h.call(t, http.MethodGet, flakyPath, "", &d); d.Trigger.Problem == nil || d.Trigger.Problem.Kind != gen.RefusedDeliveries {
		t.Errorf("trigger problem = %+v, want refused deliveries", d.Trigger.Problem)
	}
}

// A trigger the current definition does not have is not found, as is a factory the caller may
// not see; a caller refused the factory gets 403.
func TestIntakeTrigger_NotFound(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	var p gen.Problem
	if code := h.call(t, http.MethodGet, intakePath+"/triggers/no-such-trigger", "", &p); code != http.StatusNotFound ||
		p.Code == nil || *p.Code != "trigger-not-found" {
		t.Errorf("unknown trigger = %d %+v, want 404 trigger-not-found", code, p)
	}

	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories/no-such-factory/intake", "", &p); code != http.StatusNotFound {
		t.Errorf("unknown factory = %d, want 404", code)
	}

	tests := []struct {
		name string
		deny func(api.AccessRequest) error
		want int
	}{
		{name: "hidden", deny: func(r api.AccessRequest) error {
			if r.Factory != "" {
				return ports.ErrNotFound
			}

			return nil
		}, want: http.StatusNotFound},
		{name: "refused on the factory", deny: func(r api.AccessRequest) error {
			if r.Factory != "" {
				return errors.New("not this factory")
			}

			return nil
		}, want: http.StatusForbidden},
	}

	for _, tt := range tests {
		h.seam.deny(tt.deny)

		for _, path := range []string{intakePath, flakyPath} {
			if res := h.request(t, http.MethodGet, path, "", nil); res.status != tt.want {
				t.Errorf("%s: %s = %d %s, want %d", tt.name, path, res.status, res.body, tt.want)
			}
		}
	}
}

// mergedAndSpent records a merge of merged and a finished run of spent that cost usd.
func (h *harness) mergedAndSpent(t *testing.T, merged, spent domain.Item, usd float64) {
	t.Helper()

	if err := h.store.AppendEvents(inOrg(t), domain.Event{
		FactoryID: acmeID, ItemID: merged.ID, At: oct(23, 2), Kind: domain.EventMerged, Station: stInbox, Actor: domain.ActorEngine,
	}); err != nil {
		t.Fatal(err)
	}

	run, err := h.store.StartRun(inOrg(t), ports.NewRun{FactoryID: acmeID, ItemID: spent.ID, Station: stInbox, Agent: "writer", Model: "m", StartedAt: oct(23, 8)})
	if err != nil {
		t.Fatal(err)
	}

	if err := h.store.FinishRun(inOrg(t), run, domain.RunResult{Status: domain.RunSucceeded, CostUSD: usd}, oct(23, 9)); err != nil {
		t.Fatal(err)
	}
}

// eventLines are a trigger's events as one line each: title, instant, outcome, item and reason.
func eventLines(events []gen.IntakeEvent) []string {
	var out []string

	for _, e := range events {
		s := fmt.Sprintf("%s %s %s", e.Title, e.At.UTC().Format("Jan 2 15:04"), e.Outcome)
		if e.ItemNumber != nil {
			s += fmt.Sprintf(" #%d", *e.ItemNumber)
		}

		if e.Reason != nil {
			s += ": " + *e.Reason
		}

		out = append(out, s)
	}

	return out
}
