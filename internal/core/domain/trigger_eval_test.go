package domain

import (
	"fmt"
	"testing"
)

// An evaluation lists When, the configured only steps with the model check last, Create, then the
// configured limits; a skip names its step in the firing's reason and leaves the later steps not
// reached.
func TestTriggerFiringSteps_InOrder(t *testing.T) {
	t.Parallel()

	tr := Trigger{
		Only:   TriggerOnly{FailuresInARow: 2, Dedupe: "check", ModelAgrees: &ModelAgrees{Question: "Worth it?"}},
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
