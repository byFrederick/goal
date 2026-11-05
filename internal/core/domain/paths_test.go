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

// Entries compare repository and path: an `owner/name:` prefix names the repository, any host
// and case alike, an entry without one is in the item's first repository, and one whose
// repository is unknown meets every repository's paths.
func TestRepositoryPathsOverlap(t *testing.T) {
	t.Parallel()

	const (
		web = "github.com/acme/web"
		api = "github.com/acme/api"
	)

	for _, tc := range []struct {
		name   string
		a      []string
		firstA string
		b      []string
		firstB string
		want   []PathOverlap
	}{
		{"same glob, same first repository", []string{"internal/**"}, web, []string{"internal/a.go"}, web, []PathOverlap{{"internal/**", "internal/a.go"}}},
		{"same glob in two repositories", []string{"internal/**"}, web, []string{"internal/**"}, api, nil},
		{"unprefixed against a prefix naming the first repository", []string{"acme/web:internal/**"}, api, []string{"internal/a.go"}, web,
			[]PathOverlap{{"acme/web:internal/**", "internal/a.go"}}},
		{"prefix naming another repository", []string{"acme/api:internal/**"}, web, []string{"internal/a.go"}, web, nil},
		{"mixed-case prefix", []string{"Acme/Web:internal/**"}, api, []string{"internal/**"}, "gitlab.example.com/ACME/web",
			[]PathOverlap{{"Acme/Web:internal/**", "internal/**"}}},
		{"two prefixes in one repository", []string{"acme/web:go.mod"}, "", []string{"acme/web:go.mod"}, "", []PathOverlap{{"acme/web:go.mod", "acme/web:go.mod"}}},
		{"unknown repository meets every repository", []string{"internal/**"}, "", []string{"acme/api:internal/a.go", "internal/b.go"}, web,
			[]PathOverlap{{"internal/**", "acme/api:internal/a.go"}, {"internal/**", "internal/b.go"}}},
		{"a drive letter is not a prefix", []string{"C:internal/**"}, web, []string{"C:internal/a.go"}, web, []PathOverlap{{"C:internal/**", "C:internal/a.go"}}},
		{"a colon without a slash before it is not a prefix", []string{"docs:a/b.md"}, web, []string{"docs:a/*.md"}, web, []PathOverlap{{"docs:a/b.md", "docs:a/*.md"}}},
		{"a wildcard prefix is not a prefix", []string{"acme/*:a"}, web, []string{"acme/*:a"}, api, nil},
		{"every pair is reported", []string{"acme/api:a/**", "b/x"}, web, []string{"acme/api:a/y", "b/*", "acme/api:b/x"}, api,
			[]PathOverlap{{"acme/api:a/**", "acme/api:a/y"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := RepositoryPathsOverlap(tc.a, tc.firstA, tc.b, tc.firstB); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("RepositoryPathsOverlap(%q, %q, %q, %q) = %v, want %v", tc.a, tc.firstA, tc.b, tc.firstB, got, tc.want)
			}

			if got := RepositoryPathsOverlap(tc.b, tc.firstB, tc.a, tc.firstA); len(got) != len(tc.want) {
				t.Errorf("reversed = %v, want %d pairs (overlap is symmetric)", got, len(tc.want))
			}
		})
	}
}

// RepositoryGlobs keeps the entries in one repository, without their prefix.
func TestRepositoryGlobs(t *testing.T) {
	t.Parallel()

	paths := []string{"internal/**", "acme/web:web/**", "acme/api:api/**", "C:x"}

	if got, want := RepositoryGlobs(paths, "github.com/acme/web", "github.com/acme/web"), []string{"internal/**", "web/**", "C:x"}; !reflect.DeepEqual(got, want) {
		t.Errorf("web's globs = %q, want %q", got, want)
	}

	if got, want := RepositoryGlobs(paths, "github.com/acme/web", "github.com/acme/api"), []string{"api/**"}; !reflect.DeepEqual(got, want) {
		t.Errorf("api's globs = %q, want %q", got, want)
	}

	if got, want := RepositoryGlobs(paths, "", "github.com/acme/api"), []string{"internal/**", "api/**", "C:x"}; !reflect.DeepEqual(got, want) {
		t.Errorf("api's globs with no first repository = %q, want %q", got, want)
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
