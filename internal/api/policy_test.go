package api_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
)

const (
	policyPath = acmePath + "/policy"
	// The models of the harness's install (GOAL_MODELS), the first the default.
	modelOpus  = "claude-opus-5-5"
	modelHaiku = "claude-haiku-5-5"
	// agentFrontend is line's agent of the web lane, which names its own model.
	agentFrontend = "frontend"

	codePolicyInvalid  = "policy-invalid"
	codePolicyConflict = "policy-conflict"
)

// policyBody is a valid policy: both of the install's models, haiku the default.
const policyBody = `{"allowed_models":["claude-opus-5-5","claude-haiku-5-5"],"default_model":"claude-haiku-5-5",
"denied_commands":["Bash(terraform apply:*)"],
"tools":{"write_outside_branch":"off","merge_pull_request":"review_approval","read_web":"ask","use_credentials":"attached_only"},
"box_time_limit_minutes":90}`

// An organization that saved nothing reads the default policy of the install's models at
// revision 0, with its revision as the ETag, and the install's egress addresses.
func TestGetOrgPolicy_DefaultAtRevisionZero(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	res := h.request(t, http.MethodGet, policyPath, "", nil)
	got := decode[gen.OrgPolicyView](t, res, http.StatusOK)

	if res.header.Get("ETag") != `"0"` || got.Revision != 0 || got.UpdatedAt != nil {
		t.Errorf("ETag %q, revision %d, updated %v; want \"0\", 0 and none", res.header.Get("ETag"), got.Revision, got.UpdatedAt)
	}

	p := got.Policy
	if !slices.Equal(p.AllowedModels, []string{modelOpus, modelHaiku}) || p.DefaultModel != modelOpus || len(p.DeniedCommands) != 0 ||
		p.BoxTimeLimitMinutes != domain.DefaultBoxTimeLimitMinutes {
		t.Errorf("policy = %+v; want every model, opus the default, nothing denied, 180 minutes", p)
	}

	want := gen.ToolPolicy{WriteOutsideBranch: "ask", MergePullRequest: "review_approval", ReadWeb: "allowed", UseCredentials: "attached_only"}
	if p.Tools != want {
		t.Errorf("tools = %+v; want %+v", p.Tools, want)
	}

	if !slices.Equal(got.EgressAddresses, []string{testEgress}) {
		t.Errorf("egress addresses = %v; want [%s]", got.EgressAddresses, testEgress)
	}
}

// Who uses each model and each box size is computed from the published definitions: an agent's
// own model, else its factory's runtime.model, else the policy's default; a factory's size is its
// runtime.size, medium when it names none. A model nobody uses has no users.
func TestGetOrgPolicy_UsageAndSizes(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	got := decode[gen.OrgPolicyView](t, h.request(t, http.MethodGet, policyPath, "", nil), http.StatusOK)

	// acme's writer names no model and acme no runtime.model: the default, opus. line's backend
	// takes line's runtime.model (claude-fable-5-1, not one of the install's), its frontend haiku.
	wantModels := []gen.PolicyModel{
		{Id: modelOpus, Label: "Claude Opus 5.5", UsedBy: []gen.ModelUse{{FactoryId: acmeID, Name: acmeSlug, Agents: []string{"writer"}}}},
		{Id: modelHaiku, Label: "Claude Haiku 5.5", UsedBy: []gen.ModelUse{{FactoryId: lineID, Name: lineSlug, Agents: []string{agentFrontend}}}},
	}

	if !slices.EqualFunc(got.Models, wantModels, equalModel) {
		t.Errorf("models = %+v; want %+v", got.Models, wantModels)
	}

	both := []gen.SizeUse{{FactoryId: acmeID, Name: acmeSlug}, {FactoryId: lineID, Name: lineSlug}}
	wantSizes := []gen.PolicyBoxSize{
		{Size: gen.Small, Vcpu: 1, MemoryBytes: 2 << 30, Factories: []gen.SizeUse{}},
		{Size: gen.Medium, Vcpu: 2, MemoryBytes: 4 << 30, Factories: both},
		{Size: gen.Large, Vcpu: 4, MemoryBytes: 8 << 30, Factories: []gen.SizeUse{}},
	}

	if !slices.EqualFunc(got.BoxSizes, wantSizes, func(a, b gen.PolicyBoxSize) bool {
		return a.Size == b.Size && a.Vcpu == b.Vcpu && a.MemoryBytes == b.MemoryBytes && slices.Equal(a.Factories, b.Factories)
	}) {
		t.Errorf("box sizes = %+v; want %+v", got.BoxSizes, wantSizes)
	}

	// A new default moves the agents that name no model to it.
	put := h.request(t, http.MethodPut, policyPath, policyBody, quoted("0"))
	after := decode[gen.OrgPolicyView](t, put, http.StatusOK)

	if len(after.Models[0].UsedBy) != 0 {
		t.Errorf("opus after haiku became the default is used by %+v; want nobody", after.Models[0].UsedBy)
	}

	wantHaiku := []gen.ModelUse{{FactoryId: acmeID, Name: acmeSlug, Agents: []string{"writer"}}, {FactoryId: lineID, Name: lineSlug, Agents: []string{agentFrontend}}}
	if !slices.EqualFunc(after.Models[1].UsedBy, wantHaiku, equalUse) {
		t.Errorf("haiku is used by %+v; want %+v", after.Models[1].UsedBy, wantHaiku)
	}
}

func equalModel(a, b gen.PolicyModel) bool {
	return a.Id == b.Id && a.Label == b.Label && slices.EqualFunc(a.UsedBy, b.UsedBy, equalUse)
}

func equalUse(a, b gen.ModelUse) bool {
	return a.FactoryId == b.FactoryId && a.Name == b.Name && slices.Equal(a.Agents, b.Agents)
}

// A valid save answers the policy at its next revision, with that revision as the ETag, and a
// read returns it.
func TestPutOrgPolicy_SavesAtTheNextRevision(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	res := h.request(t, http.MethodPut, policyPath, policyBody, quoted("0"))
	got := decode[gen.OrgPolicyView](t, res, http.StatusOK)

	if res.header.Get("ETag") != `"1"` || got.Revision != 1 || got.UpdatedAt == nil {
		t.Errorf("ETag %q, revision %d, updated %v; want \"1\", 1 and a time", res.header.Get("ETag"), got.Revision, got.UpdatedAt)
	}

	read := decode[gen.OrgPolicyView](t, h.request(t, http.MethodGet, policyPath, "", nil), http.StatusOK)
	if p := read.Policy; p.DefaultModel != modelHaiku || !slices.Equal(p.DeniedCommands, []string{"Bash(terraform apply:*)"}) ||
		p.BoxTimeLimitMinutes != 90 || p.Tools.WriteOutsideBranch != "off" || p.Tools.ReadWeb != "ask" || read.Revision != 1 {
		t.Errorf("read after the save = %+v at %d; want the saved policy at 1", p, read.Revision)
	}

	if next := h.request(t, http.MethodPut, policyPath, policyBody, quoted("1")); next.status != http.StatusOK || next.header.Get("ETag") != `"2"` {
		t.Errorf("a save on revision 1 = %d ETag %q; want 200 \"2\"", next.status, next.header.Get("ETag"))
	}
}

// A save without If-Match is 428: without it, it could overwrite a change it never saw.
func TestPutOrgPolicy_WithoutIfMatchIsPreconditionRequired(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	refused(t, "save without If-Match", h.request(t, http.MethodPut, policyPath, policyBody, nil), http.StatusPreconditionRequired, codeNoIfMatch)
	refused(t, "save with an If-Match that is no revision", h.request(t, http.MethodPut, policyPath, policyBody, quoted("abc")), http.StatusBadRequest, "")
}

// A save on a revision that is not the stored one is 409 and changes nothing.
func TestPutOrgPolicy_StaleIfMatchConflicts(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	decode[gen.OrgPolicyView](t, h.request(t, http.MethodPut, policyPath, policyBody, quoted("0")), http.StatusOK)

	other := `{"allowed_models":["claude-opus-5-5"],"default_model":"claude-opus-5-5","denied_commands":[],
"tools":{"write_outside_branch":"ask","merge_pull_request":"off","read_web":"off","use_credentials":"off"},"box_time_limit_minutes":10}`
	refused(t, "save on revision 0", h.request(t, http.MethodPut, policyPath, other, quoted("0")), http.StatusConflict, codePolicyConflict)

	read := decode[gen.OrgPolicyView](t, h.request(t, http.MethodGet, policyPath, "", nil), http.StatusOK)
	if read.Revision != 1 || read.Policy.BoxTimeLimitMinutes != 90 || read.Policy.DefaultModel != modelHaiku {
		t.Errorf("policy after the stale save = %+v at %d; want the first save unchanged", read.Policy, read.Revision)
	}
}

// A model the install does not offer (GOAL_MODELS) is 422, even when the definition could name it.
func TestPutOrgPolicy_ModelNotInstalledIsInvalid(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	body := `{"allowed_models":["claude-opus-5-5","claude-fable-5-1"],"default_model":"claude-opus-5-5","denied_commands":[],
"tools":{"write_outside_branch":"ask","merge_pull_request":"off","read_web":"off","use_credentials":"off"},"box_time_limit_minutes":10}`

	p := refused(t, "a model not installed", h.request(t, http.MethodPut, policyPath, body, quoted("0")), http.StatusUnprocessableEntity, codePolicyInvalid)
	if p.Detail == nil || !strings.Contains(*p.Detail, "claude-fable-5-1") {
		t.Errorf("detail %v; want it to name the model", p.Detail)
	}

	unchanged(t, h)
}

// The default is exactly one of the allowed models: none, another model or a list of two is 422.
func TestPutOrgPolicy_DefaultNotOneAllowedModelIsInvalid(t *testing.T) {
	t.Parallel()

	tools := `"tools":{"write_outside_branch":"ask","merge_pull_request":"off","read_web":"off","use_credentials":"off"},"box_time_limit_minutes":10}`
	tests := map[string]string{
		"no default":            `{"allowed_models":["claude-opus-5-5"],"default_model":"","denied_commands":[],` + tools,
		"a default not allowed": `{"allowed_models":["claude-opus-5-5"],"default_model":"claude-haiku-5-5","denied_commands":[],` + tools,
		"two defaults":          `{"allowed_models":["claude-opus-5-5","claude-haiku-5-5"],"default_model":"claude-opus-5-5,claude-haiku-5-5","denied_commands":[],` + tools,
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			refused(t, name, h.request(t, http.MethodPut, policyPath, body, quoted("0")), http.StatusUnprocessableEntity, codePolicyInvalid)
			unchanged(t, h)
		})
	}
}

// A member reads the policy but may not save one: org:manage is the owner's and admins'.
func TestPutOrgPolicy_MemberIsForbidden(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)

	if res := h.as(t, viewerToken, http.MethodGet, policyPath, ""); res.status != http.StatusOK {
		t.Errorf("a member reading the policy = %d %s; want 200", res.status, res.body)
	}

	header := cookie(viewerToken)
	header.Set("If-Match", `"0"`)
	refused(t, "a member saving", h.request(t, http.MethodPut, policyPath, policyBody, header), http.StatusForbidden, "forbidden")

	if res := h.as(t, ownerToken, http.MethodGet, policyPath, ""); res.header.Get("ETag") != `"0"` {
		t.Errorf("ETag after the member's refusal = %q; want \"0\"", res.header.Get("ETag"))
	}
}

// unchanged checks that the organization still has the default policy.
func unchanged(t *testing.T, h *harness) {
	t.Helper()

	if res := h.request(t, http.MethodGet, policyPath, "", nil); res.header.Get("ETag") != `"0"` {
		t.Errorf("ETag after the refusal = %q; want \"0\": a refusal saves nothing", res.header.Get("ETag"))
	}
}
