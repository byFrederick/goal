package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
)

const (
	webModelProposal = "/v1/orgs/acme/factories/web/agents/frontend-engineer/model-proposal"
	stModelBuild     = "build"
)

// modelEstimate is the estimate of trying claude-opus-5-5 for frontend-engineer on two items.
func modelEstimate() gen.ModelProposalEstimate {
	return gen.ModelProposalEstimate{
		Agent: "frontend-engineer", CurrentModel: cliHaiku, Model: cliOpus, Tries: 6, EstimateUsd: 4.5, BudgetLeftUsd: 12.25,
		Items: []gen.ModelProposalItem{
			{
				ItemRef: "web#12", ItemTitle: "Fix the login timeout", Station: stModelBuild, RunId: "r12", LineResult: "passed first time, $0.42",
				CostUsd: 0.42, RunCostUsd: 0.42,
			},
			{
				ItemRef: "web#9", ItemTitle: "Add the export", Station: stModelBuild, RunId: "r9", LineResult: "sent back 2 times at review, $1.10",
				SentBack: 2, CostUsd: 1.1, RunCostUsd: 1.08,
			},
		},
	}
}

// modelGoald answers the factory list and the model-proposal route: the estimate without confirm,
// proposal 7 with it, or refusal for both when set. sent are the route's request bodies.
func modelGoald(t *testing.T, refusal *gen.Problem, status int) (*httptest.Server, func() []string) {
	t.Helper()

	var (
		mu   sync.Mutex
		sent []string
	)

	mux := http.NewServeMux()
	mux.HandleFunc(listFactoriesRoute, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusOK, gen.FactoryList{Factories: []gen.Factory{{Id: "f1", Name: searchFactory}}})
	})
	mux.HandleFunc("POST "+webModelProposal, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		mu.Lock()

		sent = append(sent, string(body))
		mu.Unlock()

		var req gen.ModelProposalRequest

		_ = json.Unmarshal(body, &req)

		switch {
		case refusal != nil:
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(refusal)
		case req.Confirm != nil && *req.Confirm:
			var p gen.Proposal

			p.Number, p.Title, p.State = 7, "Try claude-opus-5-5 for frontend-engineer", gen.ProposalStateProposed
			writeJSON(t, w, http.StatusCreated, gen.ModelProposalResult{Estimate: modelEstimate(), Proposal: &p})
		default:
			writeJSON(t, w, http.StatusOK, gen.ModelProposalResult{Estimate: modelEstimate()})
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()

		return append([]string(nil), sent...)
	}
}

// goalTryModel runs goal agent try-model args against api with stdin and returns what it printed
// and its exit code as the golden files hold them.
func goalTryModel(t *testing.T, api, stdin string, args ...string) string {
	t.Helper()

	var stdout, stderr bytes.Buffer

	out := &printer{w: &stdout, e: &stderr, in: strings.NewReader(stdin)}
	getenv := testEnv(t, api, map[string]string{envOrg: orgAcme})
	code := 0

	if err := errors.Join(agentCommand(t.Context(), out, getenv, append([]string{subTryModel}, args...)), out.err); err != nil {
		code = 1
		_, _ = fmt.Fprintln(&stderr, "goal:", err)
	}

	return fmt.Sprintf("exit %d\n--- stdout\n%s--- stderr\n%s", code, stdout.String(), stderr.String())
}

const (
	estimateBody = `{"model":"claude-opus-5-5"}`
	confirmBody  = `{"confirm":true,"model":"claude-opus-5-5"}`
)

func TestAgent_TryModelAsksThenProposes(t *testing.T) {
	t.Parallel()

	srv, sent := modelGoald(t, nil, 0)

	checkGolden(t, "agent-try-model", goalTryModel(t, srv.URL, "y\n", "frontend-engineer", "claude-opus-5-5"))

	if got := sent(); len(got) != 2 || got[0] != estimateBody || got[1] != confirmBody {
		t.Errorf("goald was sent %q, want the estimate then the confirm", got)
	}
}

func TestAgent_TryModelAnsweredNoProposesNothing(t *testing.T) {
	t.Parallel()

	srv, sent := modelGoald(t, nil, 0)

	got := goalTryModel(t, srv.URL, "n\n", "frontend-engineer", "claude-opus-5-5")
	if !strings.Contains(got, "nothing was proposed\n") || !strings.HasPrefix(got, "exit 0") {
		t.Errorf("answered no, it printed\n%s", got)
	}

	if s := sent(); len(s) != 1 || s[0] != estimateBody {
		t.Errorf("goald was sent %q, want the estimate only", s)
	}
}

func TestAgent_TryModelYesSkipsTheQuestion(t *testing.T) {
	t.Parallel()

	srv, sent := modelGoald(t, nil, 0)

	got := goalTryModel(t, srv.URL, "", "frontend-engineer", "claude-opus-5-5", "--yes", "--factory", searchFactory)
	if strings.Contains(got, "[y/N]") || !strings.Contains(got, "✓ proposal 7: Try claude-opus-5-5 for frontend-engineer, 6 tries queued\n") ||
		!strings.Contains(got, "see it with: goal jonah show 7 --factory web\n") {
		t.Errorf("--yes printed\n%s", got)
	}

	if s := sent(); len(s) != 2 || s[1] != confirmBody {
		t.Errorf("goald was sent %q, want the estimate then the confirm", s)
	}
}

func TestAgent_TryModelJSONPrintsTheEstimateOnly(t *testing.T) {
	t.Parallel()

	srv, sent := modelGoald(t, nil, 0)

	got := goalTryModel(t, srv.URL, "", "frontend-engineer", "claude-opus-5-5", "--json")
	if !strings.Contains(got, `"estimate_usd":4.5`) || strings.Contains(got, "[y/N]") || len(sent()) != 1 {
		t.Errorf("--json printed\n%s\nafter %d requests, want the estimate as goald answered it", got, len(sent()))
	}
}

func TestAgent_TryModelRefusals(t *testing.T) {
	t.Parallel()

	est := modelEstimate()
	est.EstimateUsd, est.BudgetLeftUsd = 30, 2

	for _, c := range []struct {
		code   string
		status int
		want   string
	}{
		{codeBudgetExhausted, http.StatusConflict, "refused: the 6 tries would cost about $30.00, more than the $2.00 left of Jonah's budget"},
		{codeModelNotAllowed, http.StatusUnprocessableEntity, "refused: the model is not one the organization allows"},
		{codeAgentNotTriable, http.StatusUnprocessableEntity, "refused: the agent needs MCP servers or secrets"},
		{codeNoFinishedItems, http.StatusUnprocessableEntity, "refused: the agent has no finished item"},
	} {
		code := c.code
		srv, _ := modelGoald(t, &gen.Problem{Title: "refused", Status: c.status, Code: &code, ModelEstimate: &est}, c.status)

		if got := goalTryModel(t, srv.URL, "y\n", "frontend-engineer", "claude-opus-5-5"); !strings.HasPrefix(got, "exit 1") || !strings.Contains(got, c.want) {
			t.Errorf("%s printed\n%s\nwant %q", c.code, got, c.want)
		}
	}

	if got := goalTryModel(t, "http://127.0.0.1:1", "", "frontend-engineer"); !strings.Contains(got, "usage") && !strings.HasPrefix(got, "exit 1") {
		t.Errorf("one argument printed\n%s", got)
	}
}
