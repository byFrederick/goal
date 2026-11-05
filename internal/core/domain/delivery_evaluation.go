package domain

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// EvaluationState is where a delivery's evaluation for one factory is (ADR 0040 decision 13).
// Values match delivery_evaluations.state.
type EvaluationState string

const (
	// EvaluationPending waits for an instance's loop to claim it.
	EvaluationPending EvaluationState = "pending"
	// EvaluationDone has its outcome.
	EvaluationDone EvaluationState = "done"
	// EvaluationFailed failed MaxEvaluationAttempts times; its reason is the last error.
	EvaluationFailed EvaluationState = "failed"
)

// EvaluationOutcome is what a delivery led to in one factory: the second part of the delivery
// log's vocabulary. Values match delivery_evaluations.outcome.
type EvaluationOutcome string

const (
	// EvaluationCreated filed an item.
	EvaluationCreated EvaluationOutcome = "created"
	// EvaluationSkipped filed none, with the reason.
	EvaluationSkipped EvaluationOutcome = "skipped"
	// EvaluationFailedOutcome goes with EvaluationFailed.
	EvaluationFailedOutcome EvaluationOutcome = "failed"
)

// MaxEvaluationAttempts is how many times an evaluation is tried before it is failed.
const MaxEvaluationAttempts = 5

// EvaluationLease is how long one claim of an evaluation holds it; a row whose lease ran out is
// claimed again, by any instance.
const EvaluationLease = 2 * time.Minute

// maxEvaluationReason is the longest reason an evaluation keeps, in characters.
const maxEvaluationReason = 500

// EvaluationBackoff is how long after a failed attempt, the attempts-th, the next one waits:
// 30 seconds, doubling, at most an hour.
func EvaluationBackoff(attempts int) time.Duration {
	d := 30 * time.Second
	for i := 1; i < attempts && d < time.Hour; i++ {
		d *= 2
	}

	return min(d, time.Hour)
}

// NewDeliveryEvaluation is one factory's evaluation of a delivery, which the receiver adds in
// the receipt's transaction: the triggers that name the delivery there, and Skip, the reason it
// is done and skipped at once, or "" to leave it pending.
type NewDeliveryEvaluation struct {
	FactoryID FactoryID
	Triggers  []string
	Skip      string
}

// DeliveryEvaluation is one factory's evaluation of a delivery to an organization's webhook.
type DeliveryEvaluation struct {
	ID        string
	FactoryID FactoryID
	// Delivery is the delivery's row; WebhookID, DeliveryID (the sender's id) and Event are its.
	Delivery   WebhookDeliveryID
	WebhookID  WebhookEndpointID
	DeliveryID string
	Event      string
	Triggers   []string
	State      EvaluationState
	Attempts   int
	NextAt     time.Time
	// LeaseHolder and LeaseUntil are the claim that holds it; "" and nil when none does.
	LeaseHolder string
	LeaseUntil  *time.Time
	Outcome     EvaluationOutcome
	Reason      string
	ItemID      ItemID
	// ItemNumber is ItemID's number while that item exists, read by DeliveryEvaluationsOf; 0
	// otherwise.
	ItemNumber int64
	CreatedAt  time.Time
}

// ClaimedEvaluation is an evaluation a loop claimed, with the delivery's body.
type ClaimedEvaluation struct {
	DeliveryEvaluation

	Body []byte
}

// EvaluationFinish ends the claim of evaluation ID by Holder at its Attempts-th attempt: done
// with its outcome, reason and item; or, for an error, pending again at NextAt, or failed.
type EvaluationFinish struct {
	ID       string
	Holder   string
	Attempts int
	State    EvaluationState
	Outcome  EvaluationOutcome
	Reason   string
	ItemID   ItemID
	NextAt   time.Time
	At       time.Time
}

// FinishEvaluation is how a claimed evaluation e ends at at: done with the firings its triggers
// recorded (Created with the first item when one created an item, Skipped with their reasons
// otherwise), or, when err is set, pending again after EvaluationBackoff, or failed with err after
// MaxEvaluationAttempts attempts.
func FinishEvaluation(e ClaimedEvaluation, firings []TriggerFiring, err error, at time.Time) EvaluationFinish {
	out := EvaluationFinish{ID: e.ID, Holder: e.LeaseHolder, Attempts: e.Attempts, At: at}

	switch {
	case err != nil && e.Attempts >= MaxEvaluationAttempts:
		out.State, out.Outcome, out.Reason = EvaluationFailed, EvaluationFailedOutcome, truncate(err.Error(), maxEvaluationReason)
	case err != nil:
		out.State, out.Reason, out.NextAt = EvaluationPending, truncate(err.Error(), maxEvaluationReason), at.Add(EvaluationBackoff(e.Attempts))
	default:
		out.State = EvaluationDone
		out.Outcome, out.Reason, out.ItemID = EvaluationResult(firings)
	}

	return out
}

// ReasonNoTriggerMatches is a done evaluation's reason when no trigger's filters matched.
const ReasonNoTriggerMatches = "no trigger's filters match the event"

// EvaluationResult is the outcome of an evaluation whose triggers recorded firings: Created with
// the first item one created, else Skipped with each firing's trigger and reason.
func EvaluationResult(firings []TriggerFiring) (EvaluationOutcome, string, ItemID) {
	if i := slices.IndexFunc(firings, func(f TriggerFiring) bool { return f.Outcome == FiringCreated }); i >= 0 {
		return EvaluationCreated, "", firings[i].ItemID
	}

	if len(firings) == 0 {
		return EvaluationSkipped, ReasonNoTriggerMatches, ""
	}

	reasons := make([]string, 0, len(firings))
	for _, f := range firings {
		reasons = append(reasons, "trigger "+f.Trigger+": "+f.Reason)
	}

	return EvaluationSkipped, truncate(strings.Join(reasons, "; "), maxEvaluationReason), ""
}

// NotTheFactorysRepository is the reason an evaluation is skipped at once for a delivery from a
// repository the factory doesn't have (ADR 0041 decision 13).
func NotTheFactorysRepository(repo string, l Line) string {
	name := l.Name
	if name == "" {
		name = string(l.FactoryID)
	}

	return fmt.Sprintf("%s is not one of %s's repositories", repo, name)
}

// EvaluationFor is l's evaluation of ev, a delivery to w: false when no enabled trigger of l
// names it, or w's opening to l excludes its repository, so it never reaches l. A repository l
// doesn't have makes it skipped at once, with the reason.
func EvaluationFor(l Line, w WebhookEndpoint, ev IntakeEvent) (NewDeliveryEvaluation, bool) {
	var names []string

	for _, t := range l.Triggers {
		if t.NamesDelivery(w.Name, w.Sender, ev.Event) {
			names = append(names, t.Name)
		}
	}

	if len(names) == 0 {
		return NewDeliveryEvaluation{}, false
	}

	repo := ev.Fields[FieldRepo]
	if o, ok := w.Opening(l.FactoryID); w.OrgLevel() && (!ok || !o.Admits(repo)) {
		return NewDeliveryEvaluation{}, false
	}

	out := NewDeliveryEvaluation{FactoryID: l.FactoryID, Triggers: names}
	if !l.HasRepository(repo) {
		out.Skip = NotTheFactorysRepository(repo, l)
	}

	return out, true
}
