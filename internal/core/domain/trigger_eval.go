package domain

// The steps of a trigger's evaluation, in the order they run (ADR 0028 decision 1): When, each
// configured Only if condition (the model check last), Create, then each configured limit.
const (
	StepWhen     = "when"
	StepFailures = "only.failuresInARow"
	StepDedupe   = "only.dedupe"
	StepModel    = "only.modelAgrees"
	StepCreate   = "create"
	StepPerDay   = "limits.perDay"
	StepBudget   = "limits.pauseOnBudget"
)

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
// step's name; a model check's is the model's, so it is shaped by the event and untrusted.
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
	// Title is create.title rendered with plain values; Body is create.brief rendered with quoted
	// values, then the event's fields in a block marked as untrusted data (UntrustedEventBlock).
	Title, Body string
}

// TriggerEvaluation is a trigger's evaluation of one event (ADR 0028 decision 1): each of its
// steps in order, the item Create rendered (nil when Create was not reached) and the model check's
// answer (nil when the check did not run or gave no answer).
type TriggerEvaluation struct {
	Steps []TriggerStep
	Item  *TriggerItem
	Model *ModelAnswer
}

// NewTriggerEvaluation is t's evaluation before any step ran: each step t has, every one not
// reached. A check is one of t's steps when it is configured: only.failuresInARow above 0,
// only.dedupe, only.modelAgrees, limits.perDay above 0 and limits.pauseOnBudget.
func NewTriggerEvaluation(t Trigger) TriggerEvaluation {
	steps := []string{StepWhen}

	if t.Only.FailuresInARow > 0 {
		steps = append(steps, StepFailures)
	}

	if t.Only.Dedupe != "" {
		steps = append(steps, StepDedupe)
	}

	if t.Only.ModelAgrees != nil {
		steps = append(steps, StepModel)
	}

	steps = append(steps, StepCreate)

	if t.Limits.PerDay > 0 {
		steps = append(steps, StepPerDay)
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
