package domain

import "testing"

// The four edge kinds are valid, turn among them; anything else is not.
func TestEdgeKind_Valid(t *testing.T) {
	t.Parallel()

	for _, k := range []EdgeKind{EdgePlanned, EdgeLinked, EdgeOverlap, EdgeTurn} {
		if !k.Valid() {
			t.Errorf("%q is not valid, want valid", k)
		}
	}

	for _, k := range []EdgeKind{"", "Turn", "blocks"} {
		if k.Valid() {
			t.Errorf("%q is valid, want not", k)
		}
	}
}
