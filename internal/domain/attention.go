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

// AndonKind is why the andon cord was pulled.
type AndonKind string

// Andon kinds, as stored in andon_pulls.kind.
const (
	// AndonManual is a person's pull, phase 2.2's only kind.
	AndonManual AndonKind = "manual"
	// AndonRework is the engine's pull on a gate's rework rate (later work; the kind is reserved).
	AndonRework AndonKind = "rework"
)

// AndonPull is one stop of a factory's line, open until someone clears it. While a pull is open
// the engine releases and claims nothing at the factory (package engine). A factory has at most
// one open pull. Phase 2.2-C defined the type with the reason, puller and time; E extends it, and
// there is still one domain.AndonPull.
type AndonPull struct {
	ID        int64
	FactoryID FactoryID
	Kind      AndonKind
	// Station is the gate that triggered a rework pull; "" for a manual one.
	Station string
	Reason  string
	// PulledBy is the puller's Actor.Ref(): "system:engine" or "user:local".
	PulledBy string
	PulledAt time.Time
	// ClearedBy is the clearer's Actor.Ref(); "" while the pull is open.
	ClearedBy string
	// ClearedAt is zero while the pull is open.
	ClearedAt time.Time
	// Note is what the clearer wrote, if anything.
	Note string
	// Data holds the pull's details, such as a rework pull's rejected, window and top_reason;
	// never nil once stored.
	Data map[string]any
}

// Open reports whether the pull still stops the line.
func (p AndonPull) Open() bool { return p.ClearedAt.IsZero() }
