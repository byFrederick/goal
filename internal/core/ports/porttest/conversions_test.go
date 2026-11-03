package porttest_test

import (
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/core/ports/porttest/conversiontest"
)

// conversionIDs hands out the organization and factory ids of the conversions suite.
type conversionIDs struct{ n atomic.Int64 }

func (c *conversionIDs) next() string {
	return fmt.Sprintf("0b6f1c9e-0000-7000-9000-%012d", c.n.Add(1))
}

// harness is a fresh fake whose factories are registered as NewFactory makes them.
func (c *conversionIDs) harness(*testing.T) conversiontest.Harness {
	fake := porttest.NewFactoryConversions()
	newFactory := func(_ *testing.T, org domain.OrgID) domain.FactoryID {
		f := domain.FactoryID(c.next())
		fake.AddFactory(org, f)

		return f
	}

	return conversiontest.Harness{Conversions: fake, NewOrg: func(*testing.T) domain.OrgID { return domain.OrgID(c.next()) }, NewFactory: newFactory}
}

// The fake runs the factory conversions suite.
func TestFactoryConversions_Conformance(t *testing.T) {
	t.Parallel()

	conversiontest.Run(t, (&conversionIDs{}).harness)
}
