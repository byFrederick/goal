package domain

import (
	"cmp"
	"slices"
	"time"
)

// A teardown is the after-action breakdown of one item (ADR 0019 decision 3): where its lead
// time, cost and human help went. It reads only what Goal records (spans, runs, item events and
// touches), so it is computed when asked and stored nowhere; on an open item it is the breakdown
// so far.
//
// Its rework loops follow these rules, so the API and the CLI report the same numbers:
//
//   - A send-back is one loop per new rework round: a span whose round is higher than the span
//     before it. Its cause is the gate_rejected, rejected or moved event that opened the round
//     (in that order when several share the moment, so a rejection and the move it makes count
//     once), its station the one that sent the item back (the span's ReworkBy) and To the one it
//     went to. It lasts from the event until the item is back at the sending station, or until
//     the teardown's end, and costs the runs started in that time.
//   - A check_failed or run_retried event is a loop at its station: the work is done again by the
//     next run there. It lasts from the event until that run ends (or the teardown's end while it
//     runs) and costs that run; with no later run there it lasts and costs nothing.
//   - Rework's totals count the time inside any loop once and each run once, so a check failure
//     inside a send-back's loop adds no time or cost twice.

// TeardownTarget is what an item is set against: its factory's lead-time target for its class
// and its per-item budget; zero is none (or its factory is not loaded). Stations are the ids of
// its line's stations in the line's order, which orders the teardown's stations; none when its
// factory is not loaded.
type TeardownTarget struct {
	LeadTime  time.Duration
	BudgetUSD float64
	Stations  []string
}

// RunOutcome is how a run ended as a teardown names it: its status, or what its output met.
type RunOutcome string

// Run outcomes.
const (
	OutcomeRunning     RunOutcome = "running"
	OutcomeFinished    RunOutcome = "finished"
	OutcomeCheckFailed RunOutcome = "check_failed"
	OutcomeSentBack    RunOutcome = "sent_back"
	OutcomeMerged      RunOutcome = "merged"
	OutcomeFailed      RunOutcome = "failed"
	OutcomeCancelled   RunOutcome = "cancelled"
)

// TeardownTime is time spent working (in progress) and waiting (in any other state).
type TeardownTime struct {
	Working time.Duration
	Waiting time.Duration
}

// Total is the working and the waiting time.
func (t TeardownTime) Total() time.Duration { return t.Working + t.Waiting }

func (t *TeardownTime) add(o TeardownTime) {
	t.Working += o.Working
	t.Waiting += o.Waiting
}

// TeardownStation is the time an item spent at one station, over all its visits.
type TeardownStation struct {
	Station string
	TeardownTime
}

// TeardownRun is one agent run of the item.
type TeardownRun struct {
	ID      RunID
	Station string
	Agent   string
	Model   string
	Turns   int
	// Took is the run's time, until now while it runs.
	Took    time.Duration
	CostUSD float64
	Status  RunStatus
	Outcome RunOutcome
}

// TeardownRunTotal sums the item's runs.
type TeardownRunTotal struct {
	Runs    int
	Turns   int
	Took    time.Duration
	CostUSD float64
}

// ReworkLoop is one time the item did work again, and why (the rules above).
type ReworkLoop struct {
	// Cause is the kind of the event that caused it: gate_rejected, rejected, moved, check_failed
	// or run_retried.
	Cause EventKind
	// Station is where the loop started: the station that sent the item back, or the one whose
	// check failed or whose run was retried.
	Station string
	// To is the station a send-back went to; "" for a loop at its station.
	To string
	// Reason is the event's reason, or a failed check's command; "" without one.
	Reason  string
	At      time.Time
	Time    time.Duration
	CostUSD float64
	// EventID is the causing event's, so the trace can show it.
	EventID int64
}

// ReworkTotal sums an item's rework loops.
type ReworkTotal struct {
	Loops   int
	Time    time.Duration
	CostUSD float64
}

// TeardownTouch is one human touch with how long the item waited for it: from the opening of
// the span it ended (or happened in) until the touch.
type TeardownTouch struct {
	Touch

	Wait time.Duration
}

// Teardown is the after-action breakdown of one item.
type Teardown struct {
	// Open is whether the item is still open (it has an open span): the breakdown so far.
	Open bool
	// LeadTime is from its first span opening until its last closed, or until now while open.
	LeadTime time.Duration
	Target   TeardownTarget
	// CostUSD is what its runs cost.
	CostUSD float64
	// Stations are the stations it spent time at, in the line's order (Target.Stations): a
	// send-back to an earlier station does not move it down. Stations not in the line come after,
	// in the order it first reached them, and with no line order all do.
	Stations []TeardownStation
	Total    TeardownTime
	Runs     []TeardownRun
	RunTotal TeardownRunTotal
	// Loops are its rework loops in time order.
	Loops   []ReworkLoop
	Rework  ReworkTotal
	Touches []TeardownTouch
	// TouchWait is the time it waited on people, over its touches.
	TouchWait time.Duration
}

// TeardownOf breaks one item down from its spans (in order), its runs (oldest first), its events
// (in append order) and its touches (in time order), set against target, at now.
func TeardownOf(spans []Span, runs []Run, events []Event, touches []Touch, target TeardownTarget, now time.Time) Teardown {
	td := Teardown{Target: target, Touches: make([]TeardownTouch, 0, len(touches))}
	end := now

	if len(spans) > 0 {
		td.Open = spans[len(spans)-1].LeftAt.IsZero()
		if !td.Open {
			end = spans[len(spans)-1].LeftAt
		}

		td.LeadTime = end.Sub(spans[0].EnteredAt)
	}

	td.Stations, td.Total = stationTimes(spans, target.Stations, end)
	td.Runs, td.RunTotal = teardownRuns(runs, events, now)
	td.CostUSD = td.RunTotal.CostUSD

	loops := append(sendBackLoops(spans, runs, events, end), redoLoops(runs, events, now)...)
	td.Loops, td.Rework = reworkTotal(loops)

	for _, t := range touches {
		var wait time.Duration
		if i := slices.IndexFunc(spans, func(sp Span) bool {
			return !t.At.Before(sp.EnteredAt) && (sp.LeftAt.IsZero() || !t.At.After(sp.LeftAt))
		}); i >= 0 {
			wait = t.At.Sub(spans[i].EnteredAt)
		}

		td.Touches = append(td.Touches, TeardownTouch{Touch: t, Wait: wait})
		td.TouchWait += wait
	}

	return td
}

// stationTimes sums spans by station, an open span until end, in the order of line (station ids); a
// station not in it comes after them, in the order the item first reached it.
func stationTimes(spans []Span, line []string, end time.Time) ([]TeardownStation, TeardownTime) {
	var (
		rows  = []TeardownStation{}
		total TeardownTime
	)

	for _, sp := range spans {
		left := sp.LeftAt
		if left.IsZero() {
			left = end
		}

		var t TeardownTime
		if sp.State == StateInProgress {
			t.Working = left.Sub(sp.EnteredAt)
		} else {
			t.Waiting = left.Sub(sp.EnteredAt)
		}

		i := slices.IndexFunc(rows, func(r TeardownStation) bool { return r.Station == sp.Station })
		if i < 0 {
			rows, i = append(rows, TeardownStation{Station: sp.Station}), len(rows)
		}

		rows[i].add(t)
		total.add(t)
	}

	slices.SortStableFunc(rows, func(a, b TeardownStation) int {
		return cmp.Compare(linePlace(line, a.Station), linePlace(line, b.Station))
	})

	return rows, total
}

// linePlace is station's place in line, or after every station of it when it is not there.
func linePlace(line []string, station string) int {
	if i := slices.Index(line, station); i >= 0 {
		return i
	}

	return len(line)
}

// outcomes are the events that say how a run's output ended, by kind.
var outcomes = map[EventKind]RunOutcome{
	EventCheckFailed: OutcomeCheckFailed, EventGateRejected: OutcomeSentBack, EventMerged: OutcomeMerged,
}

// teardownRuns describes runs, each with its outcome, and sums them. A failed check names a
// run's outcome over anything that came after it.
func teardownRuns(runs []Run, events []Event, now time.Time) ([]TeardownRun, TeardownRunTotal) {
	met := map[RunID]RunOutcome{}

	for _, e := range events {
		o, ok := outcomes[e.Kind]
		if id := runOfEvent(runs, e); ok && id != "" && met[id] != OutcomeCheckFailed {
			met[id] = o
		}
	}

	out, total := make([]TeardownRun, 0, len(runs)), TeardownRunTotal{}

	for _, r := range runs {
		tr := TeardownRun{
			ID: r.ID, Station: r.Station, Agent: r.Agent, Model: r.Model, Turns: r.Result.Turns,
			Took: runTook(r, now), CostUSD: r.Result.CostUSD, Status: r.Result.Status, Outcome: runOutcome(r, met),
		}

		out = append(out, tr)
		total.Runs++
		total.Turns += tr.Turns
		total.Took += tr.Took
		total.CostUSD += tr.CostUSD
	}

	return out, total
}

// runOutcome is r's outcome: running while it runs, else what its output met, else its status.
func runOutcome(r Run, met map[RunID]RunOutcome) RunOutcome {
	o, ok := met[r.ID]

	switch {
	case r.Result.Status == RunRunning:
		return OutcomeRunning
	case ok:
		return o
	case r.Result.Status == RunFailed:
		return OutcomeFailed
	case r.Result.Status == RunCancelled:
		return OutcomeCancelled
	default:
		return OutcomeFinished
	}
}

// runTook is how long r ran, until now while it runs; 0 for a run that ended without an end.
func runTook(r Run, now time.Time) time.Duration {
	switch {
	case !r.EndedAt.IsZero():
		return r.EndedAt.Sub(r.StartedAt)
	case r.Result.Status == RunRunning:
		return now.Sub(r.StartedAt)
	default:
		return 0
	}
}

// runOfEvent is the run e belongs to: its own, else the last run at its station started by then.
func runOfEvent(runs []Run, e Event) RunID {
	if e.RunID != "" {
		return e.RunID
	}

	var id RunID

	for _, r := range runs {
		if r.Station == e.Station && !r.StartedAt.After(e.At) {
			id = r.ID
		}
	}

	return id
}

// loop is a rework loop being measured: until it ended and the runs it cost.
type loop struct {
	ReworkLoop

	until time.Time
	runs  []Run
}

// sendBacks are the kinds that open a rework round, the most specific first.
var sendBacks = []EventKind{EventGateRejected, EventRejected, EventMoved}

// sendBackLoops is a loop for each new rework round among spans (the rules at the top of this
// file), each until the item is back at the station that sent it, or until end.
func sendBackLoops(spans []Span, runs []Run, events []Event, end time.Time) []loop {
	var loops []loop

	for i := 1; i < len(spans); i++ {
		sp := spans[i]
		if sp.ReworkRound <= spans[i-1].ReworkRound {
			continue
		}

		cause, ok := roundCause(events, spans[i-1].EnteredAt, sp.EnteredAt)
		if !ok {
			continue
		}

		from := cmp.Or(sp.ReworkBy, cause.Station)
		l := loop{ReworkLoop: loopOf(cause, from), until: end}
		l.To = sp.Station

		if j := slices.IndexFunc(spans[i+1:], func(s Span) bool { return s.Station == from }); j >= 0 {
			l.until = spans[i+1+j].EnteredAt
		}

		for _, r := range runs {
			if !r.StartedAt.Before(cause.At) && r.StartedAt.Before(l.until) {
				l.runs = append(l.runs, r)
			}
		}

		loops = append(loops, l)
	}

	return loops
}

// redoLoops is a loop for each check_failed and run_retried event: until the next run at its
// station ends (now while it runs), and none without one.
func redoLoops(runs []Run, events []Event, now time.Time) []loop {
	var loops []loop

	for _, e := range events {
		if e.Kind != EventCheckFailed && e.Kind != EventRunRetried {
			continue
		}

		l := loop{ReworkLoop: loopOf(e, e.Station), until: e.At}
		own := runOfEvent(runs, e)

		if i := slices.IndexFunc(runs, func(r Run) bool {
			return r.Station == e.Station && r.ID != own && !r.StartedAt.Before(e.At)
		}); i >= 0 {
			l.runs, l.until = runs[i:i+1], cmp.Or(runs[i].EndedAt, now)
		}

		loops = append(loops, l)
	}

	return loops
}

// reworkTotal measures loops in time order and sums them, the time inside any loop and each run
// counted once.
func reworkTotal(loops []loop) ([]ReworkLoop, ReworkTotal) {
	slices.SortFunc(loops, func(a, b loop) int { return cmp.Or(a.At.Compare(b.At), cmp.Compare(a.EventID, b.EventID)) })

	out, total := make([]ReworkLoop, 0, len(loops)), ReworkTotal{Loops: len(loops)}
	counted := map[RunID]bool{}

	var covered time.Time // the end of the time already counted; loops are in start order

	for _, l := range loops {
		l.Time = l.until.Sub(l.At)

		for _, r := range l.runs {
			l.CostUSD += r.Result.CostUSD

			if !counted[r.ID] {
				counted[r.ID] = true
				total.CostUSD += r.Result.CostUSD
			}
		}

		if from := later(l.At, covered); l.until.After(from) {
			total.Time += l.until.Sub(from)
			covered = l.until
		}

		out = append(out, l.ReworkLoop)
	}

	return out, total
}

// roundCause is the event that opened a rework round entered at at, after the previous span
// opened (since): the latest send-back by then, the most specific of those at one moment.
func roundCause(events []Event, since, at time.Time) (Event, bool) {
	var (
		cause Event
		found bool
	)

	for _, e := range events {
		rank := slices.Index(sendBacks, e.Kind)
		if rank < 0 || e.At.Before(since) || e.At.After(at) {
			continue
		}

		if !found || e.At.After(cause.At) || (e.At.Equal(cause.At) && rank < slices.Index(sendBacks, cause.Kind)) {
			cause, found = e, true
		}
	}

	return cause, found
}

// keyCommand is the data key of a command check's command line (check_passed, check_failed).
const keyCommand = "command"

// loopOf starts a loop caused by e at station.
func loopOf(e Event, station string) ReworkLoop {
	reason, _ := e.Data["reason"].(string)
	if e.Kind == EventCheckFailed {
		reason, _ = e.Data[keyCommand].(string)
	}

	return ReworkLoop{Cause: e.Kind, Station: station, Reason: reason, At: e.At, EventID: e.ID}
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}

	return a
}
