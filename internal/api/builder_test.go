package api_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// flakyObjects is a bucket that can be made to fail.
type flakyObjects struct {
	*porttest.ObjectStore

	down atomic.Bool
}

var errBucketDown = errors.New("bucket down: s3://catalog-internal")

func (f *flakyObjects) List(ctx context.Context, prefix string) ([]ports.ObjectInfo, error) {
	if f.down.Load() {
		return nil, errBucketDown
	}

	return f.ObjectStore.List(ctx, prefix)
}

func (f *flakyObjects) Get(ctx context.Context, key string) (ports.Object, error) {
	if f.down.Load() {
		return ports.Object{}, errBucketDown
	}

	return f.ObjectStore.Get(ctx, key)
}

var skillCommit = strings.Repeat("c", 40)

// skillOwner owns the test skills.
const skillOwner = "acme"

// syncSkills puts lint (with a text and a binary file) and fmt into the catalog, as the
// operator's goal catalog skills sync does.
func (h *harness) syncSkills(t *testing.T) {
	t.Helper()

	for _, s := range []struct {
		name     string
		installs int64
		files    map[string]string
	}{
		{"lint", 30, map[string]string{"SKILL.md": "---\nname: lint\ndescription: Lints the code.\n---\n", "bin/tool": "\x00\xffbinary"}},
		{"fmt", 10, map[string]string{"SKILL.md": "---\ndescription: Formats it.\n---\n"}},
	} {
		h.skillSource.Add(ports.SkillRef{Owner: skillOwner, Repo: "skills", Skill: s.name, Commit: skillCommit}, s.installs, s.files)
	}

	r, err := h.builder.SyncSkills(t.Context(), h.skillSource, nil, []string{""}, 10)
	if err != nil || r.Failed() != 0 {
		t.Fatalf("sync = %+v, %v", r, err)
	}
}

func TestBuilder_Meta(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	var m gen.BuilderMeta
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/builder/meta", "", &m); code != http.StatusOK {
		t.Fatalf("meta = %d", code)
	}

	want := gen.BuilderMeta{
		Models: []gen.BuilderModel{
			{Id: "claude-opus-5-5", Label: "Claude Opus 5.5", Default: true}, {Id: modelHaiku, Label: "Claude Haiku 5.5"},
		},
		Limits: gen.BuilderLimits{SkillBytes: domain.MaxSkillBytes, SkillFiles: domain.MaxSkillFiles},
	}

	got, _ := json.Marshal(m)
	if wantJSON, _ := json.Marshal(want); string(got) != string(wantJSON) {
		t.Errorf("meta = %s, want %s", got, wantJSON)
	}
}

func TestBuilder_SearchSkillsIsEmptyUntilSynced(t *testing.T) {
	t.Parallel()

	var empty gen.SkillPage
	if code := newHarness(t).call(t, http.MethodGet, "/v1/orgs/acme/builder/skills", "", &empty); code != http.StatusOK ||
		len(empty.Skills) != 0 || empty.NextAfter != nil {
		t.Errorf("an unsynced catalog = %d %+v, want an empty last page", code, empty)
	}
}

func TestBuilder_SearchSkills(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.syncSkills(t)

	var page gen.SkillPage
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/builder/skills?limit=1", "", &page); code != http.StatusOK {
		t.Fatalf("first page = %d", code)
	}

	lint := gen.SkillListing{
		Key: "acme/skills/lint", Owner: skillOwner, Repo: "skills", Skill: "lint", Commit: skillCommit, Description: "Lints the code.",
		Installs: 30, Files: 2, Bytes: int64(len("---\nname: lint\ndescription: Lints the code.\n---\n") + len("\x00\xffbinary")),
	}

	got, _ := json.Marshal(page)
	if want, _ := json.Marshal(gen.SkillPage{Skills: []gen.SkillListing{lint}, NextAfter: &lint.Key}); string(got) != string(want) {
		t.Errorf("first page = %s, want %s", got, want)
	}

	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/builder/skills?limit=1&after=acme/skills/lint", "", &page); code != http.StatusOK ||
		len(page.Skills) != 1 || page.Skills[0].Skill != "fmt" || page.NextAfter != nil {
		t.Errorf("second page = %d %+v", code, page)
	}
}

func TestBuilder_SearchSkillsRefusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for _, q := range []string{"limit=51", "limit=0", "q=" + strings.Repeat("x", 201)} {
		if code := h.call(t, http.MethodGet, "/v1/orgs/acme/builder/skills?"+q, "", nil); code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", q, code)
		}
	}

	var p gen.Problem
	if code := h.call(t, http.MethodGet, "/v1/orgs/nope/builder/skills", "", &p); code != http.StatusNotFound || deref(p.Code) != "org-not-found" {
		t.Errorf("unknown organization = %d %+v, want 404 org-not-found", code, p)
	}
}

func TestBuilder_SkillFiles(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.syncSkills(t)

	var out gen.SkillFiles
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/builder/skills/acme/skills/lint/files", "", &out); code != http.StatusOK {
		t.Fatalf("files = %d", code)
	}

	text, err := out.Files["SKILL.md"].AsTextFile()
	if err != nil || !strings.Contains(text.Text, "Lints the code.") {
		t.Errorf("SKILL.md = %+v, %v; want it as text", text, err)
	}

	var raw map[string]map[string]string
	if b, _ := json.Marshal(out.Files); json.Unmarshal(b, &raw) != nil || raw["bin/tool"]["base64"] != base64.StdEncoding.EncodeToString([]byte("\x00\xffbinary")) {
		t.Errorf("bin/tool = %v, want it in base64", raw["bin/tool"])
	}

	if out.Skill.Key != "acme/skills/lint" || out.Skill.Commit != skillCommit {
		t.Errorf("skill = %+v", out.Skill)
	}

	var p gen.Problem
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/builder/skills/acme/skills/nope/files", "", &p); code != http.StatusNotFound ||
		deref(p.Code) != "skill-not-found" {
		t.Errorf("unknown skill = %d %+v, want 404 skill-not-found", code, p)
	}

	h.skills.down.Store(true)

	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/builder/skills/acme/skills/lint/files", "", &p); code != http.StatusServiceUnavailable ||
		deref(p.Code) != "catalog-unavailable" || strings.Contains(deref(p.Detail), "catalog-internal") {
		t.Errorf("bucket down = %d %+v, want 503 catalog-unavailable without the bucket's error", code, p)
	}
}

func TestBuilder_CatalogUnavailable(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.skills.down.Store(true)

	var p gen.Problem
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/builder/skills", "", &p); code != http.StatusServiceUnavailable ||
		deref(p.Code) != "catalog-unavailable" {
		t.Errorf("search with the bucket down = %d %+v, want 503 catalog-unavailable", code, p)
	}
}
