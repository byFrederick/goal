package engine_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/engine"
	"github.com/byFrederick/goal/internal/core/ports"
)

// trNightly is the schedule trigger the try tests run.
const trNightly = "nightly"

// nightly is a schedule trigger firing every minute that creates a feature at the work station
// titled with the instant, asking the model isRegression.
func nightly() domain.Trigger {
	return domain.Trigger{
		Name: trNightly, Enabled: true, On: domain.OnSchedule, Cron: "* * * * *",
		Only:   domain.TriggerOnly{ModelAgrees: &domain.ModelAgrees{Question: isRegression}},
		Create: domain.TriggerCreate{Type: typeFeature, Station: stWork, Title: "Audit of {{instant}}", Brief: "Audit {{instant}}."},
		Limits: domain.TriggerLimits{PerDay: 5},
	}
}

// fireNightly fires the harness's schedules at t0 and returns trNightly's firing.
func (h *harness) fireNightly(t *testing.T) domain.TriggerFiring {
	t.Helper()

	tick, err := h.e.FireSchedules(t.Context(), t0)
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range tick.Firings {
		if f.Trigger == trNightly {
			return f
		}
	}

	t.Fatalf("FireSchedules = %+v; want a firing of %s", tick.Firings, trNightly)

	return domain.TriggerFiring{}
}

// try tries trigger against firing as domain.LocalUser.
func (h *harness) try(t *testing.T, trigger, firing string) domain.TriggerTry {
	t.Helper()

	got, err := h.e.TryTrigger(inOrg(t), factoryID, trigger, firing, domain.LocalUser)
	if err != nil {
		t.Fatal(err)
	}

	return got
}

// intakeState is everything a try must leave as it was: the factory's items and the events of
// item, the trigger's firings, its count for limits.perDay and the audit log.
func (h *harness) intakeState(t *testing.T, item domain.ItemID) (string, []domain.AuditEntry) {
	t.Helper()

	ctx := inOrg(t)

	events, err := h.store.ItemEvents(ctx, item, 0, 100)
	if err != nil {
		t.Fatal(err)
	}

	firings, err := h.intake.Firings(ctx, factoryID, trNightly, 100)
	if err != nil {
		t.Fatal(err)
	}

	created, err := h.intake.CreatedSince(ctx, factoryID, trNightly, t0.Truncate(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	audit, err := h.tenancy.AuditLog(ctx, ports.AuditQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}

	return fmt.Sprintf("items %d, item events %d, firings %+v, created today %d", h.items(t), len(events), firings, created), audit
}

// A try that would create an item creates none, records no firing and leaves the perDay count as
// it was; it adds only its trigger.tried entry, without the event's text or the brief, and the
// connection.used entry of the model check's connection.
func TestTryTrigger_WritesNothing(t *testing.T) {
	t.Parallel()

	h, mc := modelHarness(t, domain.ModelAnswer{Worth: true, Reason: "Worth it", CostUSD: 0.01}, nightly())
	f := h.fireNightly(t)
	wantCreated(t, f)

	before, audit := h.intakeState(t, f.ItemID)

	got := h.try(t, trNightly, f.ID)

	after, audit2 := h.intakeState(t, f.ItemID)
	if after != before {
		t.Errorf("after the try: %s\nbefore: %s", after, before)
	}

	if _, skipped := got.Evaluation.Skipped(); skipped || got.Evaluation.Item == nil || got.Firing.ID != f.ID {
		t.Fatalf("try = %+v; want firing %s, an item it would create", got, f.ID)
	}

	if n := len(mc.Calls()); n != 2 {
		t.Errorf("Ask was called %d times, want twice: the firing and the try", n)
	}

	checkTried(t, audit2[:len(audit2)-len(audit)], f)
}

// checkTried checks that added, the audit entries a try of f added newest first, are its
// trigger.tried entry, naming the trigger, the firing and would_create but none of the event's
// text, the brief or the model's reason, and the model connection's connection.used.
func checkTried(t *testing.T, added []domain.AuditEntry, f domain.TriggerFiring) {
	t.Helper()

	var actions []string
	for _, a := range added {
		actions = append(actions, a.Action)
	}

	if strings.Join(actions, " ") != domain.AuditTriggerTried+" "+domain.AuditConnectionUsed {
		t.Fatalf("audit entries added = %v; want trigger.tried and connection.used", actions)
	}

	tried := added[0]
	if tried.TargetKind != domain.AuditTargetTrigger || tried.TargetID != trNightly || tried.FactoryID != factoryID ||
		tried.Data["firing"] != f.ID || tried.Data["outcome"] != "would_create" {
		t.Errorf("trigger.tried = %+v; want the trigger, the firing and would_create", tried)
	}

	if s := fmt.Sprint(tried.Data); strings.Contains(s, "Audit") || strings.Contains(s, "Worth it") {
		t.Errorf("trigger.tried data %s holds the event's text, the brief or the model's reason", s)
	}
}

// A try reports every step in order: a dedupe skip leaves the later steps not reached and asks no
// model; a model's no skips with its reason and the check's cost.
func TestTryTrigger_ReportsEachStep(t *testing.T) {
	t.Parallel()

	t.Run("dedupe", tryDedupeSkip)
	t.Run("model no", tryModelNo)
	t.Run("would create", tryWouldCreate)
}

// A dedupe skip leaves the later steps not reached and asks no model.
func tryDedupeSkip(t *testing.T) {
	t.Parallel()

	tr := nightly()
	tr.Only.Dedupe = "instant"
	h, mc := modelHarness(t, domain.ModelAnswer{Worth: true, Reason: "Worth it"}, tr)
	f := h.fireNightly(t)
	wantCreated(t, f)

	got := h.try(t, trNightly, f.ID).Evaluation
	want := fmt.Sprintf("[{when passed the trigger's schedule fired} {only.dedupe skipped item %s is open for the same instant} "+
		"{only.modelAgrees not_reached } {create not_reached } {limits.perDay not_reached }]", f.ItemID)

	if s := fmt.Sprint(got.Steps); s != want {
		t.Errorf("steps = %s\nwant %s", s, want)
	}

	if got.Item != nil || got.Model != nil || len(mc.Calls()) != 1 {
		t.Errorf("item %+v, model %+v, %d calls; want no item, no model check, only the firing's call", got.Item, got.Model, len(mc.Calls()))
	}
}

// A model's no skips with its reason, the answer and its cost.
func tryModelNo(t *testing.T) {
	t.Parallel()

	h, _ := modelHarness(t, domain.ModelAnswer{Reason: "A flaky test", InputTokens: 300, OutputTokens: 20, CostUSD: 0.004}, nightly())
	f := h.fireNightly(t)
	wantSkipped(t, f, domain.StepModel)

	got := h.try(t, trNightly, f.ID).Evaluation
	want := "[{when passed the trigger's schedule fired} {only.modelAgrees skipped A flaky test} {create not_reached } {limits.perDay not_reached }]"

	if s := fmt.Sprint(got.Steps); s != want {
		t.Errorf("steps = %s\nwant %s", s, want)
	}

	if got.Model == nil || got.Model.CostUSD != 0.004 || got.Model.InputTokens != 300 || got.FiringReason() != f.Reason {
		t.Errorf("model %+v, reason %q; want the answer with its cost, the firing's reason %q", got.Model, got.FiringReason(), f.Reason)
	}
}

// An event no step skips renders the item it would create.
func tryWouldCreate(t *testing.T) {
	t.Parallel()

	tr := nightly()
	tr.Only.ModelAgrees = nil
	h := intakeHarness(t, tr)
	f := h.fireNightly(t)

	got := h.try(t, trNightly, f.ID).Evaluation
	want := "[{when passed the trigger's schedule fired} {create passed } {limits.perDay passed 1 of the 5 items a day created today (UTC)}]"

	if s := fmt.Sprint(got.Steps); s != want {
		t.Errorf("steps = %s\nwant %s", s, want)
	}

	key := t0.Format(time.RFC3339)
	if it := got.Item; it == nil || it.Title != "Audit of "+key || it.Station != stWork || it.Type != typeFeature ||
		!strings.HasPrefix(it.Body, "Audit \""+key+"\".") {
		t.Errorf("item = %+v; want the rendered title and brief at %s", got.Item, stWork)
	}
}

// A try refuses a firing that is not among the trigger's newest, a trigger the line does not have
// and a firing whose event was not stored (anything but a schedule's).
func TestTryTrigger_Refusals(t *testing.T) {
	t.Parallel()

	tr := nightly()
	tr.Only.ModelAgrees = nil
	h := intakeHarness(t, tr, failedCheck())
	h.fireNightly(t)

	github := h.receive(t, failure("check-run-1", "unit"))

	for _, c := range []struct {
		name, trigger, firing string
		want                  error
	}{
		{"unknown firing", trNightly, "no-such-firing", engine.ErrFiringNotFound},
		{"another trigger's firing", trNightly, github.ID, engine.ErrFiringNotFound},
		{"unknown trigger", "no-such-trigger", github.ID, engine.ErrTriggerNotFound},
		{"github firing", trFlaky, github.ID, engine.ErrEventNotStored},
	} {
		_, err := h.e.TryTrigger(inOrg(t), factoryID, c.trigger, c.firing, domain.LocalUser)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}

	if !errors.Is(engine.ErrFiringNotFound, ports.ErrNotFound) || !errors.Is(engine.ErrTriggerNotFound, ports.ErrNotFound) {
		t.Error("the not-found refusals do not wrap ports.ErrNotFound")
	}
}
