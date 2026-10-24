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
	// EventAgentCancelled is an agent run the engine stopped; its item went back to its queue.
	// Data reason says why ("budget": the factory's weekly budget is used up, policy stop).
	EventAgentCancelled EventKind = "agent_cancelled"
	EventDone           EventKind = "done"
	EventRetried        EventKind = "retried"
	EventMerged         EventKind = "merged"
	// EventUsage is a running agent's report of one turn's token usage; a run's usage events
	// count its turns so far.
	EventUsage EventKind = "usage"
	// EventNote is a person's note for the agents working the item; data text and actor (ref).
	// The next run's brief carries it.
	EventNote EventKind = "note"
	// EventPaused is a person stopping an open item where it is: it is blocked until a retry.
	// Data reason (optional) and actor (ref).
	EventPaused EventKind = "paused"
	// EventLeaseTaken is a person's action on an item in progress (move, pause, send back) that
	// took its lease: the instance running it stops the run. Data actor (ref) and action.
	EventLeaseTaken EventKind = "lease_taken"
)

// Audit of items.
const (
	// AuditItemNoted is a note left on an item; its data is the note's length, never its text.
	AuditItemNoted = "item.noted"
	// AuditItemMoved is a person's move of an item to another station (from, to, reason).
	AuditItemMoved = "item.moved"
	// AuditItemPaused is a person pausing an item (station, reason).
	AuditItemPaused = "item.paused"
	// AuditItemRejected is a person sending an item back for rework (from, to, reason).
	AuditItemRejected = "item.rejected"
	// AuditTargetItem is the target kind of entries about an item.
	AuditTargetItem = "item"
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
