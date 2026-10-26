package domain

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
)

func TestParseVerdict_RejectionNeedsAReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		output string
		want   Decision
		bad    bool
	}{
		{name: "approve", output: `{"decision":"approve"}`, want: Approve},
		{name: "reject with reason", output: `{"decision":"reject","reason":"no evidence"}`, want: Reject},
		{name: "reject without reason", output: `{"decision":"reject"}`, bad: true},
		{name: "unknown decision", output: `{"decision":"maybe"}`, bad: true},
		{name: "not json", output: `ok`, bad: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			v, err := ParseVerdict(json.RawMessage(tt.output))
			if tt.bad {
				if !errors.Is(err, ErrBadVerdict) {
					t.Fatalf("err = %v, want ErrBadVerdict", err)
				}

				return
			}

			if err != nil || v.Decision != tt.want {
				t.Fatalf("ParseVerdict = %+v, %v; want %s", v, err, tt.want)
			}
		})
	}
}

// A reviewer's contract may give a category; the verdict keeps it for the agent statistics.
func TestParseVerdict_KeepsTheCategory(t *testing.T) {
	t.Parallel()

	v, err := ParseVerdict(json.RawMessage(`{"decision":"reject","reason":"no links","category":"Claims without evidence"}`))
	if err != nil || v.Category != catEvidence {
		t.Fatalf("ParseVerdict = %+v, %v; want the category kept", v, err)
	}
}

// PathsFrom reads the output field a definition maps to the item's paths; an unmapped field, a
// missing or empty one and one that is not a list of strings set none.
func TestPathsFrom(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, field, output string
		want                []string
	}{
		{"mapped", "touches", `{"touches":["internal/a/**","docs/a.md"]}`, []string{"internal/a/**", "docs/a.md"}},
		{"blank entries dropped", "touches", `{"touches":["", "internal/a/**"]}`, []string{"internal/a/**"}},
		{"unmapped", "", `{"touches":["internal/a/**"]}`, nil},
		{"absent", "touches", `{"lane":"backend"}`, nil},
		{"empty", "touches", `{"touches":[]}`, nil},
		{"only blanks", "touches", `{"touches":[""]}`, nil},
		{"not a list", "touches", `{"touches":"internal/a/**"}`, nil},
		{"not strings", "touches", `{"touches":[1,2]}`, nil},
		{"not an object", "touches", `[1]`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := PathsFrom(json.RawMessage(tt.output), tt.field)
			if !slices.Equal(got, tt.want) || (got == nil) != (tt.want == nil) {
				t.Errorf("PathsFrom(%s, %q) = %#v, want %#v", tt.output, tt.field, got, tt.want)
			}
		})
	}
}
