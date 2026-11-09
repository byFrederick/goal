package api_test

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
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

// intakeItem creates an item of acme from trigger (none when "") at the instant given.
func (h *harness) intakeItem(t *testing.T, trigger string, at time.Time) domain.Item {
	t.Helper()

	it, err := h.store.CreateItem(inOrg(t), domain.NewItem{
		FactoryID: acmeID, Type: typeFix, Title: "from " + cmp.Or(trigger, "a person"), Station: stInbox, State: domain.StateQueued,
		Class: domain.ClassStandard, CreatedAt: at, Trigger: trigger,
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

// items, from and to count acme's 48 newest items by default, over the days the oldest and newest
// of them were created; the triggers are the definition's, in its order.
func TestIntake_NewestItems(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for range 2 { // older than the newest 48
		h.intakeItem(t, "", oct(10, 0))
	}

	start := oct(20, 0)
	for i := range 48 {
		h.intakeItem(t, "", start.Add(time.Duration(i)*time.Hour))
	}

	var in gen.Intake
	if code := h.call(t, http.MethodGet, intakePath, "", &in); code != http.StatusOK {
		t.Fatalf("intake = %d", code)
	}

	if got, want := fmt.Sprintf("%d items %s to %s", in.Items, in.From, in.To), "48 items 2026-10-20 to 2026-10-21"; got != want {
		t.Errorf("intake = %s, want %s", got, want)
	}

	checkIntakeTriggers(t, in.Triggers)

	var five gen.Intake
	if h.call(t, http.MethodGet, intakePath+"?items=5", "", &five); five.Items != 5 || five.From == nil || five.From.String() != "2026-10-21" {
		t.Errorf("intake of 5 items = %+v, want the newest 5, from 2026-10-21", five)
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
		// The keys written before ADR 0040 decision 14 show as its forms.
		`flaky-test-on-main: github on github.check_run, when "github.check_run", enabled true, creates fix at inbox lane - expedite true, problem true`,
		`deploy-failed: webhook on ci-hooks.deploy, when "ci-hooks.deploy", enabled false, creates fix at inbox lane - expedite false, problem true`,
		`new-incidents: schedule on schedule, when "*/5 * * * *, UTC", enabled true, creates fix at inbox lane - expedite false, problem false`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("triggers =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// A factory without items has no window and counts none.
func TestIntake_NewestItemsEmpty(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	var in gen.Intake
	if code := h.call(t, http.MethodGet, intakePath, "", &in); code != http.StatusOK || in.Items != 0 || in.From != nil || in.To != nil ||
		len(in.Triggers) != 4 {
		t.Errorf("intake = %d %+v, want no items, no window and the 4 triggers", code, in)
	}

	for _, tr := range in.Triggers {
		if !slices.Equal(tr.Days, make([]int, 7)) {
			t.Errorf("%s days = %v, want 7 zeros", tr.Name, tr.Days)
		}
	}
}

// Items whose creator recorded no source count too: items reads no source.
func TestIntake_NewestItemsCountUnknownSources(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for i := range 3 {
		h.intakeItem(t, "", oct(20, i))
	}

	h.intakeItem(t, flakyName, oct(21, 2))

	var in gen.Intake
	if code := h.call(t, http.MethodGet, intakePath, "", &in); code != http.StatusOK {
		t.Fatalf("intake = %d", code)
	}

	if got, want := fmt.Sprintf("%d items %s to %s", in.Items, in.From, in.To), "4 items 2026-10-20 to 2026-10-21"; got != want {
		t.Errorf("intake = %s, want %s", got, want)
	}
}

// A trigger shows the items it created on each of the last 7 UTC days, this week's items, merges
// and spend, and its events newest first: created, merged or skipped with the reason.
func TestIntake_TriggerStats(t *testing.T) {
	t.Parallel()

	h := newHarness(t) // now is Oct 23, 09:00 UTC

	old := h.intakeItem(t, flakyName, oct(16, 8)) // before the week and the 7 days
	h.fire(t, flakyName, "TestOld", old.ID, "", oct(16, 8))

	c := h.intakeItem(t, flakyName, oct(21, 10))
	h.fire(t, flakyName, "TestOrgRLS", c.ID, "", oct(21, 10))
	h.fire(t, flakyName, "TestLeaseExpiry", "", "only.failuresInARow: failed once", oct(22, 12))

	a := h.intakeItem(t, flakyName, oct(23, 1))
	h.fire(t, flakyName, "TestBoardStream", a.ID, "", oct(23, 1))

	b := h.intakeItem(t, flakyName, oct(23, 8))
	h.fire(t, flakyName, "TestLeaseExpiry", b.ID, "", oct(23, 8))

	nightly := h.intakeItem(t, nightlyName, oct(23, 4))
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

	endpoint := func(name string, sender domain.WebhookSender) domain.WebhookEndpointID {
		t.Helper()

		if _, err := h.secrets.Create(inOrg(t), domain.NewSecret{Name: domain.WebhookSecretName(name), Value: "test-secret"}, domain.LocalUser); err != nil {
			t.Fatal(err)
		}

		w, err := h.intake.CreateWebhook(inOrg(t), domain.NewWebhook{Name: name, Sender: sender, Secret: domain.WebhookSecretName(name)})
		if err != nil {
			t.Fatal(err)
		}

		if _, err := h.intake.PatchWebhook(inOrg(t), w.ID, domain.WebhookPatch{Open: []domain.WebhookOpening{{FactoryID: acmeID}}}); err != nil {
			t.Fatal(err)
		}

		return w.ID
	}

	gh := endpoint("github-app", domain.SenderGitHub)
	endpoint("ci-hooks", domain.SenderStandard)

	for _, at := range []time.Time{oct(10, 0), oct(20, 0), oct(23, 8)} { // the first is older than 7 days
		if _, err := h.intake.RecordDelivery(inOrg(t), domain.NewWebhookDelivery{
			EndpointID: gh, ReceivedAt: at, Outcome: domain.DeliveryRefused, Reason: signatureMismatch,
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

// Deliveries no trigger names count as delivered, so they leave the refused deliveries a
// trigger's problem counts as they are.
func TestIntake_RefusedIgnoresUnnamedDeliveries(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	if _, err := h.secrets.Create(inOrg(t), domain.NewSecret{Name: domain.WebhookSecretName("github-app"), Value: "test-secret"}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	w, err := h.intake.CreateWebhook(inOrg(t), domain.NewWebhook{Name: "github-app", Sender: domain.SenderGitHub, Secret: domain.WebhookSecretName("github-app")})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := h.intake.PatchWebhook(inOrg(t), w.ID, domain.WebhookPatch{Open: []domain.WebhookOpening{{FactoryID: acmeID}}}); err != nil {
		t.Fatal(err)
	}

	for i, at := range []time.Time{oct(20, 0), oct(23, 8)} {
		if _, err := h.intake.RecordDelivery(inOrg(t), domain.NewWebhookDelivery{
			EndpointID: w.ID, ReceivedAt: at, Outcome: domain.DeliveryRefused, Reason: signatureMismatch,
		}); err != nil {
			t.Fatal(err)
		}

		h.receiveUnnamed(t, string(w.ID), fmt.Sprintf("unnamed-%d", i), at)
	}

	h.receiveUnnamed(t, string(w.ID), "unnamed-only", oct(21, 0))

	var in gen.Intake
	h.call(t, http.MethodGet, intakePath, "", &in)

	i := slices.IndexFunc(in.Triggers, func(tr gen.IntakeTrigger) bool { return tr.Name == flakyName })
	if i < 0 {
		t.Fatalf("triggers = %+v; want flaky", in.Triggers)
	}

	if p := in.Triggers[i].Problem; p == nil || p.Message != "2 deliveries refused in the last 7 days" {
		t.Errorf("flaky problem = %+v; want the 2 refused deliveries only", p)
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

	run, err := h.store.StartRun(inOrg(t), ports.NewRun{FactoryID: acmeID, ItemID: spent.ID, Station: stInbox, Agent: writerAgent, Model: "m", StartedAt: oct(23, 8)})
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

// scheduled records a firing of nightly-audit for the instant at, created with item when it is
// set and skipped otherwise, and returns it.
func (h *harness) scheduled(t *testing.T, item domain.ItemID, at time.Time) domain.TriggerFiring {
	t.Helper()

	f := domain.TriggerFiring{
		FactoryID: acmeID, Trigger: nightlyName, Source: domain.TriggerSchedule, EventKey: domain.OnSchedule,
		IdempotencyKey: at.UTC().Format(time.RFC3339), Outcome: domain.FiringSkipped, Reason: "limits.perDay: earlier", At: at,
	}
	if item != "" {
		f.Outcome, f.ItemID, f.Reason = domain.FiringCreated, item, ""
	}

	got, _, err := h.intake.RecordFiring(inOrg(t), f)
	if err != nil {
		t.Fatal(err)
	}

	return got
}

const nightlyTry = intakePath + "/triggers/" + nightlyName + "/try"

// tryBody is a try's request body naming firing.
func tryBody(firing string) string {
	return `{"firing":"` + firing + `"}`
}

// A schedule firing the trigger would create an item for answers would_create with the item, each
// step passed, and creates nothing; the trigger's events name their ids for it.
func TestTryTrigger_Matched(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	f := h.scheduled(t, "", oct(22, 4))

	var d gen.IntakeTriggerDetail
	if h.call(t, http.MethodGet, intakePath+"/triggers/"+nightlyName, "", &d); len(d.Events) != 1 || d.Events[0].Id != f.ID {
		t.Fatalf("events = %+v; want the firing %s", d.Events, f.ID)
	}

	var got gen.TriggerTry
	if code := h.call(t, http.MethodPost, nightlyTry, tryBody(f.ID), &got); code != http.StatusOK {
		t.Fatalf("try = %d", code)
	}

	checkWouldCreate(t, got, f)

	if it := got.Item; it == nil || it.Title != "Nightly audit" || it.Type != "feature" || it.Station != stInbox || it.Expedite ||
		!strings.Contains(it.Brief, "untrusted") {
		t.Errorf("item = %+v; want the Nightly audit feature at inbox, its brief marking the event untrusted", got.Item)
	}

	h.wantTriedOnly(t, f)
}

// wantTriedOnly checks that acme has no item and one trigger.tried entry, for nightly-audit and f.
func (h *harness) wantTriedOnly(t *testing.T, f domain.TriggerFiring) {
	t.Helper()

	if items, err := h.store.ListItems(inOrg(t), ports.ItemQuery{FactoryID: acmeID}); err != nil || len(items) != 0 {
		t.Errorf("items = %d, %v; want none", len(items), err)
	}

	tried, err := h.tenancy.AuditLog(inOrg(t), ports.AuditQuery{Limit: 10, Action: domain.AuditTriggerTried})
	if err != nil || len(tried) != 1 || tried[0].TargetID != nightlyName || tried[0].Data["firing"] != f.ID {
		t.Errorf("trigger.tried = %+v, %v; want one naming nightly-audit and the firing", tried, err)
	}
}

// checkWouldCreate checks that got, a try of f, matched and would create an item, each step passed.
func checkWouldCreate(t *testing.T, got gen.TriggerTry, f domain.TriggerFiring) {
	t.Helper()

	if !got.Matched || got.Outcome != gen.TriggerTryOutcomeWouldCreate || got.Reason != nil || got.Firing != f.ID || got.ModelCheck != nil {
		t.Errorf("try = %+v; want matched, would_create, no reason, no model check", got)
	}

	var steps []string
	for _, s := range got.Steps {
		steps = append(steps, string(s.Step)+" "+string(s.Outcome))
	}

	if want := "when passed, create passed, limits.perDay passed"; strings.Join(steps, ", ") != want {
		t.Errorf("steps = %v; want %s", steps, want)
	}
}

// A firing the trigger would skip answers skipped, with the step that skips it and its reason, and
// the later steps not reached.
func TestTryTrigger_Skipped(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	today := h.intakeItem(t, nightlyName, h.clock.Now().Add(-time.Hour))
	f := h.scheduled(t, today.ID, h.clock.Now().Add(-time.Hour))

	var got gen.TriggerTry
	if code := h.call(t, http.MethodPost, nightlyTry, tryBody(f.ID), &got); code != http.StatusOK {
		t.Fatalf("try = %d", code)
	}

	want := "limits.perDay: 1 items created today (UTC), the limit is 1"
	if !got.Matched || got.Outcome != gen.TriggerTryOutcomeSkipped || deref(got.Reason) != want {
		t.Errorf("try = matched %t, %s %q; want matched, skipped %q", got.Matched, got.Outcome, deref(got.Reason), want)
	}

	if last := got.Steps[len(got.Steps)-1]; last.Step != gen.LimitsPerDay || last.Outcome != gen.TriggerTryStepOutcomeSkipped ||
		deref(last.Reason) != "1 items created today (UTC), the limit is 1" {
		t.Errorf("perDay step = %+v; want skipped with its reason", last)
	}
}

// A try answers 404 for a firing not among the trigger's newest and for an unknown trigger, 422
// for a firing whose event was not stored, 400 without a firing, and 403 to a viewer.
func TestTryTrigger_Refusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	f := h.scheduled(t, "", oct(22, 4))
	h.fire(t, flakyName, "TestOrgRLS", "", "only.dedupe: open", oct(22, 5))

	var d gen.IntakeTriggerDetail
	h.call(t, http.MethodGet, flakyPath, "", &d)

	if len(d.Events) != 1 {
		t.Fatalf("flaky events = %+v; want one", d.Events)
	}

	for _, c := range []struct {
		what, path, body string
		status           int
		code             string
	}{
		{"an unknown firing", nightlyTry, tryBody("no-such-firing"), http.StatusNotFound, "firing-not-found"},
		{"another trigger's firing", nightlyTry, tryBody(d.Events[0].Id), http.StatusNotFound, "firing-not-found"},
		{"an unknown trigger", intakePath + "/triggers/no-such-trigger/try", tryBody(f.ID), http.StatusNotFound, "trigger-not-found"},
		{"a GitHub firing", flakyPath + "/try", tryBody(d.Events[0].Id), http.StatusUnprocessableEntity, "event-not-stored"},
	} {
		if code, p := problemOfCall(t, h, http.MethodPost, c.path, c.body); code != c.status || deref(p.Code) != c.code {
			t.Errorf("%s: %d %s, want %d %s", c.what, code, deref(p.Code), c.status, c.code)
		}
	}

	if code := h.call(t, http.MethodPost, nightlyTry, `{"firing":""}`, nil); code != http.StatusBadRequest {
		t.Errorf("a try without a firing = %d, want 400", code)
	}

	h.as(acmeMember(domain.FactoryViewer))

	if code, p := problemOfCall(t, h, http.MethodPost, nightlyTry, tryBody(f.ID)); code != http.StatusForbidden || deref(p.Code) != "forbidden" {
		t.Errorf("a viewer's try = %d %s, want 403 forbidden", code, deref(p.Code))
	}

	if tried, _ := h.tenancy.AuditLog(inOrg(t), ports.AuditQuery{Limit: 10, Action: domain.AuditTriggerTried}); len(tried) != 0 {
		t.Errorf("trigger.tried entries = %d, want none for refused tries", len(tried))
	}
}

// Intake answers the definition's disabled triggers as its suggestions, in definition order, so
// the web names no station or work type of its own (ADR 0040 decision 17).
func TestIntake_SuggestionsAreTheDisabledTriggers(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	var in gen.Intake
	if code := h.call(t, http.MethodGet, intakePath, "", &in); code != http.StatusOK {
		t.Fatalf("intake = %d", code)
	}

	if len(in.Suggestions) != 1 {
		t.Fatalf("suggestions = %+v; want deploy-failed only", in.Suggestions)
	}

	s := in.Suggestions[0]
	if got := fmt.Sprintf("%s: %s on %s, when %q, creates %s at %s expedite %v titled %q", s.Name, s.Source, s.On, s.When,
		s.Create.Type, s.Create.Station, s.Create.Expedite, s.Create.Title); got != `deploy-failed: webhook on ci-hooks.deploy, when "ci-hooks.deploy", creates fix at inbox expedite false titled "A deploy failed"` {
		t.Errorf("suggestion = %s", got)
	}
}

// A schedule's missed gap shows among the trigger's events with its outcome, its reason and how
// many instants it covers (ADR 0040 decision 16).
func TestIntakeTrigger_MissedEvent(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	if _, _, err := h.intake.RecordFiring(inOrg(t), domain.TriggerFiring{
		FactoryID: acmeID, Trigger: nightlyName, Source: domain.TriggerSchedule, EventKey: domain.OnSchedule,
		IdempotencyKey: "missed/2026-10-20T04:00:00Z", Outcome: domain.FiringMissed, Missed: 3,
		Reason: "3 instants were not fired", At: oct(22, 4),
	}); err != nil {
		t.Fatal(err)
	}

	var d gen.IntakeTriggerDetail
	if code := h.call(t, http.MethodGet, intakePath+"/triggers/"+nightlyName, "", &d); code != http.StatusOK || len(d.Events) != 1 {
		t.Fatalf("trigger = %d %+v; want its one event", code, d.Events)
	}

	if e := d.Events[0]; e.Outcome != gen.IntakeEventOutcomeMissed || e.Missed != 3 || e.ItemNumber != nil ||
		e.Reason == nil || *e.Reason != "3 instants were not fired" {
		t.Errorf("event = %+v; want missed, 3 instants, with its reason", e)
	}
}

const (
	incidentsName       = "new-incidents"
	incidentsPath       = intakePath + "/triggers/" + incidentsName + "/checks"
	unknownTrigger      = intakePath + "/triggers/no-such-trigger"
	codeTriggerNotFound = "trigger-not-found"
)

// check runs a check of new-incidents queued at the instant given through the intake: done with answer (filing
// an item for each file entry, at most perCheck 2), or failed with reason when answer is "".
func (h *harness) check(t *testing.T, at time.Time, answer, reason string) {
	t.Helper()

	if _, _, err := h.intake.Fire(inOrg(t), domain.TriggerFiring{
		FactoryID: acmeID, Trigger: incidentsName, Source: domain.TriggerSchedule, EventKey: domain.OnSchedule,
		IdempotencyKey: at.UTC().Format(time.RFC3339), Outcome: domain.FiringChecked, CheckState: domain.CheckQueued, At: at,
	}, nil, domain.FireLimits{}); err != nil {
		t.Fatal(err)
	}

	_, l, err := h.intake.ClaimCheck(inOrg(t), acmeID, "goald-a", at, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	res := domain.CheckResult{State: domain.CheckFailed, Reason: reason, At: at.Add(time.Minute)}

	if answer != "" {
		a, err := domain.ParseCheckAnswer(json.RawMessage(answer))
		if err != nil {
			t.Fatal(err)
		}

		var items []domain.CheckItem
		for _, f := range a.File {
			items = append(items, domain.CheckItem{Key: f.Key, Item: domain.NewItem{
				FactoryID: acmeID, Type: typeFix, Title: f.Title, Body: f.Brief, Station: stInbox, State: domain.StateQueued, CreatedAt: at,
				Origin: domain.OriginTrigger, Trigger: incidentsName, Untrusted: true,
			}})
		}

		res = domain.CheckResult{
			State: domain.CheckDone, CostUSD: 0.03, Decisions: a.Decisions(), Saved: a.State, Items: items, Limits: domain.CheckLimits{PerCheck: 2},
			At: at.Add(time.Minute),
		}
	}

	if err := h.intake.FinishCheck(inOrg(t), l, res); err != nil {
		t.Fatal(err)
	}
}

// The trigger's checks list newest first, each with when, its state, cost and failure, and what it
// filed (with the item's ref), noted and skipped with reasons.
func TestTriggerChecks_List(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.check(t, oct(22, 4), `{"file": [{"key": "INC-1", "title": "API down", "brief": "5xx"}, {"key": "INC-2", "title": "Disk", "brief": "db"},
		{"key": "INC-3", "title": "Late", "brief": "x"}], "skip": [{"key": "INC-4", "reason": "a test page"}]}`, "")
	h.check(t, oct(22, 5), "", "timed out after 5m0s")

	var got gen.TriggerCheckList
	if code := h.call(t, http.MethodGet, incidentsPath, "", &got); code != http.StatusOK || len(got.Checks) != 2 {
		t.Fatalf("checks = %d %+v; want two", code, got)
	}

	if failed := got.Checks[0]; failed.State != gen.TriggerCheckStateFailed || deref(failed.Reason) != "timed out after 5m0s" || len(failed.Filed) != 0 ||
		failed.CheckedAt == nil {
		t.Errorf("newest check = %+v; want failed with its reason", failed)
	}

	expectDoneCheck(t, got.Checks[1])

	if code := h.call(t, http.MethodGet, incidentsPath+"?limit=1", "", &got); code != http.StatusOK || len(got.Checks) != 1 {
		t.Errorf("limit=1 = %d, %d checks; want one", code, len(got.Checks))
	}

	if code, p := problemOfCall(t, h, http.MethodGet, unknownTrigger+"/checks", ""); code != http.StatusNotFound || deref(p.Code) != codeTriggerNotFound {
		t.Errorf("an unknown trigger's checks = %d %s; want 404 %s", code, deref(p.Code), codeTriggerNotFound)
	}
}

// expectDoneCheck checks the done check with what it filed and skipped.
func expectDoneCheck(t *testing.T, done gen.TriggerCheck) {
	t.Helper()

	filed := done.Filed
	if len(filed) != 2 || filed[0].Key != "INC-1" || deref(filed[0].Ref) != "#1" {
		t.Errorf("filed = %+v; want INC-1 and INC-2 filed as #1 and #2", filed)
	}

	if done.State != gen.TriggerCheckStateDone || done.CostUsd != 0.03 || done.Try || done.Reason != nil || len(done.Noted) != 0 {
		t.Errorf("done check = %+v; want done, its cost and no notes", done)
	}

	wantSkips := []gen.TriggerCheckSkip{{Key: "INC-3", Reason: domain.CheckOverLimit}, {Key: "INC-4", Reason: "a test page"}}
	if !reflect.DeepEqual(done.Skipped, wantSkips) {
		t.Errorf("skipped = %+v; want %+v", done.Skipped, wantSkips)
	}
}

// Try queues a try check and answers 202 with it, writing a trigger.tried entry; it answers 409
// still-checking while a check is queued, 422 no-check for a trigger without one, and 404 for an
// unknown trigger.
func TestTriggerChecks_Try(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	var got gen.TriggerCheck
	if code := h.call(t, http.MethodPost, incidentsPath, "", &got); code != http.StatusAccepted || !got.Try || got.State != gen.TriggerCheckStateQueued {
		t.Fatalf("try = %d %+v; want 202 with a queued try", code, got)
	}

	tried, err := h.tenancy.AuditLog(inOrg(t), ports.AuditQuery{Limit: 10, Action: domain.AuditTriggerTried})
	if err != nil || len(tried) != 1 || tried[0].TargetID != incidentsName || tried[0].Data["firing"] != got.Id {
		t.Errorf("trigger.tried = %+v, %v; want one naming the try", tried, err)
	}

	h.clock.Advance(time.Second)

	for _, c := range []struct {
		what, path string
		status     int
		code       string
	}{
		{"a try while one is queued", incidentsPath, http.StatusConflict, "still-checking"},
		{"a trigger without a check", intakePath + "/triggers/" + nightlyName + "/checks", http.StatusUnprocessableEntity, "no-check"},
		{"an unknown trigger", unknownTrigger + "/checks", http.StatusNotFound, codeTriggerNotFound},
	} {
		if code, p := problemOfCall(t, h, http.MethodPost, c.path, ""); code != c.status || deref(p.Code) != c.code {
			t.Errorf("%s: %d %s, want %d %s", c.what, code, deref(p.Code), c.status, c.code)
		}
	}
}

// filedAt creates an open item of acme at its inbox, as n says.
func (h *harness) filedAt(t *testing.T, n domain.NewItem) domain.Item {
	t.Helper()

	n.FactoryID, n.Type, n.Station, n.State, n.Class = acmeID, typeFix, stInbox, domain.StateQueued, domain.ClassStandard
	if n.CreatedAt.IsZero() {
		n.CreatedAt = t0
	}

	it, err := h.store.CreateItem(inOrg(t), n)
	if err != nil {
		t.Fatal(err)
	}

	return it
}

// Origins count acme's items created in the window, ending now, by origin, zero included; the
// window is 30d unless asked for, and sources answer as before.
func TestIntake_OriginsByWindow(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for _, n := range []struct {
		origin domain.Origin
		age    time.Duration
	}{
		{domain.OriginPerson, time.Hour},
		{domain.OriginPerson, 10 * 24 * time.Hour},
		{domain.OriginTrigger, 2 * 24 * time.Hour},
		{domain.OriginFollowUp, 40 * 24 * time.Hour},
		{domain.OriginPlan, 100 * 24 * time.Hour},
	} {
		h.filedAt(t, domain.NewItem{Title: string(n.origin), Origin: n.origin, CreatedAt: t0.Add(-n.age)})
	}

	for _, c := range []struct{ query, want string }{
		{"", "30d {FollowUp:0 Person:2 Plan:0 Trigger:1}"},
		{"?window=7d", "7d {FollowUp:0 Person:1 Plan:0 Trigger:1}"},
		{"?window=90d", "90d {FollowUp:1 Person:2 Plan:0 Trigger:1}"},
	} {
		var in gen.Intake
		if code := h.call(t, http.MethodGet, intakePath+c.query, "", &in); code != http.StatusOK || in.Window == nil || in.Origins == nil {
			t.Fatalf("intake%s = %d %+v; want 200 with a window and origins", c.query, code, in)
		}

		if got := fmt.Sprintf("%s %+v", *in.Window, *in.Origins); got != c.want {
			t.Errorf("intake%s = %s; want %s", c.query, got, c.want)
		}

		if in.Items != 5 {
			t.Errorf("intake%s items = %d; want the 5 items as before", c.query, in.Items)
		}
	}
}

// A window other than 7d, 30d or 90d is a bad request.
func TestIntake_WindowRefused(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for _, q := range []string{"?window=1d", "?window=30", "?window="} {
		if code := h.call(t, http.MethodGet, intakePath+q, "", nil); code != http.StatusBadRequest {
			t.Errorf("intake%s = %d, want 400", q, code)
		}
	}
}

// The webhooks are the organization's open to acme, by name: receiving when enabled and an
// enabled trigger names it, no_trigger when none does, closed when turned off. A webhook not open
// to acme is left out.
func TestIntake_OpenWebhooks(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	hook := func(name string, sender domain.WebhookSender, p domain.WebhookPatch) {
		t.Helper()

		if _, err := h.secrets.Create(inOrg(t), domain.NewSecret{Name: domain.WebhookSecretName(name), Value: "test-secret"}, domain.LocalUser); err != nil {
			t.Fatal(err)
		}

		w, err := h.intake.CreateWebhook(inOrg(t), domain.NewWebhook{Name: name, Sender: sender, Secret: domain.WebhookSecretName(name)})
		if err != nil {
			t.Fatal(err)
		}

		if _, err := h.intake.PatchWebhook(inOrg(t), w.ID, p); err != nil {
			t.Fatal(err)
		}
	}

	open := []domain.WebhookOpening{{FactoryID: acmeID}}
	off := false

	hook("github-main", domain.SenderGitHub, domain.WebhookPatch{Open: open})
	hook("ci-hooks", domain.SenderStandard, domain.WebhookPatch{Open: open}) // only the disabled deploy-failed names it
	hook("pager", domain.SenderPagerDuty, domain.WebhookPatch{Open: open, Enabled: &off})
	hook("elsewhere", domain.SenderGitHub, domain.WebhookPatch{Name: new("elsewhere")})

	var in gen.Intake
	if code := h.call(t, http.MethodGet, intakePath, "", &in); code != http.StatusOK || in.Webhooks == nil {
		t.Fatalf("intake = %d %+v; want 200 with webhooks", code, in)
	}

	var got []string
	for _, w := range *in.Webhooks {
		got = append(got, fmt.Sprintf("%s %s %s", w.Name, w.Sender, w.State))
	}

	if want := []string{"ci-hooks standard no_trigger", "github-main github receiving", "pager pagerduty closed"}; !slices.Equal(got, want) {
		t.Errorf("webhooks = %v; want %v", got, want)
	}
}

// Coming up lists acme's items that haven't started: with a start soonest first, then those
// waiting only for needs, each with its open needs' refs and where it came from; started and
// closed items and an assembly's parts are left out.
func TestIntake_ComingUp(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	need := h.filedAt(t, domain.NewItem{Title: "the need"})
	h.filedAt(t, domain.NewItem{Title: "later", Origin: domain.OriginTrigger, StartAt: t0.Add(72 * time.Hour)})
	h.filedAt(t, domain.NewItem{Title: "soon", Origin: domain.OriginPerson, Via: domain.ViaCLI, CreatedBy: domain.LocalUser, StartAt: t0.Add(24 * time.Hour)})
	h.filedAt(t, domain.NewItem{Title: "waits", Origin: domain.OriginFollowUp, After: []domain.ItemID{need.ID}})
	h.filedAt(t, domain.NewItem{Title: "started", StartAt: t0.Add(time.Hour), StartedAt: t0})
	assembly := h.filedAt(t, domain.NewItem{Title: "assembly"})
	h.filedAt(t, domain.NewItem{Title: "part", ParentID: assembly.ID, Origin: domain.OriginPlan, After: []domain.ItemID{need.ID}})

	var in gen.Intake
	if code := h.call(t, http.MethodGet, intakePath, "", &in); code != http.StatusOK || in.ComingUp == nil || in.ComingUpTotal == nil {
		t.Fatalf("intake = %d %+v; want 200 with coming up", code, in)
	}

	var got []string

	for _, c := range *in.ComingUp {
		start := "-"
		if c.Start != nil {
			start = c.Start.At.Format(time.RFC3339)
		}

		via := ""
		if c.Via != nil {
			via = string(*c.Via)
		}

		got = append(got, fmt.Sprintf("%s %s %s start %s needs %v via %s by %v", c.Ref, c.Title, c.Origin, start, c.Needs, via, c.CreatedBy != nil))
	}

	want := []string{
		"acme#3 soon person start 2026-10-24T09:00:00Z needs [] via cli by true",
		"acme#2 later trigger start 2026-10-26T09:00:00Z needs [] via  by false",
		"acme#4 waits follow-up start - needs [acme#1] via  by false",
	}
	if !slices.Equal(got, want) || *in.ComingUpTotal != len(want) {
		t.Errorf("coming up = %d in all\n%s\nwant\n%s", *in.ComingUpTotal, strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// Triage is null without follow-ups for the architect; then it is the collecting triage item with
// the follow-ups that joined it, and the open items whose triage follow-ups will join it.
func TestIntake_Triage(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	var in gen.Intake
	if h.call(t, http.MethodGet, intakePath, "", &in); in.Triage != nil {
		t.Errorf("triage without follow-ups = %+v; want null", in.Triage)
	}

	landed := h.submit(t)
	h.keepFollowUp(t, landed, "Tidy the docs")
	h.land(t, landed)

	open := h.submit(t)
	h.keepFollowUp(t, open, "One")
	h.keepFollowUp(t, open, "Two")

	in = gen.Intake{}
	if h.call(t, http.MethodGet, intakePath, "", &in); in.Triage == nil || in.Triage.Item == nil {
		t.Fatalf("triage = %+v; want the collecting item", in.Triage)
	}

	it := in.Triage.Item
	if it.Ref != "acme#2" || it.Station != stInbox || it.FollowUps != 1 || it.Title == "" {
		t.Errorf("triage item = %+v; want acme#2 at the inbox with 1 follow-up", *it)
	}

	if w := in.Triage.Waiting; len(w) != 1 || w[0].Ref != open.Ref || w[0].Title != open.Title || w[0].FollowUps != 2 {
		t.Errorf("triage waiting = %+v; want %s with 2", w, open.Ref)
	}
}

// land moves it to done and runs the follow-up sweep on acme's line and others.
func (h *harness) land(t *testing.T, it gen.Item, others ...domain.Line) {
	t.Helper()

	from := domain.Position{Station: it.Station, State: domain.ItemState(it.State)}
	if _, err := h.store.Transition(inOrg(t), ports.Transition{
		ItemID: domain.ItemID(it.Id), From: from, To: domain.Position{Station: stDone, State: domain.StateDone}, At: t0,
	}); err != nil {
		t.Fatal(err)
	}

	lines := append([]domain.Line{{FactoryID: acmeID, Name: acmeName, Stations: []domain.Station{{ID: stInbox, Kind: domain.KindQueue}}}}, others...)
	if _, err := h.store.FileFollowUps(inOrg(t), lines, t0.Add(time.Hour), 20); err != nil {
		t.Fatal(err)
	}
}

// Each trigger with a check says which agent checks and the most one check spends.
func TestIntake_TriggerCheck(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	var in gen.Intake
	h.call(t, http.MethodGet, intakePath, "", &in)

	checks := map[string]string{}

	for _, tr := range in.Triggers {
		if tr.Check != nil {
			checks[tr.Name] = fmt.Sprintf("%s %.2f", tr.Check.Agent, tr.Check.MaxUsd)
		}
	}

	if len(checks) != 1 || !strings.HasPrefix(checks[incidentsName], "writer ") {
		t.Errorf("checks = %v; want new-incidents by writer only", checks)
	}
}

// A firing that queued a check, a try among them, shows as checked among the trigger's events.
func TestIntakeTrigger_CheckedEvent(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	if code := h.call(t, http.MethodPost, incidentsPath, "", nil); code != http.StatusAccepted {
		t.Fatalf("try = %d; want 202", code)
	}

	var d gen.IntakeTriggerDetail
	if code := h.call(t, http.MethodGet, intakePath+"/triggers/"+incidentsName, "", &d); code != http.StatusOK || len(d.Events) != 1 {
		t.Fatalf("trigger = %d %+v; want 200 with the try", code, d.Events)
	}

	if got := d.Events[0].Outcome; got != gen.IntakeEventOutcomeChecked {
		t.Errorf("event outcome = %s; want checked", got)
	}
}

// The trigger's read serves a check trigger's memory: empty lists and a null state before any
// check, then its open items, closed keys and saved state with when it was saved; a trigger
// without a check has none.
func TestIntakeTrigger_Memory(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	path := intakePath + "/triggers/" + incidentsName

	if want := `{"open":0,"open_items":[],"closed":0,"closed_keys":[],"state":null,"checked_up_to":null}`; !sameJSON(t, rawMemory(t, h, path), want) {
		t.Errorf("memory before a check = %s; want %s", rawMemory(t, h, path), want)
	}

	h.check(t, oct(22, 4), `{"file": [{"key": "INC-1", "title": "API down", "brief": "5xx"}], "state": {"since": "INC-1"}}`, "")

	var d gen.IntakeTriggerDetail
	if code := h.call(t, http.MethodGet, path, "", &d); code != http.StatusOK || d.Memory == nil {
		t.Fatalf("trigger = %d %+v; want 200 with its memory", code, d)
	}

	wantIncidentsMemory(t, d.Memory, oct(22, 4).Add(time.Minute))

	if m := rawMemory(t, h, intakePath+"/triggers/"+nightlyName); m != nil {
		t.Errorf("trigger without a check has memory %s; want none", m)
	}
}

// wantIncidentsMemory checks the memory after one check filed INC-1 and saved {"since": "INC-1"},
// finishing at done.
func wantIncidentsMemory(t *testing.T, m *gen.TriggerCheckMemory, done time.Time) {
	t.Helper()

	if m.Open != 1 || len(m.OpenItems) != 1 || m.OpenItems[0].Key != "INC-1" || m.OpenItems[0].Title != "API down" || m.Closed != 0 || len(m.ClosedKeys) != 0 {
		t.Errorf("memory = %+v; want INC-1 open and no closed key", m)
	}

	if m.State == nil || (*m.State)["since"] != "INC-1" || m.CheckedUpTo == nil || !m.CheckedUpTo.Equal(done) {
		t.Errorf("state = %v, checked up to %v; want the check's, at %s", m.State, m.CheckedUpTo, done)
	}
}

// rawMemory reads the trigger at path and returns its memory as sent, nil when left out.
func rawMemory(t *testing.T, h *harness, path string) json.RawMessage {
	t.Helper()

	var raw map[string]json.RawMessage
	if code := h.call(t, http.MethodGet, path, "", &raw); code != http.StatusOK {
		t.Fatalf("trigger %s = %d; want 200", path, code)
	}

	return raw["memory"]
}

// sameJSON reports whether got holds the same JSON value as want.
func sameJSON(t *testing.T, got json.RawMessage, want string) bool {
	t.Helper()

	var a, b any
	if json.Unmarshal(got, &a) != nil || json.Unmarshal([]byte(want), &b) != nil {
		return false
	}

	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)

	return string(x) == string(y)
}

// Clear answers 204, empties the saved state and writes a trigger.state_cleared entry; it answers
// 409 while a check is queued, 422 for a trigger without a check, 404 for an unknown one and 403
// to a viewer.
func TestTriggerChecks_ClearState(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	statePath := intakePath + "/triggers/" + incidentsName + "/state"

	h.check(t, oct(22, 4), `{"file": [{"key": "INC-1", "title": "API down", "brief": "5xx"}], "state": {"since": "INC-1"}}`, "")

	if code := h.call(t, http.MethodDelete, statePath, "", nil); code != http.StatusNoContent {
		t.Fatalf("clear = %d; want 204", code)
	}

	var d gen.IntakeTriggerDetail
	if code := h.call(t, http.MethodGet, intakePath+"/triggers/"+incidentsName, "", &d); code != http.StatusOK || d.Memory == nil ||
		d.Memory.State != nil || d.Memory.CheckedUpTo != nil || d.Memory.Open != 1 {
		t.Errorf("trigger after Clear = %d %+v; want no state and the open item kept", code, d.Memory)
	}

	cleared, err := h.tenancy.AuditLog(inOrg(t), ports.AuditQuery{Limit: 10, Action: domain.AuditTriggerStateCleared})
	if err != nil || len(cleared) != 1 || cleared[0].TargetID != incidentsName {
		t.Errorf("trigger.state_cleared = %+v, %v; want one naming %s", cleared, err, incidentsName)
	}

	if code := h.call(t, http.MethodPost, incidentsPath, "", nil); code != http.StatusAccepted {
		t.Fatalf("try = %d; want 202", code)
	}

	refusedClears(t, h, statePath)
}

// refusedClears checks the clears refused once a check of the trigger at statePath is queued:
// that one, a trigger without a check, an unknown trigger and a viewer's.
func refusedClears(t *testing.T, h *harness, statePath string) {
	t.Helper()

	for _, c := range []struct {
		what, path string
		status     int
		code       string
	}{
		{"a clear while a check is queued", statePath, http.StatusConflict, "still-checking"},
		{"a trigger without a check", intakePath + "/triggers/" + nightlyName + "/state", http.StatusUnprocessableEntity, "no-check"},
		{"an unknown trigger", unknownTrigger + "/state", http.StatusNotFound, codeTriggerNotFound},
	} {
		if code, p := problemOfCall(t, h, http.MethodDelete, c.path, ""); code != c.status || deref(p.Code) != c.code {
			t.Errorf("%s: %d %s, want %d %s", c.what, code, deref(p.Code), c.status, c.code)
		}
	}

	h.as(acmeMember(domain.FactoryViewer))

	if code, p := problemOfCall(t, h, http.MethodDelete, statePath, ""); code != http.StatusForbidden || deref(p.Code) != wantForbidden {
		t.Errorf("a viewer's clear = %d %s, want 403 forbidden", code, deref(p.Code))
	}
}
