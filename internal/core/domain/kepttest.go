package domain

import (
	"fmt"
	"time"
	"unicode/utf8"
)

// KeptTestID identifies one kept test (ADR 0039 decision 12).
type KeptTestID string

// MaxKeptTestStation bounds a kept test's station id, in characters (kept_tests.station).
const MaxKeptTestStation = 100

// KeptTest is a past item a person kept, at a station it passed the first time, as a test: the
// item must keep passing there. A failing kept test warns at publish and never blocks one.
type KeptTest struct {
	ID        KeptTestID
	FactoryID FactoryID
	ItemID    ItemID
	// ItemNumber and ItemTitle are the item's, read with the test.
	ItemNumber int64
	ItemTitle  string
	Station    string
	KeptBy     Actor
	KeptAt     time.Time
	// LastResult is the test's newest finished try of any source; nil for a test never run.
	LastResult *KeptTestLast
}

// KeptTestLast is a kept test's newest finished try: passed, failed or error, when it finished and
// its run (empty when it never ran).
type KeptTestLast struct {
	Result KeptTestResult
	At     time.Time
	RunID  RunID
}

// NewKeptTest is a test to keep.
type NewKeptTest struct {
	FactoryID FactoryID
	ItemID    ItemID
	Station   string
	KeptBy    Actor
	KeptAt    time.Time
}

// Check reports what makes n impossible to keep, wrapping ErrInvalid.
func (n NewKeptTest) Check() error {
	if n.FactoryID == "" || n.ItemID == "" || n.Station == "" || n.KeptBy.ID == "" || n.KeptAt.IsZero() {
		return fmt.Errorf("kept test: it needs a factory, an item, a station, who kept it and when: %w", ErrInvalid)
	}

	if utf8.RuneCountInString(n.Station) > MaxKeptTestStation {
		return fmt.Errorf("kept test: station is over %d characters: %w", MaxKeptTestStation, ErrInvalid)
	}

	return nil
}

// PassedFirstTime reports whether an item's events, in append order, show it passed station the
// first time: it was claimed there exactly once, no gate rejected it there, and it then moved on
// from there. An item sent back to the station counts as not passing it only once it was claimed
// there again, since only a claim says it was worked there twice.
func PassedFirstTime(events []Event, station string) bool {
	claims, movedOn := 0, false

	for _, e := range events {
		switch {
		case e.Kind == EventClaimed && e.Station == station:
			claims++
		case e.Kind == EventGateRejected && e.Station == station:
			return false
		case e.Kind == EventMoved && claims > 0 && movedFrom(e) == station:
			movedOn = true
		}
	}

	return claims == 1 && movedOn
}

// movedFrom is the station a move left: its data's from, else the event's station.
func movedFrom(e Event) string {
	if from, ok := e.Data["from"].(string); ok && from != "" {
		return from
	}

	return e.Station
}
