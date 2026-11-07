package porttest

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.StandingRules = (*StandingRules)(nil)

// StandingRules are standing rules kept in memory (ports.StandingRules), each change audited
// through audit.
type StandingRules struct {
	mu    sync.Mutex
	audit ports.Audit
	// rules are each organization's standing rules, oldest first; none is ever removed.
	rules map[domain.OrgID][]*domain.StandingRule
}

// NewStandingRules returns no standing rules, audited through audit (a Tenancy that knows the
// organizations).
func NewStandingRules(audit ports.Audit) *StandingRules {
	return &StandingRules{audit: audit, rules: map[domain.OrgID][]*domain.StandingRule{}}
}

// StandingRules implements ports.StandingRules.
func (s *StandingRules) StandingRules(ctx context.Context, includeRevoked bool) ([]domain.StandingRule, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, domain.ErrNoOrg
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	out := []domain.StandingRule{}

	for _, r := range s.rules[org] {
		if includeRevoked || r.Active() {
			out = append(out, copyRule(r))
		}
	}

	return out, nil
}

// CreateServerStandingRule implements ports.StandingRules.
func (s *StandingRules) CreateServerStandingRule(ctx context.Context, n domain.NewServerStandingRule, by domain.Actor) (domain.StandingRule, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.StandingRule{}, domain.ErrNoOrg
	}

	if err := n.Check(); err != nil {
		return domain.StandingRule{}, fmt.Errorf("create standing rule: %w", err)
	}

	return s.create(ctx, org, domain.StandingRule{
		Factory: n.Factory, Environment: n.Environment, Server: n.Server, Tool: n.Tool, Agent: n.Agent, CreatedBy: by, ItemID: n.Item,
	})
}

// create adds rule n to org, or returns the active rule of its key (factory, environment and
// server; tool; agent) and audits nothing.
func (s *StandingRules) create(ctx context.Context, org domain.OrgID, n domain.StandingRule) (domain.StandingRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, r := range s.rules[org] {
		if r.Active() && r.Factory == n.Factory && r.Environment == n.Environment &&
			r.Server == n.Server && r.Tool == n.Tool && r.Agent == n.Agent {
			return copyRule(r), nil
		}
	}

	id, err := uuid.NewV7()
	if err != nil {
		return domain.StandingRule{}, err
	}

	r := n
	r.ID, r.CreatedAt = domain.StandingRuleID(id.String()), time.Now().UTC().Truncate(time.Microsecond)

	if err := s.ruleAudit(ctx, r.CreatedBy, domain.AuditStandingRuleCreated, r); err != nil {
		return domain.StandingRule{}, err
	}

	s.rules[org] = append(s.rules[org], &r)

	return copyRule(&r), nil
}

// RevokeStandingRule implements ports.StandingRules.
func (s *StandingRules) RevokeStandingRule(ctx context.Context, id domain.StandingRuleID, by domain.Actor) (domain.StandingRule, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.StandingRule{}, domain.ErrNoOrg
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	i := slices.IndexFunc(s.rules[org], func(r *domain.StandingRule) bool { return r.ID == id })
	if i < 0 {
		return domain.StandingRule{}, fmt.Errorf("standing rule %s: %w", id, ports.ErrNotFound)
	}

	r := s.rules[org][i]
	if !r.Active() {
		return domain.StandingRule{}, fmt.Errorf("standing rule %s is revoked already: %w", id, ports.ErrConflict)
	}

	if err := s.ruleAudit(ctx, by, domain.AuditStandingRuleRevoked, *r); err != nil {
		return domain.StandingRule{}, err
	}

	now, revoker := time.Now().UTC().Truncate(time.Microsecond), by
	r.RevokedBy, r.RevokedAt = &revoker, &now

	return copyRule(r), nil
}

// copyRule is a copy of r that shares nothing with it.
func copyRule(r *domain.StandingRule) domain.StandingRule {
	out := *r

	if r.RevokedBy != nil {
		by, at := *r.RevokedBy, *r.RevokedAt
		out.RevokedBy, out.RevokedAt = &by, &at
	}

	return out
}

// ruleAudit appends action on rule r of its server.
func (s *StandingRules) ruleAudit(ctx context.Context, by domain.Actor, action string, r domain.StandingRule) error {
	data := map[string]any{
		"tool": r.Tool, "agent": r.Agent, "item_id": string(r.ItemID),
		"factory_id": string(r.Factory), "environment": r.Environment, "server": r.Server,
	}

	return s.audit.Append(ctx, domain.AuditEntry{Actor: by, Action: action, TargetKind: domain.AuditTargetStandingRule, TargetID: string(r.ID), Data: data})
}
