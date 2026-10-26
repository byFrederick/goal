package storetest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// claimIn claims the next item at the work station of factory into slot.
func (f *fixture) claimIn(ctx context.Context, factory domain.FactoryID, slot int) error {
	_, _, err := f.Store.ClaimNext(ctx, ports.Claim{FactoryID: factory, Station: wide, Slot: slot, Now: t1, ExpiresAt: t1.Add(ttl)})

	return err
}

// wide is the work station with enough agent slots for the cap tests.
var wide = domain.Station{ID: stWork, Kind: domain.KindAgent, Agent: agWorker, Agents: 16}

func testSlotCaps(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	other := f.newFactory(t)

	for i := range 4 {
		f.create(t, domain.NewItem{CreatedAt: t0.Add(time.Duration(i) * time.Second)})
		f.create(t, domain.NewItem{FactoryID: other, CreatedAt: t0.Add(time.Duration(i) * time.Second)})
	}

	// The organization may run 3 agents, the fixture's factory 1.
	f.SetSlotCaps(t, f.org, 3, map[domain.FactoryID]int{f.factory: 1})

	if err := f.claimIn(ctx, f.factory, 0); err != nil {
		t.Fatalf("first claim in the capped factory = %v; want a claim", err)
	}

	expect(t, "second claim in a factory capped at 1", f.claimIn(ctx, f.factory, 1), ports.ErrFactoryFull)

	if n := len(f.leases(t, stWork)); n != 1 {
		t.Errorf("after ErrFactoryFull the factory holds %d leases; want 1", n)
	}

	if w, err := f.Store.Waiting(ctx, f.factory, stWork, 10); err != nil || len(w) != 3 {
		t.Errorf("after ErrFactoryFull %d items wait, %v; want 3, unchanged", len(w), err)
	}

	if err := f.claimIn(ctx, other, 0); err != nil {
		t.Errorf("claim in an uncapped factory = %v; want a claim", err)
	}

	if err := f.claimIn(ctx, other, 1); err != nil {
		t.Errorf("third claim of an org capped at 3 = %v; want a claim", err)
	}

	expect(t, "fourth claim of an org capped at 3", f.claimIn(ctx, other, 2), ports.ErrOrgFull)

	if n := len(f.leasesOf(t, other)); n != 2 {
		t.Errorf("after ErrOrgFull the other factory holds %d leases; want 2", n)
	}

	// Per-factory caps may add up to more than the organization's: the org cap still binds.
	f.SetSlotCaps(t, f.org, 4, map[domain.FactoryID]int{f.factory: 3, other: 3})

	if err := f.claimIn(ctx, f.factory, 1); err != nil {
		t.Errorf("claim under raised caps = %v; want a claim", err)
	}

	expect(t, "claim past the org cap of 4", f.claimIn(ctx, f.factory, 2), ports.ErrOrgFull)

	// Another organization's caps and leases are its own.
	o := f.otherOrg(t)
	o.create(t, domain.NewItem{})

	if err := o.claimIn(o.ctx(t), o.factory, 0); err != nil {
		t.Errorf("claim in another org = %v; want a claim", err)
	}

	// Cleared caps bind nothing.
	f.SetSlotCaps(t, f.org, 0, map[domain.FactoryID]int{f.factory: 0, other: 0})

	if err := f.claimIn(ctx, f.factory, 2); err != nil {
		t.Errorf("claim with the caps cleared = %v; want a claim", err)
	}
}

// leasesOf returns the leases held at the work station of a factory of the fixture's org.
func (f *fixture) leasesOf(t *testing.T, factory domain.FactoryID) []domain.Lease {
	t.Helper()

	ls, err := f.Store.Leases(f.ctx(t), factory, stWork)
	if err != nil {
		t.Fatalf("Leases: %v", err)
	}

	return ls
}

// Concurrent claims of one organization, each on its own slot, never run more agents than the
// organization's cap or a factory's, however they interleave.
func testConcurrentClaimsCapped(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	other := f.newFactory(t)

	const orgCap, factoryCap, perFactory = 3, 1, 8

	for i := range perFactory {
		f.create(t, domain.NewItem{CreatedAt: t0.Add(time.Duration(i) * time.Second)})
		f.create(t, domain.NewItem{FactoryID: other, CreatedAt: t0.Add(time.Duration(i) * time.Second)})
	}

	f.SetSlotCaps(t, f.org, orgCap, map[domain.FactoryID]int{f.factory: factoryCap})

	var (
		wg              sync.WaitGroup
		claimed, capped atomic.Int64
		inFactory       atomic.Int64
	)

	for _, factory := range []domain.FactoryID{f.factory, other} {
		for slot := range perFactory {
			wg.Go(func() {
				err := f.claimIn(ctx, factory, slot)

				switch {
				case err == nil:
					claimed.Add(1)

					if factory == f.factory {
						inFactory.Add(1)
					}
				case errors.Is(err, ports.ErrOrgFull), errors.Is(err, ports.ErrFactoryFull):
					capped.Add(1)
				default:
					t.Error(fmt.Errorf("claim %s slot %d: %w", factory, slot, err))
				}
			})
		}
	}

	wg.Wait()

	if claimed.Load() != orgCap || capped.Load() != 2*perFactory-orgCap {
		t.Errorf("%d claims and %d refused by a cap; want exactly %d claims, the org cap", claimed.Load(), capped.Load(), orgCap)
	}

	if inFactory.Load() > factoryCap {
		t.Errorf("%d claims in a factory capped at %d", inFactory.Load(), factoryCap)
	}

	if n := len(f.leases(t, stWork)) + len(f.leasesOf(t, other)); n != orgCap {
		t.Errorf("the org holds %d leases; want %d", n, orgCap)
	}
}
