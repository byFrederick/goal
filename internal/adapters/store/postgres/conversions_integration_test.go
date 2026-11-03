//go:build integration

package postgres

import (
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports/porttest/conversiontest"
)

// conversionsHarness is the factory conversions on a fresh database, with its organizations and
// factories made there.
func conversionsHarness(t *testing.T) conversiontest.Harness {
	t.Helper()

	d := newConnectionsDB(t)
	h := conversiontest.Harness{Conversions: NewFactoryConversions(d.store)}
	h.NewOrg = func(t *testing.T) domain.OrgID { t.Helper(); return newOrgID(t, d.store) }
	h.NewFactory = func(t *testing.T, org domain.OrgID) domain.FactoryID {
		t.Helper()

		return saveFactory(domain.WithOrg(t.Context(), org), t, d.store)
	}

	return h
}

// The PostgreSQL adapter runs the factory conversions suite: one row per factory, kept, the
// foreign key to the organization's factories, row-level security and the advisory lock.
func TestFactoryConversions_Conformance(t *testing.T) {
	t.Parallel()

	conversiontest.Run(t, conversionsHarness)
}
