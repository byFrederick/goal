package porttest

import (
	"context"
	"fmt"
	"slices"
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

// StandingRules implements ports.Connections.
func (c *Connections) StandingRules(ctx context.Context, includeRevoked bool) ([]domain.StandingRule, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, domain.ErrNoOrg
	}

	c.d.mu.Lock()
	defer c.d.mu.Unlock()

	out := []domain.StandingRule{}

	for _, r := range c.d.rules[org] {
		if includeRevoked || r.Active() {
			out = append(out, c.d.ruleLocked(org, r))
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

	for _, r := range c.d.rules[org] {
		if r.Active() && r.ConnectionID == conn.conn.ID && r.Tool == n.Tool && r.Agent == n.Agent {
			return c.d.ruleLocked(org, r), nil
		}
	}

	id, err := uuid.NewV7()
	if err != nil {
		return domain.StandingRule{}, err
	}

	r := &domain.StandingRule{
		ID: domain.StandingRuleID(id.String()), ConnectionID: conn.conn.ID, Tool: n.Tool, Agent: n.Agent, CreatedBy: by,
		CreatedAt: time.Now().UTC().Truncate(time.Microsecond), ItemID: n.Item,
	}

	if err := c.ruleAudit(ctx, by, domain.AuditStandingRuleCreated, *r, n.Connection); err != nil {
		return domain.StandingRule{}, err
	}

	c.d.rules[org] = append(c.d.rules[org], r)

	return c.d.ruleLocked(org, r), nil
}

// RevokeStandingRule implements ports.Connections.
func (c *Connections) RevokeStandingRule(ctx context.Context, id domain.StandingRuleID, by domain.Actor) (domain.StandingRule, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.StandingRule{}, domain.ErrNoOrg
	}

	c.d.mu.Lock()
	defer c.d.mu.Unlock()

	i := slices.IndexFunc(c.d.rules[org], func(r *domain.StandingRule) bool { return r.ID == id })
	if i < 0 {
		return domain.StandingRule{}, fmt.Errorf("standing rule %s: %w", id, ports.ErrNotFound)
	}

	r := c.d.rules[org][i]
	if !r.Active() {
		return domain.StandingRule{}, fmt.Errorf("standing rule %s is revoked already: %w", id, ports.ErrConflict)
	}

	out := c.d.ruleLocked(org, r)
	if err := c.ruleAudit(ctx, by, domain.AuditStandingRuleRevoked, out, out.Connection); err != nil {
		return domain.StandingRule{}, err
	}

	now, revoker := time.Now().UTC().Truncate(time.Microsecond), by
	r.RevokedBy, r.RevokedAt = &revoker, &now

	return c.d.ruleLocked(org, r), nil
}

// ruleLocked is a copy of r with its connection's name, "" once the connection is deleted.
func (d *connectionData) ruleLocked(org domain.OrgID, r *domain.StandingRule) domain.StandingRule {
	out := *r

	for _, conn := range d.conns[org] {
		if conn.conn.ID == r.ConnectionID {
			out.Connection = conn.conn.Name
		}
	}

	if r.RevokedBy != nil {
		by, at := *r.RevokedBy, *r.RevokedAt
		out.RevokedBy, out.RevokedAt = &by, &at
	}

	return out
}

// ruleAudit appends action on rule r of the connection named name.
func (c *Connections) ruleAudit(ctx context.Context, by domain.Actor, action string, r domain.StandingRule, name string) error {
	return c.appendAudit(ctx, by, action, domain.AuditTargetStandingRule, string(r.ID), map[string]any{
		"connection": name, "connection_id": string(r.ConnectionID), "tool": r.Tool, "agent": r.Agent, "item_id": string(r.ItemID),
	})
}
