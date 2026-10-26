package domain

import (
	"fmt"
	"reflect"
	"testing"
)

const (
	goMod   = "go.mod"
	webTree = "web/**"
)

func TestPathsOverlap(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		a, b []string
		want []PathOverlap
	}{
		{"equal literals", []string{goMod}, []string{goMod}, []PathOverlap{{goMod, goMod}}},
		{"different literals", []string{goMod}, []string{"go.sum"}, nil},
		{
			"literal and a glob that matches it",
			[]string{"internal/core/domain/edge.go"},
			[]string{"internal/core/domain/*.go"},
			[]PathOverlap{{"internal/core/domain/edge.go", "internal/core/domain/*.go"}},
		},
		{"literal and a glob that does not match it", []string{"internal/core/domain/edge.md"}, []string{"internal/core/domain/*.go"}, nil},
		{
			"double star below a directory",
			[]string{"internal/core/engine/**"},
			[]string{"internal/core/engine/claim.go"},
			[]PathOverlap{{"internal/core/engine/**", "internal/core/engine/claim.go"}},
		},
		{"double star matches the directory itself", []string{webTree}, []string{"web"}, []PathOverlap{{webTree, "web"}}},
		{"any go file and the web tree", []string{"**/*.go"}, []string{webTree}, []PathOverlap{{"**/*.go", webTree}}},
		{"disjoint trees", []string{"internal/**"}, []string{webTree}, nil},
		{"star stays inside one segment", []string{"*.md"}, []string{"docs/*.md"}, nil},
		{"star against a deeper literal", []string{"docs/*"}, []string{"docs/adr/0001.md"}, nil},
		{"two stars in one segment", []string{"a*b"}, []string{"*c*"}, []PathOverlap{{"a*b", "*c*"}}},
		{"stars that cannot meet", []string{"a*"}, []string{"b*"}, nil},
		{
			"braces",
			[]string{"web/src/**/*.{ts,tsx}"},
			[]string{"web/src/a.tsx"},
			[]PathOverlap{{"web/src/**/*.{ts,tsx}", "web/src/a.tsx"}},
		},
		{"braces without the extension", []string{"web/src/**/*.{ts,tsx}"}, []string{"web/src/a.css"}, nil},
		{
			"braces across segments",
			[]string{"{internal/core/domain,web}/x.go"},
			[]string{"web/*.go"},
			[]PathOverlap{{"{internal/core/domain,web}/x.go", "web/*.go"}},
		},
		{"question mark", []string{"a?c"}, []string{"abc"}, []PathOverlap{{"a?c", "abc"}}},
		{"question mark is one character", []string{"a?c"}, []string{"ac"}, nil},
		{"class matches", []string{"file[0-9].go"}, []string{"file7.go"}, []PathOverlap{{"file[0-9].go", "file7.go"}}},
		{"class does not match", []string{"file[0-9].go"}, []string{"filex.go"}, nil},
		{"classes that intersect", []string{"[a-f]"}, []string{"[e-k]"}, []PathOverlap{{"[a-f]", "[e-k]"}}},
		{"classes that do not intersect", []string{"[a-c]"}, []string{"[x-z]"}, nil},
		{"negated class against a literal it excludes", []string{"[!a]"}, []string{"a"}, nil},
		{"negated class against a literal it allows", []string{"[^a]"}, []string{"b"}, []PathOverlap{{"[^a]", "b"}}},
		{"negated class against a class: conservative", []string{"[!a]"}, []string{"[a]"}, []PathOverlap{{"[!a]", "[a]"}}},
		{"escaped star is literal", []string{`a\*`}, []string{"ab"}, nil},
		{"leading dot slash", []string{"./go.mod"}, []string{goMod}, []PathOverlap{{"./go.mod", goMod}}},
		{"empty list", nil, []string{"**"}, nil},
		{"blank glob", []string{" "}, []string{"**"}, nil},
		{
			"every pair is reported",
			[]string{"a/**", "b/x", "c"},
			[]string{"b/*", "a/y"},
			[]PathOverlap{{"a/**", "a/y"}, {"b/x", "b/*"}},
		},
		{"adversarial double stars", []string{"**/**/**/**/**/**/**/**/**/**/a"}, []string{"**/**/**/**/**/**/**/**/**/**/b"}, nil},
		{
			"adversarial double stars that meet",
			[]string{"**/**/**/**/**/a/**/**/**/**"},
			[]string{"**/**/**/**/**/b/**/**/**/**"},
			[]PathOverlap{{"**/**/**/**/**/a/**/**/**/**", "**/**/**/**/**/b/**/**/**/**"}},
		},
		{"adversarial stars", []string{"*a*a*a*a*a*a*a*a*a*a*a*a*a*a*a*a*x"}, []string{"*a*a*a*a*a*a*a*a*a*a*a*a*a*a*a*a*y"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := PathsOverlap(tc.a, tc.b); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("PathsOverlap(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}

			if got := PathsOverlap(tc.b, tc.a); len(got) != len(tc.want) {
				t.Errorf("PathsOverlap(%q, %q) = %v, want %d pairs (overlap is symmetric)", tc.b, tc.a, got, len(tc.want))
			}
		})
	}
}

// A glob whose braces expand past the limit is held as overlapping everything.
func TestPathsOverlap_TooManyAlternativesOverlapEverything(t *testing.T) {
	t.Parallel()

	g := "{a,b,c,d}{a,b,c,d}{a,b,c,d}{a,b,c,d}{a,b,c,d}"
	if got := PathsOverlap([]string{g}, []string{"zzz/y"}); len(got) != 1 {
		t.Errorf("PathsOverlap(%q, zzz/y) = %v, want one conservative pair", g, got)
	}
}

func BenchmarkPathsOverlap(b *testing.B) {
	a := make([]string, 0, 12)
	c := make([]string, 0, 12)

	for i := range 12 {
		a = append(a, fmt.Sprintf("internal/pkg%d/**/*.{go,sql}", i))
		c = append(c, fmt.Sprintf("**/pkg%d/file[0-9]_?.go", i+6))
	}

	for b.Loop() {
		PathsOverlap(a, c)
	}
}
