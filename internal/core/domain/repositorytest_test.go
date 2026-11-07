package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// validRepositoryTest is a passing read test of acme/app with a passing CI check.
func validRepositoryTest() domain.RepositoryTest {
	return domain.RepositoryTest{
		Factory: "f", URL: "github.com/acme/app", Access: domain.RepositoryTestRead, Secret: "repo-token",
		At: time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC), Tested: true, OK: true, Summary: "Can read",
		ReadOK: true, CITested: true, CIOK: true, CIReason: "GitHub shows the checks of acme/app",
	}
}

// Check takes a test with or without a CI check, and refuses a CI check that passed without
// being tested and a CI reason over MaxRepositoryTestText characters.
func TestRepositoryTestCheck_CICheck(t *testing.T) {
	t.Parallel()

	noCI := validRepositoryTest()
	noCI.CITested, noCI.CIOK, noCI.CIReason = false, false, ""

	failedCI := validRepositoryTest()
	failedCI.CIOK, failedCI.CIReason = false, "the token has no read access to Checks and Commit statuses"

	for name, ok := range map[string]domain.RepositoryTest{"passing": validRepositoryTest(), "absent": noCI, "failing": failedCI} {
		if err := ok.Check(); err != nil {
			t.Errorf("Check of a test with %s CI = %v; want nil", name, err)
		}
	}

	untestedOK := validRepositoryTest()
	untestedOK.CITested = false

	long := validRepositoryTest()
	long.CIReason = strings.Repeat("x", domain.MaxRepositoryTestText+1)

	for name, bad := range map[string]domain.RepositoryTest{"CIOK without CITested": untestedOK, "a long CI reason": long} {
		if err := bad.Check(); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("Check of %s = %v; want ErrInvalid", name, err)
		}
	}
}
