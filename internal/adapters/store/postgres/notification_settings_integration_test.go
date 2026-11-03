//go:build integration

package postgres

import (
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports/porttest/notificationsettingtest"
)

func TestNotificationSettings_Conformance(t *testing.T) {
	t.Parallel()

	notificationsettingtest.Run(t, func(t *testing.T) notificationsettingtest.Harness {
		t.Helper()

		d := newConnectionsDB(t)

		return notificationsettingtest.Harness{
			Settings: NewNotificationSettings(d.store), Secrets: NewSecrets(d.store, testKey(t, "k1")), Audit: d.store,
			NewOrg: func(t *testing.T) domain.OrgID { t.Helper(); return newOrgID(t, d.store) },
		}
	})
}
