package porttest_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/core/ports/porttest/connectiontest"
)

// The fake must keep the database's connection rules, or tests built on it prove nothing.
func TestConnections_Conformance(t *testing.T) {
	t.Parallel()

	connectiontest.Run(t, func(*testing.T) connectiontest.Harness {
		tn := porttest.NewTenancy(time.Now)
		keys := porttest.NewMasterKeys()
		conns := porttest.NewConnections(keys.Key(), tn)

		var orgs atomic.Int64

		return connectiontest.Harness{
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
			Rekey: func(_ context.Context, key ports.MasterKey) (int, error) { return conns.Rekey(key) },
		}
	})
}
