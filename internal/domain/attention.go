package domain

import "time"

// Attention is what a factory needs from people right now: the counts behind the app's badges.
type Attention struct {
	Factory FactoryID
	// WaitingHuman counts the items in state waiting_human: every decision waiting on a person
	// (phase 2.2 has no identity, so nobody's decisions are filtered out).
	WaitingHuman int
	// WaitingByType splits WaitingHuman by the items' work type, by type; empty when nothing waits.
	WaitingByType []TypeCount
	// Blocked counts the items in state blocked.
	Blocked int
	// Andon is the factory's open andon pull, nil while the line runs.
	Andon *AndonPull
}

// TypeCount is a count of items of one work type.
type TypeCount struct {
	Type  string
	Count int
}

// AndonPull is an open stop of a factory's line. Phase 2.2-E extends this same type (its id,
// kind, station, who cleared it and when); there is one domain.AndonPull.
type AndonPull struct {
	Reason string
	// PulledBy is the puller's Actor.Ref(): "system:engine" or "user:local".
	PulledBy string
	PulledAt time.Time
}
