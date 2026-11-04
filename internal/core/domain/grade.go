package domain

import (
	"encoding/json"
	"fmt"
	"strings"
)

// GradeOf is which result a grade run grades (ADR 0039 decision 10): a try's, or the tried item's
// original or final one, which check the grader both ways.
type GradeOf string

// What a grade run grades. Values match the worker's spec.grade.result.kind.
const (
	// GradeTry is a try's output and diff.
	GradeTry GradeOf = "try"
	// GradeOriginal is the item's original result, which did not fix the reason it went back.
	GradeOriginal GradeOf = "original"
	// GradeFinal is the item's final accepted result, which did.
	GradeFinal GradeOf = "final"
)

// Valid reports whether g is one of the three results a grade run grades.
func (g GradeOf) Valid() bool { return g == GradeTry || g == GradeOriginal || g == GradeFinal }

// GradeMaterial is what a grade run reads: the reason the item went back and the result to grade,
// its output and its diff. The gateway serves it in the grade bundle's spec.grade.
type GradeMaterial struct {
	Reason string
	Output string
	Diff   string
}

// GradeAnswer is a grade run's output: the grader's reason first, then its verdict, which is
// fixed, not fixed or unknown.
type GradeAnswer struct {
	Reason  string         `json:"reason"`
	Verdict PastTryVerdict `json:"verdict"`
}

// ParseGradeAnswer reads a grade run's output, {"reason": "…", "verdict": "fixed" | "not_fixed" |
// "unknown"}, refusing one without a reason or with another verdict with ErrInvalid.
func ParseGradeAnswer(output json.RawMessage) (GradeAnswer, error) {
	var a GradeAnswer
	if err := json.Unmarshal(output, &a); err != nil {
		return GradeAnswer{}, fmt.Errorf("grade answer: %w: %w", ErrInvalid, err)
	}

	switch {
	case strings.TrimSpace(a.Reason) == "":
		return GradeAnswer{}, fmt.Errorf("grade answer: no reason: %w", ErrInvalid)
	case !a.Verdict.graded():
		return GradeAnswer{}, fmt.Errorf("grade answer: verdict %q: %w", a.Verdict, ErrInvalid)
	}

	return a, nil
}
