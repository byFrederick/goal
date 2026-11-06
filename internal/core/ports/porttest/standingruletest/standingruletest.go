// Package standingruletest is the conformance suite for ports.StandingRules (ADR 0031 decision
// 7, ADR 0037 decision 18). The in-memory fake in porttest and the PostgreSQL adapter run it, so
// tests on the fake hold for the database's rules: one active rule per factory, environment,
// server, tool and agent, revoked once, every change audited, and organizations kept apart. The
// rules keyed by connection are tested with the connections, in connectiontest.
package standingruletest

import (
	"context"
	"errors"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Harness is one StandingRules under test.
type Harness struct {
	StandingRules ports.StandingRules
	Audit         ports.Audit
	// NewOrg returns a new organization.
	NewOrg func(t *testing.T) domain.OrgID
}

// Run runs the suite. Tests run in parallel and call newHarness once each; harnesses must be
// isolated from each other.
func Run(t *testing.T, newHarness func(t *testing.T) Harness) {
	t.Helper()

	tests := []struct {
		name string
		test func(f *fixture)
	}{
		{"CreateServerStandingRule_OneActivePerKey", testCreate},
		{"CreateServerStandingRule_Refused", testCreateRefused},
		{"RevokeStandingRule_ServerKey", testRevoke},
		{"StandingRules_ActiveUnlessRevoked", testActiveUnlessRevoked},
		{"StandingRules_OtherOrgSeesNothing", testIsolation},
		{"Methods_NoOrgIsAnError", testNoOrg},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			tc.test(&fixture{Harness: h, t: t, r: h.StandingRules})
		})
	}
}

// The names, actors and item the suite uses.
const (
	server      = "acme-docs"
	otherServer = "acme-git"
	toolComment = "create_comment"
	toolIssue   = "get_issue"
	architect   = "architect"
	reviewer    = "reviewer"
	approvedOn  = domain.ItemID("00000000-0000-7000-8000-0000000000bb")
	// ruleFactory is the factory the rules are for; no factory of that id need exist.
	ruleFactory = domain.FactoryID("00000000-0000-7000-8000-0000000000f1")
	noRule      = domain.StandingRuleID("00000000-0000-7000-8000-0000000000cc")
)

var (
	admin  = domain.Actor{Kind: domain.ActorUser, ID: "00000000-0000-7000-8000-000000000001", Name: "Ada"}
	editor = domain.Actor{Kind: domain.ActorUser, ID: "00000000-0000-7000-8000-000000000002", Name: "Eve"}
)

type fixture struct {
	Harness

	t *testing.T
	r ports.StandingRules
}

func (f *fixture) in(org domain.OrgID) context.Context { return domain.WithOrg(f.t.Context(), org) }

// key is a rule for agent to call tool of server srv in ruleFactory's environment env.
func key(env, srv, tool, agent string) domain.NewServerStandingRule {
	return domain.NewServerStandingRule{Factory: ruleFactory, Environment: env, Server: srv, Tool: tool, Agent: agent, Item: approvedOn}
}

// rule creates n in org, failing on an error.
func (f *fixture) rule(org domain.OrgID, n domain.NewServerStandingRule, by domain.Actor) domain.StandingRule {
	f.t.Helper()

	got, err := f.r.CreateServerStandingRule(f.in(org), n, by)
	if err != nil {
		f.t.Fatalf("CreateServerStandingRule %+v: %v", n, err)
	}

	return got
}

// rules are org's standing rules, oldest first.
func (f *fixture) rules(org domain.OrgID, includeRevoked bool) []domain.StandingRule {
	f.t.Helper()

	got, err := f.r.StandingRules(f.in(org), includeRevoked)
	if err != nil {
		f.t.Fatalf("StandingRules: %v", err)
	}

	return got
}

func (f *fixture) audit(org domain.OrgID, action string) []domain.AuditEntry {
	f.t.Helper()

	got, err := f.Audit.AuditLog(f.in(org), ports.AuditQuery{Limit: 100, Action: action})
	if err != nil {
		f.t.Fatal(err)
	}

	return got
}

// one is org's only entry of action, failing when there is not exactly one.
func (f *fixture) one(org domain.OrgID, action string) domain.AuditEntry {
	f.t.Helper()

	got := f.audit(org, action)
	if len(got) != 1 {
		f.t.Fatalf("%d %s entries; want one: %+v", len(got), action, got)
	}

	return got[0]
}

// ruleKey is what a standing rule is kept for: its key, tool, agent role and item.
type ruleKey struct {
	connection     domain.ConnectionID
	connectionName string
	factory        domain.FactoryID
	environment    string
	server, tool   string
	agent          string
	item           domain.ItemID
}

func keyOf(r domain.StandingRule) ruleKey {
	return ruleKey{r.ConnectionID, r.Connection, r.Factory, r.Environment, r.Server, r.Tool, r.Agent, r.ItemID}
}

// wantRule is the rule key(DefaultEnvironment, server, tool, agent) makes, approved on approvedOn.
func wantRule(tool, agent string) domain.StandingRule {
	return domain.StandingRule{Factory: ruleFactory, Environment: domain.DefaultEnvironment, Server: server, Tool: tool, Agent: agent, ItemID: approvedOn}
}

// revokedBy is the id of the actor who revoked r, "" while it is active.
func revokedBy(r domain.StandingRule) string {
	if r.RevokedBy == nil {
		return ""
	}

	return r.RevokedBy.ID
}

// hasData reports whether e's data holds every entry of want.
func hasData(e domain.AuditEntry, want map[string]any) bool {
	for k, v := range want {
		if e.Data[k] != v {
			return false
		}
	}

	return true
}

func (f *fixture) expect(ok bool, format string, args ...any) {
	f.t.Helper()

	if !ok {
		f.t.Errorf(format, args...)
	}
}

func (f *fixture) refused(what string, err, want error) {
	f.t.Helper()

	if !errors.Is(err, want) {
		f.t.Errorf("%s = %v; want %v", what, err, want)
	}
}

func testCreate(f *fixture) {
	acme := f.NewOrg(f.t)

	r := f.rule(acme, key(domain.DefaultEnvironment, server, toolComment, architect), admin)
	f.expect(r.ID != "" && keyOf(r) == keyOf(wantRule(toolComment, architect)) && r.CreatedBy.ID == admin.ID && !r.CreatedAt.IsZero() && r.Active(),
		"CreateServerStandingRule = %+v; want an active rule keyed by server, created by admin", r)

	e := f.one(acme, domain.AuditStandingRuleCreated)
	f.expect(e.TargetKind == domain.AuditTargetStandingRule && e.TargetID == string(r.ID) && e.Actor.ID == admin.ID && hasData(e, map[string]any{
		"factory_id": string(ruleFactory), "environment": domain.DefaultEnvironment, "server": server,
		"tool": toolComment, "agent": architect, "item_id": string(approvedOn),
	}), "standing_rule.created = %+v", e)

	again := f.rule(acme, key(domain.DefaultEnvironment, server, toolComment, architect), editor)
	f.expect(again.ID == r.ID && again.CreatedBy.ID == admin.ID, "CreateServerStandingRule of an active key = %+v; want the rule %s", again, r.ID)
	f.one(acme, domain.AuditStandingRuleCreated)

	for name, n := range map[string]domain.NewServerStandingRule{
		"another environment": key("backend", server, toolComment, architect),
		"another server":      key(domain.DefaultEnvironment, otherServer, toolComment, architect),
		"another tool":        key(domain.DefaultEnvironment, server, toolIssue, architect),
		"another agent":       key(domain.DefaultEnvironment, server, toolComment, reviewer),
	} {
		other := f.rule(acme, n, admin)
		f.expect(other.ID != r.ID, "a rule for %s is the same rule", name)
	}

	all := f.rules(acme, false)
	f.expect(len(all) == 5 && all[0].ID == r.ID, "StandingRules = %+v; want the first rule, then the four others", all)
}

func testCreateRefused(f *fixture) {
	acme := f.NewOrg(f.t)
	ctx := f.in(acme)

	for name, n := range map[string]domain.NewServerStandingRule{
		"a malformed environment": key("Default", server, toolComment, architect),
		"no server":               key(domain.DefaultEnvironment, "", toolComment, architect),
		"no tool":                 key(domain.DefaultEnvironment, server, "", architect),
		"a malformed agent":       key(domain.DefaultEnvironment, server, toolComment, "Architect"),
	} {
		_, err := f.r.CreateServerStandingRule(ctx, n, admin)
		f.refused("CreateServerStandingRule of "+name, err, domain.ErrInvalid)
	}

	f.expect(len(f.rules(acme, true)) == 0, "rules after refused creates = %+v; want none", f.rules(acme, true))
	f.expect(len(f.audit(acme, domain.AuditStandingRuleCreated)) == 0, "a refused create was audited")
}

func testRevoke(f *fixture) {
	acme := f.NewOrg(f.t)
	ctx := f.in(acme)
	r := f.rule(acme, key(domain.DefaultEnvironment, server, toolComment, architect), admin)

	got, err := f.r.RevokeStandingRule(ctx, r.ID, editor)
	if err != nil {
		f.t.Fatalf("RevokeStandingRule: %v", err)
	}

	f.expect(got.ID == r.ID && keyOf(got) == keyOf(r) && !got.Active() && got.RevokedAt != nil && revokedBy(got) == editor.ID,
		"RevokeStandingRule = %+v; want the server rule revoked by the editor", got)

	e := f.one(acme, domain.AuditStandingRuleRevoked)
	f.expect(e.TargetKind == domain.AuditTargetStandingRule && e.TargetID == string(r.ID) && e.Actor.ID == editor.ID &&
		hasData(e, map[string]any{"server": server, "factory_id": string(ruleFactory)}),
		"standing_rule.revoked = %+v", e)

	_, err = f.r.RevokeStandingRule(ctx, r.ID, editor)
	f.refused("RevokeStandingRule again", err, ports.ErrConflict)
	_, err = f.r.RevokeStandingRule(ctx, noRule, editor)
	f.refused("RevokeStandingRule of no rule", err, ports.ErrNotFound)
	f.one(acme, domain.AuditStandingRuleRevoked)

	renewed := f.rule(acme, key(domain.DefaultEnvironment, server, toolComment, architect), admin)
	f.expect(renewed.ID != r.ID && renewed.Active(), "a rule for a revoked rule's key = %+v; want a new active one", renewed)
}

func testActiveUnlessRevoked(f *fixture) {
	acme := f.NewOrg(f.t)
	r := f.rule(acme, key(domain.DefaultEnvironment, server, toolComment, architect), admin)
	kept := f.rule(acme, key(domain.DefaultEnvironment, server, toolIssue, architect), admin)

	if _, err := f.r.RevokeStandingRule(f.in(acme), r.ID, editor); err != nil {
		f.t.Fatalf("RevokeStandingRule: %v", err)
	}

	active := f.rules(acme, false)
	f.expect(len(active) == 1 && active[0].ID == kept.ID, "active rules = %+v; want only %s", active, kept.ID)

	all := f.rules(acme, true)
	f.expect(len(all) == 2 && all[0].ID == r.ID && !all[0].Active() && all[0].RevokedBy != nil && all[0].RevokedBy.ID == editor.ID &&
		all[1].ID == kept.ID,
		"every rule = %+v; want the revoked one first, then the active one", all)
}

func testIsolation(f *fixture) {
	acme, labs := f.NewOrg(f.t), f.NewOrg(f.t)
	r := f.rule(acme, key(domain.DefaultEnvironment, server, toolComment, architect), admin)

	f.expect(len(f.rules(labs, true)) == 0, "another org's StandingRules = %+v; want nothing", f.rules(labs, true))

	_, err := f.r.RevokeStandingRule(f.in(labs), r.ID, admin)
	f.refused("another org's RevokeStandingRule", err, ports.ErrNotFound)

	same := f.rule(labs, key(domain.DefaultEnvironment, server, toolComment, architect), admin)
	f.expect(same.ID != r.ID, "another org's rule of the same key is acme's rule")

	active := f.rules(acme, false)
	f.expect(len(active) == 1 && active[0].ID == r.ID && active[0].Active(), "acme's rules changed through labs: %+v", active)
	f.one(acme, domain.AuditStandingRuleCreated)
}

func testNoOrg(f *fixture) {
	ctx := f.t.Context()

	_, err := f.r.StandingRules(ctx, true)
	f.refused("StandingRules", err, domain.ErrNoOrg)
	_, err = f.r.CreateServerStandingRule(ctx, key(domain.DefaultEnvironment, server, toolComment, architect), admin)
	f.refused("CreateServerStandingRule", err, domain.ErrNoOrg)
	_, err = f.r.RevokeStandingRule(ctx, noRule, admin)
	f.refused("RevokeStandingRule", err, domain.ErrNoOrg)
}
