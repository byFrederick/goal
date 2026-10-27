package domain_test

import (
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
)

func TestJudge_Scores(t *testing.T) {
	t.Parallel()

	all := domain.Judge{Slug: "spec-followed"}
	some := domain.Judge{Slug: "spec-followed", Agents: []string{"builder", "fixer"}}

	for _, c := range []struct {
		judge domain.Judge
		agent string
		want  bool
	}{
		{all, "builder", true},
		{all, "anyone", true},
		{some, "fixer", true},
		{some, "reviewer", false},
		{some, "", false},
	} {
		if got := c.judge.Scores(c.agent); got != c.want {
			t.Errorf("judge with agents %v scores %q = %v, want %v", c.judge.Agents, c.agent, got, c.want)
		}
	}
}

func TestJudge_Outcome(t *testing.T) {
	t.Parallel()

	j := domain.Judge{Outcomes: []domain.JudgeOutcome{
		{Score: 1, Label: "followed", Meaning: "Every point of the spec is done."},
		{Score: 0, Label: "missed", Meaning: "A point of the spec is not done."},
	}}

	if o, ok := j.Outcome("missed"); !ok || o.Score != 0 {
		t.Errorf("Outcome(missed) = %+v, %v; want score 0", o, ok)
	}

	if _, ok := j.Outcome("Missed"); ok {
		t.Error("Outcome(Missed) found an outcome; labels match exactly")
	}
}

func TestJudge_Passes(t *testing.T) {
	t.Parallel()

	j := domain.Judge{PassMark: 0.5}

	for score, want := range map[float64]bool{0.49: false, 0.5: true, 1: true} {
		if got := j.Passes(score); got != want {
			t.Errorf("Passes(%v) at pass mark 0.5 = %v, want %v", score, got, want)
		}
	}
}
