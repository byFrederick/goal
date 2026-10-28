package config

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
)

// A value outside a schema enum gets one fix: the allowed value closest by edit distance, the
// first in the schema's order on a tie. A const gets none.
func TestEnumFix_ReplacesWithTheNearestValue(t *testing.T) {
	t.Parallel()

	replace := func(section, path, value string) []domain.ProblemFix {
		return []domain.ProblemFix{{
			Label: "Use " + value, Section: section,
			Patch: []domain.PatchOp{{Op: domain.PatchReplace, Path: path, Value: json.RawMessage(`"` + value + `"`)}},
		}}
	}

	tests := []struct {
		name, doc, message string
		want               []domain.ProblemFix
	}{
		{
			name: "runtime size", doc: edited(runtimeModel, runtimeModel+"    size: xlarge\n"),
			message: "spec.runtime.size: value must be one of 'small', 'medium', 'large'",
			want:    replace(domain.SectionBasics, "/size", "large"),
		},
		{
			// robot is 4 edits from both agent and done; agent comes first in the schema.
			name: "station kind", doc: edited("        kind: agent\n", "        kind: robot\n", workAgentLine, ""),
			message: "spec.board.stations[1].kind: value must be one of 'queue', 'agent', 'human', 'done'",
			want:    replace(domain.SectionLine, "/stations/1/kind", "agent"),
		},
		{
			// Every size is 10 edits away; small comes first.
			name: "no close match", doc: edited(runtimeModel, runtimeModel+"    size: zzzzzzzzzz\n"),
			message: "spec.runtime.size: value must be one of 'small', 'medium', 'large'",
			want:    replace(domain.SectionBasics, "/size", "small"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, problems := Check(checkTree(tt.doc, nil))

			i := indexOf(problems, codeSchemaEnum)
			if i < 0 {
				t.Fatalf("no %s problem in %+v", codeSchemaEnum, problems)
			}

			p := problems[i]
			if p.Message != tt.message || p.Check != checkSchema || p.Severity != domain.SeverityError {
				t.Errorf("problem = %+v, want message %q from the schema check", p, tt.message)
			}

			if !reflect.DeepEqual(p.Fixes, tt.want) {
				t.Errorf("fixes =\n%+v\nwant\n%+v", p.Fixes, tt.want)
			}
		})
	}
}

// A const violation names the one value allowed, so it gets no fix.
func TestEnumFix_NoneForAConst(t *testing.T) {
	t.Parallel()

	_, problems := Check(checkTree(edited("kind: Factory\n", "kind: Factor\n"), nil))

	i := indexOf(problems, codeSchemaEnum)
	if i < 0 {
		t.Fatalf("no %s problem in %+v", codeSchemaEnum, problems)
	}

	if fixes := problems[i].Fixes; fixes != nil {
		t.Errorf("fixes = %+v, want none", fixes)
	}
}

func TestEnumFix_Nearest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		got  string
		want []any
		best string
		ok   bool
	}{
		{got: "xlarge", want: []any{"small", "medium", "large"}, best: "large", ok: true},
		{got: "", want: []any{"ab", "a"}, best: "a", ok: true},
		{got: "b", want: []any{"ab", "ba"}, best: "ab", ok: true},
		{got: "x", want: []any{1, 2}},
		{got: "x", want: []any{1, "y"}, best: "y", ok: true},
	}

	for _, tt := range tests {
		best, ok := nearest(tt.got, tt.want)
		if best != tt.best || ok != tt.ok {
			t.Errorf("nearest(%q, %v) = %q, %v; want %q, %v", tt.got, tt.want, best, ok, tt.best, tt.ok)
		}
	}
}
