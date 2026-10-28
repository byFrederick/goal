package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/engine"
)

// DecideToolApproval records the request's actor's decision on the tool call an item's agent
// asked to make (ADR 0031): approve, with Always allow when the caller also manages the
// organization's connections, or deny with a note. The engine puts the item back in line.
func (s *server) DecideToolApproval(ctx context.Context, req gen.DecideToolApprovalRequestObject) (gen.DecideToolApprovalResponseObject, error) {
	// The item first: who may not see or decide it learns nothing from a bad body.
	by, it, err := s.decide(ctx, req.Item)
	if err != nil {
		return nil, err
	}

	approve, alwaysAllow, note, err := toolDecisionOf(req.Body)
	if err != nil {
		return nil, err
	}

	// Before the engine: a refused Always allow creates no rule and records no decision.
	if alwaysAllow {
		forbidden := fmt.Errorf("%w: always allow needs %s", errForbidden, permConnectionManage)
		if err := refusal(s.ask(ctx, permConnectionManage, ""), forbidden); err != nil {
			return nil, fmt.Errorf("always allow: %w", err)
		}
	}

	if it, err = s.engine.DecideToolApproval(ctx, it.ID, by, approve, alwaysAllow, note); err != nil {
		return nil, err
	}

	out := s.itemOf(ctx, it)
	if out.Question, out.ToolApproval, err = s.openAsks(ctx, it.ID); err != nil {
		return nil, err
	}

	return gen.DecideToolApproval200JSONResponse(out), nil
}

// toolDecisionOf checks a decision's body: approve or deny, always_allow only with approve, and a
// note only with deny, 1 to 2000 characters once trimmed.
func toolDecisionOf(body *gen.ToolDecision) (approve, alwaysAllow bool, note string, err error) {
	if body == nil {
		return false, false, "", fmt.Errorf("%w: a decision takes decision approve or deny", errInvalid)
	}

	switch body.Decision {
	case gen.Approve:
		approve = true
	case gen.Deny:
	default:
		return false, false, "", fmt.Errorf("%w: decision must be approve or deny", errInvalid)
	}

	alwaysAllow = deref(body.AlwaysAllow)

	switch {
	case alwaysAllow && !approve:
		return false, false, "", fmt.Errorf("%w: always_allow only comes with approve", errInvalid)
	case body.Note == nil:
		return approve, alwaysAllow, "", nil
	case approve:
		return false, false, "", fmt.Errorf("%w: a note only comes with deny", errInvalid)
	}

	note = strings.TrimSpace(*body.Note)
	if note == "" || utf8.RuneCountInString(note) > engine.MaxDenyNoteRunes {
		return false, false, "", fmt.Errorf("%w: a note takes 1 to %d characters", errInvalid, engine.MaxDenyNoteRunes)
	}

	return approve, alwaysAllow, note, nil
}

// toolApprovalOf is an open tool approval as the contract has it.
func toolApprovalOf(a domain.ToolApproval) *gen.ToolApproval {
	return &gen.ToolApproval{
		Id: a.EventID, Connection: a.Connection, Tool: a.Tool, Input: a.Input, Agent: a.Agent, RunId: string(a.RunID),
		AskedAt: a.At, AlwaysAllowOffered: a.AlwaysAllowOffered,
	}
}

// ListStandingRules lists the organization's standing rules, oldest first: the active ones, and
// the revoked ones too on request.
func (s *server) ListStandingRules(ctx context.Context, req gen.ListStandingRulesRequestObject) (gen.ListStandingRulesResponseObject, error) {
	rules, err := s.connections.StandingRules(ctx, deref(req.Params.Revoked))
	if err != nil {
		return nil, fmt.Errorf("list standing rules: %w", err)
	}

	out := gen.ListStandingRules200JSONResponse{Rules: make([]gen.StandingRule, 0, len(rules))}
	for _, r := range rules {
		out.Rules = append(out.Rules, s.standingRuleOf(r))
	}

	return out, nil
}

// RevokeStandingRule revokes one of the organization's standing rules through the engine.
func (s *server) RevokeStandingRule(ctx context.Context, req gen.RevokeStandingRuleRequestObject) (gen.RevokeStandingRuleResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("revoke standing rule: the request has no actor")
	}

	r, err := s.engine.RevokeStandingRule(ctx, domain.StandingRuleID(req.Rule), by)
	if err != nil {
		return nil, err
	}

	return gen.RevokeStandingRule200JSONResponse(s.standingRuleOf(r)), nil
}

func (s *server) standingRuleOf(r domain.StandingRule) gen.StandingRule {
	out := gen.StandingRule{
		Id: string(r.ID), ConnectionId: string(r.ConnectionID), Connection: optional(r.Connection), Tool: r.Tool, Agent: r.Agent,
		CreatedBy: s.actorOut(r.CreatedBy), CreatedAt: r.CreatedAt, ItemId: string(r.ItemID), RevokedAt: r.RevokedAt,
	}

	if r.RevokedBy != nil {
		by := s.actorOut(*r.RevokedBy)
		out.RevokedBy = &by
	}

	return out
}

// toolApprovalProblem maps the refusals of tool approvals and standing rules, each a 409 with a
// code of its own: a decision on an item with no open tool approval, Always allow where it is not
// offered, an approval of the item while its tool approval is open, and a revoke of a revoked
// rule. The handler refuses a malformed decision before the engine sees it.
func toolApprovalProblem(err error) (gen.Problem, bool) {
	var code string

	switch {
	case errors.Is(err, engine.ErrNoOpenToolApproval):
		code = "no-open-tool-approval"
	case errors.Is(err, engine.ErrAlwaysAllowNotOffered):
		code = "always-allow-not-offered"
	case errors.Is(err, engine.ErrToolApprovalOpen):
		code = "tool-approval-open"
	case errors.Is(err, engine.ErrStandingRuleRevoked):
		code = "standing-rule-revoked"
	default:
		return gen.Problem{}, false
	}

	p := detailed(http.StatusConflict, err)
	p.Code = optional(code)

	return p, true
}
