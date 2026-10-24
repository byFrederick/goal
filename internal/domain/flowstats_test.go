package domain

import "testing"

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
