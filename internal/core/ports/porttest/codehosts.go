package porttest

import (
	"cmp"
	"context"
	"slices"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.CodeHosts = (*Tenancy)(nil)

// CodeHosts implements ports.CodeHosts.
func (t *Tenancy) CodeHosts(ctx context.Context) ([]domain.CodeHost, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	r, err := t.liveLocked(ctx)
	if err != nil {
		return nil, err
	}

	return slices.Clone(r.codeHosts), nil
}

// SetCodeHosts implements ports.CodeHosts. As the PostgreSQL adapter, it trusts that hosts were
// checked with domain.CheckCodeHosts, and leaves who may set them to the API's authorizer.
func (t *Tenancy) SetCodeHosts(ctx context.Context, by domain.Actor, hosts []domain.CodeHost) ([]domain.CodeHost, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	r, err := t.liveLocked(ctx)
	if err != nil {
		return nil, err
	}

	next := slices.Clone(hosts)
	slices.SortFunc(next, func(a, b domain.CodeHost) int { return cmp.Compare(a.Host, b.Host) })

	if slices.Equal(next, r.codeHosts) {
		return slices.Clone(r.codeHosts), nil
	}

	t.appendLocked(r.org.ID, orgEntry(by, domain.AuditOrgCodeHostsSet, r.org, domain.CodeHostsAuditData(next, r.codeHosts)))
	r.codeHosts = next

	return slices.Clone(next), nil
}
