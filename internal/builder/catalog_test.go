package builder_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/builder"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// everySkill is the seeded catalog in search order.
const everySkill = "d,b,c,a"

func keys(p builder.SkillPage) string {
	out := make([]string, 0, len(p.Skills))
	for _, s := range p.Skills {
		out = append(out, s.Skill)
	}

	return strings.Join(out, ",")
}

// seedCatalog syncs four skills: installs order them d (40), b and c (20, by key), a (10).
func seedCatalog(t *testing.T, f *fixture) {
	t.Helper()

	for _, s := range []struct {
		name, description string
		installs          int64
	}{{"a", "Alpha tool.", 10}, {"b", "Terraform style.", 20}, {"c", "Writes TERRAFORM modules.", 20}, {"d", "Delta.", 40}} {
		f.source.Add(ref("acme", "skills", s.name, commit('a')), s.installs, map[string]string{domain.SkillFile: skillMD(s.description)})
	}

	if _, err := f.svc.SyncSkills(t.Context(), f.source, nil, []string{""}, 10); err != nil {
		t.Fatal(err)
	}
}

func TestSearchSkills_OrderPagingAndMatch(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	seedCatalog(t, f)

	tests := []struct {
		name, q, after string
		limit          int
		want, next     string
	}{
		{name: "all, most installed first, then by key", want: everySkill},
		{name: "a page", limit: 2, want: "d,b", next: "acme/skills/b"},
		{name: "the next page", limit: 2, after: "acme/skills/b", want: "c,a"},
		{name: "description, any case", q: "terraform", want: "b,c"},
		{name: "key", q: "ACME/SKILLS/A", want: "a"},
		{name: "nothing", q: "kubernetes", want: ""},
		{name: "an after no longer listed ends the paging", after: "acme/skills/zz", want: ""},
		{name: "a limit out of range is the default", limit: 500, want: everySkill},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p, err := f.svc.SearchSkills(t.Context(), tt.q, tt.limit, tt.after)
			if err != nil || keys(p) != tt.want || p.NextAfter != tt.next {
				t.Errorf("search = %q next %q (%v), want %q next %q", keys(p), p.NextAfter, err, tt.want, tt.next)
			}
		})
	}
}

// The parsed index is reused for five minutes, then re-read only when the bucket's object
// changed (its ETag moved).
func TestSearchSkills_IndexCachedByETag(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	seedCatalog(t, f)

	if p, _ := f.svc.SearchSkills(t.Context(), "", 0, ""); keys(p) != everySkill {
		t.Fatalf("search = %s", keys(p))
	}

	// Another writer (the operator's CLI) adds a skill.
	other := builder.New(f.objects, f.clock, builder.Config{})
	f.source.Add(ref("acme", "skills", "e", commit('a')), 99, map[string]string{domain.SkillFile: skillMD("Echo.")})

	if _, err := other.SyncSkills(t.Context(), f.source, []ports.SkillRef{ref("acme", "skills", "e", "")}, nil, 0); err != nil {
		t.Fatal(err)
	}

	f.clock.Advance(4 * time.Minute)

	if p, _ := f.svc.SearchSkills(t.Context(), "", 0, ""); keys(p) != everySkill {
		t.Errorf("within five minutes search = %s, want the cached index", keys(p))
	}

	f.clock.Advance(2 * time.Minute)

	if p, _ := f.svc.SearchSkills(t.Context(), "", 0, ""); keys(p) != "d,b,c,a,e" {
		t.Errorf("after five minutes search = %s, want the new index", keys(p))
	}
}

// A malformed index is logged once per content (rewriting the same bytes keeps its ETag) and
// reads as an empty catalog.
func TestSearchSkills_MalformedIndexIsEmpty(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"not json":       "{",
		"invalid key":    `[{"key":"acme/../x","owner":"acme","repo":"..","skill":"x","commit":"` + commit('a') + `","files":1,"hash":"sha256:` + strings.Repeat("0", 64) + `"}]`,
		"invalid hash":   `[{"key":"acme/skills/x","owner":"acme","repo":"skills","skill":"x","commit":"` + commit('a') + `","files":1,"hash":"md5:1"}]`,
		"too many files": `[{"key":"acme/skills/x","owner":"acme","repo":"skills","skill":"x","commit":"` + commit('a') + `","files":201,"hash":"sha256:` + strings.Repeat("0", 64) + `"}]`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t)
			if _, err := f.objects.Put(t.Context(), indexKey, []byte(body)); err != nil {
				t.Fatal(err)
			}

			for range 2 {
				f.clock.Advance(10 * time.Minute)

				if _, err := f.objects.Put(t.Context(), indexKey, []byte(body)); err != nil {
					t.Fatal(err)
				}

				if p, err := f.svc.SearchSkills(t.Context(), "", 0, ""); err != nil || len(p.Skills) != 0 {
					t.Errorf("search = %+v, %v; want an empty page", p, err)
				}
			}

			if n := strings.Count(f.log.String(), "builder.skills_index_invalid"); n != 1 {
				t.Errorf("logged %d times, want once: %s", n, f.log)
			}
		})
	}
}

func TestSearchSkills_EmptyCatalog(t *testing.T) {
	t.Parallel()

	p, err := newFixture(t).svc.SearchSkills(t.Context(), "", 0, "")
	if err != nil || len(p.Skills) != 0 || p.NextAfter != "" {
		t.Errorf("search = %+v, %v; want an empty last page", p, err)
	}
}

func TestSkillFiles_ReturnsTheCheckedFolder(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.source.Add(ref("acme", "skills", "lint", commit('a')), 3, map[string]string{
		domain.SkillFile: skillMD("Lint."), "bin/tool": "\x00\x01binary",
	})
	f.sync(t, ref("acme", "skills", "lint", ""))

	key := builder.SkillKey{Owner: "acme", Repo: "skills", Skill: "lint"}

	sk, files, err := f.svc.SkillFiles(t.Context(), key)
	if err != nil || sk.Commit != commit('a') || len(files) != 2 || string(files["bin/tool"]) != "\x00\x01binary" {
		t.Fatalf("files = %+v %v, %v", sk, files, err)
	}

	if sk.Origin() != "skills.sh:acme/skills@lint#"+commit('a') {
		t.Errorf("origin = %s", sk.Origin())
	}

	for _, k := range []builder.SkillKey{{Owner: "acme", Repo: "skills", Skill: "nope"}, {Owner: "acme", Repo: "..", Skill: "lint"}} {
		if _, _, err := f.svc.SkillFiles(t.Context(), k); !errors.Is(err, domain.ErrSkillNotFound) {
			t.Errorf("files of %s = %v, want skill not found", k, err)
		}
	}

	// A file changed in the bucket behind the index's back is refused, and logged.
	if _, err := f.objects.Put(t.Context(), "catalog/skills/acme/skills/lint/"+commit('a')+"/bin/tool", []byte("evil")); err != nil {
		t.Fatal(err)
	}

	if _, _, err := f.svc.SkillFiles(t.Context(), key); !errors.Is(err, domain.ErrCatalogUnavailable) {
		t.Errorf("files of a tampered folder = %v, want catalog unavailable", err)
	}

	// So is one with a file more than the index lists.
	if _, err := f.objects.Put(t.Context(), "catalog/skills/acme/skills/lint/"+commit('a')+"/extra.md", []byte("x")); err != nil {
		t.Fatal(err)
	}

	if _, _, err := f.svc.SkillFiles(t.Context(), key); !errors.Is(err, domain.ErrCatalogUnavailable) {
		t.Errorf("files of a folder with an extra file = %v, want catalog unavailable", err)
	}

	if !strings.Contains(f.log.String(), "builder.skill_files_failed") {
		t.Errorf("log = %s, want builder.skill_files_failed", f.log)
	}
}
