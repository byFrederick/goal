package domain

import "fmt"

// The steps of a trigger's evaluation, in the order they run (ADR 0028 decision 1): When, each
// configured Only if condition (a deprecated model check last), Create, then each configured
// limit.
const (
	StepWhen     = "when"
	StepFailures = "only.failuresInARow"
	StepDedupe   = "only.dedupe"
	StepModel    = "only.modelAgrees"
	StepCreate   = "create"
	StepPerDay   = "limits.perDay"
	StepPerHour  = "limits.perHour"
	StepBudget   = "limits.pauseOnBudget"
)

// ModelCheckRemoved is only.modelAgrees's reason: the model check is gone (ADR 0040 decision 18),
// so a trigger that still has one skips its events, as a check that gave no answer did.
const ModelCheckRemoved = "the model check is removed; turn this trigger into a check (check:) to have an agent decide"

// DedupeSkip is only.dedupe's reason when item is open for the same value of field.
func DedupeSkip(item ItemID, field string) string {
	return fmt.Sprintf("item %s is open for the same %s", item, field)
}

// PerDaySkip is limits.perDay's reason when created items were created today, the limit n.
func PerDaySkip(created, n int) string {
	return fmt.Sprintf("%d items created today (UTC), the limit is %d", created, n)
}

// PerHourSkip is limits.perHour's reason when created items were created in the hour before.
func PerHourSkip(created, n int) string {
	return fmt.Sprintf("%d items created in the last hour, the limit is %d", created, n)
}

// FireLimits are what a firing that creates an item checks again under its trigger's lock (ADR
// 0040 decision 16), so they hold across goald instances: only.dedupe's field (with the
// firing's Dedupe value), limits.perDay and limits.perHour.
type FireLimits struct {
	DedupeField     string
	PerDay, PerHour int
}

// LimitsOf are t's FireLimits.
func LimitsOf(t Trigger) FireLimits {
	return FireLimits{DedupeField: t.Only.Dedupe, PerDay: t.Limits.PerDay, PerHour: t.Limits.PerHour}
}

// FireCounts are what a store read under the trigger's lock: the open item with the firing's
// dedupe value ("" for none), and how many items the trigger created since the UTC day began and
// in the hour before the firing.
type FireCounts struct {
	OpenDedupe      ItemID
	Today, LastHour int
}

// Skip is the reason (step: why, as FiringReason writes it) the firing is skipped by l with
// counts c, "" when it may create its item: only.dedupe, then limits.perDay, then limits.perHour.
func (l FireLimits) Skip(c FireCounts) string {
	switch {
	case l.DedupeField != "" && c.OpenDedupe != "":
		return StepDedupe + ": " + DedupeSkip(c.OpenDedupe, l.DedupeField)
	case l.PerDay > 0 && c.Today >= l.PerDay:
		return StepPerDay + ": " + PerDaySkip(c.Today, l.PerDay)
	case l.PerHour > 0 && c.LastHour >= l.PerHour:
		return StepPerHour + ": " + PerHourSkip(c.LastHour, l.PerHour)
	}

	return ""
}

// StepOutcome is what one step of an evaluation did with the event.
type StepOutcome string

const (
	// StepPassed let the event go on.
	StepPassed StepOutcome = "passed"
	// StepSkipped stopped it: the trigger creates no item.
	StepSkipped StepOutcome = "skipped"
	// StepNotReached was not run, as a step before it skipped the event.
	StepNotReached StepOutcome = "not_reached"
)

// TriggerStep is one step of an evaluation. Reason is a sentence a person reads, without the
// step's name.
type TriggerStep struct {
	Step    string
	Outcome StepOutcome
	Reason  string
}

// TriggerItem is the item a trigger's Create step renders for an event: what intake creates when
// no step skips it.
type TriggerItem struct {
	Type    string
	Station string
	State   ItemState
	Lane    string
	Class   ClassOfService
	// Title is create.title rendered with quoted values (EventTitle); Body is create.brief rendered with quoted
	// values, then the event's fields in a block marked as untrusted data (UntrustedEventBlock).
	Title, Body string
}

// TriggerEvaluation is a trigger's evaluation of one event (ADR 0028 decision 1): each of its
// steps in order and the item Create rendered (nil when Create was not reached).
type TriggerEvaluation struct {
	Steps []TriggerStep
	Item  *TriggerItem
}

// NewTriggerEvaluation is t's evaluation before any step ran: each step t has, every one not
// reached. A check is one of t's steps when it is configured: only.failuresInARow above 0,
// only.dedupe, a deprecated only.modelAgrees, limits.perDay and limits.perHour above 0, and limits.pauseOnBudget.
func NewTriggerEvaluation(t Trigger) TriggerEvaluation {
	steps := []string{StepWhen}

	if t.Only.FailuresInARow > 0 {
		steps = append(steps, StepFailures)
	}

	if t.Only.Dedupe != "" {
		steps = append(steps, StepDedupe)
	}

	if t.Only.ModelAgrees {
		steps = append(steps, StepModel)
	}

	steps = append(steps, StepCreate)

	if t.Limits.PerDay > 0 {
		steps = append(steps, StepPerDay)
	}

	if t.Limits.PerHour > 0 {
		steps = append(steps, StepPerHour)
	}

	if t.Limits.PauseOnBudget {
		steps = append(steps, StepBudget)
	}

	out := TriggerEvaluation{Steps: make([]TriggerStep, len(steps))}
	for i, s := range steps {
		out.Steps[i] = TriggerStep{Step: s, Outcome: StepNotReached}
	}

	return out
}

// Pass records that step passed, with why.
func (e *TriggerEvaluation) Pass(step, reason string) {
	e.set(step, StepPassed, reason)
}

// Skip records that step skipped the event, with why; the steps after it stay not reached.
func (e *TriggerEvaluation) Skip(step, reason string) {
	e.set(step, StepSkipped, reason)
}

func (e *TriggerEvaluation) set(step string, o StepOutcome, reason string) {
	for i := range e.Steps {
		if e.Steps[i].Step == step {
			e.Steps[i].Outcome, e.Steps[i].Reason = o, reason
		}
	}
}

// Skipped is the step that skipped the event, if one did.
func (e TriggerEvaluation) Skipped() (TriggerStep, bool) {
	for _, s := range e.Steps {
		if s.Outcome == StepSkipped {
			return s, true
		}
	}

	return TriggerStep{}, false
}

// Matched reports whether the event reached the trigger: its When step passed.
func (e TriggerEvaluation) Matched() bool {
	return len(e.Steps) > 0 && e.Steps[0].Step == StepWhen && e.Steps[0].Outcome == StepPassed
}

// FiringReason is the reason a skipped firing records: the step that skipped the event, a colon
// and its reason; "" when no step did.
func (e TriggerEvaluation) FiringReason() string {
	s, ok := e.Skipped()
	if !ok {
		return ""
	}

	return s.Step + ": " + s.Reason
}

// TriggerTry is a dry run of a trigger against one of its past firings (ADR 0028 decision 2):
// the firing tried and the evaluation of its event now. A try creates nothing.
type TriggerTry struct {
	Firing     TriggerFiring
	Evaluation TriggerEvaluation
}

// The audit entry of a try (ADR 0028 decision 2), whose target is the trigger by its name.
const (
	AuditTriggerTried  = "trigger.tried"
	AuditTargetTrigger = "trigger"
)

// The outcomes of a try: the item the event would create, or the step that would skip it.
const (
	TryWouldCreate = "would_create"
	TrySkipped     = "skipped"
)

// Outcome is TryWouldCreate when no step skipped the event, TrySkipped otherwise.
func (e TriggerEvaluation) Outcome() string {
	if _, skipped := e.Skipped(); skipped {
		return TrySkipped
	}

	return TryWouldCreate
}
