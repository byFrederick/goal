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

		var factories atomic.Int64

		return storetest.Harness{
			Store: s,
			NewFactory: func(*testing.T) domain.FactoryID {
				return domain.FactoryID(fmt.Sprintf("00000000-0000-4000-8000-%012d", factories.Add(1)))
			},
			Runs: func(_ *testing.T, id domain.ItemID) []domain.Run { return s.Runs(id) },
		}
	})
}
