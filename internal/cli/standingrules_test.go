package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

// The standing rules rulesGoald holds, each keyed by an environment's MCP server: one active and
// one revoked in the default environment and one active in staging. Revoking ruleGone is a 404
// and revoking ruleRevoked is refused.
const (
	ruleActive  = "rule-1"
	ruleRevoked = "rule-2"
	ruleStaging = "rule-4"
	ruleGone    = "rule-9"
)

type rulesGoald struct {
	mu     sync.Mutex
	called []string
}

func (g *rulesGoald) serve(t *testing.T, at time.Time) *httptest.Server {
	t.Helper()

	pat := gen.Actor{Id: "u-1", Kind: "user", Name: "Pat"}
	sam := gen.Actor{Id: "u-2", Kind: "user", Name: "Sam"}
	rule := func(id, environment, agent, tool string, by gen.Actor) gen.StandingRule {
		return gen.StandingRule{
			Id: id, FactoryId: "f-1", Environment: environment, Server: connGitHub, Agent: agent, Tool: tool, CreatedBy: by, CreatedAt: at,
		}
	}
	active := rule(ruleActive, "default", roleArchitect, toolComment, pat)
	revoked := rule(ruleRevoked, "default", "reviewer", "merge_pull_request", pat)
	revoked.RevokedAt, revoked.RevokedBy = &at, &sam
	staging := rule(ruleStaging, "staging", roleArchitect, "create_issue", sam)

	refuse := func(w http.ResponseWriter, status int, code string) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(status)

		p := gen.Problem{Title: "Refused", Status: status}
		if code != "" {
			p.Code = &code
		}

		_ = json.NewEncoder(w).Encode(p)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/orgs/acme/standing-rules", func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.called = append(g.called, "list "+r.URL.RawQuery)
		g.mu.Unlock()

		rules := []gen.StandingRule{active, staging}
		if r.URL.Query().Get("revoked") == "true" {
			rules = []gen.StandingRule{active, revoked, staging}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(gen.StandingRuleList{Rules: rules})
	})
	mux.HandleFunc("DELETE /v1/orgs/acme/standing-rules/{rule}", func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.called = append(g.called, subRevoke+" "+r.PathValue("rule"))
		g.mu.Unlock()

		switch r.PathValue("rule") {
		case ruleGone:
			refuse(w, http.StatusNotFound, "")
		case ruleRevoked:
			refuse(w, http.StatusConflict, "standing-rule-revoked")
		case ruleStaging:
			out := staging
			out.RevokedAt, out.RevokedBy = &at, &sam

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(out)
		default:
			out := active
			out.RevokedAt, out.RevokedBy = &at, &sam

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(out)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

func TestStandingRules_ListAndRevoke(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	when := at.Local().Format(time.DateTime)
	g := &rulesGoald{}
	env := testEnv(t, g.serve(t, at).URL, map[string]string{envOrg: orgAcme})

	activeLine := ruleActive + "  architect may call default/github create_comment  by Pat at " + when + "\n"
	stagingLine := ruleStaging + "  architect may call staging/github create_issue  by Sam at " + when + "\n"
	revokedLine := ruleRevoked + "  reviewer may call default/github merge_pull_request  by Pat at " + when + "  revoked by Sam at " + when + "\n"

	if got, want := runLines(t, env, []string{cmdStandingRules, subList}), activeLine+stagingLine; got != want {
		t.Errorf("list printed %q; want %q", got, want)
	}

	if got, want := runLines(t, env, []string{cmdStandingRules, subList, "--revoked"}), activeLine+revokedLine+stagingLine; got != want {
		t.Errorf("list --revoked printed %q; want %q", got, want)
	}

	var listed gen.StandingRuleList
	if got := runLines(t, env, []string{cmdStandingRules, subList, "--json"}); json.Unmarshal([]byte(got), &listed) != nil || len(listed.Rules) != 2 {
		t.Errorf("list --json printed %q; want goald's answer", got)
	}

	want := "✓ revoked " + ruleActive + ": architect asks again before calling default/github create_comment\n"
	if got := runLines(t, env, []string{cmdStandingRules, subRevoke, ruleActive}); got != want {
		t.Errorf("revoke printed %q; want %q", got, want)
	}

	want = "✓ revoked " + ruleStaging + ": architect asks again before calling staging/github create_issue\n"
	if got := runLines(t, env, []string{cmdStandingRules, subRevoke, ruleStaging}); got != want {
		t.Errorf("revoke of staging's rule printed %q; want %q", got, want)
	}

	wantRefusal(t, env, []string{cmdStandingRules, subRevoke, ruleGone}, "no standing rule "+ruleGone)
	wantRefusal(t, env, []string{cmdStandingRules, subRevoke, ruleRevoked}, "standing rule "+ruleRevoked+" is already revoked")

	wantCalled := []string{"list ", "list revoked=true", "list ", subRevoke + " " + ruleActive, subRevoke + " " + ruleStaging, subRevoke + " " + ruleGone, subRevoke + " " + ruleRevoked}
	if !slices.Equal(g.called, wantCalled) {
		t.Errorf("goald got %v; want %v", g.called, wantCalled)
	}

	wantUsage(t, env, []string{cmdStandingRules}, []string{cmdStandingRules, subShow}, []string{cmdStandingRules, subList, "x"},
		[]string{cmdStandingRules, subRevoke}, []string{cmdStandingRules, subRevoke, "a", "b"}, []string{cmdStandingRules, subRevoke, "  "})
}
