package porttest_test

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/core/ports/porttest/storetest"
)

// The fake must pass the same suite as the PostgreSQL adapter, or engine tests prove nothing.
func TestStore_Conformance(t *testing.T) {
	t.Parallel()

	storetest.Run(t, func(*testing.T) storetest.Harness {
		s := porttest.NewStore()
		tn := porttest.NewTenancy(time.Now).WithStore(s)

		var orgs, factories, versions atomic.Int64

		return storetest.Harness{
			Store: s,
			Flow:  s,
			Audit: tn,
			LeaseSpans: func(_ *testing.T, org domain.OrgID, id domain.ItemID) []domain.LeaseSpan {
				return s.LeaseSpans(org, id)
			},
			NewOrg: func(t *testing.T) domain.OrgID {
				t.Helper()

				o, err := tn.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: fmt.Sprintf("acme-%d", orgs.Add(1)), Name: "Acme"})
				if err != nil {
					t.Fatal(err)
				}

				return o.ID
			},
			NewFactory: func(_ *testing.T, org domain.OrgID) domain.FactoryID {
				id := domain.FactoryID(fmt.Sprintf("00000000-0000-4000-8000-%012d", factories.Add(1)))
				s.AddFactory(org, id)

				return id
			},
			SetSlotCaps: func(_ *testing.T, org domain.OrgID, maxSlots int, factories map[domain.FactoryID]int) {
				s.SetSlotCaps(org, maxSlots, factories)
			},
			NewVersion: func(*testing.T, domain.OrgID, domain.FactoryID) domain.DefinitionVersionID {
				return domain.DefinitionVersionID(fmt.Sprintf("00000000-0000-4000-a000-%012d", versions.Add(1)))
			},
		}
	})
}
