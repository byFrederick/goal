package postgres

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The audit data keys of standing rules.
const (
	auditRuleConnection   = "connection"
	auditRuleConnectionID = "connection_id"
	auditRuleTool         = "tool"
	auditRuleAgent        = "agent"
	auditRuleItem         = "item_id"
)

// StandingRules implements ports.Connections over 00075's standing_rules.
func (c *Connections) StandingRules(ctx context.Context, includeRevoked bool) ([]domain.StandingRule, error) {
	rows, err := fetch(ctx, c.s.read, func(q *db.Queries) ([]db.StandingRulesRow, error) { return q.StandingRules(ctx, includeRevoked) })
	if err != nil {
		return nil, fmt.Errorf("standing rules: %w", err)
	}

	out := make([]domain.StandingRule, 0, len(rows))
	for _, r := range rows {
		out = append(out, standingRuleFrom(r))
	}

	return out, nil
}

// CreateStandingRule implements ports.Connections. The connection is locked against its deletion
// until the rule is written; a concurrent create of the same key loses to the partial unique
// index and returns the rule that won.
func (c *Connections) CreateStandingRule(ctx context.Context, n domain.NewStandingRule, by domain.Actor) (domain.StandingRule, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.StandingRule{}, domain.ErrNoOrg
	}

	if err := n.Check(); err != nil {
		return domain.StandingRule{}, fmt.Errorf("create standing rule: %w", err)
	}

	item, err := uuid.Parse(string(n.Item))
	if err != nil {
		return domain.StandingRule{}, fmt.Errorf("create standing rule: item %q: %w", n.Item, domain.ErrInvalid)
	}

	id, err := newID()
	if err != nil {
		return domain.StandingRule{}, err
	}

	var out domain.StandingRule

	err = c.s.inTx(ctx, func(q *db.Queries) error {
		connID, err := q.ConnectionForRule(ctx, n.Connection)
		if notFound(err) {
			return fmt.Errorf("connection %q: %w", n.Connection, ports.ErrNotFound)
		}

		if err != nil {
			return err
		}

		out = domain.StandingRule{
			ID: domain.StandingRuleID(id.String()), ConnectionID: domain.ConnectionID(connID.String()), Connection: n.Connection,
			Tool: n.Tool, Agent: n.Agent, CreatedBy: by, ItemID: n.Item,
		}

		at, err := q.InsertStandingRule(ctx, db.InsertStandingRuleParams{
			ID: id, ConnectionID: connID, Tool: n.Tool, Agent: n.Agent, CreatedBy: by.Ref(), ItemID: item,
		})
		if notFound(err) {
			active, err := q.ActiveStandingRule(ctx, db.ActiveStandingRuleParams{ConnectionID: connID, Tool: n.Tool, Agent: n.Agent})
			if err != nil {
				return err
			}

			out.ID, out.CreatedBy, out.CreatedAt = domain.StandingRuleID(active.ID.String()), namedActor(active.CreatedBy, ""), active.CreatedAt.UTC()
			out.ItemID = domain.ItemID(active.ItemID.String())

			return nil
		}

		if err != nil {
			return err
		}

		out.CreatedAt = at.UTC()

		return ruleAudit(ctx, q, by, domain.AuditStandingRuleCreated, id, connID, n.Connection, n.Tool, n.Agent, item)
	})
	if err != nil {
		return domain.StandingRule{}, fmt.Errorf("create standing rule for %s %s %s: %w", n.Connection, n.Tool, n.Agent, err)
	}

	return out, nil
}

// RevokeStandingRule implements ports.Connections.
func (c *Connections) RevokeStandingRule(ctx context.Context, id domain.StandingRuleID, by domain.Actor) (domain.StandingRule, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.StandingRule{}, domain.ErrNoOrg
	}

	ruleID, err := uuid.Parse(string(id))
	if err != nil {
		return domain.StandingRule{}, fmt.Errorf("standing rule %q: %w", id, ports.ErrNotFound)
	}

	var out domain.StandingRule

	err = c.s.inTx(ctx, func(q *db.Queries) error {
		r, err := q.StandingRule(ctx, ruleID)
		if notFound(err) {
			return ports.ErrNotFound
		}

		if err != nil {
			return err
		}

		if r.RevokedAt != nil {
			return fmt.Errorf("revoked already: %w", ports.ErrConflict)
		}

		ref := by.Ref()

		at, err := q.RevokeStandingRule(ctx, db.RevokeStandingRuleParams{RevokedBy: &ref, ID: ruleID})
		if err != nil {
			return err
		}

		r.RevokedBy, r.RevokedAt = &ref, at
		out = standingRuleFrom(db.StandingRulesRow(r))

		return ruleAudit(ctx, q, by, domain.AuditStandingRuleRevoked, ruleID, r.ConnectionID, out.Connection, r.Tool, r.Agent, r.ItemID)
	})
	if err != nil {
		return domain.StandingRule{}, fmt.Errorf("revoke standing rule %s: %w", id, err)
	}

	return out, nil
}

// revokeConnectionRules revokes the active standing rules of connection id, named name, in q's
// transaction, auditing each by by.
func revokeConnectionRules(ctx context.Context, q *db.Queries, id uuid.UUID, name string, by domain.Actor) error {
	ref := by.Ref()

	rows, err := q.RevokeConnectionStandingRules(ctx, db.RevokeConnectionStandingRulesParams{RevokedBy: &ref, ConnectionID: id})
	if err != nil {
		return fmt.Errorf("revoke its standing rules: %w", err)
	}

	for _, r := range rows {
		if err := ruleAudit(ctx, q, by, domain.AuditStandingRuleRevoked, r.ID, id, name, r.Tool, r.Agent, r.ItemID); err != nil {
			return err
		}
	}

	return nil
}

// ruleAudit appends action on standing rule id of connection connID, named name.
func ruleAudit(ctx context.Context, q *db.Queries, by domain.Actor, action string, id, connID uuid.UUID, name, tool, agent string,
	item uuid.UUID,
) error {
	return appendAudit(ctx, q, domain.AuditEntry{
		Actor: by, Action: action, TargetKind: domain.AuditTargetStandingRule, TargetID: id.String(), Data: map[string]any{
			auditRuleConnection: name, auditRuleConnectionID: connID.String(), auditRuleTool: tool, auditRuleAgent: agent,
			auditRuleItem: item.String(),
		},
	})
}

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

func standingRuleFrom(r db.StandingRulesRow) domain.StandingRule {
	out := domain.StandingRule{
		ID: domain.StandingRuleID(r.ID.String()), ConnectionID: domain.ConnectionID(r.ConnectionID.String()), Connection: deref(r.Connection),
		Tool: r.Tool, Agent: r.Agent, CreatedBy: namedActor(r.CreatedBy, ""), CreatedAt: r.CreatedAt.UTC(), ItemID: domain.ItemID(r.ItemID.String()),
		RevokedAt: utc(r.RevokedAt),
	}

	if r.RevokedBy != nil {
		a := namedActor(*r.RevokedBy, "")
		out.RevokedBy = &a
	}

	return out
}
