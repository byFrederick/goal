package domain

import "slices"

// WaitingKind says why a waiting item is not claimed: after the items it needs (planned or
// linked edges), until items whose files overlap its own are done (overlap edges), or until the
// item holding its merge target's turn is done (turn edges, ADR 0043).
type WaitingKind string

// Waiting kinds.
const (
	WaitingAfter WaitingKind = "after"
	WaitingFiles WaitingKind = "files"
	WaitingTurn  WaitingKind = "turn"
)

// Waiting is why a waiting item is held: the open needs that hold it, in edge order, and for
// kind files the item's own globs that overlap theirs, in the item's order. For kind turn,
// Target is the integration branch the turn is for, empty for the default branch.
type Waiting struct {
	Kind   WaitingKind
	Items  []OpenNeed
	Files  []string
	Target string
}

// WaitingFor is why it is held by needs, its open needs oldest edge first. It reports false
// unless it is waiting (queued or ready) and has an open need. Planned and linked needs win over
// overlap ones, and overlap ones over turn ones: kind after lists them; otherwise kind files
// lists the overlap needs and Files the globs of it.Paths that PathsOverlap matches against any
// of their paths, each once; otherwise kind turn lists the turn needs, with Target the branch
// its merge target's turn is for (the integration branch, "" for the default branch). It is
// derived on every read and never stored.
func WaitingFor(it Item, needs []OpenNeed, target string) (Waiting, bool) {
	if !it.State.Waiting() || len(needs) == 0 {
		return Waiting{}, false
	}

	var after, overlap, turn []OpenNeed

	for _, n := range needs {
		switch n.Kind {
		case EdgeOverlap:
			overlap = append(overlap, n)
		case EdgeTurn:
			turn = append(turn, n)
		default:
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
