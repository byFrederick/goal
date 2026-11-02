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
// and its per-item budget; zero is none (or its factory is not loaded).
type TeardownTarget struct {
	LeadTime  time.Duration
	BudgetUSD float64
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
	// Stations are the stations it spent time at, in the order it first reached them.
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
	td := Teardown{
		Target: target, Stations: []TeardownStation{}, Runs: make([]TeardownRun, 0, len(runs)),
		Loops: []ReworkLoop{}, Touches: make([]TeardownTouch, 0, len(touches)),
	}

	end := now
	if len(spans) > 0 {
		td.Open = spans[len(spans)-1].LeftAt.IsZero()
		if !td.Open {
			end = spans[len(spans)-1].LeftAt
		}

		td.LeadTime = end.Sub(spans[0].EnteredAt)
	}

	td.Stations, td.Total = stationTimes(spans, end)
	td.Runs, td.RunTotal = teardownRuns(runs, events, now)
	td.CostUSD = td.RunTotal.CostUSD
	td.Loops, td.Rework = reworkLoops(spans, runs, events, end, now)

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

// stationTimes sums spans by station, in the order the item first reached each, an open span
// until end.
func stationTimes(spans []Span, end time.Time) ([]TeardownStation, TeardownTime) {
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

	return rows, total
}

// teardownRuns describes runs, each with its outcome, and sums them.
func teardownRuns(runs []Run, events []Event, now time.Time) ([]TeardownRun, TeardownRunTotal) {
	met := map[RunID]RunOutcome{}

	for _, e := range events {
		var o RunOutcome

		switch e.Kind {
		case EventCheckFailed:
			o = OutcomeCheckFailed
		case EventGateRejected:
			o = OutcomeSentBack
		case EventMerged:
			o = OutcomeMerged
		default:
			continue
		}

		if id := runOfEvent(runs, e); id != "" && met[id] != OutcomeCheckFailed {
			met[id] = o
		}
	}

	out, total := make([]TeardownRun, 0, len(runs)), TeardownRunTotal{}

	for _, r := range runs {
		tr := TeardownRun{
			ID: r.ID, Station: r.Station, Agent: r.Agent, Model: r.Model, Turns: r.Result.Turns,
			Took: runTook(r, now), CostUSD: r.Result.CostUSD, Status: r.Result.Status,
		}

		switch o, ok := met[r.ID]; {
		case r.Result.Status == RunRunning:
			tr.Outcome = OutcomeRunning
		case ok:
			tr.Outcome = o
		case r.Result.Status == RunFailed:
			tr.Outcome = OutcomeFailed
		case r.Result.Status == RunCancelled:
			tr.Outcome = OutcomeCancelled
		default:
			tr.Outcome = OutcomeFinished
		}

		out = append(out, tr)
		total.Runs++
		total.Turns += tr.Turns
		total.Took += tr.Took
		total.CostUSD += tr.CostUSD
	}

	return out, total
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

// sendBacks are the kinds that open a rework round, the most specific first.
var sendBacks = []EventKind{EventGateRejected, EventRejected, EventMoved}

// reworkLoops finds the loops of rework (the rules at the top of this file) and their totals.
func reworkLoops(spans []Span, runs []Run, events []Event, end, now time.Time) ([]ReworkLoop, ReworkTotal) {
	type loop struct {
		ReworkLoop

		until time.Time
		runs  []Run
	}

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
		until := end

		if j := slices.IndexFunc(spans[i+1:], func(s Span) bool { return s.Station == from }); j >= 0 {
			until = spans[i+1+j].EnteredAt
		}

		l := loop{ReworkLoop: loopOf(cause, from), until: until}
		l.To = sp.Station

		for _, r := range runs {
			if !r.StartedAt.Before(cause.At) && r.StartedAt.Before(until) {
				l.runs = append(l.runs, r)
			}
		}

		loops = append(loops, l)
	}

	for _, e := range events {
		if e.Kind != EventCheckFailed && e.Kind != EventRunRetried {
			continue
		}

		l := loop{ReworkLoop: loopOf(e, e.Station), until: e.At}
		own := runOfEvent(runs, e)

		if i := slices.IndexFunc(runs, func(r Run) bool {
			return r.Station == e.Station && r.ID != own && !r.StartedAt.Before(e.At)
		}); i >= 0 {
			redo := runs[i]
			l.runs, l.until = []Run{redo}, redo.EndedAt

			if redo.EndedAt.IsZero() {
				l.until = now
			}
		}

		loops = append(loops, l)
	}

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

// loopOf starts a loop caused by e at station.
func loopOf(e Event, station string) ReworkLoop {
	reason, _ := e.Data["reason"].(string)
	if e.Kind == EventCheckFailed {
		reason, _ = e.Data["command"].(string)
	}

	return ReworkLoop{Cause: e.Kind, Station: station, Reason: reason, At: e.At, EventID: e.ID}
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}

	return a
}
