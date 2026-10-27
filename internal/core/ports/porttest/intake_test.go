package porttest_test

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/core/ports/porttest/intaketest"
)

// The fake must keep the database's intake rules, or the engine's trigger tests built on it
// prove nothing.
func TestIntake_Conformance(t *testing.T) {
	t.Parallel()

	intaketest.Run(t, func(*testing.T) intaketest.Harness {
		s := porttest.NewStore()
		tn := porttest.NewTenancy(time.Now).WithStore(s)
		keys := porttest.NewMasterKeys()
		in := porttest.NewIntake(keys.Key(), s)

		var orgs, factories atomic.Int64

		return intaketest.Harness{
			Intake: func(key ports.MasterKey) ports.Intake { return in.WithKey(key) },
			Key:    keys.Key,
			Store:  s,
			NewOrg: func(t *testing.T) domain.OrgID {
				t.Helper()

				n := orgs.Add(1)

				o, err := tn.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: fmt.Sprintf("acme-%d", n), Name: fmt.Sprintf("Acme %d", n)})
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
		}
	})
}
