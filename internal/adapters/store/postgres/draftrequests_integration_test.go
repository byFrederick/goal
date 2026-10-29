//go:build integration

package postgres

import (
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports/porttest/draftrequesttest"
)

// The PostgreSQL adapter runs the draft requests suite: the one-open-request index, the
// cross-organization claim with FOR UPDATE SKIP LOCKED, the fenced leases, the requeue of ended
// leases and row-level security, each on a fresh database.
func TestDraftRequests_Conformance(t *testing.T) {
	t.Parallel()

	draftrequesttest.Run(t, func(t *testing.T) draftrequesttest.Harness {
		t.Helper()

		d := newConnectionsDB(t)

		return draftrequesttest.Harness{
			Requests: NewDraftRequests(d.store),
			NewOrg:   func(t *testing.T) domain.OrgID { t.Helper(); return newOrgID(t, d.store) },
			NewFactory: func(t *testing.T, org domain.OrgID) domain.FactoryID {
				t.Helper()

				return saveFactory(domain.WithOrg(t.Context(), org), t, d.store)
			},
			DeleteOrg: func(t *testing.T, org domain.OrgID) {
				t.Helper()

				if err := d.store.DeleteOrg(domain.WithOrg(t.Context(), org), domain.LocalUser, time.Now()); err != nil {
					t.Fatal(err)
				}
			},
		}
	})
}
