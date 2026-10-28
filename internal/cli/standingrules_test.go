package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

// The standing rules rulesGoald holds: one active, one revoked, and one active on a deleted
// connection. Revoking ruleGone is a 404 and revoking ruleRevoked is refused.
const (
	ruleActive  = "rule-1"
	ruleRevoked = "rule-2"
	ruleDeleted = "rule-3"
	ruleGone    = "rule-9"
)

type rulesGoald struct {
	mu     sync.Mutex
	called []string
}

func (g *rulesGoald) serve(t *testing.T, at time.Time) *httptest.Server {
	t.Helper()

	conn := connGitHub
	pat := gen.Actor{Id: "u-1", Kind: "user", Name: "Pat"}
	sam := gen.Actor{Id: "u-2", Kind: "user", Name: "Sam"}
	active := gen.StandingRule{Id: ruleActive, Agent: roleArchitect, Connection: &conn, Tool: toolComment, CreatedBy: pat, CreatedAt: at}
	revoked := gen.StandingRule{
		Id: ruleRevoked, Agent: "reviewer", Connection: &conn, Tool: "merge_pull_request", CreatedBy: pat, CreatedAt: at,
		RevokedAt: &at, RevokedBy: &sam,
	}
	deleted := gen.StandingRule{Id: ruleDeleted, Agent: roleArchitect, Tool: "send_message", CreatedBy: sam, CreatedAt: at}

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

		rules := []gen.StandingRule{active, deleted}
		if r.URL.Query().Get("revoked") == "true" {
			rules = []gen.StandingRule{active, revoked, deleted}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(gen.StandingRuleList{Rules: rules})
	})
	mux.HandleFunc("DELETE /v1/orgs/acme/standing-rules/{rule}", func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.called = append(g.called, "revoke "+r.PathValue("rule"))
		g.mu.Unlock()

		switch r.PathValue("rule") {
		case ruleGone:
			refuse(w, http.StatusNotFound, "")
		case ruleRevoked:
			refuse(w, http.StatusConflict, "standing-rule-revoked")
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

	var out bytes.Buffer
	if err := run(t.Context(), []string{"standing-rules", subList}, &printer{w: &out}, env, nil); err != nil {
		t.Fatal(err)
	}

	activeLine := ruleActive + "  architect may call github create_comment  by Pat at " + when + "\n"
	deletedLine := ruleDeleted + "  architect may call (deleted) send_message  by Sam at " + when + "\n"
	revokedLine := ruleRevoked + "  reviewer may call github merge_pull_request  by Pat at " + when + "  revoked by Sam at " + when + "\n"

	if want := activeLine + deletedLine; out.String() != want {
		t.Errorf("list printed %q; want %q", out.String(), want)
	}

	out.Reset()

	if err := run(t.Context(), []string{"standing-rules", subList, "--revoked"}, &printer{w: &out}, env, nil); err != nil {
		t.Fatal(err)
	}

	if want := activeLine + revokedLine + deletedLine; out.String() != want {
		t.Errorf("list --revoked printed %q; want %q", out.String(), want)
	}

	out.Reset()

	if err := run(t.Context(), []string{"standing-rules", subList, "--json"}, &printer{w: &out}, env, nil); err != nil {
		t.Fatal(err)
	}

	var listed gen.StandingRuleList
	if err := json.Unmarshal(out.Bytes(), &listed); err != nil || len(listed.Rules) != 2 {
		t.Errorf("list --json printed %q (%v); want goald's answer", out.String(), err)
	}

	out.Reset()

	if err := run(t.Context(), []string{"standing-rules", "revoke", ruleActive}, &printer{w: &out}, env, nil); err != nil {
		t.Fatal(err)
	}

	if want := "✓ revoked " + ruleActive + ": architect asks again before calling github create_comment\n"; out.String() != want {
		t.Errorf("revoke printed %q; want %q", out.String(), want)
	}

	for _, tt := range []struct {
		rule string
		want string
	}{
		{rule: ruleGone, want: "no standing rule " + ruleGone},
		{rule: ruleRevoked, want: "standing rule " + ruleRevoked + " is already revoked"},
	} {
		err := run(t.Context(), []string{"standing-rules", "revoke", tt.rule}, &printer{w: &bytes.Buffer{}}, env, nil)
		if err == nil || err.Error() != tt.want {
			t.Errorf("revoke %s = %v; want %q", tt.rule, err, tt.want)
		}
	}

	if want := []string{"list ", "list revoked=true", "list ", "revoke " + ruleActive, "revoke " + ruleGone, "revoke " + ruleRevoked}; !slices.Equal(g.called, want) {
		t.Errorf("goald got %v; want %v", g.called, want)
	}

	for _, args := range [][]string{
		{"standing-rules"}, {"standing-rules", "show"}, {"standing-rules", subList, "x"},
		{"standing-rules", "revoke"}, {"standing-rules", "revoke", "a", "b"}, {"standing-rules", "revoke", "  "},
	} {
		if err := run(t.Context(), args, &printer{w: &bytes.Buffer{}}, env, nil); !errors.Is(err, errUsage) {
			t.Errorf("goal %v = %v; want a usage error", args, err)
		}
	}
}
