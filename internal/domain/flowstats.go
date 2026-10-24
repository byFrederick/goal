package domain

import "time"

// Span is one stretch of an item's life at one station in one state (item_spans): it opens when
// the item arrives there or changes state, and closes at the next change of station, state or
// rework round. An item that is neither done nor cancelled has exactly one open span. The store
// keeps spans itself (a database trigger in PostgreSQL), so no code path can forget them.
type Span struct {
	Station string
	State   ItemState
	// ReworkRound is the item's rework count while in this span.
	ReworkRound int
	// ReworkBy is the station that sent the item back for this round; "" in round 0.
	ReworkBy  string
	EnteredAt time.Time
	// LeftAt is zero while the span is open.
	LeftAt time.Time
}

// LeaseOutcome is how a lease on an item ended: what became of the slot's work. Values match
// lease_spans.outcome.
type LeaseOutcome string

// Lease outcomes.
const (
	// LeaseAdvanced: the output was accepted; the item moved on, or its gate approved it.
	LeaseAdvanced LeaseOutcome = "advanced"
	// LeaseRejected: this station's gate rejected the item; it went back for rework.
	LeaseRejected LeaseOutcome = "rejected"
	// LeaseEscalated: the gate rejected it past its rework allowance; a person decides.
	LeaseEscalated LeaseOutcome = "escalated"
	// LeaseBlocked: the run failed or its output could not be used; the item is blocked.
	LeaseBlocked LeaseOutcome = "blocked"
	// LeaseReleased: the run was stopped (shutdown, or the weekly budget); the item is queued again.
	LeaseReleased LeaseOutcome = "released"
	// LeaseExpired: the holder stopped heartbeating; the sweep requeued the item.
	LeaseExpired LeaseOutcome = "expired"
	// LeaseTaken: a person moved, paused or sent the item back while it ran.
	LeaseTaken LeaseOutcome = "taken"
)

// Finished reports whether the agent completed its task: advanced, rejected or escalated.
func (o LeaseOutcome) Finished() bool {
	return o == LeaseAdvanced || o == LeaseRejected || o == LeaseEscalated
}

// LeaseSpan is one lease from claim to release (lease_spans): how long a slot held an item and
// how that ended. The store records it in the transaction that deletes the lease.
type LeaseSpan struct {
	ItemID  ItemID
	Station string
	Agent   string
	Slot    int
	// ReworkRound is the item's rework count while the lease was held.
	ReworkRound int
	AcquiredAt  time.Time
	EndedAt     time.Time
	Outcome     LeaseOutcome
}

// CurrentRun is an item's agent run in progress, as the Item screen shows it.
type CurrentRun struct {
	// Run is the running run (status running).
	Run Run
	// Slot is the lease's slot at the station, 0-based.
	Slot int
	// Turns counts the turns the run has reported so far (its usage events).
	Turns int
}
