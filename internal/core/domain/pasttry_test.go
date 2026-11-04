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
