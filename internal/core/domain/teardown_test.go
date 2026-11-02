package domain

import (
	"math"
	"reflect"
	"testing"
	"time"
)

// Names of the teardown tests' line, its person and its check.
const (
	tdMerge   = "merge"
	tdModel   = "claude-sonnet-5-5"
	tdPerson  = "user:u1"
	tdName    = "Maya"
	tdCommand = "task check"
)

// teardownCase is one item TeardownOf breaks down, and the teardown it should answer.
type teardownCase struct {
	name    string
	spans   []Span
	runs    []Run
	events  []Event
	touches []Touch
	target  TeardownTarget
	now     time.Time
	want    Teardown
}

var tdStart = time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)

// tdAt is n minutes after the start.
func tdAt(n int) time.Time { return tdStart.Add(tdMinutes(n)) }

func tdMinutes(n int) time.Duration { return time.Duration(n) * time.Minute }

// tdSpan is a span from minute from to minute to; to < 0 leaves it open.
func tdSpan(station string, state ItemState, round, from, to int) Span {
	sp := Span{Station: station, State: state, ReworkRound: round, EnteredAt: tdAt(from)}
	if round > 0 {
		sp.ReworkBy = tdMerge
	}

	if to >= 0 {
		sp.LeftAt = tdAt(to)
	}

	return sp
}

// tdRun is a run at build from minute from to minute to; to < 0 leaves it running.
func tdRun(id RunID, from, to int, status RunStatus, usd float64, turns int) Run {
	r := Run{
		ID: id, Station: stationBuild, Agent: testAgent, Model: tdModel, StartedAt: tdAt(from),
		Result: RunResult{Status: status, CostUSD: usd, Turns: turns},
	}
	if to >= 0 {
		r.EndedAt = tdAt(to)
	}

	return r
}

func tdRunOf(r Run, took int, outcome RunOutcome) TeardownRun {
	return TeardownRun{
		ID: r.ID, Station: r.Station, Agent: r.Agent, Model: r.Model, Turns: r.Result.Turns,
		Took: tdMinutes(took), CostUSD: r.Result.CostUSD, Status: r.Result.Status, Outcome: outcome,
	}
}

func tdRow(station string, working, waiting int) TeardownStation {
	return TeardownStation{Station: station, TeardownTime: TeardownTime{Working: tdMinutes(working), Waiting: tdMinutes(waiting)}}
}

// tdRejected is a person sending the item back from merge to build at minute at.
func tdRejected(id int64, at int, reason string) []Event {
	return []Event{
		{ID: id, At: tdAt(at), Kind: EventRejected, Station: tdMerge, Actor: tdName, Data: map[string]any{"actor": tdPerson, "reason": reason}},
		{ID: id + 1, At: tdAt(at), Kind: EventMoved, Station: tdMerge, Actor: tdName, Data: map[string]any{"from": tdMerge, "to": stationBuild}},
	}
}

var tdTarget = TeardownTarget{LeadTime: 6 * time.Hour, BudgetUSD: 4}

func teardownSimpleCases() []teardownCase {
	clean := tdRun("c1", 10, 40, RunSucceeded, 1.5, 20)

	return []teardownCase{
		{
			name:   "no agent has picked it up",
			spans:  []Span{tdSpan("inbox", StateQueued, 0, 0, -1)},
			target: tdTarget, now: tdAt(30),
			want: Teardown{
				Open: true, LeadTime: tdMinutes(30), Target: tdTarget,
				Stations: []TeardownStation{tdRow("inbox", 0, 30)}, Total: TeardownTime{Waiting: tdMinutes(30)},
				Runs: []TeardownRun{}, Loops: []ReworkLoop{}, Touches: []TeardownTouch{},
			},
		},
		{
			name: "a clean pass",
			spans: []Span{
				tdSpan("inbox", StateQueued, 0, 0, 5), tdSpan(stationBuild, StateQueued, 0, 5, 10),
				tdSpan(stationBuild, StateInProgress, 0, 10, 40), tdSpan(tdMerge, StateWaitingHuman, 0, 40, 50),
			},
			runs:   []Run{clean},
			target: tdTarget, now: tdAt(90),
			want: Teardown{
				LeadTime: tdMinutes(50), Target: tdTarget, CostUSD: 1.5,
				Stations: []TeardownStation{tdRow("inbox", 0, 5), tdRow(stationBuild, 30, 5), tdRow(tdMerge, 0, 10)},
				Total:    TeardownTime{Working: tdMinutes(30), Waiting: tdMinutes(20)},
				Runs:     []TeardownRun{tdRunOf(clean, 30, OutcomeFinished)},
				RunTotal: TeardownRunTotal{Runs: 1, Turns: 20, Took: tdMinutes(30), CostUSD: 1.5},
				Loops:    []ReworkLoop{}, Touches: []TeardownTouch{},
			},
		},
	}
}

// A check failure at build, then a person sends the item back from merge.
func teardownReworkCase() teardownCase {
	r1, r2, r3 := tdRun("r1", 2, 10, RunSucceeded, 1, 10), tdRun("r2", 11, 20, RunSucceeded, 0.5, 5), tdRun("r3", 32, 42, RunSucceeded, 0.4, 4)
	checkFailed := Event{
		ID: 10, RunID: "r1", At: tdAt(10), Kind: EventCheckFailed, Station: stationBuild,
		Data: map[string]any{keyCommand: tdCommand, "exit_code": 1},
	}

	return teardownCase{
		name: "a check failure, then a send-back",
		spans: []Span{
			tdSpan(stationBuild, StateQueued, 0, 0, 2), tdSpan(stationBuild, StateInProgress, 0, 2, 20),
			tdSpan(tdMerge, StateWaitingHuman, 0, 20, 30), tdSpan(stationBuild, StateQueued, 1, 30, 32),
			tdSpan(stationBuild, StateInProgress, 1, 32, 42), tdSpan(tdMerge, StateWaitingHuman, 1, 42, 50),
		},
		runs:   []Run{r1, r2, r3},
		events: append([]Event{checkFailed}, tdRejected(20, 30, "needs a test")...),
		target: tdTarget, now: tdAt(90),
		want: Teardown{
			LeadTime: tdMinutes(50), Target: tdTarget, CostUSD: 1.9,
			Stations: []TeardownStation{tdRow(stationBuild, 28, 4), tdRow(tdMerge, 0, 18)},
			Total:    TeardownTime{Working: tdMinutes(28), Waiting: tdMinutes(22)},
			Runs:     []TeardownRun{tdRunOf(r1, 8, OutcomeCheckFailed), tdRunOf(r2, 9, OutcomeFinished), tdRunOf(r3, 10, OutcomeFinished)},
			RunTotal: TeardownRunTotal{Runs: 3, Turns: 19, Took: tdMinutes(27), CostUSD: 1.9},
			Loops: []ReworkLoop{
				{Cause: EventCheckFailed, Station: stationBuild, Reason: tdCommand, At: tdAt(10), Time: tdMinutes(10), CostUSD: 0.5, EventID: 10},
				{Cause: EventRejected, Station: tdMerge, To: stationBuild, Reason: "needs a test", At: tdAt(30), Time: tdMinutes(12), CostUSD: 0.4, EventID: 20},
			},
			Rework:  ReworkTotal{Loops: 2, Time: tdMinutes(22), CostUSD: 0.9},
			Touches: []TeardownTouch{},
		},
	}
}

// A question answered at build, then the merge: each touch waited from its span's opening.
func teardownTouchesCase() teardownCase {
	answered := Touch{EventID: 30, Kind: EventQuestionAnswered, Station: stationBuild, At: tdAt(25), Actor: tdPerson, ActorName: tdName}
	merged := Touch{EventID: 40, Kind: EventMerged, Station: tdMerge, At: tdAt(45), Actor: tdPerson, ActorName: tdName, Planned: true}
	h1, h2 := tdRun("h1", 0, 10, RunSucceeded, 0.3, 3), tdRun("h2", 27, 35, RunSucceeded, 0.2, 2)

	return teardownCase{
		name: "human touches",
		spans: []Span{
			tdSpan(stationBuild, StateInProgress, 0, 0, 10), tdSpan(stationBuild, StateWaitingHuman, 0, 10, 25),
			tdSpan(stationBuild, StateQueued, 0, 25, 27), tdSpan(stationBuild, StateInProgress, 0, 27, 35),
			tdSpan(tdMerge, StateWaitingHuman, 0, 35, 45),
		},
		runs:    []Run{h1, h2},
		events:  []Event{{ID: 40, At: tdAt(45), Kind: EventMerged, Station: tdMerge, Actor: tdName, Data: map[string]any{"actor": tdPerson}}},
		touches: []Touch{answered, merged},
		now:     tdAt(90),
		want: Teardown{
			LeadTime: tdMinutes(45), CostUSD: 0.5,
			Stations:  []TeardownStation{tdRow(stationBuild, 18, 17), tdRow(tdMerge, 0, 10)},
			Total:     TeardownTime{Working: tdMinutes(18), Waiting: tdMinutes(27)},
			Runs:      []TeardownRun{tdRunOf(h1, 10, OutcomeFinished), tdRunOf(h2, 8, OutcomeFinished)},
			RunTotal:  TeardownRunTotal{Runs: 2, Turns: 5, Took: tdMinutes(18), CostUSD: 0.5},
			Loops:     []ReworkLoop{},
			Touches:   []TeardownTouch{{Touch: answered, Wait: tdMinutes(15)}, {Touch: merged, Wait: tdMinutes(10)}},
			TouchWait: tdMinutes(25),
		},
	}
}

// Open: sent back from merge and not back there yet; the round's first run lost its sandbox, and
// the run that does it again is still running.
func teardownOpenCase() teardownCase {
	o1, o2, o3 := tdRun("o1", 0, 20, RunSucceeded, 1, 10), tdRun("o2", 30, 35, RunFailed, 0.1, 1), tdRun("o3", 36, -1, RunRunning, 0, 0)
	retried := Event{ID: 50, At: tdAt(35), Kind: EventRunRetried, Station: stationBuild, Actor: ActorEngine, Data: map[string]any{"reason": "sandbox lost"}}

	return teardownCase{
		name: "an open item",
		spans: []Span{
			tdSpan(stationBuild, StateInProgress, 0, 0, 20), tdSpan(tdMerge, StateWaitingHuman, 0, 20, 25),
			tdSpan(stationBuild, StateQueued, 1, 25, 30), tdSpan(stationBuild, StateInProgress, 1, 30, -1),
		},
		runs:   []Run{o1, o2, o3},
		events: append(tdRejected(45, 25, "add docs"), retried),
		target: tdTarget, now: tdAt(50),
		want: Teardown{
			Open: true, LeadTime: tdMinutes(50), Target: tdTarget, CostUSD: 1.1,
			Stations: []TeardownStation{tdRow(stationBuild, 40, 5), tdRow(tdMerge, 0, 5)},
			Total:    TeardownTime{Working: tdMinutes(40), Waiting: tdMinutes(10)},
			Runs:     []TeardownRun{tdRunOf(o1, 20, OutcomeFinished), tdRunOf(o2, 5, OutcomeFailed), tdRunOf(o3, 14, OutcomeRunning)},
			RunTotal: TeardownRunTotal{Runs: 3, Turns: 11, Took: tdMinutes(39), CostUSD: 1.1},
			Loops: []ReworkLoop{
				{Cause: EventRejected, Station: tdMerge, To: stationBuild, Reason: "add docs", At: tdAt(25), Time: tdMinutes(25), CostUSD: 0.1, EventID: 45},
				{Cause: EventRunRetried, Station: stationBuild, Reason: "sandbox lost", At: tdAt(35), Time: tdMinutes(15), EventID: 50},
			},
			Rework:  ReworkTotal{Loops: 2, Time: tdMinutes(25), CostUSD: 0.1},
			Touches: []TeardownTouch{},
		},
	}
}

// TeardownOf breaks an item down from its spans, runs, events and touches: where its time went at
// each station, what each run cost and how it ended, each loop of rework with its time and cost,
// and how long each human touch waited.
func TestTeardownOf(t *testing.T) {
	t.Parallel()

	cases := append(teardownSimpleCases(), teardownReworkCase(), teardownTouchesCase(), teardownOpenCase())

	for _, tt := range cases {
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
