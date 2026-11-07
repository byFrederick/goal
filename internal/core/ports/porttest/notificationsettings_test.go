package porttest_test

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/core/ports/porttest/notificationsettingtest"
)

// The fake must keep the database's notification setting rules, or tests built on it prove nothing.
func TestNotificationSettings_Conformance(t *testing.T) {
	t.Parallel()

	notificationsettingtest.Run(t, func(*testing.T) notificationsettingtest.Harness {
		tn := porttest.NewTenancy(time.Now)
		secrets := porttest.NewOrgSecrets(porttest.NewMasterKeys().Key("k1"), tn)

		var orgs atomic.Int64

		return notificationsettingtest.Harness{
			Settings: porttest.NewNotificationSettings(secrets, tn), Secrets: secrets, Audit: tn,
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
