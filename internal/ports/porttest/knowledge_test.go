package porttest_test

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
	"github.com/byFrederick/goal/internal/ports/porttest/knowledgetest"
)

// The fake must keep the database's knowledge rules, or tests built on it prove nothing.
func TestKnowledge_Conformance(t *testing.T) {
	t.Parallel()

	knowledgetest.Run(t, func(*testing.T) knowledgetest.Harness {
		tn := porttest.NewTenancy(time.Now)
		conns := porttest.NewConnections(porttest.NewMasterKeys().Key("k1"), tn)

		var orgs atomic.Int64

		return knowledgetest.Harness{
			Knowledge: porttest.NewKnowledge(conns, tn), Connections: conns, Audit: tn,
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
