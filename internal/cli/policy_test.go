package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

// policyGoald serves acme's policy at revision 3 and records the save it was sent.
type policyGoald struct {
	srv *httptest.Server

	mu      sync.Mutex
	view    gen.OrgPolicyView
	ifMatch string
	saved   *gen.OrgPolicyInput
	// bump makes another save land after each read: the policy moves on a revision.
	bump bool
}

func newPolicyGoald(t *testing.T) *policyGoald {
	t.Helper()

	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	g := &policyGoald{view: gen.OrgPolicyView{
		Revision: 3, UpdatedAt: &at,
		Policy: gen.OrgPolicyInput{
			AllowedModels: []string{"claude-opus-5-5", "claude-haiku-5-5"}, DefaultModel: "claude-opus-5-5",
			DeniedCommands: []string{"Bash(terraform apply:*)"}, BoxTimeLimitMinutes: 180,
			Tools: gen.ToolPolicy{WriteOutsideBranch: "off", MergePullRequest: "review_approval", ReadWeb: "ask", UseCredentials: "attached_only"},
		},
		Models: []gen.PolicyModel{
			{Id: "claude-opus-5-5", Label: "Claude Opus 5.5", UsedBy: []gen.ModelUse{
				{FactoryId: "f1", Name: "support", Agents: []string{"responder"}}, {FactoryId: "f2", Name: "web", Agents: []string{"backend", "frontend"}},
			}},
			{Id: "claude-sonnet-5-5", Label: "Claude Sonnet 5.5", UsedBy: []gen.ModelUse{}},
			{Id: "claude-haiku-5-5", Label: "Claude Haiku 5.5", UsedBy: []gen.ModelUse{}},
		},
		BoxSizes: []gen.PolicyBoxSize{
			{Size: gen.Small, Vcpu: 1, MemoryBytes: 2 << 30, Factories: []gen.SizeUse{}},
			{Size: gen.Medium, Vcpu: 2, MemoryBytes: 4 << 30, Factories: []gen.SizeUse{{FactoryId: "f1", Name: "support"}}},
			{Size: gen.Large, Vcpu: 4, MemoryBytes: 8 << 30, Factories: []gen.SizeUse{{FactoryId: "f2", Name: "web"}}},
		},
		EgressAddresses: []string{"203.0.113.0/24"},
	}}

	reply := func(w http.ResponseWriter, v gen.OrgPolicyView) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"`+jsonInt(v.Revision)+`"`)
		_ = json.NewEncoder(w).Encode(v)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/orgs/acme/policy", func(w http.ResponseWriter, _ *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()

		reply(w, g.view)

		if g.bump {
			g.view.Revision++
		}
	})
	mux.HandleFunc("PUT /v1/orgs/acme/policy", func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()

		g.ifMatch = r.Header.Get("If-Match")

		var b gen.OrgPolicyInput
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		g.saved = &b

		if g.ifMatch != `"`+jsonInt(g.view.Revision)+`"` {
			detail := "the policy changed since it was read: read it again"

			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(gen.Problem{Title: "Conflict", Status: http.StatusConflict, Detail: &detail, Code: new("policy-conflict")})

			return
		}

		g.view.Policy = b
		g.view.Revision++
		reply(w, g.view)
	})

	g.srv = httptest.NewServer(mux)
	t.Cleanup(g.srv.Close)

	return g
}

func jsonInt(n int64) string {
	b, _ := json.Marshal(n)

	return string(b)
}

// goalPolicy runs goal policy with args against g.
func (g *policyGoald) goalPolicy(t *testing.T, args ...string) (string, error) {
	t.Helper()

	var buf bytes.Buffer

	err := run(t.Context(), append([]string{"policy"}, args...), &printer{w: &buf}, testEnv(t, g.srv.URL, map[string]string{envOrg: orgAcme}), nil)

	return buf.String(), err
}

// goal policy get prints the policy, who uses each model and box size, and the egress addresses.
func TestPolicy_GetPrintsThePolicyAndItsFacts(t *testing.T) {
	t.Parallel()

	out, err := newPolicyGoald(t).goalPolicy(t, "get")
	if err != nil {
		t.Fatal(err)
	}

	checkGolden(t, "policy-get", out)
}

// goal policy set changes what its flags name, keeps the rest it read (the tool defaults among
// them) and saves on the revision it read.
func TestPolicy_SetKeepsWhatItRead(t *testing.T) {
	t.Parallel()

	g := newPolicyGoald(t)

	out, err := g.goalPolicy(t, "set", "--models", "claude-opus-5-5,claude-sonnet-5-5", "--default", "claude-sonnet-5-5",
		"--deny-command", "Bash(git push --force:*)", "--deny-command", "Bash(rm -rf /:*)", "--box-limit", "90m")
	if err != nil {
		t.Fatal(err)
	}

	if g.ifMatch != `"3"` {
		t.Errorf("If-Match = %q; want the revision read, \"3\"", g.ifMatch)
	}

	want := gen.OrgPolicyInput{
		AllowedModels: []string{"claude-opus-5-5", "claude-sonnet-5-5"}, DefaultModel: "claude-sonnet-5-5",
		DeniedCommands: []string{"Bash(git push --force:*)", "Bash(rm -rf /:*)"}, BoxTimeLimitMinutes: 90,
		Tools: gen.ToolPolicy{WriteOutsideBranch: "off", MergePullRequest: "review_approval", ReadWeb: "ask", UseCredentials: "attached_only"},
	}

	if got, _ := json.Marshal(g.saved); string(got) != mustJSON(t, want) {
		t.Errorf("saved %s; want %s", got, mustJSON(t, want))
	}

	checkGolden(t, "policy-set", out)
}

// A flag the set does not give leaves its value as read: --box-limit alone changes nothing else.
func TestPolicy_SetOneFlagLeavesTheRest(t *testing.T) {
	t.Parallel()

	g := newPolicyGoald(t)

	if _, err := g.goalPolicy(t, "set", "--box-limit", "2h"); err != nil {
		t.Fatal(err)
	}

	read := g.view.Policy
	read.BoxTimeLimitMinutes = 120

	if got, _ := json.Marshal(g.saved); string(got) != mustJSON(t, read) {
		t.Errorf("saved %s; want what was read with a 120-minute limit", got)
	}
}

// goal policy set without a change, with a box limit that is not whole positive minutes, or with
// arguments is a usage error, and sends nothing.
func TestPolicy_SetRefusesBadFlags(t *testing.T) {
	t.Parallel()

	g := newPolicyGoald(t)

	for _, args := range [][]string{{"set"}, {"set", "--box-limit", "0m"}, {"set", "--box-limit", "90s"}, {"set", "--box-limit", "soon"}, {"set", "acme"}, {"get", "x"}, {"frob"}} {
		if _, err := g.goalPolicy(t, args...); err == nil || !strings.HasPrefix(err.Error(), "usage:") {
			t.Errorf("goal policy %v = %v; want the usage", args, err)
		}
	}

	if g.saved != nil {
		t.Errorf("a refused command line saved %+v", g.saved)
	}
}

// A save on a policy that changed since it was read prints goald's refusal.
func TestPolicy_SetStaleIsGoaldsRefusal(t *testing.T) {
	t.Parallel()

	g := newPolicyGoald(t)

	// Another save lands between this command's read and its write.
	g.bump = true

	_, err := g.goalPolicy(t, "set", "--default", "claude-haiku-5-5")
	if err == nil || err.Error() != "Conflict: the policy changed since it was read: read it again" {
		t.Errorf("goal policy set on a stale read = %v; want goald's conflict", err)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()

	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}
