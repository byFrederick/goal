package domain

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
)

// MaxInFlight is how many entries an In flight section lists at most (ADR 0015).
const MaxInFlight = 30

// InFlightEntry is one open item or assembly of a factory as an In flight section lists it.
type InFlightEntry struct {
	// Ref is the entry's <factory>#<number>.
	Ref     string
	Title   string
	State   ItemState
	Station string
	// Paths are the globs the entry expects to change: an item's paths, or the union of an
	// assembly's open parts' part paths. Nil when none are known.
	Paths     []string
	UpdatedAt time.Time
}

// InFlightSection is the `## In flight` section of a run's feedback (ADR 0015 decision 1): at
// most MaxInFlight of entries, ranked by how many of an entry's globs have a literal prefix (up
// to the first wildcard) that request contains, highest first, then by most recently updated;
// entries without paths come last. It states what it lists and nothing else.
func InFlightSection(request string, entries []InFlightEntry) string {
	if len(entries) == 0 {
		return "## In flight\n\nNo other item or assembly of the factory is open."
	}

	type ranked struct {
		InFlightEntry

		score int
	}

	rs := make([]ranked, len(entries))
	for i, e := range entries {
		rs[i] = ranked{InFlightEntry: e, score: namedGlobs(request, e.Paths)}
	}

	slices.SortStableFunc(rs, func(a, b ranked) int {
		if c := cmp.Compare(boolOrder(len(a.Paths) == 0), boolOrder(len(b.Paths) == 0)); c != 0 {
			return c
		}

		if c := cmp.Compare(b.score, a.score); c != 0 {
			return c
		}

		return b.UpdatedAt.Compare(a.UpdatedAt)
	})

	shown := rs[:min(len(rs), MaxInFlight)]

	var b strings.Builder

	fmt.Fprintf(&b, "## In flight\n\nThe factory's other open items and assemblies, with the paths each expects to change, ranked by how many of their paths this item names (%d of %d):\n", len(shown), len(rs))

	for _, r := range shown {
		paths := "no paths known"
		if len(r.Paths) > 0 {
			paths = "paths " + strings.Join(r.Paths, ", ")
		}

		fmt.Fprintf(&b, "\n- %s %q: %s at %s, %s", r.Ref, r.Title, r.State, r.Station, paths)
	}

	return b.String()
}

// boolOrder is 1 for true and 0 for false.
func boolOrder(v bool) int {
	if v {
		return 1
	}

	return 0
}

// namedGlobs is how many of globs have a non-empty literal prefix, up to the first wildcard
// (*, ?, [ or {), that request contains.
func namedGlobs(request string, globs []string) int {
	n := 0

	for _, g := range globs {
		prefix := g
		if i := strings.IndexAny(g, "*?[{"); i >= 0 {
			prefix = g[:i]
		}

		if prefix = strings.TrimSpace(prefix); prefix != "" && strings.Contains(request, prefix) {
			n++
		}
	}

	return n
}
