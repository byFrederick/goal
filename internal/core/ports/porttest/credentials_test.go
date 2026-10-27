package porttest_test

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/core/ports/porttest/credentialtest"
)

// The fake must keep the database's credential rules, or tests built on it prove nothing.
func TestCredentials_Conformance(t *testing.T) {
	t.Parallel()

	credentialtest.Run(t, func(*testing.T) credentialtest.Harness {
		tn := porttest.NewTenancy(time.Now)
		keys := porttest.NewMasterKeys()
		conns := porttest.NewConnections(keys.Key(), tn)
		creds := porttest.NewCredentials(keys.Key(), tn, conns)

		var orgs atomic.Int64

		return credentialtest.Harness{
			Credentials: func(key ports.MasterKey) ports.Credentials { return creds.WithKey(key) },
			Connections: func(key ports.MasterKey) ports.Connections { return conns.WithKey(key) },
			Key:         keys.Key,
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
