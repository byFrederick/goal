package postgres

import (
	"context"
	"fmt"

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
	auditRuleFactoryID    = "factory_id"
	auditRuleEnvironment  = "environment"
	auditRuleServer       = "server"
)

var _ ports.StandingRules = (*StandingRules)(nil)

// StandingRules are the standing rules in 00075's standing_rules (ports.StandingRules).
type StandingRules struct {
	s *Store
}

// NewStandingRules returns the standing rules on s's pool.
func NewStandingRules(s *Store) *StandingRules { return &StandingRules{s: s} }

// StandingRules implements ports.StandingRules.
func (c *StandingRules) StandingRules(ctx context.Context, includeRevoked bool) ([]domain.StandingRule, error) {
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

// CreateServerStandingRule implements ports.StandingRules. A concurrent create of the same key
// loses to 00091's partial unique index and returns the rule that won.
func (c *StandingRules) CreateServerStandingRule(ctx context.Context, n domain.NewServerStandingRule, by domain.Actor) (domain.StandingRule, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.StandingRule{}, domain.ErrNoOrg
	}

	if err := n.Check(); err != nil {
		return domain.StandingRule{}, fmt.Errorf("create standing rule: %w", err)
	}

	factory, err := uuid.Parse(string(n.Factory))
	if err != nil {
		return domain.StandingRule{}, fmt.Errorf("create standing rule: factory %q: %w", n.Factory, domain.ErrInvalid)
	}

	item, err := uuid.Parse(string(n.Item))
	if err != nil {
		return domain.StandingRule{}, fmt.Errorf("create standing rule: item %q: %w", n.Item, domain.ErrInvalid)
	}

	id, err := newID()
	if err != nil {
		return domain.StandingRule{}, err
	}

	out := domain.StandingRule{
		ID: domain.StandingRuleID(id.String()), Factory: n.Factory, Environment: n.Environment, Server: n.Server, Tool: n.Tool,
		Agent: n.Agent, CreatedBy: by, ItemID: n.Item,
	}

	err = c.s.inTx(ctx, func(q *db.Queries) error {
		at, err := q.InsertServerStandingRule(ctx, db.InsertServerStandingRuleParams{
			ID: id, FactoryID: &factory, Environment: &n.Environment, Server: &n.Server, Tool: n.Tool, Agent: n.Agent,
			CreatedBy: by.Ref(), ItemID: item,
		})
		if notFound(err) {
			active, err := q.ActiveServerStandingRule(ctx, db.ActiveServerStandingRuleParams{
				FactoryID: &factory, Environment: &n.Environment, Server: &n.Server, Tool: n.Tool, Agent: n.Agent,
			})
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

		return ruleAudit(ctx, q, by, domain.AuditStandingRuleCreated, out)
	})
	if err != nil {
		return domain.StandingRule{}, fmt.Errorf("create standing rule for %s %s %s %s: %w", n.Environment, n.Server, n.Tool, n.Agent, err)
	}

	return out, nil
}

// RevokeStandingRule implements ports.StandingRules.
func (c *StandingRules) RevokeStandingRule(ctx context.Context, id domain.StandingRuleID, by domain.Actor) (domain.StandingRule, error) {
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

		return ruleAudit(ctx, q, by, domain.AuditStandingRuleRevoked, out)
	})
	if err != nil {
		return domain.StandingRule{}, fmt.Errorf("revoke standing rule %s: %w", id, err)
	}

	return out, nil
}

// ruleAudit appends action on standing rule r: its connection's id and name, or its factory,
// environment and server.
func ruleAudit(ctx context.Context, q *db.Queries, by domain.Actor, action string, r domain.StandingRule) error {
	data := map[string]any{auditRuleTool: r.Tool, auditRuleAgent: r.Agent, auditRuleItem: string(r.ItemID)}
	if r.ConnectionID != "" {
		data[auditRuleConnection], data[auditRuleConnectionID] = r.Connection, string(r.ConnectionID)
	} else {
		data[auditRuleFactoryID], data[auditRuleEnvironment], data[auditRuleServer] = string(r.Factory), r.Environment, r.Server
	}

	return appendAudit(ctx, q, domain.AuditEntry{
		Actor: by, Action: action, TargetKind: domain.AuditTargetStandingRule, TargetID: string(r.ID), Data: data,
	})
}

func standingRuleFrom(r db.StandingRulesRow) domain.StandingRule {
	out := domain.StandingRule{
		ID: domain.StandingRuleID(r.ID.String()), Connection: deref(r.Connection), Environment: deref(r.Environment), Server: deref(r.Server),
		Tool: r.Tool, Agent: r.Agent, CreatedBy: namedActor(r.CreatedBy, ""), CreatedAt: r.CreatedAt.UTC(), ItemID: domain.ItemID(r.ItemID.String()),
		RevokedAt: utc(r.RevokedAt),
	}

	if r.ConnectionID != nil {
		out.ConnectionID = domain.ConnectionID(r.ConnectionID.String())
	}

	if r.FactoryID != nil {
		out.Factory = domain.FactoryID(r.FactoryID.String())
	}

	if r.RevokedBy != nil {
		a := namedActor(*r.RevokedBy, "")
		out.RevokedBy = &a
	}

	return out
}
