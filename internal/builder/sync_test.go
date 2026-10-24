package builder_test

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/builder"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

var t0 = time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)

const indexKey = "catalog/skills/index.json"

func commit(c byte) string { return strings.Repeat(string(c), 40) }

func ref(owner, repo, skill, c string) ports.SkillRef {
	return ports.SkillRef{Owner: owner, Repo: repo, Skill: skill, Commit: c}
}

func skillMD(description string) string {
	return "---\nname: x\ndescription: " + description + "\n---\n\n# Skill\n"
}

type fixture struct {
	svc     *builder.Service
	objects *porttest.ObjectStore
	clock   *porttest.Clock
	source  *porttest.SkillSource
	log     *bytes.Buffer
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	f := &fixture{
		objects: porttest.NewObjectStore(), clock: porttest.NewClock(t0), source: porttest.NewSkillSource(), log: &bytes.Buffer{},
	}
	f.svc = builder.New(f.objects, f.clock, builder.Config{
		Models: []builder.Model{{ID: "claude-opus-5-5", Label: "Claude Opus 5.5"}},
		Logger: slog.New(slog.NewJSONHandler(f.log, nil)),
	})

	return f
}

// sync syncs refs and fails the test on an index error.
func (f *fixture) sync(t *testing.T, refs ...ports.SkillRef) builder.SyncReport {
	t.Helper()

	r, err := f.svc.SyncSkills(t.Context(), f.source, refs, nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	return r
}

func statuses(r builder.SyncReport) string {
	var b strings.Builder

	for _, res := range r.Results {
		b.WriteString(res.Key + ":" + string(res.Status) + " ")
	}

	return strings.TrimSpace(b.String())
}

func TestParseRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, in string
		want     ports.SkillRef
		ok       bool
	}{
		{name: "head", in: "acme/skills@lint", want: ref("acme", "skills", "lint", ""), ok: true},
		{name: "pinned", in: "acme/skills@lint#" + commit('a'), want: ref("acme", "skills", "lint", commit('a')), ok: true},
		{name: "origin form", in: "skills.sh:acme/skills@lint", want: ref("acme", "skills", "lint", ""), ok: true},
		{name: "short commit", in: "acme/skills@lint#abc123"},
		{name: "upper-case commit", in: "acme/skills@lint#" + strings.Repeat("A", 40)},
		{name: "traversal", in: "acme/..@lint"},
		{name: "dot skill", in: "acme/skills@."},
		{name: "path in skill", in: "acme/skills@a/b"},
		{name: "no skill", in: "acme/skills"},
		{name: "space", in: "acme/my skills@lint"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := builder.ParseRef(tt.in)
			if tt.ok != (err == nil) || (tt.ok && got != tt.want) {
				t.Errorf("ParseRef(%q) = %+v, %v; want %+v, ok %v", tt.in, got, err, tt.want, tt.ok)
			}
		})
	}
}

// A sync writes a commit's folder and the index once; a sync that finds nothing new writes
// nothing.
func TestSyncSkills_WritesOnce(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.source.Add(ref("acme", "skills", "lint", commit('a')), 10, map[string]string{
		domain.SkillFile: skillMD(`"Lints   things\n\twell."`), "scripts/run.sh": "echo hi\n",
	})

	r := f.sync(t, ref("acme", "skills", "lint", ""))
	if statuses(r) != "acme/skills/lint:added" || r.Skills != 1 {
		t.Fatalf("first sync = %+v, want lint added", r)
	}

	file := "catalog/skills/acme/skills/lint/" + commit('a') + "/scripts/run.sh"

	if r := f.sync(t, ref("acme", "skills", "lint", "")); statuses(r) != "acme/skills/lint:unchanged" {
		t.Errorf("second sync = %s, want unchanged", statuses(r))
	}

	if f.objects.Versions(file) != 1 || f.objects.Versions(indexKey) != 1 {
		t.Errorf("versions: file %d, index %d; want one each", f.objects.Versions(file), f.objects.Versions(indexKey))
	}

	skills, err := f.svc.ListSkills(t.Context(), "")
	if err != nil || len(skills) != 1 || skills[0].Description != "Lints things well." || skills[0].Files != 2 {
		t.Errorf("list = %+v, %v; want lint with its one-line description and two files", skills, err)
	}
}

// A newer head adds a folder beside the old one, which stays: drafts may have vendored it.
func TestSyncSkills_NeverDeletes(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.source.Add(ref("acme", "skills", "lint", commit('a')), 0, map[string]string{domain.SkillFile: skillMD("Lints.")})
	f.sync(t, ref("acme", "skills", "lint", ""))

	f.source.Add(ref("acme", "skills", "lint", commit('b')), 0, map[string]string{domain.SkillFile: skillMD("Lints, v2.")})

	if r := f.sync(t, ref("acme", "skills", "lint", "")); statuses(r) != "acme/skills/lint:added" {
		t.Errorf("sync of a new head = %s, want added", statuses(r))
	}

	if _, err := f.objects.Get(t.Context(), "catalog/skills/acme/skills/lint/"+commit('a')+"/SKILL.md", ""); err != nil {
		t.Errorf("the old commit's folder is gone: %v", err)
	}

	skills, err := f.svc.ListSkills(t.Context(), "")
	if err != nil || len(skills) != 1 || skills[0].Commit != commit('b') || skills[0].Description != "Lints, v2." {
		t.Errorf("list = %+v, %v; want lint at the new commit", skills, err)
	}
}

// A failed skill is reported, keeps its previous entry, and does not stop the others.
func TestSyncSkills_ReportsFailures(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.source.Add(ref("acme", "skills", "lint", commit('a')), 0, map[string]string{domain.SkillFile: skillMD("Lint.")})
	f.source.Add(ref("acme", "skills", "nomd", commit('a')), 0, map[string]string{"README.md": "no skill file"})
	f.source.Add(ref("acme", "skills", "big", commit('a')), 0, map[string]string{
		domain.SkillFile: skillMD("Big."), "blob.bin": strings.Repeat("x", domain.MaxSkillBytes),
	})
	f.source.Add(ref("acme", "skills", "traversal", commit('a')), 0, map[string]string{domain.SkillFile: "x", "../evil": "x"})
	f.sync(t, ref("acme", "skills", "lint", ""))

	f.source.Add(ref("acme", "skills", "lint", commit('b')), 0, map[string]string{domain.SkillFile: skillMD("Lint 2.")})
	f.source.FailFetch(ref("acme", "skills", "lint", ""), errors.New("upstream down"))

	r := f.sync(t,
		ref("acme", "skills", "lint", ""), ref("acme", "skills", "nomd", ""), ref("acme", "skills", "big", ""),
		ref("acme", "skills", "traversal", ""), ref("acme", "skills", "gone", ""), ref("acme", "skills", "nomd", commit('c')),
	)

	want := "acme/skills/lint:failed acme/skills/nomd:failed acme/skills/big:failed acme/skills/traversal:failed acme/skills/gone:failed"
	if statuses(r) != want || r.Failed() != 5 {
		t.Errorf("report = %s (%d failed), want %s", statuses(r), r.Failed(), want)
	}

	if !errors.Is(r.Results[2].Err, ports.ErrTooLarge) || !errors.Is(r.Results[4].Err, ports.ErrNotFound) {
		t.Errorf("errors = %v, %v; want too large and not found", r.Results[2].Err, r.Results[4].Err)
	}

	skills, err := f.svc.ListSkills(t.Context(), "")
	if err != nil || len(skills) != 1 || skills[0].Commit != commit('a') {
		t.Errorf("list = %+v, %v; want lint still at its first commit", skills, err)
	}

	for _, key := range []string{"nomd", "big", "traversal"} {
		if infos, _ := f.objects.List(t.Context(), "catalog/skills/acme/skills/"+key+"/"); len(infos) != 0 {
			t.Errorf("failed skill %s wrote %d files", key, len(infos))
		}
	}
}

// A commit's folder already in the bucket is never overwritten: a sync that finds it different
// fails the skill.
func TestSyncSkills_RefusesAFolderThatDiffers(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.source.Add(ref("acme", "skills", "lint", commit('a')), 0, map[string]string{domain.SkillFile: skillMD("Lint.")})

	tampered := "catalog/skills/acme/skills/lint/" + commit('a') + "/SKILL.md"
	if _, err := f.objects.Put(t.Context(), tampered, []byte("something else")); err != nil {
		t.Fatal(err)
	}

	r := f.sync(t, ref("acme", "skills", "lint", ""))
	if statuses(r) != "acme/skills/lint:failed" || f.objects.Versions(tampered) != 1 {
		t.Errorf("report %s, %d versions; want failed and the object untouched", statuses(r), f.objects.Versions(tampered))
	}
}

// Search terms add their top listings, with their installs; a failing search is reported; a
// source that answers another commit than the one pinned is refused.
func TestSyncSkills_SearchAndPinning(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.source.Add(ref("acme", "skills", "lint-go", commit('a')), 50, map[string]string{domain.SkillFile: skillMD("Go.")})
	f.source.Add(ref("acme", "skills", "lint-ts", commit('a')), 90, map[string]string{domain.SkillFile: skillMD("TS.")})
	f.source.Add(ref("acme", "skills", "lint-py", commit('a')), 10, map[string]string{domain.SkillFile: skillMD("Py.")})

	f.source.FailSearch(func(q string) error {
		if q == "broken" {
			return errors.New("directory down")
		}

		return nil
	})

	r, err := f.svc.SyncSkills(t.Context(), f.source, []ports.SkillRef{ref("acme", "skills", "lint-py", commit('a'))},
		[]string{"lint", "broken"}, 2)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := statuses(r), "search:broken:failed acme/skills/lint-py:added acme/skills/lint-ts:added acme/skills/lint-go:added"; got != want {
		t.Errorf("report = %s, want %s", got, want)
	}

	skills, err := f.svc.ListSkills(t.Context(), "lint")
	if err != nil || len(skills) != 3 || skills[0].Skill != "lint-ts" || skills[0].Installs != 90 {
		t.Errorf("list = %+v, %v; want lint-ts first with 90 installs", skills, err)
	}

	if r := f.sync(t, ref("acme", "skills", "lint-go", commit('f'))); r.Failed() != 1 {
		t.Errorf("a pinned commit the source does not have = %s, want failed", statuses(r))
	}
}

// A malformed index is never replaced by a sync.
func TestSyncSkills_RefusesAMalformedIndex(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.source.Add(ref("acme", "skills", "lint", commit('a')), 0, map[string]string{domain.SkillFile: skillMD("Lint.")})

	if _, err := f.objects.Put(t.Context(), indexKey, []byte("{not json")); err != nil {
		t.Fatal(err)
	}

	if _, err := f.svc.SyncSkills(t.Context(), f.source, []ports.SkillRef{ref("acme", "skills", "lint", "")}, nil, 0); err == nil {
		t.Error("sync over a malformed index succeeded")
	}

	if f.objects.Versions(indexKey) != 1 {
		t.Error("the malformed index was replaced")
	}

	if _, err := f.svc.SyncSkills(t.Context(), f.source, nil, nil, 0); err == nil {
		t.Error("a sync of nothing succeeded")
	}
}
