//go:build integration

package postgres

import (
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest/orgpolicytest"
)

func TestOrgPolicies_Conformance(t *testing.T) {
	t.Parallel()

	orgpolicytest.Run(t, func(t *testing.T) orgpolicytest.Harness {
		t.Helper()

		d := newConnectionsDB(t)

		return orgpolicytest.Harness{
			OrgPolicies: func(models []string) ports.OrgPolicies { return NewOrgPolicies(d.store, models) },
			Audit:       d.store,
			NewOrg:      func(t *testing.T) domain.OrgID { t.Helper(); return newOrgID(t, d.store) },
		}
	})
}
