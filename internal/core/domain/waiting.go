package domain

import "slices"

// WaitingKind says why a waiting item is not claimed: after the items it needs (planned or
// linked edges), until items whose files overlap its own are done (overlap edges), until the
// item holding its merge target's turn is done (turn edges, ADR 0043), or until CI on its
// commits finishes (an open CI wait, ADR 0044).
type WaitingKind string

// Waiting kinds.
const (
	WaitingAfter WaitingKind = "after"
	WaitingFiles WaitingKind = "files"
	WaitingTurn  WaitingKind = "turn"
	WaitingCI    WaitingKind = "ci"
)

// Waiting is why a waiting item is held: the open needs that hold it, in edge order, and for
// kind files the item's own globs that overlap theirs, in the item's order. For kind turn,
// Target is the integration branch the turn is for, empty for the default branch. For kind ci,
// CI is the item's open CI waits at its station, by start, and CIFailure the latest wait of the
// same visit that ended failed or timed_out, the one that sent the item back, or nil.
type Waiting struct {
	Kind      WaitingKind
	Items     []OpenNeed
	Files     []string
	Target    string
	CI        []CIWaitRecord
	CIFailure *CIWaitRecord
}

// WaitingFor is why it is held, by its open needs oldest edge first and by ci, its CI waits at
// its station by start (as ports.Store.CIWaits returns them). It reports false unless it is
// waiting (queued or ready) and has an open need or an open CI wait. Planned and linked needs
// win over overlap ones, overlap ones over turn ones, and turn ones over CI waits: kind after
// lists them; otherwise kind files lists the overlap needs and Files the globs of it.Paths that
// PathsOverlap matches against any of their paths, each once; otherwise kind turn lists the turn
// needs, with Target the branch its merge target's turn is for (the integration branch, "" for
// the default branch); otherwise kind ci lists the open waits. It is derived on every read and
// never stored.
func WaitingFor(it Item, needs []OpenNeed, target string, ci []CIWaitRecord) (Waiting, bool) {
	if !it.State.Waiting() {
		return Waiting{}, false
	}

	if len(needs) == 0 {
		return waitingForCI(ci)
	}

	var after, overlap, turn []OpenNeed

	for _, n := range needs {
		switch n.Kind {
		case EdgeOverlap:
			overlap = append(overlap, n)
		case EdgeTurn:
			turn = append(turn, n)
		case EdgePlanned, EdgeLinked:
			after = append(after, n)
		}
	}

	if len(after) > 0 {
		return Waiting{Kind: WaitingAfter, Items: after}, true
	}

	if len(overlap) == 0 {
		return Waiting{Kind: WaitingTurn, Items: turn, Target: target}, true
	}

	var theirs []string
	for _, n := range overlap {
		theirs = append(theirs, n.Paths...)
	}

	var files []string

	for _, p := range PathsOverlap(it.Paths, theirs) {
		if !slices.Contains(files, p.A) {
			files = append(files, p.A)
		}
	}

	return Waiting{Kind: WaitingFiles, Items: overlap, Files: files}, true
}

// waitingForCI is kind ci with the open waits of ci and the failure of their visit, or false
// when none is open.
func waitingForCI(ci []CIWaitRecord) (Waiting, bool) {
	var open []CIWaitRecord

	for _, w := range ci {
		if w.Open() {
			open = append(open, w)
		}
	}

	if len(open) == 0 {
		return Waiting{}, false
	}

	out := Waiting{Kind: WaitingCI, CI: open}
	visit := open[len(open)-1].Visit

	for _, w := range ci {
		if !w.Open() && w.Visit == visit && (w.Outcome == CIFailed || w.Outcome == CITimedOut) {
			out.CIFailure = &w
		}
	}

	return out, true
}
