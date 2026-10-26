package storetest

import (
	"reflect"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// A transition with Paths replaces the item's paths, an empty non-nil Paths clears them and a
// nil one keeps them; the item and the store's reads agree.
func testPathsSetByTransition(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{})
	if it.Paths != nil {
		t.Fatalf("new item paths = %q; want none", it.Paths)
	}

	_, l := f.claim(t, work, 0, t1)
	globs := []string{"queries/*.sql", "internal/adapters/postgres/**"}

	tr := next(it, l, t1.Add(time.Minute))
	tr.Paths = globs

	moved, err := f.Store.Transition(f.ctx(t), tr)
	if err != nil {
		t.Fatal(err)
	}

	samePaths(t, "after Paths set", moved, f.item(t, it.ID), globs)

	at := domain.Position{Station: stNext, State: domain.StateQueued}
	step := func(paths []string, minute time.Duration) domain.Item {
		t.Helper()

		got, err := f.Store.Transition(f.ctx(t), ports.Transition{
			ItemID: it.ID, From: at, To: at, Paths: paths, At: t1.Add(minute * time.Minute),
		})
		if err != nil {
			t.Fatal(err)
		}

		return got
	}

	samePaths(t, "after nil Paths", step(nil, 2), f.item(t, it.ID), globs)
	samePaths(t, "after empty Paths", step([]string{}, 3), f.item(t, it.ID), nil)
}

// samePaths checks that the transition's result and the item read back both have want.
func samePaths(t *testing.T, when string, moved, read domain.Item, want []string) {
	t.Helper()

	if !reflect.DeepEqual(moved.Paths, want) || !reflect.DeepEqual(read.Paths, want) {
		t.Errorf("%s: Transition paths %q, Item paths %q; want %q", when, moved.Paths, read.Paths, want)
	}
}
