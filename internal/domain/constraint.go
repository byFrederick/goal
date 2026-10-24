package domain

import (
	"cmp"
	"math"
	"slices"
	"time"
)

// ConstraintSource is how a line's constraint was found.
type ConstraintSource string

// Constraint sources.
const (
	// SourceConfigured: the definition's flow.constraint names the station.
	SourceConfigured ConstraintSource = "configured"
	// SourceDetected: the agent station busiest over the window of lease history.
	SourceDetected ConstraintSource = "detected"
	// SourceSnapshot: the agent station with the most work per slot now (Line.Constraint), while
	// there is no lease history to measure.
	SourceSnapshot ConstraintSource = "snapshot"
)

// Candidate is an agent station measured as a possible constraint over a window.
type Candidate struct {
	Station string
	// Slots is the station's capacity (Station.Capacity).
	Slots int
	// Busy is the share of the window's slot time that leases held (StationTimes.BusyRatio).
	Busy  float64
	Times StationTimes
}

// Candidates ranks the line's agent stations by their busy share over window (the measured
// stretch: from the factory's first lease, when that is later than the window's start), busiest
// first, ties in line order, at most n. A station without times is idle. There are none without
// a window to measure.
func (l Line) Candidates(times []StationTimes, window time.Duration, n int) []Candidate {
	if window <= 0 {
		return nil
	}

	var out []Candidate

	for _, st := range l.Stations {
		if st.Kind != KindAgent {
			continue
		}

		c := Candidate{Station: st.ID, Slots: st.Capacity(), Times: StationTimes{Station: st.ID}}
		if i := slices.IndexFunc(times, func(t StationTimes) bool { return t.Station == st.ID }); i >= 0 {
			c.Times = times[i]
		}

		c.Busy = c.Times.BusyRatio(c.Slots, window)
		out = append(out, c)
	}

	slices.SortStableFunc(out, func(a, b Candidate) int { return cmp.Compare(b.Busy, a.Busy) })

	return out[:min(n, len(out))]
}

// ConstraintOf is the line's constraint and how it was found: the configured station, else the
// busiest candidate when any was busy, else the snapshot of loads now ("" when no agent station
// has work).
func (l Line) ConstraintOf(cands []Candidate, loads map[string]StationLoad) (string, ConstraintSource) {
	if _, ok := l.Station(l.Flow.Constraint); ok {
		return l.Flow.Constraint, SourceConfigured
	}

	if len(cands) > 0 && cands[0].Busy > 0 {
		return cands[0].Station, SourceDetected
	}

	station, _ := l.Constraint(loads)

	return station, SourceSnapshot
}

// WaitSaved rules.
const (
	waitSavedMaxBusy   = 0.99
	waitSavedMinLeases = 20
)

// WaitSaved estimates how much mean queue wait at a station one more slot would save, with
// Sakasegawa's M/M/c approximation Wq(c) = ρ^(√(2(c+1))−1) / (c(1−ρ)) × service, at the same
// arrival rate (ρ' = ρc/(c+1) with c+1 slots), capped at the observed mean wait. There is no
// estimate (ok false) when the station is saturated (busy ≥ 0.99), has fewer than 20 leases to
// measure service by, has no slot, or no waiting was observed.
func WaitSaved(busy float64, slots int, meanService, observedWait time.Duration, leases int) (time.Duration, bool) {
	if busy >= waitSavedMaxBusy || leases < waitSavedMinLeases || slots < 1 || observedWait <= 0 || busy <= 0 {
		return 0, false
	}

	wq := func(rho float64, c int) float64 {
		return math.Pow(rho, math.Sqrt(2*float64(c+1))-1) / (float64(c) * (1 - rho)) * float64(meanService)
	}

	c := slots
	saved := time.Duration(math.Round(wq(busy, c) - wq(busy*float64(c)/float64(c+1), c+1)))

	return min(max(saved, 0), observedWait), true
}
