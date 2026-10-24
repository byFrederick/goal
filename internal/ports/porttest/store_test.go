package porttest_test

import (
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports/porttest"
	"github.com/byFrederick/goal/internal/ports/porttest/storetest"
)

// The fake must pass the same suite as the PostgreSQL adapter, or engine tests prove nothing.
func TestStore_Conformance(t *testing.T) {
	t.Parallel()

	storetest.Run(t, func(*testing.T) storetest.Harness {
		s := porttest.NewStore()

		var orgs, factories atomic.Int64

		return storetest.Harness{
			Store: s,
			NewOrg: func(*testing.T) domain.OrgID {
				return domain.OrgID(fmt.Sprintf("00000000-0000-4000-9000-%012d", orgs.Add(1)))
			},
			NewFactory: func(_ *testing.T, org domain.OrgID) domain.FactoryID {
				id := domain.FactoryID(fmt.Sprintf("00000000-0000-4000-8000-%012d", factories.Add(1)))
				s.AddFactory(org, id)

				return id
			},
			Runs: func(_ *testing.T, org domain.OrgID, id domain.ItemID) []domain.Run { return s.Runs(org, id) },
			SetSlotCaps: func(_ *testing.T, org domain.OrgID, maxSlots int, factories map[domain.FactoryID]int) {
				s.SetSlotCaps(org, maxSlots, factories)
			},
			RecordAndon: func(_ *testing.T, org domain.OrgID, f domain.FactoryID, p domain.AndonPull, cleared bool) {
				s.RecordAndon(org, f, p, cleared)
			},
		}
	})
}
