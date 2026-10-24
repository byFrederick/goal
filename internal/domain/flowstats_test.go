package domain

import (
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
