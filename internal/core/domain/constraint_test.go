package domain

import (
	"testing"
	"time"
)

const (
	stTriage      = "triage"
	stInvestigate = "investigate"
	stDecide      = "decide"
)

// constraintLine is intake (queue) → triage (2 agents) → investigate (3 agents, wip 3) → decide
// (human) → done.
var constraintLine = Line{Stations: []Station{
	{ID: "intake", Kind: KindQueue},
	{ID: stTriage, Kind: KindAgent, Agents: 2},
	{ID: stInvestigate, Kind: KindAgent, Agents: 3, WIP: 3},
	{ID: stDecide, Kind: KindHuman},
	{ID: "finished", Kind: KindDone},
}}

func TestLine_CandidatesRankAgentStationsByBusyShare(t *testing.T) {
	t.Parallel()

	window := 10 * time.Hour
	times := []StationTimes{
		{Station: "intake", Busy: 0, Waiting: 5 * time.Hour},
		{Station: stTriage, Busy: 4 * time.Hour, Leases: 8},        // 4 / (2 × 10) = 0.2
		{Station: stInvestigate, Busy: 27 * time.Hour, Leases: 20}, // 27 / (3 × 10) = 0.9
		{Station: stDecide, Busy: 99 * time.Hour},                  // not an agent station
	}

	got := constraintLine.Candidates(times, window, 3)
	if len(got) != 2 || got[0].Station != stInvestigate || got[0].Slots != 3 || got[0].Busy != 0.9 ||
		got[1].Station != stTriage || got[1].Busy != 0.2 || got[0].Times.Leases != 20 {
		t.Fatalf("candidates = %+v, want investigate (0.9, 3 slots) then triage (0.2)", got)
	}

	if one := constraintLine.Candidates(times, window, 1); len(one) != 1 {
		t.Errorf("at most one candidate = %+v", one)
	}

	if none := constraintLine.Candidates(times, 0, 3); len(none) != 0 {
		t.Errorf("candidates without a window = %+v, want none", none)
	}

	// An agent station without any times is a candidate at 0, after the busy ones.
	if idle := constraintLine.Candidates(times[:1], window, 3); len(idle) != 2 || idle[0].Station != stTriage || idle[0].Busy != 0 {
		t.Errorf("candidates of an idle line = %+v, want both agent stations at 0 in line order", idle)
	}
}

func TestLine_ConstraintOf(t *testing.T) {
	t.Parallel()

	loads := map[string]StationLoad{stTriage: {Waiting: 4}}
	busy := []Candidate{{Station: stInvestigate, Busy: 0.9}, {Station: stTriage, Busy: 0.2}}
	idle := []Candidate{{Station: stTriage}, {Station: stInvestigate}}

	configured := constraintLine
	configured.Flow.Constraint = stDecide

	tests := []struct {
		name   string
		line   Line
		cands  []Candidate
		want   string
		source ConstraintSource
	}{
		{name: "configured wins", line: configured, cands: busy, want: stDecide, source: SourceConfigured},
		{name: "busiest over time", line: constraintLine, cands: busy, want: stInvestigate, source: SourceDetected},
		{name: "snapshot without busy history", line: constraintLine, cands: idle, want: stTriage, source: SourceSnapshot},
		{name: "snapshot without candidates", line: constraintLine, cands: nil, want: stTriage, source: SourceSnapshot},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got, source := tt.line.ConstraintOf(tt.cands, loads); got != tt.want || source != tt.source {
				t.Errorf("ConstraintOf = %q (%s), want %q (%s)", got, source, tt.want, tt.source)
			}
		})
	}

	if got, source := constraintLine.ConstraintOf(nil, nil); got != "" || source != SourceSnapshot {
		t.Errorf("ConstraintOf an empty line = %q (%s), want none", got, source)
	}

	// A station is the constraint only when it is meaningfully loaded: busy at least
	// ConstraintMinBusy of the time, or with work waiting in front of it now. 8% busy with the
	// line empty, or with one item in progress and nothing waiting, holds nothing back.
	lightly := []Candidate{{Station: stInvestigate, Busy: 0.08}, {Station: stTriage, Busy: 0.01}}
	for name, loads := range map[string]map[string]StationLoad{
		"line empty":           nil,
		"one in progress only": {stInvestigate: {InProgress: 1}},
	} {
		if got, _ := constraintLine.ConstraintOf(lightly, loads); got != "" {
			t.Errorf("ConstraintOf a lightly loaded line (%s) = %q, want none", name, got)
		}
	}

	if got, source := constraintLine.ConstraintOf(lightly, map[string]StationLoad{stInvestigate: {Waiting: 2}}); got != stInvestigate || source != SourceSnapshot {
		t.Errorf("ConstraintOf with work waiting at a lightly busy station = %q (%s), want it from the snapshot", got, source)
	}

	half := []Candidate{{Station: stInvestigate, Busy: ConstraintMinBusy}}
	if got, source := constraintLine.ConstraintOf(half, nil); got != stInvestigate || source != SourceDetected {
		t.Errorf("ConstraintOf at the threshold = %q (%s), want it detected", got, source)
	}
}

func TestWaitSaved(t *testing.T) {
	t.Parallel()

	service := time.Hour

	// Three slots at 90%: Wq(3) = 0.9^(√8−1) / (3 × 0.1) h ≈ 2.743 h; with a fourth at
	// ρ' = 0.675, Wq(4) = 0.675^(√10−1) / (4 × 0.325) h ≈ 0.330 h: about 2.41 h saved.
	saved, ok := WaitSaved(0.9, 3, service, 10*time.Hour, 40)
	if !ok || saved < 144*time.Minute || saved > 146*time.Minute {
		t.Errorf("WaitSaved(0.9, 3) = %v, %v; want about 2h25m", saved, ok)
	}

	prev := time.Duration(0)

	for _, busy := range []float64{0.5, 0.7, 0.8, 0.9, 0.95} {
		got, ok := WaitSaved(busy, 3, service, 100*time.Hour, 40)
		if !ok || got < prev {
			t.Errorf("WaitSaved(%v) = %v, %v; want ≥ %v (monotone in busy)", busy, got, ok, prev)
		}

		prev = got
	}

	if capped, ok := WaitSaved(0.9, 3, service, 30*time.Minute, 40); !ok || capped != 30*time.Minute {
		t.Errorf("WaitSaved over a 30 min observed wait = %v, %v; want capped at 30m", capped, ok)
	}

	refused := []struct {
		name     string
		busy     float64
		slots    int
		observed time.Duration
		leases   int
	}{
		{name: "saturated", busy: 0.99, slots: 3, observed: time.Hour, leases: 40},
		{name: "few leases", busy: 0.9, slots: 3, observed: time.Hour, leases: 19},
		{name: "no waiting", busy: 0.9, slots: 3, observed: 0, leases: 40},
		{name: "no slots", busy: 0.9, slots: 0, observed: time.Hour, leases: 40},
	}
	for _, r := range refused {
		if got, ok := WaitSaved(r.busy, r.slots, service, r.observed, r.leases); ok {
			t.Errorf("WaitSaved %s = %v, true; want no estimate", r.name, got)
		}
	}
}
