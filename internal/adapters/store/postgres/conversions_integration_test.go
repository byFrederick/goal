//go:build integration

package postgres

import (
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports/porttest/conversiontest"
)

// The PostgreSQL adapter runs the factory conversions suite: one row per factory, kept, the
// foreign key to the organization's factories, row-level security and the advisory lock, each on
// a fresh database.
func TestFactoryConversions_Conformance(t *testing.T) {
	t.Parallel()

	conversiontest.Run(t, func(t *testing.T) conversiontest.Harness {
		t.Helper()

		d := newConnectionsDB(t)

		return conversiontest.Harness{
			Conversions: NewFactoryConversions(d.store),
			NewOrg:      func(t *testing.T) domain.OrgID { t.Helper(); return newOrgID(t, d.store) },
			NewFactory: func(t *testing.T, org domain.OrgID) domain.FactoryID {
				t.Helper()

				return saveFactory(domain.WithOrg(t.Context(), org), t, d.store)
			},
		}
	})
}
