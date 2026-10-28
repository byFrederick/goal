package cli

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

const (
	webProposals = "/v1/orgs/acme/factories/web/jonah/proposals"
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
	})

	var got []string

	for _, args := range [][]string{{verbApprove, "7"}, {jonahEditCmd, "#8"}} {
		out, err := goalJonah(t, srv.URL, append(args, "--factory", searchFactory)...)
		if err != nil {
			t.Fatalf("goal jonah %q: %v", args, err)
		}

		got = append(got, out)
	}

	want := []string{
		"proposal #7 published as v5\n",
		"proposal #8 written into the draft (revision 3): continue in the builder\n",
	}
	if strings.Join(got, "") != strings.Join(want, "") {
		t.Errorf("goal jonah printed\n%s\nwant\n%s", strings.Join(got, ""), strings.Join(want, ""))
	}

	wantSent := []string{
		"POST " + webProposals + "/7/approve",
		"POST " + webProposals + "/8/edit",
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

// goal jonah look refuses with the plain sentence and exits non-zero while no review loop runs,
// and it never calls goald (software#344 lifts the refusal).
func TestJonah_LookRefusesUntilTheLoopShips(t *testing.T) {
	t.Parallel()

	srv, sent := evalGoald(t, map[string]evalAnswer{})

	_, err := goalJonah(t, srv.URL, jonahLookCmd, "--factory", searchFactory)
	if err == nil || err.Error() != reviewsNotStarted {
		t.Fatalf("goal jonah look = %v, want %q", err, reviewsNotStarted)
	}

	if s := sent(); len(s) != 0 {
		t.Errorf("goald was sent %q, want no request", s)
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
		{"review-loop-not-running", []string{verbApprove, "7"}, webProposals + "/7/approve", "Reviews start when Jonah's review loop ships"},
		{"review-in-flight", []string{verbApprove, "7"}, webProposals + "/7/approve", "a review is already pending or running: wait for it to finish"},
		{"learning-budget-exhausted", []string{verbApprove, "7"}, webProposals + "/7/approve", "Jonah's runs reached the factory's weekly budget (learning.jonah.weeklyUsd): try again when the last 7 days cost less"},
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
