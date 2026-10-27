package ports

import (
	"context"

	"github.com/byFrederick/goal/internal/core/domain"
)

// OrgPolicies is the organization policy (ADR 0024), for the organization in ctx: one per
// organization, a ceiling over every factory definition.
type OrgPolicies interface {
	// Get is the organization's policy, or domain.DefaultOrgPolicy of the platform's models, at
	// Revision 0, when it never saved one.
	Get(ctx context.Context) (domain.OrgPolicy, error)
	// Put saves p when the stored policy is at revision (0: none saved yet) and returns it at the
	// next revision, auditing org_policy.updated in the same transaction. p must pass Validate
	// (domain.ErrInvalid); another revision is ErrConflict and changes nothing. p's Revision,
	// UpdatedBy and UpdatedAt are ignored.
	Put(ctx context.Context, p domain.OrgPolicy, revision int64, by domain.Actor) (domain.OrgPolicy, error)
}
