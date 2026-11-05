package domain

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// An evaluation lists When, the configured only steps with the model check last, Create, then the
// configured limits; a skip names its step in the firing's reason and leaves the later steps not
// reached.
func TestTriggerFiringSteps_InOrder(t *testing.T) {
	t.Parallel()

	tr := Trigger{
		Only:   TriggerOnly{FailuresInARow: 2, Dedupe: "check", ModelAgrees: true},
		Limits: TriggerLimits{PerDay: 3, PauseOnBudget: true},
	}

	e := NewTriggerEvaluation(tr)
	e.Pass(StepWhen, "")
	e.Pass(StepFailures, "")
	e.Skip(StepDedupe, "item 7 is open for the same check")

	got := fmt.Sprint(e.Steps)
	want := "[{when passed } {only.failuresInARow passed } {only.dedupe skipped item 7 is open for the same check} " +
		"{only.modelAgrees not_reached } {create not_reached } {limits.perDay not_reached } {limits.pauseOnBudget not_reached }]"

	if got != want {
		t.Errorf("steps = %s\nwant %s", got, want)
	}

	if !e.Matched() || e.FiringReason() != "only.dedupe: item 7 is open for the same check" {
		t.Errorf("matched %t, reason %q; want matched, skipped by only.dedupe", e.Matched(), e.FiringReason())
	}
}

// A trigger without checks has When and Create only; with nothing skipped there is no reason.
func TestTriggerFiringSteps_NoChecks(t *testing.T) {
	t.Parallel()

	e := NewTriggerEvaluation(Trigger{})
	if got := fmt.Sprint(e.Steps); got != "[{when not_reached } {create not_reached }]" {
		t.Errorf("steps = %s; want when and create", got)
	}

	if e.Matched() {
		t.Error("matched before When ran")
	}

	e.Pass(StepWhen, "")
	e.Pass(StepCreate, "")

	if _, skipped := e.Skipped(); skipped || e.FiringReason() != "" {
		t.Errorf("skipped %t, reason %q; want neither", skipped, e.FiringReason())
	}
}

// A missed firing is a schedule's and counts at least one instant; no other firing counts any.
func TestTriggerFiring_MissedCountsItsInstants(t *testing.T) {
	t.Parallel()

	f := TriggerFiring{
		FactoryID: "f", Trigger: "nightly", Source: TriggerSchedule, EventKey: OnSchedule, IdempotencyKey: "missed/2026-10-27T10:00:00Z",
		Outcome: FiringMissed, Missed: 3, At: time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC),
	}

	if err := f.Check(); err != nil {
		t.Fatalf("Check = %v; want a missed firing of 3 instants", err)
	}

	for name, bad := range map[string]func(TriggerFiring) TriggerFiring{
		"missed without a count":    func(f TriggerFiring) TriggerFiring { f.Missed = 0; return f },
		"a count on a skipped":      func(f TriggerFiring) TriggerFiring { f.Outcome = FiringSkipped; return f },
		"a negative count":          func(f TriggerFiring) TriggerFiring { f.Missed = -1; return f },
		"missed from a webhook":     func(f TriggerFiring) TriggerFiring { f.Source = TriggerWebhook; return f },
		"missed that names an item": func(f TriggerFiring) TriggerFiring { f.ItemID = "i"; return f },
	} {
		if err := bad(f).Check(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: Check = %v; want ErrInvalid", name, err)
		}
	}
}
