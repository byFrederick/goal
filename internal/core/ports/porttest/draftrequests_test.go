package porttest_test

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/core/ports/porttest/draftrequesttest"
)

// The fake must keep the database's draft request rules, or tests built on it prove nothing.
func TestDraftRequests_Fake(t *testing.T) {
	t.Parallel()

	draftrequesttest.Run(t, func(*testing.T) draftrequesttest.Harness {
		tn := porttest.NewTenancy(time.Now)
		d := porttest.NewDraftRequests(tn, time.Now)

		var n atomic.Int64

		return draftrequesttest.Harness{
			Requests: d,
			NewOrg: func(t *testing.T) domain.OrgID {
				t.Helper()

				i := n.Add(1)

				o, err := tn.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: fmt.Sprintf("acme-%d", i), Name: fmt.Sprintf("Acme %d", i)})
				if err != nil {
					t.Fatal(err)
				}

				return o.ID
			},
			NewFactory: func(_ *testing.T, org domain.OrgID) domain.FactoryID {
				id := domain.FactoryID(fmt.Sprintf("01900000-0000-7000-a000-%012d", n.Add(1)))
				d.AddFactory(org, id)

				return id
			},
			DeleteOrg: func(t *testing.T, org domain.OrgID) {
				t.Helper()

				if err := tn.DeleteOrg(domain.WithOrg(t.Context(), org), domain.LocalUser, time.Now()); err != nil {
					t.Fatal(err)
				}
			},
		}
	})
}
