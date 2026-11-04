package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPastTryResult_Check(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		res  PastTryResult
		ok   bool
	}{
		{name: "a run that ended, waiting for the grader", res: PastTryResult{Diff: "diff", CostUSD: 0.5, At: at}, ok: true},
		{name: "an error with its reason", res: PastTryResult{Verdict: PastTryError, Error: "the commit is gone", At: at}, ok: true},
		{name: "not fixed with its reason", res: PastTryResult{Verdict: PastTryNotFixed, Reason: "check `go test` exited 1", At: at}, ok: true},
		{name: "fixed with its reason", res: PastTryResult{Verdict: PastTryFixed, Reason: "the test passes now", At: at}, ok: true},
		{name: "unknown with its reason", res: PastTryResult{Verdict: PastTryUnknown, Reason: "the grader misread the original", At: at}, ok: true},
		{name: "an error without its reason", res: PastTryResult{Verdict: PastTryError, At: at}},
		{name: "not fixed without its reason", res: PastTryResult{Verdict: PastTryNotFixed, At: at}},
		{name: "fixed with a blank reason", res: PastTryResult{Verdict: PastTryFixed, Reason: " \n", At: at}},
		{name: "unknown without its reason", res: PastTryResult{Verdict: PastTryUnknown, At: at}},
		{name: "a reason without a grade", res: PastTryResult{Reason: "why", At: at}},
		{name: "an unknown verdict", res: PastTryResult{Verdict: "fail", Reason: "why", At: at}},
		{name: "a reason past its bound", res: PastTryResult{Verdict: PastTryNotFixed, Reason: strings.Repeat("é", MaxPastTryReason+1), At: at}},
		{name: "a negative cost", res: PastTryResult{CostUSD: -1, At: at}},
		{name: "no finish time", res: PastTryResult{}},
	}

	for _, tt := range tests {
		err := tt.res.Check()
		if tt.ok != (err == nil) {
			t.Errorf("%s: Check = %v; want ok %v", tt.name, err, tt.ok)
		}

		if err != nil && !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: Check = %v; want ErrInvalid", tt.name, err)
		}
	}
}

// lineReview is the station the line results send items back at.
const lineReview = "review"

func TestNewPastTry_CheckAttempt(t *testing.T) {
	t.Parallel()

	n := NewPastTry{ProposalID: "p", ItemID: "i", Station: stationBuild, OriginalRunID: "r", Model: "m"}

	for _, attempt := range []int{0, 1, ModelTriesPerItem} {
		n.Attempt = attempt
		if err := n.Check(); err != nil {
			t.Errorf("Check with attempt %d = %v, want nil", attempt, err)
		}
	}

	if n.Attempt = 0; n.TryAttempt() != 1 {
		t.Errorf("TryAttempt of attempt 0 = %d, want 1", n.TryAttempt())
	}

	for _, attempt := range []int{-1, ModelTriesPerItem + 1} {
		n.Attempt = attempt
		if err := n.Check(); !errors.Is(err, ErrInvalid) {
			t.Errorf("Check with attempt %d = %v, want ErrInvalid", attempt, err)
		}
	}
}

func TestAgentItem_LineResult(t *testing.T) {
	t.Parallel()

	cases := []struct {
		sent []string
		cost float64
		want string
	}{
		{nil, 0.42, "passed first time, $0.42"},
		{[]string{lineReview}, 0.9, "sent back once at review, $0.90"},
		{[]string{lineReview, lineReview}, 1.1, "sent back 2 times at review, $1.10"},
		{[]string{lineReview, "test", lineReview}, 2, "sent back 3 times at review and test, $2.00"},
	}

	for _, c := range cases {
		if got := (AgentItem{SentBackAt: c.sent, CostUSD: c.cost}).LineResult(); got != c.want {
			t.Errorf("LineResult of %v, $%v = %q, want %q", c.sent, c.cost, got, c.want)
		}
	}
}

// A kept test's try needs its content key, and a try with no proposal needs a kept test.
func TestNewPastTry_CheckKeptTest(t *testing.T) {
	t.Parallel()

	n := NewPastTry{KeptTestID: "k", ContentKey: "key", ItemID: "i", Station: stationBuild, OriginalRunID: "r", Model: "m"}
	if err := n.Check(); err != nil {
		t.Errorf("Check of a publish's kept-test try = %v, want nil", err)
	}

	for name, bad := range map[string]NewPastTry{
		"no content key":         {KeptTestID: "k", ItemID: "i", Station: stationBuild, OriginalRunID: "r", Model: "m"},
		"no proposal, no test":   {ContentKey: "key", ItemID: "i", Station: stationBuild, OriginalRunID: "r", Model: "m"},
		"content key past bound": {KeptTestID: "k", ContentKey: strings.Repeat("a", MaxContentKey+1), ItemID: "i", Station: stationBuild, OriginalRunID: "r", Model: "m"},
	} {
		if err := bad.Check(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: Check = %v, want ErrInvalid", name, err)
		}
	}
}

// A kept test's try is running until done, then passed, failed or error by its verdict; the
// result of several is running, then failed, then passed, then error.
func TestPastTry_KeptResult(t *testing.T) {
	t.Parallel()

	done := func(v PastTryVerdict) PastTry { return PastTry{ID: PastTryID(v), State: PastTryDone, Verdict: v} }

	for want, try := range map[KeptTestResult]PastTry{
		KeptTestRunning: {State: PastTryQueued}, KeptTestPassed: done(PastTryFixed), KeptTestFailed: done(PastTryNotFixed),
		KeptTestError: done(PastTryError),
	} {
		if got := try.KeptResult(); got != want {
			t.Errorf("KeptResult of %+v = %s, want %s", try, got, want)
		}
	}

	if got, by, _ := KeptResultOf([]PastTry{done(PastTryFixed), done(PastTryNotFixed), done(PastTryError)}); got != KeptTestFailed || by.Verdict != PastTryNotFixed {
		t.Errorf("KeptResultOf = %s by %+v, want failed by the try that failed", got, by)
	}

	if got, _, _ := KeptResultOf([]PastTry{done(PastTryError), done(PastTryFixed)}); got != KeptTestPassed {
		t.Errorf("KeptResultOf of an error and a pass = %s, want passed", got)
	}

	if _, _, ok := KeptResultOf(nil); ok {
		t.Error("KeptResultOf of no tries is ok, want false")
	}
}

// A content key depends only on the files: a version with whole-file edits that make it another
// version's files has that version's key.
func TestPastTry_ContentKeyOfAppliedEdits(t *testing.T) {
	t.Parallel()

	base := map[string][]byte{"factory.yaml": []byte("a: 1\n"), "agents/w.md": []byte("# w\n")}
	draft := map[string][]byte{"factory.yaml": []byte("a: 1\n"), "agents/w.md": []byte("# w, test first\n")}

	edited, err := ApplyEdits(base, []ProposalEdit{{Path: "agents/w.md", Replacements: []Replacement{{Old: "# w\n", New: "# w, test first\n"}}}})
	if err != nil {
		t.Fatal(err)
	}

	whole, err := ApplyEdits(base, []ProposalEdit{{Path: "agents/w.md", Content: "# w, test first\n", BaseSHA256: string(HashBlob(base["agents/w.md"]))}})
	if err != nil {
		t.Fatal(err)
	}

	if ContentKey(edited) != ContentKey(draft) || ContentKey(whole) != ContentKey(draft) {
		t.Error("the key of base with the edits differs from the draft's with the same files")
	}

	if ContentKey(base) == ContentKey(draft) || string(base["agents/w.md"]) != "# w\n" {
		t.Error("the key of other files matches, or ApplyEdits changed its input")
	}
}
