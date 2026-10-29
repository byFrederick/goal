package porttest_test

import (
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/core/ports/porttest/notificationtest"
)

// The fake must keep the database's rules, or tests built on it prove nothing.
func TestNotificationPreferences_Conformance(t *testing.T) {
	t.Parallel()

	notificationtest.Run(t, func(*testing.T) notificationtest.Harness {
		var users atomic.Int64

		return notificationtest.Harness{
			Prefs: porttest.NewNotificationPreferences(),
			NewUser: func(*testing.T) domain.UserID {
				return domain.UserID(fmt.Sprintf("00000000-0000-7000-8000-%012d", users.Add(1)))
			},
		}
	})
}
