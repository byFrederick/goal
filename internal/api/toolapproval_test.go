package api_test

import (
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The Ask-first call an agent made, as the gateway reports it.
const (
	approvalConnection = "repo"
	approvalTool       = "create_comment"
	approvalInput      = `{"body":"LGTM","issue":7}`
	// githubTokenField is the github kind's secret field.
	githubTokenField = "token"
	// freshOwnerSession is the owner's session signed in just now.
	freshOwnerSession = "fresh-owner-now"
)

// toolAsked seeds a line item whose agent asked at build to call repo's create_comment, offering
// Always allow when offered: it waits there for a decision, as the engine leaves it.
func (h *harness) toolAsked(t *testing.T, offered bool) domain.Item {
	t.Helper()

	it := h.seed(t, stBuild, domain.StateInProgress, t0.Add(-time.Hour))

	return h.move(t, it, domain.Position{Station: stBuild, State: domain.StateWaitingHuman}, func(tr *ports.Transition) {
		tr.Events = []domain.Event{{
			FactoryID: it.FactoryID, ItemID: it.ID, At: h.clock.Now(), Kind: domain.EventToolApprovalRequested, Station: stBuild,
			Actor: agBackend, RunID: askingRun,
			Data: map[string]any{
				domain.KeyConnection: approvalConnection, domain.KeyTool: approvalTool,
				domain.KeyInput: map[string]any{"issue": 7, "body": "LGTM"}, domain.KeyAgent: agBackend, domain.KeyRunID: askingRun,
				domain.KeyAlwaysAllowOffered: offered,
			},
		}}
	})
}

// withRepo adds the organization connection repo, whose tool the agent asked to call.
func (h *harness) withRepo(t *testing.T) {
	t.Helper()

	if _, err := h.conns.Create(inOrg(t), domain.NewConnection{
		Name: approvalConnection, Kind: kindGitHub, Secrets: map[string]string{githubTokenField: "ghp_test"},
	}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}
}

func toolApprovalPath(it domain.Item) string {
	return "/v1/orgs/acme/items/" + url.PathEscape(string(it.ID)) + "/tool-approval"
}

// signInOwnerNow gives the owner the session freshOwnerToken, signed in just now.
func (h signedIn) signInOwnerNow() {
	h.identity.AddSession(freshOwnerToken, ownerID, domain.Session{ID: freshOwnerSession, CreatedAt: t0, ExpiresAt: t0.Add(time.Hour), Browser: true})
}

// checkRule checks r is backend's active rule on repo's create_comment, approved on it.
func checkRule(t *testing.T, rules []domain.StandingRule, it domain.Item) {
	t.Helper()

	if len(rules) != 1 || rules[0].Connection != approvalConnection || rules[0].Tool != approvalTool ||
		rules[0].Agent != agBackend || rules[0].ItemID != it.ID || !rules[0].Active() {
		t.Errorf("rules %+v; want one active rule for backend on repo's create_comment", rules)
	}
}

// decisionData is a decision event's data without the approval id and the actor, which it
// must have.
func decisionData(t *testing.T, e domain.Event) string {
	t.Helper()

	if e.Data[domain.KeyApprovalID] == nil {
		t.Errorf("event %s %v; want the approval id", e.Kind, e.Data)
	}

	data := map[string]any{}

	for k, v := range e.Data {
		if k != domain.KeyApprovalID && k != "actor" {
			data[k] = v
		}
	}

	return asJSON(t, data)
}

// rules are the organization's standing rules, the revoked ones too.
func (h *harness) rules(t *testing.T) []domain.StandingRule {
	t.Helper()

	rules, err := h.conns.StandingRules(inOrg(t), true)
	if err != nil {
		t.Fatal(err)
	}

	return rules
}

// lastEvent is an item's latest event.
func (h *harness) lastEvent(t *testing.T, it domain.Item) domain.Event {
	t.Helper()

	events, err := h.store.ItemEvents(inOrg(t), it.ID, 0, 1000)
	if err != nil || len(events) == 0 {
		t.Fatalf("events of %s = %v, %v", it.ID, events, err)
	}

	return events[len(events)-1]
}

// getItem carries the open tool approval, with the asking agent's role name, until it is decided.
func TestAPI_ItemShowsItsOpenToolApproval(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	it := h.toolAsked(t, true)
	path := "/v1/orgs/acme/items/" + url.PathEscape(string(it.ID))

	res := h.request(t, http.MethodGet, path, "", nil)
	got := decode[gen.Item](t, res, http.StatusOK)

	if got.ToolApproval == nil {
		t.Fatalf("item %s; want its open tool approval", res.body)
	}

	a := *got.ToolApproval
	if a.Id == 0 || a.Connection != approvalConnection || a.Tool != approvalTool || asJSON(t, a.Input) != approvalInput ||
		a.Agent != agBackend || a.RunId != askingRun || !a.AskedAt.Equal(h.clock.Now()) || !a.AlwaysAllowOffered {
		t.Errorf("tool approval %+v; want repo's create_comment with its input, asked by backend in the asking run", a)
	}

	decode[gen.Item](t, h.request(t, http.MethodPost, toolApprovalPath(it), `{"decision":"deny"}`, nil), http.StatusOK)

	res = h.request(t, http.MethodGet, path, "", nil)
	if got := decode[gen.Item](t, res, http.StatusOK); got.ToolApproval != nil || strings.Contains(string(res.body), `"tool_approval"`) {
		t.Errorf("tool approval after the decision %+v (%s); want none", got.ToolApproval, res.body)
	}
}

// A decision puts the item back in its station's queue without rework and records it; Always
// allow also creates the standing rule.
func TestAPI_ToolApprovalDecides(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, body string
		kind       domain.EventKind
		data       string
		rule       bool
	}{
		{name: "approve once", body: `{"decision":"approve"}`, kind: domain.EventToolApprovalGranted, data: `{"always_allow":false}`},
		{name: "always allow", body: `{"decision":"approve","always_allow":true}`, kind: domain.EventToolApprovalGranted, data: `{"always_allow":true}`, rule: true},
		{name: "deny with a note", body: `{"decision":"deny","note":"  Not on that issue.  "}`, kind: domain.EventToolApprovalDenied, data: `{"note":"Not on that issue."}`},
		{name: "deny", body: `{"decision":"deny"}`, kind: domain.EventToolApprovalDenied, data: `{}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			h.withRepo(t)
			it := h.toolAsked(t, true)

			got := decode[gen.Item](t, h.request(t, http.MethodPost, toolApprovalPath(it), tt.body, nil), http.StatusOK)
			if got.Station != stBuild || got.State != gen.ItemStateQueued || got.ReworkCount != 0 || got.ToolApproval != nil {
				t.Errorf("item %s/%s rework %d approval %+v; want queued at build, no rework, no approval",
					got.Station, got.State, got.ReworkCount, got.ToolApproval)
			}

			if e := h.lastEvent(t, it); e.Kind != tt.kind || decisionData(t, e) != tt.data {
				t.Errorf("event %s %v; want %s with %s", e.Kind, e.Data, tt.kind, tt.data)
			}

			if rules := h.rules(t); tt.rule {
				checkRule(t, rules, it)
			} else if len(rules) != 0 {
				t.Errorf("rules %+v; want none", rules)
			}
		})
	}
}

func TestAPI_ToolApprovalRefusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.withRepo(t)
	open := h.toolAsked(t, true)
	notOffered := h.toolAsked(t, false)
	none := h.seed(t, stBuild, domain.StateWaitingHuman, t0)
	question := h.asked(t, "Organization", "Factory")
	cancelled := h.toolAsked(t, true)

	if _, err := h.engine.Cancel(inOrg(t), cancelled.ID, domain.LocalUser, ""); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name, body string
		item       domain.Item
		want       int
		code       string
	}{
		{name: "no body", item: open, want: http.StatusBadRequest},
		{name: "no decision", body: `{}`, item: open, want: http.StatusBadRequest},
		{name: "an unknown decision", body: `{"decision":"maybe"}`, item: open, want: http.StatusBadRequest},
		{name: "always allow with deny", body: `{"decision":"deny","always_allow":true}`, item: open, want: http.StatusBadRequest},
		{name: "a note with approve", body: `{"decision":"approve","note":"ok"}`, item: open, want: http.StatusBadRequest},
		{name: "an empty note", body: `{"decision":"deny","note":""}`, item: open, want: http.StatusBadRequest},
		{name: "a blank note", body: `{"decision":"deny","note":" \n "}`, item: open, want: http.StatusBadRequest},
		{name: "a note too long", body: `{"decision":"deny","note":"` + strings.Repeat("é", 2001) + `"}`, item: open, want: http.StatusBadRequest},
		{name: "always allow not offered", body: `{"decision":"approve","always_allow":true}`, item: notOffered, want: http.StatusConflict, code: "always-allow-not-offered"},
		{name: "no open approval", body: `{"decision":"approve"}`, item: none, want: http.StatusConflict, code: "no-open-tool-approval"},
		{name: "an open question", body: `{"decision":"deny"}`, item: question, want: http.StatusConflict, code: "no-open-tool-approval"},
		{name: "a cancelled item", body: `{"decision":"approve"}`, item: cancelled, want: http.StatusConflict, code: codeItemClosed},
	}

	for _, tt := range tests {
		res := h.request(t, http.MethodPost, toolApprovalPath(tt.item), tt.body, nil)
		if p := decode[gen.Problem](t, res, tt.want); deref(p.Code) != tt.code {
			t.Errorf("%s: code %q; want %q (%s)", tt.name, deref(p.Code), tt.code, deref(p.Detail))
		}
	}

	for _, it := range []domain.Item{open, notOffered} {
		got, err := h.store.Item(inOrg(t), it.ID)
		if err != nil {
			t.Fatal(err)
		}

		if got.State != domain.StateWaitingHuman {
			t.Errorf("item %s; refused decisions change nothing", got.State)
		}
	}

	if rules := h.rules(t); len(rules) != 0 {
		t.Errorf("rules %+v; refused decisions create none", rules)
	}
}

// Deciding needs item:decide; Always allow also needs connection:manage, from a recent sign-in,
// and a refusal leaves the item waiting with no rule.
func TestAPI_ToolApprovalAlwaysAllowNeedsConnectionManage(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)
	h.withRepo(t)
	must(t, h.identity.GrantFactoryRole(testOrg, viewerID, lineID, domain.FactoryViewer))

	it := h.toolAsked(t, true)
	always := `{"decision":"approve","always_allow":true}`

	if p := problem(t, h.as(t, viewerToken, http.MethodPost, toolApprovalPath(it), `{"decision":"approve"}`)); p.Status != http.StatusForbidden {
		t.Errorf("a factory viewer approving = %+v; want 403", p)
	}

	must(t, h.identity.GrantFactoryRole(testOrg, viewerID, lineID, domain.FactoryApprover))

	if p := problem(t, h.as(t, viewerToken, http.MethodPost, toolApprovalPath(it), always)); p.Status != http.StatusForbidden {
		t.Errorf("an approver without connection:manage allowing always = %+v; want 403", p)
	}

	if p := problem(t, h.as(t, ownerToken, http.MethodPost, toolApprovalPath(it), always)); p.Status != http.StatusForbidden || deref(p.Code) != codeReauth {
		t.Errorf("the owner signed in an hour ago allowing always = %+v; want 403 %s", p, codeReauth)
	}

	if got, err := h.store.Item(inOrg(t), it.ID); err != nil || got.State != domain.StateWaitingHuman || len(h.rules(t)) != 0 {
		t.Fatalf("item %s (%v), rules %+v; refused Always allow changes nothing", got.State, err, h.rules(t))
	}

	h.signInOwnerNow()

	if res := h.as(t, freshOwnerToken, http.MethodPost, toolApprovalPath(it), always); res.status != http.StatusOK {
		t.Fatalf("the owner signed in just now allowing always = %d %s; want 200", res.status, res.body)
	}

	if rules := h.rules(t); len(rules) != 1 || rules[0].CreatedBy.ID != string(ownerID) {
		t.Errorf("rules %+v; want the owner's one", rules)
	}

	// Without Always allow, an approver decides.
	other := h.toolAsked(t, true)
	if res := h.as(t, viewerToken, http.MethodPost, toolApprovalPath(other), `{"decision":"deny","note":"no"}`); res.status != http.StatusOK {
		t.Errorf("an approver denying = %d %s; want 200", res.status, res.body)
	}
}

// Approving the item while its tool approval is open is refused: the item waits on the call.
func TestAPI_ApproveRefusedWhileToolApprovalOpen(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	it := h.toolAsked(t, true)

	res := h.request(t, http.MethodPost, "/v1/orgs/acme/items/"+url.PathEscape(string(it.ID))+"/approve", "", nil)
	if p := decode[gen.Problem](t, res, http.StatusConflict); deref(p.Code) != "tool-approval-open" {
		t.Errorf("approve = %+v; want 409 tool-approval-open", p)
	}
}

const standingRulesPath = "/v1/orgs/acme/standing-rules"

// alwaysAllowed approves a new tool approval with Always allow, signed in with header,
// returning its rule.
func (h *harness) alwaysAllowed(t *testing.T, header http.Header) gen.StandingRule {
	t.Helper()

	it := h.toolAsked(t, true)
	decode[gen.Item](t, h.request(t, http.MethodPost, toolApprovalPath(it), `{"decision":"approve","always_allow":true}`, header), http.StatusOK)

	list := decode[gen.StandingRuleList](t, h.request(t, http.MethodGet, standingRulesPath, "", header), http.StatusOK)
	if len(list.Rules) == 0 {
		t.Fatal("no standing rule after Always allow")
	}

	return list.Rules[len(list.Rules)-1]
}

// checkListedRule checks rule is the local user's active rule for backend on repo's
// create_comment.
func checkListedRule(t *testing.T, rule gen.StandingRule) {
	t.Helper()

	if rule.Id == "" || rule.ConnectionId == "" || deref(rule.Connection) != approvalConnection || rule.Tool != approvalTool ||
		rule.Agent != agBackend || rule.CreatedBy.Id != domain.LocalUser.ID || rule.ItemId == "" || rule.RevokedAt != nil || rule.RevokedBy != nil {
		t.Errorf("rule %+v; want backend's active rule on repo's create_comment", rule)
	}
}

// The list has the active rules, and the revoked ones on request; revoking returns the rule.
func TestAPI_StandingRulesListAndRevoke(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	if list := decode[gen.StandingRuleList](t, h.request(t, http.MethodGet, standingRulesPath, "", nil), http.StatusOK); list.Rules == nil || len(list.Rules) != 0 {
		t.Errorf("rules before any = %+v; want an empty list", list.Rules)
	}

	h.withRepo(t)
	rule := h.alwaysAllowed(t, nil)

	checkListedRule(t, rule)

	revoked := decode[gen.StandingRule](t, h.request(t, http.MethodDelete, standingRulesPath+"/"+rule.Id, "", nil), http.StatusOK)
	if revoked.Id != rule.Id || revoked.RevokedAt == nil || revoked.RevokedBy == nil || revoked.RevokedBy.Id != domain.LocalUser.ID {
		t.Errorf("revoked %+v; want the rule, revoked by the caller", revoked)
	}

	if list := decode[gen.StandingRuleList](t, h.request(t, http.MethodGet, standingRulesPath, "", nil), http.StatusOK); len(list.Rules) != 0 {
		t.Errorf("active rules after the revoke = %+v; want none", list.Rules)
	}

	all := decode[gen.StandingRuleList](t, h.request(t, http.MethodGet, standingRulesPath+"?revoked=true", "", nil), http.StatusOK)
	if len(all.Rules) != 1 || !reflect.DeepEqual(all.Rules[0], revoked) {
		t.Errorf("all rules = %+v; want the revoked one", all.Rules)
	}
}

func TestAPI_StandingRulesRefusals(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)
	h.withRepo(t)
	h.signInOwnerNow()
	rule := h.alwaysAllowed(t, cookie(freshOwnerToken))

	// Every member reads them; only who manages connections revokes them.
	if res := h.as(t, viewerToken, http.MethodGet, standingRulesPath, ""); res.status != http.StatusOK {
		t.Errorf("a member listing = %d %s; want 200", res.status, res.body)
	}

	if p := problem(t, h.as(t, viewerToken, http.MethodDelete, standingRulesPath+"/"+rule.Id, "")); p.Status != http.StatusForbidden {
		t.Errorf("a member revoking = %+v; want 403", p)
	}

	if p := problem(t, h.as(t, freshOwnerToken, http.MethodDelete, standingRulesPath+"/019a4000-0000-7000-8000-0000000000ff", "")); p.Status != http.StatusNotFound {
		t.Errorf("revoking an unknown rule = %+v; want 404", p)
	}

	if res := h.as(t, freshOwnerToken, http.MethodDelete, standingRulesPath+"/"+rule.Id, ""); res.status != http.StatusOK {
		t.Fatalf("the owner revoking = %d %s; want 200", res.status, res.body)
	}

	if p := problem(t, h.as(t, freshOwnerToken, http.MethodDelete, standingRulesPath+"/"+rule.Id, "")); p.Status != http.StatusConflict || deref(p.Code) != "standing-rule-revoked" {
		t.Errorf("revoking again = %+v; want 409 standing-rule-revoked", p)
	}

	if res := h.as(t, viewerToken, http.MethodGet, standingRulesPath+"?revoked=true", ""); res.status != http.StatusOK ||
		len(decode[gen.StandingRuleList](t, res, http.StatusOK).Rules) != 1 {
		t.Errorf("a member listing the revoked = %d %s; want the revoked rule", res.status, res.body)
	}
}
