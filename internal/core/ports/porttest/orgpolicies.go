package porttest

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.OrgPolicies = (*OrgPolicies)(nil)

// OrgPolicies keeps each organization's policy in memory with the database's rules: the platform
// default until a save, saves fenced by the revision and checked with Validate, and an
// org_policy.updated entry through audit for each save.
type OrgPolicies struct {
	models []string
	audit  ports.Audit
	now    func() time.Time

	mu   sync.Mutex
	rows map[domain.OrgID]domain.OrgPolicy
}

// NewOrgPolicies returns policies whose default allows models, audited through audit (a Tenancy
// that knows the organizations), with now as the time of a save.
func NewOrgPolicies(models []string, audit ports.Audit, now func() time.Time) *OrgPolicies {
	return &OrgPolicies{models: slices.Clone(models), audit: audit, now: now, rows: map[domain.OrgID]domain.OrgPolicy{}}
}

// Get implements ports.OrgPolicies.
func (o *OrgPolicies) Get(ctx context.Context) (domain.OrgPolicy, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.OrgPolicy{}, domain.ErrNoOrg
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	p, ok := o.rows[org]
	if !ok {
		return domain.DefaultOrgPolicy(o.models), nil
	}

	return clonePolicy(p), nil
}

// Put implements ports.OrgPolicies.
func (o *OrgPolicies) Put(ctx context.Context, p domain.OrgPolicy, revision int64, by domain.Actor) (domain.OrgPolicy, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.OrgPolicy{}, domain.ErrNoOrg
	}

	if err := p.Validate(); err != nil {
		return domain.OrgPolicy{}, err
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	if o.rows[org].Revision != revision {
		return domain.OrgPolicy{}, ports.ErrConflict
	}

	p = clonePolicy(p)
	p.Revision, p.UpdatedBy, p.UpdatedAt = revision+1, by, o.now().UTC().Truncate(time.Microsecond)

	err := o.audit.Append(ctx, domain.AuditEntry{
		Actor: by, Action: domain.AuditOrgPolicyUpdated, TargetKind: domain.AuditTargetOrgPolicy, TargetID: string(org),
		Data: map[string]any{"revision": p.Revision},
	})
	if err != nil {
		return domain.OrgPolicy{}, err
	}

	o.rows[org] = p

	return clonePolicy(p), nil
}

func clonePolicy(p domain.OrgPolicy) domain.OrgPolicy {
	p.AllowedModels, p.DeniedCommands = slices.Clone(p.AllowedModels), slices.Clone(p.DeniedCommands)

	return p
}
