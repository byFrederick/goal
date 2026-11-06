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

// toolNames are a test's tool names sorted without repeats; nil for none, as the column holds
// NULL.
func toolNames(tools []string) []string {
	if len(tools) == 0 {
		return nil
	}

	out := slices.Clone(tools)
	slices.Sort(out)

	return slices.Compact(out)
}

var _ ports.StandingRules = (*StandingRules)(nil)

// StandingRules are standing rules kept in memory (ports.StandingRules), each change audited
// through audit. A Connections keeps its own (Connections.StandingRules), so the rules it makes
// for a connection are listed there and its Delete revokes them, as the database's one
// standing_rules table does.
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

// CreateStandingRule implements ports.Connections.
func (c *Connections) CreateStandingRule(ctx context.Context, n domain.NewStandingRule, by domain.Actor) (domain.StandingRule, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.StandingRule{}, domain.ErrNoOrg
	}

	if err := n.Check(); err != nil {
		return domain.StandingRule{}, fmt.Errorf("create standing rule: %w", err)
	}

	c.d.mu.Lock()
	defer c.d.mu.Unlock()

	conn, ok := c.d.conns[org][n.Connection]
	if !ok {
		return domain.StandingRule{}, fmt.Errorf("connection %q: %w", n.Connection, ports.ErrNotFound)
	}

	return c.d.rules.create(ctx, org, domain.StandingRule{
		ConnectionID: conn.conn.ID, Connection: conn.conn.Name, Tool: n.Tool, Agent: n.Agent, CreatedBy: by, ItemID: n.Item,
	})
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

// create adds rule n to org, or returns the active rule of its key (connection, or factory,
// environment and server; tool; agent) and audits nothing.
func (s *StandingRules) create(ctx context.Context, org domain.OrgID, n domain.StandingRule) (domain.StandingRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, r := range s.rules[org] {
		if r.Active() && r.ConnectionID == n.ConnectionID && r.Factory == n.Factory && r.Environment == n.Environment &&
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

// revokeConnection revokes org's active rules of connection id, which is being deleted, and drops
// its name from all of its rules, as the database's rules lose it with the connection row.
func (s *StandingRules) revokeConnection(ctx context.Context, org domain.OrgID, id domain.ConnectionID, by domain.Actor) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Truncate(time.Microsecond)

	for _, r := range s.rules[org] {
		if r.ConnectionID != id {
			continue
		}

		if r.Active() {
			if err := s.ruleAudit(ctx, by, domain.AuditStandingRuleRevoked, *r); err != nil {
				return err
			}

			revoker := by
			r.RevokedBy, r.RevokedAt = &revoker, &now
		}

		r.Connection = ""
	}

	return nil
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

// ruleAudit appends action on rule r, of its connection or of its server.
func (s *StandingRules) ruleAudit(ctx context.Context, by domain.Actor, action string, r domain.StandingRule) error {
	data := map[string]any{"tool": r.Tool, "agent": r.Agent, "item_id": string(r.ItemID)}
	if r.ConnectionID != "" {
		data["connection"], data["connection_id"] = r.Connection, string(r.ConnectionID)
	} else {
		data["factory_id"], data["environment"], data["server"] = string(r.Factory), r.Environment, r.Server
	}

	return s.audit.Append(ctx, domain.AuditEntry{Actor: by, Action: action, TargetKind: domain.AuditTargetStandingRule, TargetID: string(r.ID), Data: data})
}
