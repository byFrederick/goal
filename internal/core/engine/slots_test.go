package engine_test

import (
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

const thirdFactory domain.FactoryID = "5c0d7a52-8b1e-4f43-9d3a-2e6b7c8f9a02"

func TestTick_SkipsAFactoryAtItsSlotCap(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	h := newHarness(t, lineOf(agentStation(stWork, agWorker, 5, 5)), porttest.Block(release, `{}`))

	second := lineOf(agentStation(stWork, agWorker, 5, 5))
	second.FactoryID = thirdFactory
	e := h.engineWith(t, h.store, 0, h.line, second)

	h.submitTo(t, e, testOrg, factoryID, 3)
	h.submitTo(t, e, testOrg, thirdFactory, 3)
	h.store.SetSlotCaps(testOrg, 0, map[domain.FactoryID]int{factoryID: 1})

	for range 2 { // the cap holds across ticks while the runs last
		if err := e.Tick(t.Context()); err != nil {
			t.Fatal(err)
		}
	}

	if a, b := h.held(t, testOrg, factoryID), h.held(t, testOrg, thirdFactory); a != 1 || b != 3 {
		t.Errorf("claimed %d and %d, want 1 (the factory's cap) and 3 (every item of the other factory)", a, b)
	}

	close(release)
	h.wait(t, e)
}

func TestTick_AnOrgAtItsSlotCapStopsOnlyThatOrg(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	h, other, org := twoOrgs(t, porttest.Block(release, `{}`))

	second := lineOf(agentStation(stWork, agWorker, 5, 5))
	second.FactoryID = thirdFactory
	e := h.engineWith(t, h.store, 0, h.line, second, other)

	h.submitTo(t, e, testOrg, factoryID, 4)
	h.submitTo(t, e, testOrg, thirdFactory, 4)
	h.submitTo(t, e, org, otherFactory, 3)
	// Per-factory caps that add up past the organization's: the organization's binds.
	h.store.SetSlotCaps(testOrg, 3, map[domain.FactoryID]int{factoryID: 2, thirdFactory: 2})

	for range 2 {
		if err := e.Tick(t.Context()); err != nil {
			t.Fatal(err)
		}
	}

	a, b := h.held(t, testOrg, factoryID), h.held(t, testOrg, thirdFactory)
	if a+b != 3 || a > 2 || b > 2 {
		t.Errorf("the capped org claimed %d and %d, want 3 in all (its cap), at most 2 per factory", a, b)
	}

	if c := h.held(t, org, otherFactory); c != 3 {
		t.Errorf("the other org claimed %d, want every one of its 3 items", c)
	}

	close(release)
	h.wait(t, e)
}
