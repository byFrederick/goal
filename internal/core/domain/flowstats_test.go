package domain

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// A finished task is one the agent completed, whatever the gate made of it; a lease that ended
// any other way did not finish its task.
func TestLeaseOutcome_Finished(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		outcome  LeaseOutcome
		finished bool
	}{
		{LeaseAdvanced, true},
		{LeaseRejected, true},
		{LeaseEscalated, true},
		{LeaseBlocked, false},
		{LeaseReleased, false},
		{LeaseExpired, false},
		{LeaseTaken, false},
		{"", false},
	} {
		if got := tt.outcome.Finished(); got != tt.finished {
			t.Errorf("%q.Finished() = %v, want %v", tt.outcome, got, tt.finished)
		}
	}
}

// The busy share is the slot time leases held over the slot time the window offers, never more
// than all of it (the station may have had more slots earlier in the window).
func TestStationTimes_BusyRatio(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		busy   time.Duration
		slots  int
		window time.Duration
		want   float64
	}{
		{"two slots, 8 of 20 slot hours", 8 * time.Hour, 2, 10 * time.Hour, 0.4},
		{"idle", 0, 3, time.Hour, 0},
		{"more than the slots now", 3 * time.Hour, 1, 2 * time.Hour, 1},
		{"no slot counts as one", time.Hour, 0, 4 * time.Hour, 0.25},
		{"empty window", time.Hour, 1, 0, 0},
	} {
		if got := (StationTimes{Busy: tt.busy}).BusyRatio(tt.slots, tt.window); got != tt.want {
			t.Errorf("%s: BusyRatio = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// The queue grows when more items wait now than a day earlier and shrinks when fewer do.
func TestQueueTrend(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		now, dayAgo int
		want        Trend
	}{
		{3, 1, TrendGrowing},
		{2, 2, TrendSteady},
		{0, 0, TrendSteady},
		{1, 4, TrendShrinking},
	} {
		if got := QueueTrend(tt.now, tt.dayAgo); got != tt.want {
			t.Errorf("QueueTrend(%d, %d) = %q, want %q", tt.now, tt.dayAgo, got, tt.want)
		}
	}
}

// Labels the send-back tests group by.
const (
	catEvidence  = "Claims without evidence"
	testsMissing = "tests missing"
)

// A send-back is labelled by the reviewer's category when it gave one, else by its reason,
// trimmed, lower-cased and cut to 120 characters, so the same complaint groups together.
func TestVerdictLabel(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("é", 130)

	for _, tt := range []struct {
		category, reason, want string
	}{
		{catEvidence, "No links to dashboards", catEvidence},
		{"  ", "  Tests Missing  ", testsMissing},
		{"", long, strings.Repeat("é", 120)},
		{"", "", ""},
	} {
		if got := VerdictLabel(tt.category, tt.reason); got != tt.want {
			t.Errorf("VerdictLabel(%q, %q) = %q, want %q", tt.category, tt.reason, got, tt.want)
		}
	}
}

// Tallies count labels, most frequent first and alphabetically among equals; blanks are not
// counted.
func TestTallies(t *testing.T) {
	t.Parallel()

	got := Tallies([]string{testsMissing, catEvidence, testsMissing, "", "Bash"})
	want := []Tally{{testsMissing, 2}, {"Bash", 1}, {catEvidence, 1}}

	if !slices.Equal(got, want) {
		t.Errorf("Tallies = %v, want %v", got, want)
	}
}

func TestAgentStats_Ratios(t *testing.T) {
	t.Parallel()

	a := AgentStats{TasksFinished: 4, Advanced: 3, SentBack: 2, SpendUSD: 2}

	if r, ok := a.SentBackRatio(); !ok || r != 2.0/3 {
		t.Errorf("SentBackRatio = %v, %v; want 2/3", r, ok)
	}

	if p, ok := a.SpendPerTask(); !ok || p != 0.5 {
		t.Errorf("SpendPerTask = %v, %v; want 0.5", p, ok)
	}

	if _, ok := (AgentStats{SpendUSD: 1}).SpendPerTask(); ok {
		t.Error("SpendPerTask with no task finished; want none")
	}

	if _, ok := (AgentStats{}).SentBackRatio(); ok {
		t.Error("SentBackRatio with nothing advanced; want none")
	}
}
