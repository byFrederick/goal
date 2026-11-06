package ports

import (
	"context"

	"github.com/byFrederick/goal/internal/core/domain"
)

// StandingRules are the organization's standing rules (ADR 0031 decision 7): an always-allow of
// one tool for one agent, keyed by factory, environment and MCP server for a run on secrets (ADR
// 0037 decision 18), or by connection for an older run. Every change is audited in its
// transaction, and every method refuses a context without an organization (domain.ErrNoOrg).
type StandingRules interface {
	// StandingRules are the organization's standing rules, oldest first, of either key: the active
	// ones, and the revoked ones too with includeRevoked.
	StandingRules(ctx context.Context, includeRevoked bool) ([]domain.StandingRule, error)
	// CreateServerStandingRule creates a standing rule keyed by n's factory, environment and MCP
	// server, for a run on secrets (ADR 0037 decision 8; standing_rule.created). When its key
	// (factory, environment, server, tool, agent) has an active rule already, it returns that rule
	// and audits nothing. The factory, environment and server need not exist: a rule outlives
	// them. domain.ErrInvalid for a malformed n (NewServerStandingRule.Check).
	CreateServerStandingRule(ctx context.Context, n domain.NewServerStandingRule, by domain.Actor) (domain.StandingRule, error)
	// RevokeStandingRule revokes rule id of either key (standing_rule.revoked). ErrNotFound when
	// there is none; ErrConflict when it is revoked already.
	RevokeStandingRule(ctx context.Context, id domain.StandingRuleID, by domain.Actor) (domain.StandingRule, error)
}
