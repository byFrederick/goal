// Package jonah is Jonah's goald loops (ADR 0039): the daily review that turns a factory's
// evidence into proposals, the tries of past items that test a proposal, and the watch on every
// published change. Any goald instance may run them: work is claimed under leases in PostgreSQL,
// so the loops keep no state of their own (AGENTS.md rule 4).
package jonah

import (
	"context"

	"github.com/byFrederick/goal/internal/core/domain"
)

// Orgs lists the organizations the loops serve; ports.Tenancy satisfies it.
type Orgs interface {
	ActiveOrgs(ctx context.Context) ([]domain.OrgID, error)
}

// truncate cuts s to at most n bytes without splitting a UTF-8 sequence.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}

	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}

	return s[:n]
}
