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

// CurrentRun is an item's agent run in progress, as the Item screen shows it.
type CurrentRun struct {
	// Run is the running run (status running).
	Run Run
	// Slot is the lease's slot at the station, 0-based.
	Slot int
	// Turns counts the turns the run has reported so far (its usage events).
	Turns int
}
