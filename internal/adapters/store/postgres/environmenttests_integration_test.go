//go:build integration

package postgres

import (
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports/porttest/environmenttesttest"
)

// The PostgreSQL adapter runs the environment tests suite: the upsert by factory, environment
// and server, the foreign key to the organization's factories and row-level security, each on a
// fresh database.
func TestEnvironmentTests_Conformance(t *testing.T) {
	t.Parallel()

	environmenttesttest.Run(t, func(t *testing.T) environmenttesttest.Harness {
		t.Helper()

		d := newConnectionsDB(t)

		return environmenttesttest.Harness{
			Tests:  NewEnvironmentTests(d.store),
			NewOrg: func(t *testing.T) domain.OrgID { t.Helper(); return newOrgID(t, d.store) },
			NewFactory: func(t *testing.T, org domain.OrgID) domain.FactoryID {
				t.Helper()

				return saveFactory(domain.WithOrg(t.Context(), org), t, d.store)
			},
		}
	})
}
