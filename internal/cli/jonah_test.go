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
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

const (
	webProposals = "/v1/orgs/acme/factories/web/jonah/proposals"
	webReviews   = "/v1/orgs/acme/factories/web/jonah/reviews"
)

func goalJonah(t *testing.T, api string, args ...string) (string, error) {
	t.Helper()

	var buf bytes.Buffer

	err := run(t.Context(), append([]string{"jonah"}, args...), &printer{w: &buf}, testEnv(t, api, map[string]string{envOrg: orgAcme}), nil)

	return buf.String(), err
}

func proposalEntry(number int64, title string, state gen.ProposalState, paths ...string) gen.ProposalEntry {
	return gen.ProposalEntry{
		Number: number, Title: title, State: state, Paths: paths, Agents: []string{},
		CreatedAt: time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC), EvidenceCount: 2,
	}
}

func jonahRefusal(status int, code string) gen.Problem {
	return gen.Problem{Title: http.StatusText(status), Status: status, Code: &code, Detail: optional("goald's own words")}
}

func TestJonah_ListPrintsTheProposals(t *testing.T) {
	t.Parallel()

	srv, sent := evalGoald(t, map[string]evalAnswer{
		listFactoriesRoute: {http.StatusOK, gen.FactoryList{Factories: []gen.Factory{{Id: "f1", Name: searchFactory}}}},
		"GET " + webProposals: {http.StatusOK, gen.ProposalPage{Proposals: []gen.ProposalEntry{
			proposalEntry(7, "Ask the reviewer to check the lease", gen.ProposalStateProposed, "agents/reviewer.md"),
			proposalEntry(6, "Split the build station's checks", gen.ProposalStateRejected, "agents/builder.md", "factory.yaml"),
		}}},
	})

	got, err := goalJonah(t, srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	checkGolden(t, "jonah-list", got)

	for _, row := range []string{
		"7       Ask the reviewer to check the lease  proposed  agents/reviewer.md",
		"6       Split the build station's checks     rejected  agents/builder.md, factory.yaml",
	} {
		if !strings.Contains(got, row) {
			t.Errorf("goal jonah printed\n%s\nwant a row %q", got, row)
		}
	}

	if _, err := goalJonah(t, srv.URL, "--factory", searchFactory, "--state", "proposed"); err != nil {
		t.Fatal(err)
	}

	if _, err := goalJonah(t, srv.URL, "--state", "maybe"); !errors.Is(err, errUsage) {
		t.Errorf("goal jonah --state maybe = %v, want the usage", err)
	}

	want := []string{listFactoriesRoute, "GET " + webProposals, "GET " + webProposals}
	if s := sent(); strings.Join(s, "\n") != strings.Join(want, "\n") {
		t.Errorf("goald was sent\n%s\nwant\n%s", strings.Join(s, "\n"), strings.Join(want, "\n"))
	}
}

func TestJonah_ShowPrintsADiff(t *testing.T) {
	t.Parallel()

	entry := proposalEntry(7, "Ask the reviewer to check the lease", gen.ProposalStateProposed, "agents/reviewer.md", "evals/cases/lease.yaml")
	p := gen.Proposal{
		Number: entry.Number, Title: entry.Title, State: entry.State, Paths: entry.Paths, Agents: []string{"reviewer"},
		CreatedAt: entry.CreatedAt, EvidenceCount: 2, Summary: "Two items came back because the lease was not checked.",
		MadeAgainstVersion: 4, PublishesAsVersion: 5,
		Evidence: []gen.ProposalEvidence{
			{ItemRef: "web#12", ItemTitle: "Fix the lease", Station: optional("review"), Quote: "the lease is never renewed"},
			{ItemRef: "web#15", ItemTitle: "Renew leases", Quote: "same lease bug"},
		},
		Files: []gen.ProposalFile{
			{
				Path: "agents/reviewer.md", BaseSha256: strings.Repeat("a", 64),
				BaseContent: "# Reviewer\n\nRead the diff.\nRun the tests.\n",
				Content:     "# Reviewer\n\nRead the diff.\nCheck that every lease is renewed.\nRun the tests.\n",
			},
			{Path: "evals/cases/lease.yaml", Content: "name: lease\n"},
		},
	}

	srv, sent := evalGoald(t, map[string]evalAnswer{"GET " + webProposals + "/7": {http.StatusOK, p}})

	got, err := goalJonah(t, srv.URL, subShow, "7", "--factory", searchFactory)
	if err != nil {
		t.Fatal(err)
	}

	checkGolden(t, "jonah-show", got)

	for _, want := range []string{
		`web#12 at review: "the lease is never renewed"`,
		"--- a/agents/reviewer.md\n+++ b/agents/reviewer.md\n@@ -1,4 +1,5 @@\n # Reviewer\n \n Read the diff.\n+Check that every lease is renewed.\n Run the tests.\n",
		"--- /dev/null\n+++ b/evals/cases/lease.yaml\n@@ -0,0 +1 @@\n+name: lease\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("goal jonah show printed\n%s\nwant it to contain\n%s", got, want)
		}
	}

	if s := sent(); len(s) != 1 || s[0] != "GET "+webProposals+"/7" {
		t.Errorf("goald was sent %q", s)
	}
}

func TestJonah_RejectNeedsAReason(t *testing.T) {
	t.Parallel()

	srv, sent := evalGoald(t, map[string]evalAnswer{
		"POST " + webProposals + "/7/reject": {http.StatusOK, gen.ProposalDecided{Number: 7, State: gen.ProposalStateRejected}},
	})

	for _, args := range [][]string{{verbReject, "7"}, {verbReject, "7", flagReason, "  "}} {
		if _, err := goalJonah(t, srv.URL, append(args, "--factory", searchFactory)...); !errors.Is(err, errUsage) {
			t.Errorf("goal jonah %q = %v, want the usage", args, err)
		}
	}

	if s := sent(); len(s) != 0 {
		t.Fatalf("goald was sent %q before a reason was given", s)
	}

	got, err := goalJonah(t, srv.URL, verbReject, "7", flagReason, "too broad", "--factory", searchFactory)
	if err != nil {
		t.Fatal(err)
	}

	if got != "proposal #7 rejected\n" {
		t.Errorf("goal jonah reject printed %q", got)
	}

	if s := sent(); len(s) != 1 || s[0] != "POST "+webProposals+`/7/reject {"reason":"too broad"}` {
		t.Errorf("goald was sent %q", s)
	}
}

func TestJonah_DecisionsAndLook(t *testing.T) {
	t.Parallel()

	five, rev := 5, int64(3)
	srv, sent := evalGoald(t, map[string]evalAnswer{
		"POST " + webProposals + "/7/approve": {http.StatusOK, gen.ProposalDecided{Number: 7, State: gen.ProposalStatePublished, Version: &five}},
		"POST " + webProposals + "/8/edit":    {http.StatusOK, gen.ProposalDecided{Number: 8, State: gen.ProposalStateApplied, DraftRevision: &rev}},
		"POST " + webReviews: {http.StatusAccepted, gen.JonahReview{
			Id: "019a0000-0000-7000-8000-0000000000aa", State: gen.JonahReviewStatePending, Trigger: gen.JonahReviewTriggerManual,
		}},
	})

	var got []string

	for _, args := range [][]string{{verbApprove, "7"}, {jonahEditCmd, "#8"}, {jonahLookCmd}} {
		out, err := goalJonah(t, srv.URL, append(args, "--factory", searchFactory)...)
		if err != nil {
			t.Fatalf("goal jonah %q: %v", args, err)
		}

		got = append(got, out)
	}

	want := []string{
		"proposal #7 published as v5\n",
		"proposal #8 written into the draft (revision 3): continue in the builder\n",
		"review 019a0000-0000-7000-8000-0000000000aa queued: goal jonah lists its proposals once it finishes\n",
	}
	if strings.Join(got, "") != strings.Join(want, "") {
		t.Errorf("goal jonah printed\n%s\nwant\n%s", strings.Join(got, ""), strings.Join(want, ""))
	}

	wantSent := []string{
		"POST " + webProposals + "/7/approve",
		"POST " + webProposals + "/8/edit",
		"POST " + webReviews,
	}
	if s := sent(); strings.Join(s, "\n") != strings.Join(wantSent, "\n") {
		t.Errorf("goald was sent\n%s\nwant\n%s", strings.Join(s, "\n"), strings.Join(wantSent, "\n"))
	}

	for _, args := range [][]string{{subShow}, {verbApprove}, {verbApprove, "seven"}, {jonahEditCmd, "0"}, {jonahLookCmd, argExtra}, {cmdUnknownName}} {
		if _, err := goalJonah(t, srv.URL, args...); !errors.Is(err, errUsage) {
			t.Errorf("goal jonah %q = %v, want the usage", args, err)
		}
	}
}

// goal jonah look prints a review in flight or a spent budget as its one-line message.
func TestJonah_LookConflicts(t *testing.T) {
	t.Parallel()

	for code, want := range map[string]string{
		"review-in-flight":          "a review is already pending or running: wait for it to finish",
		"learning-budget-exhausted": "Jonah's runs used up its budget, learning.jonah.share of what the line spent in the last 7 days: try again when the window moves on",
	} {
		srv, _ := evalGoald(t, map[string]evalAnswer{"POST " + webReviews: {http.StatusConflict, jonahRefusal(http.StatusConflict, code)}})

		if _, err := goalJonah(t, srv.URL, jonahLookCmd, "--factory", searchFactory); err == nil || err.Error() != want {
			t.Errorf("goal jonah look on %s = %v, want %q", code, err, want)
		}
	}
}

func TestJonah_ConflictsPrintOneLine(t *testing.T) {
	t.Parallel()

	cases := []struct {
		code string
		args []string
		path string
		want string
	}{
		{"draft-open", []string{verbApprove, "7"}, webProposals + "/7/approve", "the factory has an open draft: publish or discard it, then approve again, or use goal jonah edit"},
		{"proposal-stale", []string{verbApprove, "7"}, webProposals + "/7/approve", "a file changed since Jonah made this proposal, so it no longer applies: reject it"},
		{"draft-conflict", []string{jonahEditCmd, "7"}, webProposals + "/7/edit", "the draft's copy of a file changed since Jonah made this proposal: nothing was written"},
		{"proposal-decided", []string{verbReject, "7", flagReason, "no"}, webProposals + "/7/reject", "the proposal was already decided: goal jonah show 7 says how"},
		{"review-in-flight", []string{verbApprove, "7"}, webProposals + "/7/approve", "a review is already pending or running: wait for it to finish"},
		{"learning-budget-exhausted", []string{verbApprove, "7"}, webProposals + "/7/approve", "Jonah's runs used up its budget, learning.jonah.share of what the line spent in the last 7 days: try again when the window moves on"},
	}

	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			t.Parallel()

			srv, _ := evalGoald(t, map[string]evalAnswer{"POST " + tc.path: {http.StatusConflict, jonahRefusal(http.StatusConflict, tc.code)}})

			var stdout, stderr bytes.Buffer

			args := append([]string{"jonah"}, append(tc.args, "--factory", searchFactory)...)
			code := Run(t.Context(), args, strings.NewReader(""), &stdout, &stderr, testEnv(t, srv.URL, map[string]string{envOrg: orgAcme}), nil)

			if code == 0 {
				t.Errorf("goal %q exited 0", args)
			}

			if got := stderr.String(); got != "goal: "+tc.want+"\n" {
				t.Errorf("goal %q printed %q, want %q", args, got, "goal: "+tc.want+"\n")
			}
		})
	}

	t.Run("refused", func(t *testing.T) {
		t.Parallel()

		srv, _ := evalGoald(t, map[string]evalAnswer{
			"POST " + webProposals + "/7/approve": {http.StatusForbidden, jonahRefusal(http.StatusForbidden, "permission-denied")},
		})

		var stdout, stderr bytes.Buffer

		code := Run(t.Context(), []string{"jonah", verbApprove, "7", "--factory", searchFactory}, strings.NewReader(""), &stdout, &stderr,
			testEnv(t, srv.URL, map[string]string{envOrg: orgAcme}), nil)
		if code != exitNoPerm {
			t.Errorf("a refused approve exited %d, want %d", code, exitNoPerm)
		}
	})
}

func TestUnifiedDiff_HunksKeepThreeLinesOfContext(t *testing.T) {
	t.Parallel()

	base := "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n"
	next := "1\nTWO\n3\n4\n5\n6\n7\n8\n9\n10\n11\n"

	want := "--- a/f\n+++ b/f\n" +
		"@@ -1,5 +1,5 @@\n 1\n-2\n+TWO\n 3\n 4\n 5\n" +
		"@@ -9,4 +9,3 @@\n 9\n 10\n 11\n-12\n"
	if got := unifiedDiff("f", base, next); got != want {
		t.Errorf("unifiedDiff =\n%s\nwant\n%s", got, want)
	}

	if got := unifiedDiff("f", base, base); got != "" {
		t.Errorf("unifiedDiff of equal files = %q, want nothing", got)
	}

	if got := unifiedDiff("f", "a", "b"); got != "--- a/f\n+++ b/f\n@@ -1 +1 @@\n-a\n\\ No newline at end of file\n+b\n\\ No newline at end of file\n" {
		t.Errorf("unifiedDiff without final newlines =\n%s", got)
	}
}

// tryEstimate is the estimate of trying proposal 7 again: a model proposal tried on two items.
func tryEstimate() gen.TryProposalEstimate {
	return gen.TryProposalEstimate{
		Number: 7, Tries: 6, EstimateUsd: 4.5, BudgetLeftUsd: 12.25,
		Items: []gen.TryProposalItem{
			{ItemRef: "web#12", ItemTitle: "Fix the login timeout", Station: stModelBuild, Tries: 3, RunCostUsd: 0.42},
			{ItemRef: "web#9", ItemTitle: "Add the export", Station: stModelBuild, Tries: 3, RunCostUsd: 1.08},
		},
	}
}

// tryGoald answers the factory list and proposal 7's Try again route: the estimate without
// confirm, the proposal with it, or refusal for both when set. sent are the route's request
// bodies.
func tryGoald(t *testing.T, refusal *gen.Problem) (*httptest.Server, func() []string) {
	t.Helper()

	var (
		mu   sync.Mutex
		sent []string
	)

	mux := http.NewServeMux()
	mux.HandleFunc(listFactoriesRoute, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusOK, gen.FactoryList{Factories: []gen.Factory{{Id: "f1", Name: searchFactory}}})
	})
	mux.HandleFunc("POST "+webProposals+"/7/try", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		mu.Lock()

		sent = append(sent, string(body))
		mu.Unlock()

		var req gen.TryProposalRequest

		_ = json.Unmarshal(body, &req)

		switch {
		case refusal != nil:
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(refusal.Status)
			_ = json.NewEncoder(w).Encode(refusal)
		case req.Confirm != nil && *req.Confirm:
			var p gen.Proposal

			p.Number, p.Title, p.State = 7, "Try claude-opus-5-5 for frontend-engineer", gen.ProposalStateProposed
			writeJSON(t, w, http.StatusCreated, gen.TryProposalResult{Estimate: tryEstimate(), Proposal: &p})
		default:
			writeJSON(t, w, http.StatusOK, gen.TryProposalResult{Estimate: tryEstimate()})
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

// goalJonahTry runs goal jonah try args against api with stdin and returns what it printed and its
// exit code as the golden files hold them.
func goalJonahTry(t *testing.T, api, stdin string, args ...string) string {
	t.Helper()

	var stdout, stderr bytes.Buffer

	out := &printer{w: &stdout, e: &stderr, in: strings.NewReader(stdin)}
	getenv := testEnv(t, api, map[string]string{envOrg: orgAcme})
	code := 0

	if err := errors.Join(jonahCommand(t.Context(), out, getenv, append([]string{jonahTryCmd}, args...)), out.err); err != nil {
		code = 1
		_, _ = fmt.Fprintln(&stderr, "goal:", err)
	}

	return fmt.Sprintf("exit %d\n--- stdout\n%s--- stderr\n%s", code, stdout.String(), stderr.String())
}

func TestJonah_TryAgainPrintsTheEstimate(t *testing.T) {
	t.Parallel()

	srv, sent := tryGoald(t, nil)

	checkGolden(t, "jonah-try", goalJonahTry(t, srv.URL, "y\n", "7", "--factory", searchFactory))

	if got := sent(); len(got) != 2 || got[0] != `{}` || got[1] != `{"confirm":true}` {
		t.Errorf("goald was sent %q, want the estimate then the confirm", got)
	}

	t.Run("answered no", func(t *testing.T) {
		t.Parallel()

		srv, sent := tryGoald(t, nil)
		if got := goalJonahTry(t, srv.URL, "n\n", "7"); !strings.HasPrefix(got, "exit 0") || !strings.Contains(got, "nothing was queued\n") || len(sent()) != 1 {
			t.Errorf("answered no, it printed\n%s\nafter %d requests, want the estimate only", got, len(sent()))
		}
	})

	t.Run("yes", func(t *testing.T) {
		t.Parallel()

		srv, sent := tryGoald(t, nil)
		if got := goalJonahTry(t, srv.URL, "", "#7", "--yes"); strings.Contains(got, "[y/N]") || !strings.Contains(got, "✓ 6 tries of proposal #7 queued\n") ||
			len(sent()) != 2 {
			t.Errorf("--yes printed\n%s", got)
		}
	})

	t.Run("json", func(t *testing.T) {
		t.Parallel()

		srv, sent := tryGoald(t, nil)
		if got := goalJonahTry(t, srv.URL, "", "7", "--json"); !strings.Contains(got, `"estimate_usd":4.5`) || strings.Contains(got, "[y/N]") || len(sent()) != 1 {
			t.Errorf("--json printed\n%s", got)
		}
	})
}

func TestJonah_TryAgainConflicts(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		code   string
		status int
		want   string
	}{
		{"proposal-not-tried", http.StatusConflict, "goal: " + jonahConflicts["proposal-not-tried"] + "\n"},
		{"proposal-decided", http.StatusConflict, "goal: the proposal was already decided: goal jonah show 7 says how\n"},
		{"learning-budget-exhausted", http.StatusConflict, "goal: " + jonahConflicts["learning-budget-exhausted"] + "\n"},
		{"proposal-not-triable", http.StatusUnprocessableEntity, "goald's own words"},
	} {
		p := jonahRefusal(c.status, c.code)
		srv, sent := tryGoald(t, &p)

		got := goalJonahTry(t, srv.URL, "y\n", "7")
		if !strings.HasPrefix(got, "exit 1") || !strings.Contains(got, c.want) || len(sent()) != 1 {
			t.Errorf("%s printed\n%s\nwant %q after the estimate only", c.code, got, c.want)
		}
	}
}
