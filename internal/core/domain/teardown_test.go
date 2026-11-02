package domain

import (
	"math"
	"reflect"
	"testing"
	"time"
)

// TeardownOf breaks an item down from its spans, runs, events and touches: where its time went at
// each station, what each run cost and how it ended, each loop of rework with its time and cost,
// and how long each human touch waited.
func TestTeardownOf(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	at := func(minutes int) time.Time { return t0.Add(time.Duration(minutes) * time.Minute) }
	span := func(station string, state ItemState, round int, by string, from, to int) Span {
		sp := Span{Station: station, State: state, ReworkRound: round, ReworkBy: by, EnteredAt: at(from)}
		if to >= 0 {
			sp.LeftAt = at(to)
		}

		return sp
	}
	run := func(id RunID, from, to int, status RunStatus, usd float64, turns int) Run {
		r := Run{ID: id, Station: "build", Agent: "backend", Model: "claude-sonnet-5-5", StartedAt: at(from),
			Result: RunResult{Status: status, CostUSD: usd, Turns: turns}}
		if to >= 0 {
			r.EndedAt = at(to)
		}

		return r
	}
	trun := func(r Run, took time.Duration, outcome RunOutcome) TeardownRun {
		return TeardownRun{ID: r.ID, Station: r.Station, Agent: r.Agent, Model: r.Model, Turns: r.Result.Turns,
			Took: took, CostUSD: r.Result.CostUSD, Status: r.Result.Status, Outcome: outcome}
	}
	row := func(station string, working, waiting int) TeardownStation {
		return TeardownStation{Station: station, TeardownTime: TeardownTime{
			Working: time.Duration(working) * time.Minute, Waiting: time.Duration(waiting) * time.Minute,
		}}
	}
	minutes := func(n int) time.Duration { return time.Duration(n) * time.Minute }
	byPerson := func(reason string) map[string]any { return map[string]any{"actor": "user:u1", "reason": reason} }
	target := TeardownTarget{LeadTime: 6 * time.Hour, BudgetUSD: 4}

	// A check failure at build, then a person sends the item back from merge.
	r1, r2, r3 := run("r1", 2, 10, RunSucceeded, 1, 10), run("r2", 11, 20, RunSucceeded, 0.5, 5), run("r3", 32, 42, RunSucceeded, 0.4, 4)

	// Open: sent back from merge and not back there yet; the first run of the round lost its sandbox.
	o1, o2, o3 := run("o1", 0, 20, RunSucceeded, 1, 10), run("o2", 30, 35, RunFailed, 0.1, 1), run("o3", 36, -1, RunRunning, 0, 0)

	// Touches: a question answered at build, then the merge.
	answered := Touch{EventID: 30, Kind: EventQuestionAnswered, Station: "build", At: at(25), Actor: "user:u1", ActorName: "Maya"}
	merged := Touch{EventID: 40, Kind: EventMerged, Station: "merge", At: at(45), Actor: "user:u1", ActorName: "Maya", Planned: true}
	h1, h2 := run("h1", 0, 10, RunSucceeded, 0.3, 3), run("h2", 27, 35, RunSucceeded, 0.2, 2)

	for _, tt := range []struct {
		name    string
		spans   []Span
		runs    []Run
		events  []Event
		touches []Touch
		target  TeardownTarget
		now     time.Time
		want    Teardown
	}{
		{
			name:   "no agent has picked it up",
			spans:  []Span{span("inbox", StateQueued, 0, "", 0, -1)},
			target: target, now: at(30),
			want: Teardown{
				Open: true, LeadTime: minutes(30), Target: target,
				Stations: []TeardownStation{row("inbox", 0, 30)}, Total: TeardownTime{Waiting: minutes(30)},
				Runs: []TeardownRun{}, Loops: []ReworkLoop{}, Touches: []TeardownTouch{},
			},
		},
		{
			name: "a clean pass",
			spans: []Span{
				span("inbox", StateQueued, 0, "", 0, 5), span("build", StateQueued, 0, "", 5, 10),
				span("build", StateInProgress, 0, "", 10, 40), span("merge", StateWaitingHuman, 0, "", 40, 50),
			},
			runs:   []Run{run("c1", 10, 40, RunSucceeded, 1.5, 20)},
			target: target, now: at(90),
			want: Teardown{
				LeadTime: minutes(50), Target: target, CostUSD: 1.5,
				Stations: []TeardownStation{row("inbox", 0, 5), row("build", 30, 5), row("merge", 0, 10)},
				Total:    TeardownTime{Working: minutes(30), Waiting: minutes(20)},
				Runs:     []TeardownRun{trun(run("c1", 10, 40, RunSucceeded, 1.5, 20), minutes(30), OutcomeFinished)},
				RunTotal: TeardownRunTotal{Runs: 1, Turns: 20, Took: minutes(30), CostUSD: 1.5},
				Loops:    []ReworkLoop{}, Touches: []TeardownTouch{},
			},
		},
		{
			name: "a check failure, then a send-back",
			spans: []Span{
				span("build", StateQueued, 0, "", 0, 2), span("build", StateInProgress, 0, "", 2, 20),
				span("merge", StateWaitingHuman, 0, "", 20, 30), span("build", StateQueued, 1, "merge", 30, 32),
				span("build", StateInProgress, 1, "merge", 32, 42), span("merge", StateWaitingHuman, 1, "merge", 42, 50),
			},
			runs: []Run{r1, r2, r3},
			events: []Event{
				{ID: 10, RunID: "r1", At: at(10), Kind: EventCheckFailed, Station: "build", Data: map[string]any{"command": "task check", "exit_code": 1}},
				{ID: 20, At: at(30), Kind: EventRejected, Station: "merge", Actor: "Maya", Data: byPerson("needs a test")},
				{ID: 21, At: at(30), Kind: EventMoved, Station: "merge", Actor: "Maya", Data: map[string]any{"from": "merge", "to": "build"}},
			},
			target: target, now: at(90),
			want: Teardown{
				LeadTime: minutes(50), Target: target, CostUSD: 1.9,
				Stations: []TeardownStation{row("build", 28, 4), row("merge", 0, 18)},
				Total:    TeardownTime{Working: minutes(28), Waiting: minutes(22)},
				Runs: []TeardownRun{
					trun(r1, minutes(8), OutcomeCheckFailed), trun(r2, minutes(9), OutcomeFinished), trun(r3, minutes(10), OutcomeFinished),
				},
				RunTotal: TeardownRunTotal{Runs: 3, Turns: 19, Took: minutes(27), CostUSD: 1.9},
				Loops: []ReworkLoop{
					{Cause: EventCheckFailed, Station: "build", Reason: "task check", At: at(10), Time: minutes(10), CostUSD: 0.5, EventID: 10},
					{Cause: EventRejected, Station: "merge", To: "build", Reason: "needs a test", At: at(30), Time: minutes(12), CostUSD: 0.4, EventID: 20},
				},
				Rework:  ReworkTotal{Loops: 2, Time: minutes(22), CostUSD: 0.9},
				Touches: []TeardownTouch{},
			},
		},
		{
			name: "human touches",
			spans: []Span{
				span("build", StateInProgress, 0, "", 0, 10), span("build", StateWaitingHuman, 0, "", 10, 25),
				span("build", StateQueued, 0, "", 25, 27), span("build", StateInProgress, 0, "", 27, 35),
				span("merge", StateWaitingHuman, 0, "", 35, 45),
			},
			runs:    []Run{h1, h2},
			events:  []Event{{ID: 40, At: at(45), Kind: EventMerged, Station: "merge", Actor: "Maya", Data: map[string]any{"actor": "user:u1"}}},
			touches: []Touch{answered, merged},
			now:     at(90),
			want: Teardown{
				LeadTime: minutes(45), CostUSD: 0.5,
				Stations: []TeardownStation{row("build", 18, 17), row("merge", 0, 10)},
				Total:    TeardownTime{Working: minutes(18), Waiting: minutes(27)},
				Runs:     []TeardownRun{trun(h1, minutes(10), OutcomeFinished), trun(h2, minutes(8), OutcomeFinished)},
				RunTotal: TeardownRunTotal{Runs: 2, Turns: 5, Took: minutes(18), CostUSD: 0.5},
				Loops:    []ReworkLoop{},
				Touches:  []TeardownTouch{{Touch: answered, Wait: minutes(15)}, {Touch: merged, Wait: minutes(10)}},
				TouchWait: minutes(25),
			},
		},
		{
			name: "an open item",
			spans: []Span{
				span("build", StateInProgress, 0, "", 0, 20), span("merge", StateWaitingHuman, 0, "", 20, 25),
				span("build", StateQueued, 1, "merge", 25, 30), span("build", StateInProgress, 1, "merge", 30, -1),
			},
			runs: []Run{o1, o2, o3},
			events: []Event{
				{ID: 45, At: at(25), Kind: EventRejected, Station: "merge", Actor: "Maya", Data: byPerson("add docs")},
				{ID: 46, At: at(25), Kind: EventMoved, Station: "merge", Actor: "Maya", Data: map[string]any{"from": "merge", "to": "build"}},
				{ID: 50, At: at(35), Kind: EventRunRetried, Station: "build", Actor: ActorEngine, Data: map[string]any{"reason": "sandbox lost"}},
			},
			target: target, now: at(50),
			want: Teardown{
				Open: true, LeadTime: minutes(50), Target: target, CostUSD: 1.1,
				Stations: []TeardownStation{row("build", 40, 5), row("merge", 0, 5)},
				Total:    TeardownTime{Working: minutes(40), Waiting: minutes(10)},
				Runs: []TeardownRun{
					trun(o1, minutes(20), OutcomeFinished), trun(o2, minutes(5), OutcomeFailed), trun(o3, minutes(14), OutcomeRunning),
				},
				RunTotal: TeardownRunTotal{Runs: 3, Turns: 11, Took: minutes(39), CostUSD: 1.1},
				Loops: []ReworkLoop{
					{Cause: EventRejected, Station: "merge", To: "build", Reason: "add docs", At: at(25), Time: minutes(25), CostUSD: 0.1, EventID: 45},
					{Cause: EventRunRetried, Station: "build", Reason: "sandbox lost", At: at(35), Time: minutes(15), EventID: 50},
				},
				Rework:  ReworkTotal{Loops: 2, Time: minutes(25), CostUSD: 0.1},
				Touches: []TeardownTouch{},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := TeardownOf(tt.spans, tt.runs, tt.events, tt.touches, tt.target, tt.now)
			if roundCosts(&got); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("TeardownOf =\n%+v\nwant\n%+v", got, tt.want)
			}
		})
	}
}

// roundCosts rounds td's sums of dollars to the cent, so the expectations need not repeat the
// sums' floating-point error.
func roundCosts(td *Teardown) {
	cents := func(v *float64) { *v = math.Round(*v*100) / 100 }

	cents(&td.CostUSD)
	cents(&td.RunTotal.CostUSD)
	cents(&td.Rework.CostUSD)

	for i := range td.Loops {
		cents(&td.Loops[i].CostUSD)
	}
}
