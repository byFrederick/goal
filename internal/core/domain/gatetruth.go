package domain

import (
	"cmp"
	"fmt"
	"time"
)

// GateTruth is the right decision for a gate station's run, as people's verdicts and later
// outcomes say it (ADR 0039 decision 10): what a try of a change to the gate's own agent, rules or
// contract is graded against. An empty Decision is no answer.
type GateTruth struct {
	Decision Decision
	// Reason names the person's verdict or the merge that decided it; empty without an answer.
	Reason string
}

// GateTruthOf reads the right decision for the run at line's gate station named station that started
// at since, from the item's events in append order. The window is the events at or after since,
// up to the item's next claim at the station or at one before it, since a claim there works a
// newer version of the item; a claim stamped at or before since is the run's own.
//
// Reject is right when a person sent the item back inside the window: a person's gate_rejected, a
// person's rejected that sent it to the station or before it (or to a place its events don't
// name), or a person's move to the station or before it. Approve is right when the item merged
// inside the window and no person sent it back afterwards. Anything else is no answer: an item
// still open, cancelled, or sent back by the gate agent alone. The gate agent's own gate_rejected
// and gate_approved, and their reasons, never decide it.
func GateTruthOf(events []Event, line Line, station string, since time.Time) GateTruth {
	ids := make([]string, len(line.Stations))
	for i, s := range line.Stations {
		ids[i] = s.ID
	}

	gate := linePlace(ids, station)

	var merged *Event

	for i, e := range events {
		if e.At.Before(since) {
			continue
		}

		if e.Kind == EventClaimed && e.At.After(since) && linePlace(ids, e.Station) <= gate {
			break
		}

		if e.Kind == EventMerged && merged == nil {
			merged = &events[i]
		}

		if r, ok := personSendBack(events[i+1:], e, ids, gate); ok {
			return GateTruth{Decision: Reject, Reason: r}
		}
	}

	if merged != nil {
		return GateTruth{Decision: Approve, Reason: fmt.Sprintf("the item merged at %s and no person sent it back afterwards",
			merged.At.UTC().Format(time.RFC3339))}
	}

	return GateTruth{}
}

// personSendBack is why e is a person's send-back of the item to the gate at line place gate or
// before it, with the person's own reason, and whether it is one.
func personSendBack(later []Event, e Event, line []string, gate int) (string, bool) {
	sent := false

	switch {
	case !byPerson(e):
	case e.Kind == EventGateRejected:
		sent = true
	case e.Kind == EventRejected:
		to := rejectTarget(later, e)
		sent = to == "" || linePlace(line, to) <= gate
	case e.Kind == EventMoved:
		to := dataText(e, evKeyTo)
		sent = to != "" && linePlace(line, to) <= gate
	}

	if !sent {
		return "", false
	}

	r := fmt.Sprintf("%s sent the item back at %s", cmp.Or(e.Actor, "a person"), movedFrom(e))
	if why := dataText(e, evKeyReason); why != "" {
		r += ": " + why
	}

	return r, true
}
