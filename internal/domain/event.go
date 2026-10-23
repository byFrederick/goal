package domain

import "time"

// EventKind names what happened to an item. Kinds are open: runners add their own (tool_call, ...).
type EventKind string

// Event kinds the engine writes.
const (
	EventCreated        EventKind = "created"
	EventClaimed        EventKind = "claimed"
	EventRunStarted     EventKind = "run_started"
	EventRunFinished    EventKind = "run_finished"
	EventContractFailed EventKind = "contract_failed"
	EventCheckPassed    EventKind = "check_passed"
	EventCheckFailed    EventKind = "check_failed"
	EventMoved          EventKind = "moved"
	EventGateApproved   EventKind = "gate_approved"
	EventGateRejected   EventKind = "gate_rejected"
	EventWaitingHuman   EventKind = "waiting_human"
	EventApproved       EventKind = "approved"
	EventRejected       EventKind = "rejected"
	EventBlocked        EventKind = "blocked"
	EventLeaseExpired   EventKind = "lease_expired"
	EventReleased       EventKind = "released"
	EventDone           EventKind = "done"
)

// ActorEngine is the actor of events the engine writes on its own.
const ActorEngine = "engine"

// Event is one append-only entry in an item's history (item_events).
type Event struct {
	ID        int64 // assigned by the store
	FactoryID FactoryID
	ItemID    ItemID
	RunID     RunID // empty when no run is involved
	At        time.Time
	Kind      EventKind
	Station   string
	// Actor is an agent name, ActorEngine or the human who acted.
	Actor string
	Data  map[string]any
}
