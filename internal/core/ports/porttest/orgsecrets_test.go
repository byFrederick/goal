package porttest_test

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/core/ports/porttest/secrettest"
)

// The fake must keep the database's secret rules, or tests built on it prove nothing.
func TestSecrets_Conformance(t *testing.T) {
	t.Parallel()

	secrettest.Run(t, func(*testing.T) secrettest.Harness {
		tn := porttest.NewTenancy(time.Now)
		keys := porttest.NewMasterKeys()
		secrets := porttest.NewOrgSecrets(keys.Key(), tn)
		store := porttest.NewStore()
		intake := porttest.NewIntake(keys.Key(), store)
		intake.UseSecrets(secrets)

		var orgs atomic.Int64

		return secrettest.Harness{
			Secrets:     func(key ports.MasterKey) ports.OrgSecrets { return secrets.WithKey(key) },
			Key:         keys.Key,
			CopySealed: func(t *testing.T, fromOrg domain.OrgID, from string, toOrg domain.OrgID, to string) {
				t.Helper()

				if err := secrets.CopySealed(fromOrg, from, toOrg, to); err != nil {
					t.Fatal(err)
				}
			},
			Audit: tn,
			SignWebhook: func(t *testing.T, org domain.OrgID, name string) {
				t.Helper()

				if _, err := intake.CreateWebhook(domain.WithOrg(t.Context(), org), domain.NewWebhook{Name: "github-acme", Sender: domain.SenderGitHub, Secret: name}); err != nil {
					t.Fatal(err)
				}
			},
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
