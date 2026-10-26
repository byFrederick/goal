package domain

import (
	"errors"
	"time"
)

// ErrCycle is an edge that would make an item need itself, directly or through other items.
var ErrCycle = errors.New("dependency cycle")

// EdgeKind says why an item needs another: planned by whoever split the work, linked by a
// person, or overlap (their files overlap).
type EdgeKind string

// Edge kinds.
const (
	EdgePlanned EdgeKind = "planned"
	EdgeLinked  EdgeKind = "linked"
	EdgeOverlap EdgeKind = "overlap"
)

// Valid reports whether k is a known edge kind.
func (k EdgeKind) Valid() bool {
	switch k {
	case EdgePlanned, EdgeLinked, EdgeOverlap:
		return true
	default:
		return false
	}
}

// NewEdge is a dependency to add: Item is not claimed while Needs is neither done nor
// cancelled. Both are items of the same factory.
type NewEdge struct {
	Item  ItemID
	Needs ItemID
	Kind  EdgeKind
	By    Actor
	At    time.Time
}

// Edge is a stored dependency: Item needs Needs. By is its author's Actor.Ref().
type Edge struct {
	Item  ItemID
	Needs ItemID
	Kind  EdgeKind
	By    string
	At    time.Time
}

// ItemEdges are an item's dependencies both ways: the edges it needs and the edges of the
// items that need it, each oldest first.
type ItemEdges struct {
	Needs    []Edge
	NeededBy []Edge
}

// OpenNeed is one of an item's needs whose item is neither done nor cancelled: the edge's kind
// and the needed item's id, number, title and file globs (nil when none are known; for an
// assembly, its parts' PartPaths).
type OpenNeed struct {
	Kind   EdgeKind
	Item   ItemID
	Number int64
	Title  string
	Paths  []string
}
