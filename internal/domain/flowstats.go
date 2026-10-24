package domain

import (
	"slices"
	"strings"
	"time"
)

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

// StationTimes measures one station of a factory over a window [since, until), for the board's
// station statistics and the constraint report.
type StationTimes struct {
	Station string
	// Busy is how long the station's slots held leases in the window, summed over the slots:
	// every lease span and live lease that overlaps the window, clipped to it.
	Busy time.Duration
	// Leases counts those lease spans and live leases.
	Leases int
	// WaitingNow and WaitingDayAgo count the items waiting (queued or ready) at the station at
	// until and 24 hours before it.
	WaitingNow    int
	WaitingDayAgo int
	// OldestWaiting is when the longest-waiting of the items waiting at until began waiting (its
	// open span entered); zero when none waits.
	OldestWaiting time.Time
	// Waiting is the time items spent waiting (queued or ready) at the station in the window,
	// summed over the items: every waiting span that overlaps the window, clipped to it. Over
	// the window's length it is the time-weighted number of items waiting.
	Waiting time.Duration
	// Waiters counts the items with such a span.
	Waiters int
	// Verdicts counts the gate verdicts given at the station in the window, Rejected the
	// rejections among them.
	Verdicts int
	Rejected int
}

// BusyRatio is the share of the window's slot time (slots × window, at least one slot) that
// leases held, at most 1: the station may have had more slots earlier in the window. An empty
// window has none.
func (st StationTimes) BusyRatio(slots int, window time.Duration) float64 {
	if window <= 0 {
		return 0
	}

	return min(1, st.Busy.Seconds()/(float64(max(slots, 1))*window.Seconds()))
}

// Trend is which way a station's queue moves.
type Trend string

// Queue trends.
const (
	TrendGrowing   Trend = "growing"
	TrendSteady    Trend = "steady"
	TrendShrinking Trend = "shrinking"
)

// QueueTrend compares how many items wait now with how many waited 24 hours earlier.
func QueueTrend(now, dayAgo int) Trend {
	switch {
	case now > dayAgo:
		return TrendGrowing
	case now < dayAgo:
		return TrendShrinking
	default:
		return TrendSteady
	}
}

// AgentStats measures one agent of a factory over a window [since, until): its tasks from the
// lease spans that ended in the window, its runs from those that started in it.
type AgentStats struct {
	// TasksFinished counts its lease spans with a finished outcome (LeaseOutcome.Finished).
	TasksFinished int
	// Advanced counts those that moved the item on, SentBack those of them another station later
	// sent back for rework (the item's next rework round, by a station other than the agent's).
	Advanced, SentBack int
	// Reasons label those send-backs (VerdictLabel), most frequent first (Tallies).
	Reasons []Tally
	// TaskTimeP85 is the 85th percentile of the finished lease spans' durations (linear
	// interpolation); zero when none finished.
	TaskTimeP85 time.Duration
	// SucceededRuns counts its runs that succeeded, MedianTurns their median turns.
	SucceededRuns int
	MedianTurns   float64
	// SpendUSD is the cost of all its runs, failed and retried ones included.
	SpendUSD float64
	// Denied are the tools its runs' refused calls (tool_denied events and gateway calls with
	// outcome denied) were for, most frequent first, at most MaxDeniedTools of them; DeniedCalls
	// counts every refused call.
	Denied      []Tally
	DeniedCalls int
}

// MaxDeniedTools is how many of an agent's most denied tools AgentStats returns (the agent page
// shows them).
const MaxDeniedTools = 3

// SentBackRatio is the share of the advanced tasks later sent back; none when nothing advanced.
func (a AgentStats) SentBackRatio() (float64, bool) {
	if a.Advanced == 0 {
		return 0, false
	}

	return float64(a.SentBack) / float64(a.Advanced), true
}

// SpendPerTask is the spend over the tasks finished; none when no task finished.
func (a AgentStats) SpendPerTask() (float64, bool) {
	if a.TasksFinished == 0 {
		return 0, false
	}

	return a.SpendUSD / float64(a.TasksFinished), true
}

// Tally is how many times one label occurred.
type Tally struct {
	Label string
	Count int
}

// maxLabelRunes cuts a reason used as a label, so long reasons that start alike group together.
const maxLabelRunes = 120

// VerdictLabel is what a send-back is grouped by: the verdict's category when it has one, else
// its reason trimmed, lower-cased and cut to 120 characters.
func VerdictLabel(category, reason string) string {
	if c := strings.TrimSpace(category); c != "" {
		return c
	}

	r := []rune(strings.ToLower(strings.TrimSpace(reason)))
	if len(r) > maxLabelRunes {
		r = r[:maxLabelRunes]
	}

	return string(r)
}

// Tallies counts labels, most frequent first and alphabetically among equals; blank labels are
// not counted, and none gives nil.
func Tallies(labels []string) []Tally {
	counts := map[string]int{}

	for _, l := range labels {
		if l != "" {
			counts[l]++
		}
	}

	if len(counts) == 0 {
		return nil
	}

	out := make([]Tally, 0, len(counts))
	for l, n := range counts {
		out = append(out, Tally{Label: l, Count: n})
	}

	slices.SortFunc(out, func(a, b Tally) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}

		return strings.Compare(a.Label, b.Label)
	})

	return out
}
