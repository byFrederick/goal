package domain

import "slices"

// WaitingKind says why a waiting item is not claimed: after the items it needs (planned or
// linked edges), or until items whose files overlap its own are done (overlap edges).
type WaitingKind string

// Waiting kinds.
const (
	WaitingAfter WaitingKind = "after"
	WaitingFiles WaitingKind = "files"
)

// Waiting is why a waiting item is held: the open needs that hold it, in edge order, and for
// kind files the item's own globs that overlap theirs, in the item's order.
type Waiting struct {
	Kind  WaitingKind
	Items []OpenNeed
	Files []string
}

// WaitingFor is why it is held by needs, its open needs oldest edge first. It reports false
// unless it is waiting (queued or ready) and has an open need. Planned and linked needs win over
// overlap ones: kind after lists them; otherwise kind files lists the overlap needs and Files
// the globs of it.Paths that PathsOverlap matches against any of their paths, each once. It is
// derived on every read and never stored.
func WaitingFor(it Item, needs []OpenNeed) (Waiting, bool) {
	if !it.State.Waiting() || len(needs) == 0 {
		return Waiting{}, false
	}

	var after, overlap []OpenNeed

	for _, n := range needs {
		if n.Kind == EdgeOverlap {
			overlap = append(overlap, n)
		} else {
			after = append(after, n)
		}
	}

	if len(after) > 0 {
		return Waiting{Kind: WaitingAfter, Items: after}, true
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
