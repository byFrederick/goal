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
// titled with the instant.
func nightly() domain.Trigger {
	return domain.Trigger{
		Name: trNightly, Enabled: true, On: domain.OnSchedule, Cron: "* * * * *",
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
// it was; it adds only its trigger.tried entry, without the event's text or the brief.
func TestTryTrigger_WritesNothing(t *testing.T) {
	t.Parallel()

	h := intakeHarness(t, nightly())
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

	checkTried(t, audit2[:len(audit2)-len(audit)], f)
}

// checkTried checks that added, the audit entries a try of f added newest first, are its
// trigger.tried entry, naming the trigger, the firing and would_create but none of the event's
// text or the brief.
func checkTried(t *testing.T, added []domain.AuditEntry, f domain.TriggerFiring) {
	t.Helper()

	var actions []string
	for _, a := range added {
		actions = append(actions, a.Action)
	}

	if strings.Join(actions, " ") != domain.AuditTriggerTried {
		t.Fatalf("audit entries added = %v; want trigger.tried only", actions)
	}

	tried := added[0]
	if tried.TargetKind != domain.AuditTargetTrigger || tried.TargetID != trNightly || tried.FactoryID != factoryID ||
		tried.Data["firing"] != f.ID || tried.Data["outcome"] != "would_create" {
		t.Errorf("trigger.tried = %+v; want the trigger, the firing and would_create", tried)
	}

	if s := fmt.Sprint(tried.Data); strings.Contains(s, "Audit") {
		t.Errorf("trigger.tried data %s holds the event's text or the brief", s)
	}
}

// A try reports every step in order: a dedupe skip leaves the later steps not reached; a
// deprecated model check skips with ModelCheckRemoved.
func TestTryTrigger_ReportsEachStep(t *testing.T) {
	t.Parallel()

	t.Run("dedupe", tryDedupeSkip)
	t.Run("model agrees", tryModelAgrees)
	t.Run("would create", tryWouldCreate)
}

// A dedupe skip leaves the later steps not reached.
func tryDedupeSkip(t *testing.T) {
	t.Parallel()

	tr := nightly()
	tr.Only.Dedupe = "instant"
	h := intakeHarness(t, tr)
	f := h.fireNightly(t)
	wantCreated(t, f)

	got := h.try(t, trNightly, f.ID).Evaluation
	want := fmt.Sprintf("[{when passed the trigger's schedule fired} {only.dedupe skipped item %s is open for the same instant} "+
		"{create not_reached } {limits.perDay not_reached }]", f.ItemID)

	if s := fmt.Sprint(got.Steps); s != want {
		t.Errorf("steps = %s\nwant %s", s, want)
	}

	if got.Item != nil {
		t.Errorf("item %+v; want none", got.Item)
	}
}

// A trigger that still has only.modelAgrees skips at that step, as its firing did.
func tryModelAgrees(t *testing.T) {
	t.Parallel()

	tr := nightly()
	tr.Only.ModelAgrees = true
	h := intakeHarness(t, tr)
	f := h.fireNightly(t)
	wantSkipped(t, f, domain.StepModel)

	got := h.try(t, trNightly, f.ID).Evaluation
	want := "[{when passed the trigger's schedule fired} {only.modelAgrees skipped " + domain.ModelCheckRemoved + "} {create not_reached } {limits.perDay not_reached }]"

	if s := fmt.Sprint(got.Steps); s != want || got.FiringReason() != f.Reason {
		t.Errorf("steps = %s, reason %q\nwant %s, the firing's reason %q", s, got.FiringReason(), want, f.Reason)
	}
}

// An event no step skips renders the item it would create.
func tryWouldCreate(t *testing.T) {
	t.Parallel()

	h := intakeHarness(t, nightly())
	f := h.fireNightly(t)

	got := h.try(t, trNightly, f.ID).Evaluation
	want := "[{when passed the trigger's schedule fired} {create passed } {limits.perDay passed 1 of the 5 items a day created today (UTC)}]"

	if s := fmt.Sprint(got.Steps); s != want {
		t.Errorf("steps = %s\nwant %s", s, want)
	}

	key := t0.Format(time.RFC3339)
	if it := got.Item; it == nil || it.Title != `Audit of "`+key+`"` || it.Station != stWork || it.Type != typeFeature ||
		!strings.HasPrefix(it.Body, "Audit \""+key+"\".") {
		t.Errorf("item = %+v; want the rendered title and brief at %s", got.Item, stWork)
	}
}

// A try refuses a firing that is not among the trigger's newest, a trigger the line does not have
// and a firing whose event was not stored (one that came neither from a schedule nor from a kept
// delivery, and a missed gap).
func TestTryTrigger_Refusals(t *testing.T) {
	t.Parallel()

	h := intakeHarness(t, nightly(), failedCheck())
	h.fireNightly(t)

	missed, _, err := h.intake.RecordFiring(inOrg(t), domain.TriggerFiring{
		FactoryID: factoryID, Trigger: trNightly, Source: domain.TriggerSchedule, EventKey: domain.OnSchedule,
		IdempotencyKey: "missed/2026-10-23T08:00:00Z", Outcome: domain.FiringMissed, Missed: 2, At: t0.Add(-time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}

	github := h.receive(t, failure("check-run-1", "unit"))

	for _, c := range []struct {
		name, trigger, firing string
		want                  error
	}{
		{"unknown firing", trNightly, "no-such-firing", engine.ErrFiringNotFound},
		{"another trigger's firing", trNightly, github.ID, engine.ErrFiringNotFound},
		{"unknown trigger", "no-such-trigger", github.ID, engine.ErrTriggerNotFound},
		{"github firing without a delivery", trFlaky, github.ID, engine.ErrEventNotStored},
		{"missed firing", trNightly, missed.ID, engine.ErrEventNotStored},
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

// A delivery's firing is tried on the saved trigger with the body its webhook still keeps, rebuilt
// as the evaluation made it; once the delivery is pruned it is no longer stored. A firing naming a
// webhook not open to the factory reads nothing (ADR 0040 decision 18).
func TestTryTrigger_DeliveryWithAStoredBody(t *testing.T) {
	t.Parallel()

	h, w := deliveryHarness(t, onFailedRun())
	h.deliver(t, h.e, w, "d-1", checkRun("unit", "failure"))

	if n, err := h.e.EvaluateDeliveries(t.Context()); err != nil || n != 1 {
		t.Fatalf("EvaluateDeliveries = %d, %v; want one", n, err)
	}

	firings, err := h.intake.Firings(inOrg(t), factoryID, "red-check", 1)
	if err != nil || len(firings) != 1 {
		t.Fatalf("firings = %+v, %v; want the delivery's", firings, err)
	}

	f := firings[0].Firing

	got := h.try(t, "red-check", f.ID).Evaluation
	if it := got.Item; got.Outcome() != domain.TryWouldCreate || it == nil || it.Title != `"unit" failed on "abc123"` ||
		!strings.Contains(it.Body, "<untrusted-event-data>") {
		t.Errorf("try = %+v; want the delivery's item rebuilt from its body", got)
	}

	checkForeignWebhookNotRead(t, h)

	if _, err := h.intake.PruneDeliveries(t.Context(), h.clock.Now().Add(domain.DeliveryRetention+time.Hour)); err != nil {
		t.Fatal(err)
	}

	if _, err := h.e.TryTrigger(inOrg(t), factoryID, "red-check", f.ID, domain.LocalUser); !errors.Is(err, engine.ErrEventNotStored) {
		t.Errorf("a pruned delivery's firing = %v; want ErrEventNotStored", err)
	}
}

// checkForeignWebhookNotRead checks that a firing naming a delivery of a webhook not open to the
// factory is not stored, though that webhook keeps it.
func checkForeignWebhookNotRead(t *testing.T, h *harness) {
	t.Helper()

	ctx := inOrg(t)

	closed, err := h.intake.CreateWebhook(ctx, domain.NewWebhook{Name: "github-closed", Sender: domain.SenderGitHub, Secret: "webhook-" + testWebhook})
	if err != nil {
		t.Fatal(err)
	}

	h.deliver(t, h.e, closed, "d-9", checkRun("unit", "failure"))

	foreign, _, err := h.intake.RecordFiring(ctx, domain.TriggerFiring{
		FactoryID: factoryID, Trigger: "red-check", Source: domain.TriggerGitHub, EventKey: onFailedRun().On,
		IdempotencyKey: string(closed.ID) + "/d-9", Outcome: domain.FiringSkipped, At: h.clock.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := h.e.TryTrigger(ctx, factoryID, "red-check", foreign.ID, domain.LocalUser); !errors.Is(err, engine.ErrEventNotStored) {
		t.Errorf("a firing of a webhook not open to the factory = %v; want ErrEventNotStored", err)
	}
}
