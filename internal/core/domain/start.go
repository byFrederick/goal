package domain

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"time"
)

// MaxStartAhead is how far ahead a start may be (ADR 0040 decision 3).
const MaxStartAhead = 365 * 24 * time.Hour

// StartRequest is when an item may start (ADR 0040 decision 3): at a time, after a wait, after
// other items, or a time and items. In becomes At when it is resolved; only the time is stored,
// and After becomes linked edges.
type StartRequest struct {
	// At is an RFC 3339 time, or one without a zone (2026-11-05T09:00, 2026-11-05 09:00 or
	// 2026-11-05), read in Zone. "" for none.
	At string
	// In is a wait from filing: a whole number of hours, days or weeks (36h, 7d, 2w). "" for none.
	In string
	// After are the items it starts after, as the caller resolved them.
	After []After
	// Zone reads a time without a zone: the person's; UTC when nil.
	Zone *time.Location
}

// After is one item a start follows: the ref the caller named and the item it names, nil when
// it names none the caller can see.
type After struct {
	Ref  string
	Item *Item
}

// Start is a resolved start: the time the item may start (zero for none) and the open items of
// its factory it waits for, each once.
type Start struct {
	At    time.Time
	After []ItemID
}

// startLayouts are the times without a zone a start takes, read in the person's zone.
var startLayouts = []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04", time.DateOnly}

// waitPattern is a wait: a whole number of hours, days or weeks.
var waitPattern = regexp.MustCompile(`^([1-9][0-9]{0,4})([hdw])$`)

// ParseWait reads a wait such as 36h, 7d or 2w. Anything else is ErrInvalid.
func ParseWait(s string) (time.Duration, error) {
	m := waitPattern.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("wait %q is not a number of hours, days or weeks, as in 36h, 7d or 2w: %w", s, ErrInvalid)
	}

	n, _ := strconv.Atoi(m[1]) // the pattern allows at most 5 digits

	unit := map[string]time.Duration{"h": time.Hour, "d": 24 * time.Hour, "w": 7 * 24 * time.Hour}[m[2]]

	return time.Duration(n) * unit, nil
}

// ParseStartTime reads an RFC 3339 time, or one without a zone in zone (UTC when nil). Anything
// else is ErrInvalid.
func ParseStartTime(s string, zone *time.Location) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}

	if zone == nil {
		zone = time.UTC
	}

	for _, layout := range startLayouts {
		if t, err := time.ParseInLocation(layout, s, zone); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("time %q is not RFC 3339 or a date and time such as 2026-11-05T09:00: %w", s, ErrInvalid)
}

// Resolve turns r into the start of an item of factory filed or changed at now. At and In
// cannot both be set; In becomes now + the wait. The time is at most MaxStartAhead after now.
// An after item that is done is met and left out; one that is unknown, cancelled or of another
// factory is refused. Every refusal is ErrInvalid.
func (r StartRequest) Resolve(factory FactoryID, now time.Time) (Start, error) {
	var (
		s   Start
		err error
	)

	switch {
	case r.At != "" && r.In != "":
		return Start{}, fmt.Errorf("a start has a time or a wait, not both: %w", ErrInvalid)
	case r.At != "":
		s.At, err = ParseStartTime(r.At, r.Zone)
	case r.In != "":
		var wait time.Duration

		wait, err = ParseWait(r.In)
		s.At = now.Add(wait)
	}

	if err != nil {
		return Start{}, err
	}

	if s.At.After(now.Add(MaxStartAhead)) {
		return Start{}, fmt.Errorf("a start is at most 365 days ahead, not %s: %w", s.At.Format(time.RFC3339), ErrInvalid)
	}

	if !s.At.IsZero() {
		s.At = s.At.UTC()
	}

	for _, a := range r.After {
		switch {
		case a.Item == nil:
			return Start{}, fmt.Errorf("after %s: no such item: %w", a.Ref, ErrInvalid)
		case a.Item.FactoryID != factory:
			return Start{}, fmt.Errorf("after %s: an item of another factory: %w", a.Ref, ErrInvalid)
		case a.Item.State == StateCancelled:
			return Start{}, fmt.Errorf("after %s: the item is cancelled: %w", a.Ref, ErrInvalid)
		case a.Item.State == StateDone, slices.Contains(s.After, a.Item.ID):
			continue
		}

		s.After = append(s.After, a.Item.ID)
	}

	return s, nil
}

// StartChangedEvent is the start_changed event of by's change of it's start, as it is after
// the change, at now.
func StartChangedEvent(it Item, by Actor, now time.Time) Event {
	data := map[string]any{"actor": by.Ref()}
	if !it.StartAt.IsZero() {
		data["start_at"] = it.StartAt.UTC().Format(time.RFC3339)
	}

	return Event{FactoryID: it.FactoryID, ItemID: it.ID, At: now, Kind: EventStartChanged, Station: it.Station, Actor: by.Name, Data: data}
}
