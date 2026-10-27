package domain

import (
	"errors"
	"testing"
	"time"
)

func TestJudgeScoreState_Valid(t *testing.T) {
	t.Parallel()

	for _, s := range []JudgeScoreState{JudgeScoreQueued, JudgeScoreRunning, JudgeScoreScored, JudgeScoreError} {
		if !s.Valid() {
			t.Errorf("%q.Valid() = false; want true", s)
		}
	}

	for _, s := range []JudgeScoreState{"", "done", "Scored"} {
		if s.Valid() {
			t.Errorf("%q.Valid() = true; want false", s)
		}
	}
}

func TestJudgeScoreNew_Check(t *testing.T) {
	t.Parallel()

	valid := NewJudgeScore{
		FactoryID: "00000000-0000-7000-8000-0000000000f1", VersionID: "00000000-0000-7000-8000-0000000000a1",
		Judge: "spec-fidelity", RunID: "00000000-0000-7000-8000-0000000000b1", ItemID: "00000000-0000-7000-8000-0000000000c1",
		At: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
	}

	tests := []struct {
		name string
		edit func(n *NewJudgeScore)
		ok   bool
	}{
		{"valid", func(*NewJudgeScore) {}, true},
		{"a try has neither run nor item", func(n *NewJudgeScore) { n.RunID, n.ItemID = "", "" }, true},
		{"a try with an item", func(n *NewJudgeScore) { n.RunID = "" }, true},
		{"no factory", func(n *NewJudgeScore) { n.FactoryID = "" }, false},
		{"no version", func(n *NewJudgeScore) { n.VersionID = "" }, false},
		{"no time", func(n *NewJudgeScore) { n.At = time.Time{} }, false},
		{"a run without its item", func(n *NewJudgeScore) { n.ItemID = "" }, false},
		{"judge not a slug", func(n *NewJudgeScore) { n.Judge = "Spec Fidelity" }, false},
		{"judge too short", func(n *NewJudgeScore) { n.Judge = "ab" }, false},
		{"judge starts with a digit", func(n *NewJudgeScore) { n.Judge = "1judge" }, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			n := valid
			tc.edit(&n)

			err := n.Check()
			if tc.ok && err != nil {
				t.Errorf("Check() = %v; want nil", err)
			}

			if !tc.ok && !errors.Is(err, ErrInvalid) {
				t.Errorf("Check() = %v; want ErrInvalid", err)
			}
		})
	}
}

func TestJudgeScoreResult_Check(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 10, 1, 12, 5, 0, 0, time.UTC)
	scored := JudgeResult{
		State: JudgeScoreScored, Outcome: "faithful", Score: 0.75, Passed: true, Reason: "It does what the spec asks.",
		CostUSD: 0.02, JudgeRunID: "00000000-0000-7000-8000-0000000000d1", At: at,
	}
	failed := JudgeResult{State: JudgeScoreError, Reason: "The box did not start.", CostUSD: 0.01, At: at}

	tests := []struct {
		name string
		r    JudgeResult
		edit func(r *JudgeResult)
		ok   bool
	}{
		{"scored", scored, func(*JudgeResult) {}, true},
		{"scored at 0", scored, func(r *JudgeResult) { r.Score, r.Passed = 0, false }, true},
		{"scored at 1", scored, func(r *JudgeResult) { r.Score = 1 }, true},
		{"an error", failed, func(*JudgeResult) {}, true},
		{"score below 0", scored, func(r *JudgeResult) { r.Score = -0.1 }, false},
		{"score above 1", scored, func(r *JudgeResult) { r.Score = 1.01 }, false},
		{"scored without an outcome", scored, func(r *JudgeResult) { r.Outcome = "" }, false},
		{"an error with an outcome", failed, func(r *JudgeResult) { r.Outcome = "faithful" }, false},
		{"an error that passed", failed, func(r *JudgeResult) { r.Passed = true }, false},
		{"an error with a score", failed, func(r *JudgeResult) { r.Score = 0.5 }, false},
		{"queued is no result", scored, func(r *JudgeResult) { r.State = JudgeScoreQueued }, false},
		{"running is no result", scored, func(r *JudgeResult) { r.State = JudgeScoreRunning }, false},
		{"negative cost", scored, func(r *JudgeResult) { r.CostUSD = -1 }, false},
		{"no time", scored, func(r *JudgeResult) { r.At = time.Time{} }, false},
		{"reason too long", scored, func(r *JudgeResult) { r.Reason = string(make([]byte, MaxJudgeReason+1)) }, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := tc.r
			tc.edit(&r)

			err := r.Check()
			if tc.ok && err != nil {
				t.Errorf("Check() = %v; want nil", err)
			}

			if !tc.ok && !errors.Is(err, ErrInvalid) {
				t.Errorf("Check() = %v; want ErrInvalid", err)
			}
		})
	}
}

func TestJudgeScorePassRate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		scored, passed int
		want           float64
	}{
		{0, 0, 0},
		{4, 3, 0.75},
		{2, 2, 1},
	}

	for _, tc := range tests {
		if got := PassRate(tc.scored, tc.passed); got != tc.want {
			t.Errorf("PassRate(%d, %d) = %v; want %v", tc.scored, tc.passed, got, tc.want)
		}
	}
}

func TestJudgeScoreDay(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 10, 1, 23, 30, 0, 0, time.FixedZone("x", -3*3600))
	if got, want := JudgeDayOf(at), time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC); !got.Equal(want) || got.Location() != time.UTC {
		t.Errorf("JudgeDayOf(%v) = %v; want %v, the UTC day", at, got, want)
	}
}
