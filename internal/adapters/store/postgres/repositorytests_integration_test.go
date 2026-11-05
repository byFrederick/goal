//go:build integration

package postgres

import (
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports/porttest/repositorytesttest"
)

// The PostgreSQL adapter runs the repository tests suite: the upsert by factory and url, the
// foreign key to the organization's factories with its cascade and row-level security, each on
// a fresh database.
func TestRepositoryTests_Conformance(t *testing.T) {
	t.Parallel()

	repositorytesttest.Run(t, func(t *testing.T) repositorytesttest.Harness {
		t.Helper()

		d := newConnectionsDB(t)

		return repositorytesttest.Harness{
			Tests:  NewRepositoryTests(d.store),
			NewOrg: func(t *testing.T) domain.OrgID { t.Helper(); return newOrgID(t, d.store) },
			NewFactory: func(t *testing.T, org domain.OrgID) domain.FactoryID {
				t.Helper()

				return saveFactory(domain.WithOrg(t.Context(), org), t, d.store)
			},
			DeleteFactory: func(t *testing.T, org domain.OrgID, factory domain.FactoryID) {
				t.Helper()

				err := inOrgTx(t, d.store.pool, org, func(tx pgx.Tx) error {
					if _, err := tx.Exec(t.Context(), "UPDATE factories SET archived_at = now(), archived_by = 'user:local' WHERE id = $1", string(factory)); err != nil {
						return err
					}

					_, err := tx.Exec(t.Context(), "SELECT goal_delete_factory($1)", string(factory))

					return err
				})
				if err != nil {
					t.Fatalf("delete factory %s: %v", factory, err)
				}
			},
		}
	})
}
