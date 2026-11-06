package porttest_test

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/core/ports/porttest/standingruletest"
)

// The fake must keep the database's standing rule rules, or tests built on it prove nothing.
func TestStandingRules_Conformance(t *testing.T) {
	t.Parallel()

	standingruletest.Run(t, func(*testing.T) standingruletest.Harness {
		tn := porttest.NewTenancy(time.Now)

		var orgs atomic.Int64

		return standingruletest.Harness{
			StandingRules: porttest.NewStandingRules(tn),
			Audit:         tn,
			NewOrg: func(t *testing.T) domain.OrgID {
				t.Helper()

				n := orgs.Add(1)

				o, err := tn.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: fmt.Sprintf("acme-%d", n), Name: fmt.Sprintf("Acme %d", n)})
				if err != nil {
					t.Fatal(err)
				}

				return o.ID
			},
		}
	})
}
