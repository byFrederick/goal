package domain

import (
	"strings"
	"testing"
)

// gateStation is the gate the tallies count.
const gateStation = "review"

func TestTallyGate_CountsRejectionsAndTheTopReason(t *testing.T) {
	t.Parallel()

	approved := GateVerdict{}
	reject := func(category, reason string) GateVerdict {
		return GateVerdict{Rejected: true, Category: category, Reason: reason}
	}

	tests := []struct {
		name     string
		verdicts []GateVerdict
		want     GateTally
	}{
		{name: "no verdicts", want: GateTally{Station: gateStation}},
		{name: "only approvals", verdicts: []GateVerdict{approved, approved}, want: GateTally{Station: gateStation, Verdicts: 2}},
		{
			name: "reasons count together whatever their case and spacing",
			verdicts: []GateVerdict{
				reject("", "  No evidence "), approved, reject("", "Wrong service"), reject("", "no evidence"),
			},
			want: GateTally{Station: gateStation, Verdicts: 4, Rejected: 3, TopReason: "no evidence"},
		},
		{
			name:     "a category wins over the reason text",
			verdicts: []GateVerdict{reject("evidence", "a"), reject("evidence", "b"), reject("", "c")},
			want:     GateTally{Station: gateStation, Verdicts: 3, Rejected: 3, TopReason: "evidence"},
		},
		{
			name:     "ties go to the newest",
			verdicts: []GateVerdict{reject("", "newer"), reject("", "older")},
			want:     GateTally{Station: gateStation, Verdicts: 2, Rejected: 2, TopReason: "newer"},
		},
		{
			name:     "a rejection without a reason counts without a label",
			verdicts: []GateVerdict{reject("", " ")},
			want:     GateTally{Station: gateStation, Verdicts: 1, Rejected: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := TallyGate(gateStation, tt.verdicts); got != tt.want {
				t.Errorf("TallyGate = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestReasonLabel_CutsLongReasons(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("é", 130)
	if got := ReasonLabel(long); got != strings.Repeat("é", 120) {
		t.Errorf("ReasonLabel(130 runes) has %d runes, want the first 120", len([]rune(got)))
	}
}

func TestAndonPolicy_WarnsOneRejectionBeforeTheRate(t *testing.T) {
	t.Parallel()

	policy := AndonPolicy{ReworkRate: 0.4, Window: 10} // 4 of 10 reach the rate

	tests := []struct {
		rejected int
		want     bool
	}{{0, false}, {2, false}, {3, true}, {4, true}, {7, true}}

	if got := policy.Threshold(); got != 4 {
		t.Errorf("Threshold() = %d, want 4", got)
	}

	for _, tt := range tests {
		if got := policy.Warns(GateTally{Verdicts: 10, Rejected: tt.rejected}); got != tt.want {
			t.Errorf("Warns(%d of 10) = %v, want %v", tt.rejected, got, tt.want)
		}
	}

	// A rate one rejection reaches warns only once something was rejected.
	low := AndonPolicy{ReworkRate: 0.1, Window: 10}
	if low.Warns(GateTally{Verdicts: 10}) || !low.Warns(GateTally{Verdicts: 10, Rejected: 1}) {
		t.Error("a 10% rate over 10 verdicts warns on the first rejection, not before")
	}
}
