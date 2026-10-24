package porttest_test

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports/porttest"
	"github.com/byFrederick/goal/internal/ports/porttest/tenancytest"
)

// The fake must keep the database's organization rules, or tests built on it prove nothing.
func TestTenancy_Conformance(t *testing.T) {
	t.Parallel()

	tenancytest.Run(t, func(*testing.T) tenancytest.Harness {
		store := porttest.NewStore()
		tn := porttest.NewTenancy(time.Now).WithStore(store)

		var factories atomic.Int64

		return tenancytest.Harness{
			Tenancy: tn, Audit: tn,
			NewFactory: func(_ *testing.T, org domain.OrgID) domain.FactoryID {
				id := domain.FactoryID(fmt.Sprintf("00000000-0000-4000-8000-%012d", factories.Add(1)))
				store.AddFactory(org, id)

				return id
			},
		}
	})
}
