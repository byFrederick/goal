package porttest_test

import (
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/ports/porttest"
	"github.com/byFrederick/goal/internal/ports/porttest/tenancytest"
)

// The fake must keep the database's organization rules, or tests built on it prove nothing.
func TestTenancy_Conformance(t *testing.T) {
	t.Parallel()

	tenancytest.Run(t, func(*testing.T) tenancytest.Harness {
		tn := porttest.NewTenancy(time.Now)

		return tenancytest.Harness{Tenancy: tn, Audit: tn}
	})
}
