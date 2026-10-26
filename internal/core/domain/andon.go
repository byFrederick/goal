package domain

import (
	"math"
	"strings"
	"time"
)

// Limits of what people write on the andon cord, in characters.
const (
	MaxAndonReason = 500
	MaxAndonNote   = 2000
)

// Audit of the andon cord; the target is the factory (AuditTargetFactory).
const (
	AuditAndonPulled  = "andon.pulled"
	AuditAndonCleared = "andon.cleared"
)

// DefaultAndonWindow is how many of a gate's last verdicts its tally counts when the line has no
// andon policy.
const DefaultAndonWindow = 10

// maxReasonLabel is how much of a rejection reason names it in a tally.
const maxReasonLabel = 120

// GateVerdict is one verdict of a gate station (a gate_approved or gate_rejected event).
type GateVerdict struct {
	Rejected bool
	// Category is the verdict's category when the reviewer's contract gives one.
	Category string
	Reason   string
	At       time.Time
}

// GateTally counts the last verdicts of one gate station.
type GateTally struct {
	Station  string
	Verdicts int
	Rejected int
	// TopReason is the most frequent category, or else normalized reason, of the rejections; ""
	// when none were rejected.
	TopReason string
}

// TallyGate counts verdicts, newest first, of a gate station. Ties between reasons go to the one
// given most recently.
func TallyGate(station string, verdicts []GateVerdict) GateTally {
	t := GateTally{Station: station, Verdicts: len(verdicts)}
	counts := map[string]int{}

	for _, v := range verdicts {
		if !v.Rejected {
			continue
		}

		t.Rejected++

		label := v.Category
		if label == "" {
			label = ReasonLabel(v.Reason)
		}

		if label == "" {
			continue
		}

		counts[label]++
		if counts[label] > counts[t.TopReason] {
			t.TopReason = label
		}
	}

	return t
}

// ReasonLabel normalizes a rejection reason so equal reasons count together: trimmed,
// lower-cased and cut to its first 120 characters.
func ReasonLabel(reason string) string {
	r := []rune(strings.ToLower(strings.TrimSpace(reason)))

	return string(r[:min(len(r), maxReasonLabel)])
}

// Threshold is how many rejections among the last Window verdicts reach the policy's rework rate.
func (p AndonPolicy) Threshold() int {
	// The epsilon keeps a product such as 0.4 × 10 from rounding up past 4.
	return int(math.Ceil(p.ReworkRate*float64(p.Window) - 1e-9))
}

// Warns reports whether one more rejection at the gate t tallies would reach the policy's rework
// rate: at least Threshold − 1 rejections, and at least one. The inbox shows it as a rework
// warning; the engine pulls nothing on it (an automatic pull is later work).
func (p AndonPolicy) Warns(t GateTally) bool {
	return t.Rejected > 0 && t.Rejected >= p.Threshold()-1
}
