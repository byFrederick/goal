package porttest_test

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/core/ports/porttest/knowledgetest"
)

// The fake must keep the database's knowledge rules, or tests built on it prove nothing.
func TestKnowledge_Conformance(t *testing.T) {
	t.Parallel()

	knowledgetest.Run(t, func(*testing.T) knowledgetest.Harness {
		tn := porttest.NewTenancy(time.Now)
		conns := porttest.NewConnections(porttest.NewMasterKeys().Key("k1"), tn)
		secrets := porttest.NewOrgSecrets(porttest.NewMasterKeys().Key("k1"), tn, conns)
		kb := porttest.NewKnowledge(conns, tn)
		kb.ReadSecrets(secrets)

		var orgs atomic.Int64

		return knowledgetest.Harness{
			Knowledge: kb, Connections: conns, Secrets: secrets, Audit: tn,
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
