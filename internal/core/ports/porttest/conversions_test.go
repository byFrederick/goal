package porttest_test

import (
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/core/ports/porttest/conversiontest"
)

// The fake runs the factory conversions suite.
func TestFactoryConversions_Conformance(t *testing.T) {
	t.Parallel()

	var ids atomic.Int64

	next := func() string { return fmt.Sprintf("0b6f1c9e-0000-7000-8000-%012d", ids.Add(1)) }

	conversiontest.Run(t, func(t *testing.T) conversiontest.Harness {
		t.Helper()

		c := porttest.NewFactoryConversions()

		return conversiontest.Harness{
			Conversions: c,
			NewOrg:      func(*testing.T) domain.OrgID { return domain.OrgID(next()) },
			NewFactory: func(_ *testing.T, org domain.OrgID) domain.FactoryID {
				f := domain.FactoryID(next())
				c.AddFactory(org, f)

				return f
			},
		}
	})
}
