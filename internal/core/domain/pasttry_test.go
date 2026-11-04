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

func TestNewGraderCheck_Check(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	ok := NewGraderCheck{ProposalID: "p", ItemID: "i", Result: GraderCheckOriginal, Answer: PastTryNotFixed, Reason: "the test still fails", CheckedAt: at}

	tests := []struct {
		name   string
		change func(*NewGraderCheck)
		ok     bool
	}{
		{name: "the original read as not fixed", change: func(*NewGraderCheck) {}, ok: true},
		{name: "the final read as fixed, with its run and cost", change: func(n *NewGraderCheck) {
			n.Result, n.Answer, n.RunID, n.CostUSD = GraderCheckFinal, PastTryFixed, "r", 0.02
		}, ok: true},
		{name: "an unknown answer", change: func(n *NewGraderCheck) { n.Answer = PastTryUnknown }, ok: true},
		{name: "no proposal", change: func(n *NewGraderCheck) { n.ProposalID = "" }},
		{name: "no item", change: func(n *NewGraderCheck) { n.ItemID = "" }},
		{name: "a result that is neither", change: func(n *NewGraderCheck) { n.Result = "latest" }},
		{name: "no result", change: func(n *NewGraderCheck) { n.Result = "" }},
		{name: "an error answer", change: func(n *NewGraderCheck) { n.Answer = PastTryError }},
		{name: "no answer", change: func(n *NewGraderCheck) { n.Answer = "" }},
		{name: "a blank reason", change: func(n *NewGraderCheck) { n.Reason = " \n" }},
		{name: "a reason past its bound", change: func(n *NewGraderCheck) { n.Reason = strings.Repeat("é", MaxGraderCheckReason+1) }},
		{name: "a negative cost", change: func(n *NewGraderCheck) { n.CostUSD = -0.01 }},
		{name: "no time", change: func(n *NewGraderCheck) { n.CheckedAt = time.Time{} }},
	}

	for _, tt := range tests {
		n := ok
		tt.change(&n)

		err := n.Check()
		if tt.ok != (err == nil) {
			t.Errorf("%s: Check = %v; want ok %v", tt.name, err, tt.ok)
		}

		if err != nil && !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: Check = %v; want ErrInvalid", tt.name, err)
		}
	}
}

func TestGraderCheck_Hit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		result GraderCheckResult
		answer PastTryVerdict
		hit    bool
	}{
		{GraderCheckOriginal, PastTryNotFixed, true},
		{GraderCheckOriginal, PastTryFixed, false},
		{GraderCheckOriginal, PastTryUnknown, false},
		{GraderCheckFinal, PastTryFixed, true},
		{GraderCheckFinal, PastTryNotFixed, false},
		{GraderCheckFinal, PastTryUnknown, false},
	}

	for _, tt := range tests {
		if got := (GraderCheck{Result: tt.result, Answer: tt.answer}).Hit(); got != tt.hit {
			t.Errorf("Hit of the %s result read as %s = %v; want %v", tt.result, tt.answer, got, tt.hit)
		}
	}
}

func TestHitRatesOf_CountsEachResult(t *testing.T) {
	t.Parallel()

	got := HitRatesOf([]GraderCheck{
		{Result: GraderCheckOriginal, Answer: PastTryNotFixed},
		{Result: GraderCheckFinal, Answer: PastTryFixed},
		{Result: GraderCheckOriginal, Answer: PastTryNotFixed},
		{Result: GraderCheckFinal, Answer: PastTryUnknown},
	})

	want := GraderHitRates{OriginalHits: 2, OriginalTotal: 2, FinalHits: 1, FinalTotal: 2}
	if got != want {
		t.Errorf("HitRatesOf = %+v; want %+v", got, want)
	}

	if got := HitRatesOf(nil); got != (GraderHitRates{}) {
		t.Errorf("HitRatesOf of no checks = %+v; want zero", got)
	}
}
