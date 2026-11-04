package domain

import (
	"encoding/json"
	"errors"
	"testing"
)

// ParseGradeAnswer reads the worker's grade output and refuses one without a reason or with a
// verdict outside fixed, not_fixed and unknown.
func TestParseGradeAnswer(t *testing.T) {
	t.Parallel()

	for _, v := range []PastTryVerdict{PastTryFixed, PastTryNotFixed, PastTryUnknown} {
		out := json.RawMessage(`{"reason":"It adds the missing check.","verdict":"` + string(v) + `"}`)

		got, err := ParseGradeAnswer(out)
		if err != nil || got != (GradeAnswer{Reason: "It adds the missing check.", Verdict: v}) {
			t.Errorf("ParseGradeAnswer(%s) = %+v, %v; want its reason and verdict", out, got, err)
		}
	}

	for name, out := range map[string]string{
		"no output":        ``,
		"a string":         `"fixed"`,
		"no reason":        `{"verdict":"fixed"}`,
		"a blank reason":   `{"reason":"  ","verdict":"fixed"}`,
		"no verdict":       `{"reason":"r"}`,
		"an error verdict": `{"reason":"r","verdict":"error"}`,
		"another verdict":  `{"reason":"r","verdict":"maybe"}`,
	} {
		if got, err := ParseGradeAnswer(json.RawMessage(out)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: ParseGradeAnswer = %+v, %v; want ErrInvalid", name, got, err)
		}
	}
}

func TestGradeOf_Valid(t *testing.T) {
	t.Parallel()

	for g, want := range map[GradeOf]bool{GradeTry: true, GradeOriginal: true, GradeFinal: true, "": false, "again": false} {
		if g.Valid() != want {
			t.Errorf("GradeOf(%q).Valid() = %v; want %v", g, !want, want)
		}
	}
}
