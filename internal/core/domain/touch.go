package domain

import (
	"cmp"
	"maps"
	"math"
	"slices"
	"strings"
	"time"
)

// Human touches per item (spec 4 §4.9): how often a person had to act on an item. They are
// derived from the item events already recorded; the PostgreSQL adapter keeps the same rule in
// the item_touches view (migration 00052), and storetest checks both agree.

// TouchReason says why a person had to act (spec 4 §4.10).
type TouchReason string

// Touch reasons.
const (
	ReasonMissingKnowledge TouchReason = "missing_knowledge"
	ReasonWrongDecision    TouchReason = "wrong_decision"
	// ReasonRequiredGate is a decision the line requires; planned touches start with it.
	ReasonRequiredGate TouchReason = "required_gate"
	ReasonOther        TouchReason = "other"
)

// Valid reports whether r is one of the touch reasons.
func (r TouchReason) Valid() bool {
	switch r {
	case ReasonMissingKnowledge, ReasonWrongDecision, ReasonRequiredGate, ReasonOther:
		return true
	}

	return false
}

// AuditTouchLabelled is a person's choice of a touch's reason: {event_id, reason}. It is never an
// item event, so it is not a touch itself.
const AuditTouchLabelled = "touch.labelled"

// KeyPlanned is the event data key of a decision that says whether the line required it.
const KeyPlanned = "planned"

// personRef starts the actor ref of a person (user:<id>, user:local included).
const personRef = string(ActorUser) + ":"

// Touch is one action by a person on an item: its events with the same person as data.actor and
// the same at (one transition), named by its most specific event (touchRank).
type Touch struct {
	// EventID is the naming event's; a reason is set on it.
	EventID   int64
	FactoryID FactoryID
	ItemID    ItemID
	Kind      EventKind
	Station   string
	At        time.Time
	// Actor is the person's ref (user:<id>); ActorName the name the event recorded.
	Actor     string
	ActorName string
	// Planned is a decision the line requires (PlannedEvent of the naming event).
	Planned bool
	// Reason is "" until one is set; ReasonBy is who set it ("" for the planned default).
	Reason   TouchReason
	ReasonBy string
}

// IsTouchEvent reports whether e is part of a touch: a person's event (data.actor user:…) other
// than created (the submission is the input) and lease_taken (it accompanies the person's own
// event). API keys, agents and the engine never touch.
func IsTouchEvent(e Event) bool {
	actor, _ := e.Data["actor"].(string)

	return strings.HasPrefix(actor, personRef) && e.Kind != EventCreated && e.Kind != EventLeaseTaken
}

// PlannedEvent reports whether e is a decision the line requires: data.planned when the event
// carries it (decisions do from phase 4 on), else whether it is an approval, gate approval or
// merge, as older decisions count.
func PlannedEvent(e Event) bool {
	if p, ok := e.Data[KeyPlanned].(bool); ok {
		return p
	}

	return e.Kind == EventApproved || e.Kind == EventGateApproved || e.Kind == EventMerged
}

// DefaultReason is the reason a touch event starts with: required_gate when planned, else none.
func DefaultReason(e Event) TouchReason {
	if PlannedEvent(e) {
		return ReasonRequiredGate
	}

	return ""
}

// touchNames are the kinds that name a touch, most specific first; any other kind comes after.
var touchNames = []EventKind{
	EventMerged, EventRejected, EventGateRejected, EventGateApproved, EventApproved,
	EventMoved, EventPaused, EventRetried, EventNote, EventReleased,
}

// touchRank orders the events of one touch: the lowest names it.
func touchRank(k EventKind) int {
	if i := slices.Index(touchNames, k); i >= 0 {
		return i
	}

	return len(touchNames)
}

// TouchesOf groups the touch events among one item's events (IsTouchEvent) into touches, in
// time order (at, then the naming event's id). Ties of rank are named by the earliest event.
// Reasons are left empty: they are stored apart (LabelTouch).
func TouchesOf(events []Event) []Touch {
	type key struct {
		actor string
		at    int64
	}

	names := map[key]Event{}

	for _, e := range events {
		if !IsTouchEvent(e) {
			continue
		}

		actor, _ := e.Data["actor"].(string)
		k := key{actor: actor, at: e.At.UnixMicro()}

		if cur, ok := names[k]; !ok || touchRank(e.Kind) < touchRank(cur.Kind) ||
			(touchRank(e.Kind) == touchRank(cur.Kind) && e.ID < cur.ID) {
			names[k] = e
		}
	}

	out := make([]Touch, 0, len(names))
	for k, e := range names {
		out = append(out, Touch{
			EventID: e.ID, FactoryID: e.FactoryID, ItemID: e.ItemID, Kind: e.Kind, Station: e.Station, At: e.At,
			Actor: k.actor, ActorName: e.Actor, Planned: PlannedEvent(e),
		})
	}

	slices.SortFunc(out, func(a, b Touch) int { return cmp.Or(a.At.Compare(b.At), cmp.Compare(a.EventID, b.EventID)) })

	return out
}

// FinishedItem is an item finished (done) in a window with what the touch metrics read of it.
type FinishedItem struct {
	FactoryID FactoryID
	ItemID    ItemID
	// VersionID is the definition version of its last agent run; "" without one.
	VersionID DefinitionVersionID
	DoneAt    time.Time
	Touches   []Touch
}

// TouchCount is the touches of one station, kind or reason ("" for none set).
type TouchCount struct {
	Key     string
	Touches int
	Planned int
}

// VersionTouches is the touches of the items that last ran on one definition version ("" for
// items no agent ran).
type VersionTouches struct {
	VersionID DefinitionVersionID
	Finished  int
	Touchless int
	Touches   int
	Planned   int
}

// TouchStats measures the touches of the items finished in a window (spec 4 §4.9).
type TouchStats struct {
	Finished  int
	Touchless int
	Touches   int
	Planned   int
	// Median is the median of the touches per finished item (linear interpolation).
	Median float64
	// ByStation, ByKind and ByReason count touches by the naming event's station, kind and
	// reason, the most touches first.
	ByStation []TouchCount
	ByKind    []TouchCount
	ByReason  []TouchCount
	// ByVersion counts by the definition version of each item's last run, by version id.
	ByVersion []VersionTouches
	// LineStops are the andon pulls people made in the window: the line's, not an item's.
	LineStops int
}

// Unplanned is the touches no line required.
func (s TouchStats) Unplanned() int { return s.Touches - s.Planned }

// PerItem is the mean of n over the finished items; 0 when none finished.
func (s TouchStats) PerItem(n int) float64 { return ratio(n, s.Finished) }

// TouchlessRate is the share of the finished items that had no touch; 0 when none finished.
func (s TouchStats) TouchlessRate() float64 { return ratio(s.Touchless, s.Finished) }

// PerItem is the mean of n over the version's finished items; 0 when none finished.
func (v VersionTouches) PerItem(n int) float64 { return ratio(n, v.Finished) }

// TouchlessRate is the share of the version's finished items that had no touch.
func (v VersionTouches) TouchlessRate() float64 { return ratio(v.Touchless, v.Finished) }

func ratio(n, d int) float64 {
	if d == 0 {
		return 0
	}

	return float64(n) / float64(d)
}

// FactoryTouchStats is one factory's touch statistics.
type FactoryTouchStats struct {
	FactoryID FactoryID
	TouchStats
}

// TallyTouches measures items, the items finished in a window (LineStops is left to the caller).
func TallyTouches(items []FinishedItem) TouchStats {
	var (
		s                     TouchStats
		perItem               = make([]float64, 0, len(items))
		station, kind, reason = map[string]*TouchCount{}, map[string]*TouchCount{}, map[string]*TouchCount{}
		versions              = map[DefinitionVersionID]*VersionTouches{}
		count                 = func(m map[string]*TouchCount, key string, planned bool) { add(m, key).add(planned) }
	)

	for _, it := range items {
		v := versions[it.VersionID]
		if v == nil {
			v = &VersionTouches{VersionID: it.VersionID}
			versions[it.VersionID] = v
		}

		s.Finished++
		v.Finished++

		if len(it.Touches) == 0 {
			s.Touchless++
			v.Touchless++
		}

		perItem = append(perItem, float64(len(it.Touches)))

		for _, t := range it.Touches {
			s.Touches++
			v.Touches++

			if t.Planned {
				s.Planned++
				v.Planned++
			}

			count(station, t.Station, t.Planned)
			count(kind, string(t.Kind), t.Planned)
			count(reason, string(t.Reason), t.Planned)
		}
	}

	slices.Sort(perItem)
	s.Median = median(perItem)
	s.ByStation, s.ByKind, s.ByReason = ranked(station), ranked(kind), ranked(reason)

	for _, id := range slices.Sorted(maps.Keys(versions)) {
		s.ByVersion = append(s.ByVersion, *versions[id])
	}

	return s
}

// BucketTouches measures items in n consecutive buckets of length d from start, oldest first, by
// when each was done; items outside them are left out.
func BucketTouches(items []FinishedItem, start time.Time, d time.Duration, n int) []TouchStats {
	buckets := make([][]FinishedItem, n)

	for _, it := range items {
		if i := int(it.DoneAt.Sub(start) / d); !it.DoneAt.Before(start) && i < n {
			buckets[i] = append(buckets[i], it)
		}
	}

	out := make([]TouchStats, n)
	for i, b := range buckets {
		out[i] = TallyTouches(b)
	}

	return out
}

func add(m map[string]*TouchCount, key string) *TouchCount {
	c := m[key]
	if c == nil {
		c = &TouchCount{Key: key}
		m[key] = c
	}

	return c
}

func (c *TouchCount) add(planned bool) {
	c.Touches++

	if planned {
		c.Planned++
	}
}

// ranked lists counts, the most touches first, then by key.
func ranked(m map[string]*TouchCount) []TouchCount {
	out := make([]TouchCount, 0, len(m))
	for _, c := range m {
		out = append(out, *c)
	}

	slices.SortFunc(out, func(a, b TouchCount) int { return cmp.Or(cmp.Compare(b.Touches, a.Touches), cmp.Compare(a.Key, b.Key)) })

	return out
}

// median of sorted values, linearly interpolated; 0 for none.
func median(sorted []float64) float64 {
	if len(sorted) == 0 {
		return 0
	}

	pos := 0.5 * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := min(lo+1, len(sorted)-1)

	return sorted[lo] + (pos-float64(lo))*(sorted[hi]-sorted[lo])
}
