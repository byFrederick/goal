package porttest_test

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/core/ports/porttest/orgpolicytest"
)

// The fake must keep the database's policy rules, or tests built on it prove nothing.
func TestOrgPolicies_Conformance(t *testing.T) {
	t.Parallel()

	orgpolicytest.Run(t, func(*testing.T) orgpolicytest.Harness {
		tn := porttest.NewTenancy(time.Now)

		var orgs atomic.Int64

		return orgpolicytest.Harness{
			OrgPolicies: func(models []string) ports.OrgPolicies { return porttest.NewOrgPolicies(models, tn, time.Now) },
			Audit:       tn,
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
