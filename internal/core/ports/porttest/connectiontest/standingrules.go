package connectiontest

import (
	"reflect"
	"slices"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Tools, agents and the item the tool mode and standing rule cases use.
const (
	toolComment = "create_comment"
	toolIssue   = "get_issue"
	toolDelete  = "delete_repo"
	toolGone    = "old_tool"
	architect   = "architect"
	reviewer    = "reviewer"
	approvedOn  = domain.ItemID("00000000-0000-7000-8000-0000000000bb")
)

func testRecordTestTools(f *fixture) {
	acme := f.NewOrg(f.t)
	ctx := f.in(acme)
	f.docs(acme)

	f.expect(len(f.get(acme, connDocs).ToolModes) == 0, "a new connection's tool modes = %v; want none", f.get(acme, connDocs).ToolModes)

	listed := domain.ConnectionTest{OK: true, Tools: []string{toolIssue, toolComment, toolIssue}}
	if err := f.c.RecordTest(ctx, connDocs, listed, admin); err != nil {
		f.t.Fatal(err)
	}

	got := f.get(acme, connDocs).LastTest
	f.expect(got != nil && reflect.DeepEqual(got.Tools, []string{toolComment, toolIssue}),
		"the last test's tools = %+v; want them sorted without repeats", got)

	if err := f.c.RecordTest(ctx, connDocs, domain.ConnectionTest{OK: true}, admin); err != nil {
		f.t.Fatal(err)
	}

	got = f.get(acme, connDocs).LastTest
	f.expect(got != nil && got.Tools == nil, "the last test's tools after a test that listed none = %+v; want nil", got)
}

func testUpdateToolModes(f *fixture) {
	acme := f.NewOrg(f.t)
	ctx := f.in(acme)
	f.docs(acme)

	if err := f.c.RecordTest(ctx, connDocs, domain.ConnectionTest{OK: true, Tools: []string{toolComment, toolIssue}}, admin); err != nil {
		f.t.Fatal(err)
	}

	modes := map[string]domain.ConnectionToolMode{toolComment: domain.ToolModeAsk, toolDelete: domain.ToolModeOff}

	got, err := f.c.Update(ctx, connDocs, domain.ConnectionPatch{ToolModes: modes}, editor)
	if err != nil {
		f.t.Fatalf("Update tool modes: %v", err)
	}

	f.expect(reflect.DeepEqual(got.ToolModes, modes), "Update's tool modes = %v; want %v", got.ToolModes, modes)
	f.expect(reflect.DeepEqual(f.get(acme, connDocs).ToolModes, modes), "Get's tool modes = %v; want %v, delete_repo kept though no test listed it",
		f.get(acme, connDocs).ToolModes, modes)

	e := f.one(acme, domain.AuditConnectionUpdated)
	f.expect(e.Actor.ID == editor.ID && slices.Equal(anyStrings(e.Data["tools"]), []string{toolComment, toolDelete}),
		"connection.updated = %+v; want the editor and the tools with a mode", e)

	replaced := map[string]domain.ConnectionToolMode{toolIssue: domain.ToolModeAllow, toolGone: domain.ToolModeAsk}
	if _, err := f.c.Update(ctx, connDocs, domain.ConnectionPatch{ToolModes: replaced}, editor); err != nil {
		f.t.Fatal(err)
	}

	f.expect(reflect.DeepEqual(f.get(acme, connDocs).ToolModes, replaced), "the modes after a second Update = %v; want only %v",
		f.get(acme, connDocs).ToolModes, replaced)

	desc := "the docs"
	if _, err := f.c.Update(ctx, connDocs, domain.ConnectionPatch{Description: &desc}, editor); err != nil {
		f.t.Fatal(err)
	}

	if err := f.c.RecordTest(ctx, connDocs, domain.ConnectionTest{OK: true, Tools: []string{toolIssue}}, admin); err != nil {
		f.t.Fatal(err)
	}

	f.expect(reflect.DeepEqual(f.get(acme, connDocs).ToolModes, replaced), "the modes after an Update without them and a test = %v; want %v unchanged",
		f.get(acme, connDocs).ToolModes, replaced)

	for name, bad := range map[string]map[string]domain.ConnectionToolMode{
		"an unknown mode":      {toolComment: "deny"},
		"a malformed tool":     {"create comment": domain.ToolModeAsk},
		"an empty tool's name": {"": domain.ToolModeOff},
	} {
		_, err := f.c.Update(ctx, connDocs, domain.ConnectionPatch{ToolModes: bad}, editor)
		f.refused("Update with "+name, err, domain.ErrInvalid)
	}

	f.expect(reflect.DeepEqual(f.get(acme, connDocs).ToolModes, replaced), "a refused Update changed the modes to %v", f.get(acme, connDocs).ToolModes)
}

// rule creates a standing rule for agent to call tool of the connection name, failing on an error.
func (f *fixture) rule(org domain.OrgID, name, tool, agent string, by domain.Actor) domain.StandingRule {
	f.t.Helper()

	got, err := f.c.CreateStandingRule(f.in(org), domain.NewStandingRule{Connection: name, Tool: tool, Agent: agent, Item: approvedOn}, by)
	if err != nil {
		f.t.Fatalf("CreateStandingRule %s %s %s: %v", name, tool, agent, err)
	}

	return got
}

// rules are org's standing rules' ids, oldest first.
func (f *fixture) rules(org domain.OrgID, includeRevoked bool) []domain.StandingRule {
	f.t.Helper()

	got, err := f.c.StandingRules(f.in(org), includeRevoked)
	if err != nil {
		f.t.Fatalf("StandingRules: %v", err)
	}

	return got
}

func testCreateStandingRule(f *fixture) {
	acme := f.NewOrg(f.t)
	ctx := f.in(acme)
	docs := f.docs(acme)

	r := f.rule(acme, connDocs, toolComment, architect, admin)
	f.expect(r.ID != "" && r.ConnectionID == docs.ID && r.Connection == connDocs && r.Tool == toolComment && r.Agent == architect &&
		r.ItemID == approvedOn && r.CreatedBy.ID == admin.ID && !r.CreatedAt.IsZero() && r.Active(),
		"CreateStandingRule = %+v; want an active rule of docs' id for architect", r)

	e := f.one(acme, domain.AuditStandingRuleCreated)
	f.expect(e.TargetKind == domain.AuditTargetStandingRule && e.TargetID == string(r.ID) && e.Actor.ID == admin.ID &&
		e.Data["connection"] == connDocs && e.Data["tool"] == toolComment && e.Data["agent"] == architect,
		"standing_rule.created = %+v", e)

	again := f.rule(acme, connDocs, toolComment, architect, editor)
	f.expect(again.ID == r.ID && again.CreatedBy.ID == admin.ID, "CreateStandingRule of an active key = %+v; want the rule %s", again, r.ID)
	f.one(acme, domain.AuditStandingRuleCreated)

	other := f.rule(acme, connDocs, toolComment, reviewer, admin)
	f.expect(other.ID != r.ID, "a rule for another agent is the same rule")

	_, err := f.c.CreateStandingRule(ctx, domain.NewStandingRule{Connection: "nothing", Tool: toolComment, Agent: architect, Item: approvedOn}, admin)
	f.refused("CreateStandingRule of no connection", err, ports.ErrNotFound)

	_, err = f.c.CreateStandingRule(ctx, domain.NewStandingRule{Connection: connDocs, Tool: "", Agent: architect, Item: approvedOn}, admin)
	f.refused("CreateStandingRule without a tool", err, domain.ErrInvalid)

	_, err = f.c.CreateStandingRule(ctx, domain.NewStandingRule{Connection: connDocs, Tool: toolComment, Agent: "Architect", Item: approvedOn}, admin)
	f.refused("CreateStandingRule of a malformed agent", err, domain.ErrInvalid)

	f.expect(len(f.rules(acme, true)) == 2, "rules after refused creates = %+v; want two", f.rules(acme, true))
}

func testRevokeStandingRule(f *fixture) {
	acme := f.NewOrg(f.t)
	ctx := f.in(acme)
	f.docs(acme)
	r := f.rule(acme, connDocs, toolComment, architect, admin)
	kept := f.rule(acme, connDocs, toolIssue, architect, admin)

	got, err := f.c.RevokeStandingRule(ctx, r.ID, editor)
	if err != nil {
		f.t.Fatalf("RevokeStandingRule: %v", err)
	}

	f.expect(got.ID == r.ID && !got.Active() && got.RevokedAt != nil && got.RevokedBy != nil && got.RevokedBy.ID == editor.ID &&
		got.Tool == toolComment && got.Connection == connDocs,
		"RevokeStandingRule = %+v; want the rule revoked by the editor", got)

	e := f.one(acme, domain.AuditStandingRuleRevoked)
	f.expect(e.TargetKind == domain.AuditTargetStandingRule && e.TargetID == string(r.ID) && e.Actor.ID == editor.ID,
		"standing_rule.revoked = %+v", e)

	_, err = f.c.RevokeStandingRule(ctx, r.ID, editor)
	f.refused("RevokeStandingRule again", err, ports.ErrConflict)
	_, err = f.c.RevokeStandingRule(ctx, "00000000-0000-7000-8000-0000000000cc", editor)
	f.refused("RevokeStandingRule of no rule", err, ports.ErrNotFound)
	f.one(acme, domain.AuditStandingRuleRevoked)

	active := f.rules(acme, false)
	f.expect(len(active) == 1 && active[0].ID == kept.ID, "active rules = %+v; want only %s", active, kept.ID)

	all := f.rules(acme, true)
	f.expect(len(all) == 2 && all[0].ID == r.ID && !all[0].Active() && all[0].RevokedBy.ID == editor.ID && all[1].ID == kept.ID,
		"every rule = %+v; want the revoked one first, then the active one", all)

	renewed := f.rule(acme, connDocs, toolComment, architect, admin)
	f.expect(renewed.ID != r.ID && renewed.Active(), "a rule for a revoked rule's key = %+v; want a new active one", renewed)
}

func testDeleteRevokesStandingRules(f *fixture) {
	acme := f.NewOrg(f.t)
	ctx := f.in(acme)
	docs := f.docs(acme)
	f.git(acme, sealedValue)
	r := f.rule(acme, connDocs, toolComment, architect, admin)
	gitRule := f.rule(acme, connGit, toolIssue, architect, admin)

	if err := f.c.Delete(ctx, connDocs, editor); err != nil {
		f.t.Fatal(err)
	}

	active := f.rules(acme, false)
	f.expect(len(active) == 1 && active[0].ID == gitRule.ID, "active rules after the docs connection's delete = %+v; want only git's", active)

	all := f.rules(acme, true)
	f.expect(len(all) == 2 && all[0].ID == r.ID && all[0].ConnectionID == docs.ID && all[0].Connection == "" &&
		all[0].RevokedBy != nil && all[0].RevokedBy.ID == editor.ID && all[0].RevokedAt != nil,
		"the deleted connection's rule = %+v; want it revoked by the deleter, its connection's name gone", all)

	e := f.one(acme, domain.AuditStandingRuleRevoked)
	f.expect(e.TargetID == string(r.ID) && e.Actor.ID == editor.ID, "standing_rule.revoked on delete = %+v", e)

	f.docs(acme)
	f.expect(len(f.rules(acme, false)) == 1, "a new connection of the deleted one's name has its rules: %+v", f.rules(acme, false))
}

func testStandingRulesIsolation(f *fixture) {
	acme, labs := f.NewOrg(f.t), f.NewOrg(f.t)
	f.docs(acme)
	r := f.rule(acme, connDocs, toolComment, architect, admin)

	f.expect(len(f.rules(labs, true)) == 0, "another org's StandingRules = %+v; want nothing", f.rules(labs, true))

	_, err := f.c.RevokeStandingRule(f.in(labs), r.ID, admin)
	f.refused("another org's RevokeStandingRule", err, ports.ErrNotFound)

	_, err = f.c.CreateStandingRule(f.in(labs), domain.NewStandingRule{Connection: connDocs, Tool: toolComment, Agent: architect, Item: approvedOn}, admin)
	f.refused("another org's CreateStandingRule", err, ports.ErrNotFound)

	f.expect(f.rules(acme, false)[0].Active(), "acme's rule changed through labs")
}
