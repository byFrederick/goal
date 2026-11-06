package authoring_test

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/authoring"
)

func TestTemplates_LatestVersionPerNameAndRefresh(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	meta := "title: Acme review\nsummary: Build and review\ndescription: One engineer, one review.\n" +
		"connectors: [github]\nchanged: \"2026-10-09\"\ncode: read\n"
	h.putTemplate(t, reviewTemplate, "1", meta, acmeFiles(acmeYAML))
	h.putTemplate(t, reviewTemplate, "2", meta, acmeFiles(acmeYAML))
	h.putTemplate(t, "acme-source", "1", "title: Source\ncode: none\n", acmeFiles(credentialYAML))

	list, err := h.svc.Templates(h.ctx)
	if err != nil {
		t.Fatal(err)
	}

	want := authoring.TemplateSummary{
		Name: reviewTemplate, Version: 2, Title: "Acme review", Summary: "Build and review",
		Stations: 4, Agents: 1, Connectors: []string{"github"}, Changed: "2026-10-09", Code: "read",
	}
	if len(list) != 2 || !equalSummary(list[0], want) || list[1].Name != "acme-source" ||
		!slices.Equal(list[1].Connectors, []string{"github"}) {
		t.Fatalf("templates = %+v", list)
	}

	detail, err := h.svc.Template(h.ctx, reviewTemplate, 1)
	if err != nil || detail.Version != 1 || detail.Description != "One engineer, one review." || len(detail.Model.Line) == 0 {
		t.Errorf("template v1 = %+v, %v", detail, err)
	}

	if _, err := h.svc.Template(h.ctx, reviewTemplate, 7); !errors.Is(err, authoring.ErrTemplateNotFound) {
		t.Errorf("missing version = %v", err)
	}

	// A version pushed later shows once the index is older than five minutes.
	h.putTemplate(t, reviewTemplate, "3", meta, acmeFiles(acmeYAML))

	if list, _ := h.svc.Templates(h.ctx); list[0].Version != 2 {
		t.Errorf("the index refreshed early: v%d", list[0].Version)
	}

	h.clock.Advance(6 * time.Minute)

	if list, _ := h.svc.Templates(h.ctx); list[0].Version != 3 {
		t.Errorf("the index did not refresh: v%d", list[0].Version)
	}
}

func TestTemplates_CodeIsWriteWhenTheLineMerges(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.putTemplate(t, "acme-none", "1", "title: None\ncode: none\n", acmeFiles(acmeYAML))
	h.putTemplate(t, "acme-read", "1", "title: Read\ncode: read\n", acmeFiles(acmeYAML))
	// A line that merges needs write, whatever template.yaml says.
	h.putTemplate(t, "acme-merge", "1", "title: Merge\ncode: read\n", acmeFiles(mergeYAML))
	// A version pushed before template.yaml had code reads as none, or write when its line merges.
	h.putTemplate(t, "acme-old", "1", "title: Old\n", acmeFiles(acmeYAML))
	h.putTemplate(t, "acme-old-merge", "1", "title: Old merge\n", acmeFiles(mergeYAML))

	list, err := h.svc.Templates(h.ctx)
	if err != nil {
		t.Fatal(err)
	}

	codes := map[string]string{}
	for _, s := range list {
		codes[s.Name] = s.Code
	}

	want := map[string]string{
		"acme-none": authoring.CodeNone, "acme-read": authoring.CodeRead, "acme-merge": authoring.CodeWrite,
		"acme-old": authoring.CodeNone, "acme-old-merge": authoring.CodeWrite,
	}
	if !maps.Equal(codes, want) {
		t.Errorf("code = %v, want %v", codes, want)
	}
}

func TestTemplates_VersionWithoutMetaIsNotListed(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	// A push that stopped before template.yaml: only the definition is there.
	if _, err := h.objects.Put(h.ctx, authoring.TemplatesPrefix+"acme-review/1/factory/factory.yaml", []byte(acmeYAML)); err != nil {
		t.Fatal(err)
	}

	if list, err := h.svc.Templates(h.ctx); err != nil || len(list) != 0 {
		t.Errorf("templates = %+v, %v; want none", list, err)
	}
}

// metaInvalid is the problem of a template.yaml Push refuses.
const metaInvalid = "template-meta-invalid@template.yaml:0"

// reviewTemplate is the template the tests read and push.
const reviewTemplate = "acme-review"

// nilIDYAML is acmeYAML with a template's id.
var nilIDYAML = strings.Replace(acmeYAML, "0b6f1c9e-5d0a-4c39-9a51-6f1f3f0e2a10", "00000000-0000-0000-0000-000000000000", 1)

func TestTemplateCatalog_Push(t *testing.T) {
	t.Parallel()

	meta := []byte("title: Acme review\nsummary: Build and review\ncode: read\n")
	h := newHarness(t)
	catalog := authoring.NewTemplateCatalog(h.objects, h.clock)

	if added, err := catalog.Push(h.ctx, authoring.TemplateUpload{Name: reviewTemplate, Version: 1, Meta: meta, Files: acmeFiles(nilIDYAML)}); err != nil || !added {
		t.Fatalf("push = %v, %v; want added", added, err)
	}

	// goald's index lists what the operator pushed.
	got, err := h.svc.Template(h.ctx, reviewTemplate, 0)
	if err != nil || got.Version != 1 || got.Title != "Acme review" || got.Stations != 4 || got.Agents != 1 {
		t.Fatalf("pushed template = %+v, %v", got, err)
	}

	if _, err := catalog.Push(h.ctx, authoring.TemplateUpload{Name: "Acme Review", Version: 1, Meta: meta, Files: acmeFiles(nilIDYAML)}); !errors.Is(err, authoring.ErrInvalidRequest) {
		t.Errorf("push of a bad name = %v, want ErrInvalidRequest", err)
	}

	// Validation runs before the bucket is read: an invalid upload of the stored version is
	// refused, not taken as unchanged.
	if _, err := catalog.Push(h.ctx, authoring.TemplateUpload{Name: reviewTemplate, Version: 1, Meta: []byte("title: x\n"), Files: acmeFiles(nilIDYAML)}); !isInvalid(err) {
		t.Errorf("invalid push of a stored version = %v, want *TemplateInvalidError", err)
	}
}

func isInvalid(err error) bool {
	_, ok := errors.AsType[*authoring.TemplateInvalidError](err)

	return ok
}

func TestTemplateCatalog_PushSameVersionTwice(t *testing.T) {
	t.Parallel()

	meta := []byte("title: Acme review\ncode: read\n")
	h := newHarness(t)
	catalog := authoring.NewTemplateCatalog(h.objects, h.clock)
	up := authoring.TemplateUpload{Name: reviewTemplate, Version: 1, Meta: meta, Files: acmeFiles(nilIDYAML)}

	if added, err := catalog.Push(h.ctx, up); err != nil || !added {
		t.Fatalf("first push = %v, %v; want added", added, err)
	}

	base := authoring.TemplatesPrefix + reviewTemplate + "/1/"

	before, err := h.objects.List(h.ctx, base)
	if err != nil {
		t.Fatal(err)
	}

	if added, err := catalog.Push(h.ctx, up); err != nil || added {
		t.Fatalf("second push = %v, %v; want unchanged and no error", added, err)
	}

	after, err := h.objects.List(h.ctx, base)
	if err != nil || !slices.Equal(before, after) {
		t.Errorf("objects after the second push = %+v, %v; want %+v", after, err, before)
	}

	for _, o := range after {
		if n := h.objects.Writes(o.Key); n != 1 {
			t.Errorf("%s was written %d times, want once", o.Key, n)
		}
	}
}

func TestTemplateCatalog_PushRefusesChangedVersion(t *testing.T) {
	t.Parallel()

	meta := []byte("title: Acme review\ncode: read\n")
	base := authoring.TemplatesPrefix + reviewTemplate + "/1/"

	with := func(edit func(map[string][]byte)) map[string][]byte {
		files := acmeFiles(nilIDYAML)
		edit(files)

		return files
	}

	tests := []struct {
		name  string
		meta  []byte
		files map[string][]byte
		first map[string][]byte // the files of the first push, acmeFiles when nil
		extra string            // a key put under the version's prefix before the second push
	}{
		{name: "meta-differs", meta: []byte("title: Acme review, changed\ncode: read\n"), files: acmeFiles(nilIDYAML)},
		{name: "file-differs", meta: meta, files: with(func(f map[string][]byte) { f[promptFile] = []byte("# changed\n") })},
		{name: "file-added", meta: meta, files: with(func(f map[string][]byte) { f["agents/extra.md"] = []byte("# extra\n") })},
		{name: "file-missing", meta: meta, files: acmeFiles(nilIDYAML), first: with(func(f map[string][]byte) { f["agents/extra.md"] = []byte("# extra\n") })},
		{name: "extra-object", meta: meta, files: acmeFiles(nilIDYAML), extra: base + "factory/stray.md"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			catalog := authoring.NewTemplateCatalog(h.objects, h.clock)

			first := tt.first
			if first == nil {
				first = acmeFiles(nilIDYAML)
			}

			if _, err := catalog.Push(h.ctx, authoring.TemplateUpload{Name: reviewTemplate, Version: 1, Meta: meta, Files: first}); err != nil {
				t.Fatalf("first push: %v", err)
			}

			if tt.extra != "" {
				if _, err := h.objects.Put(h.ctx, tt.extra, []byte("stray")); err != nil {
					t.Fatal(err)
				}
			}

			added, err := catalog.Push(h.ctx, authoring.TemplateUpload{Name: reviewTemplate, Version: 1, Meta: tt.meta, Files: tt.files})
			if added || !errors.Is(err, authoring.ErrTemplateExists) {
				t.Errorf("push of a changed version = %v, %v; want ErrTemplateExists", added, err)
			}
		})
	}

	// A partial earlier push, with no template.yaml, is not repaired.
	h := newHarness(t)
	catalog := authoring.NewTemplateCatalog(h.objects, h.clock)

	for p, body := range acmeFiles(nilIDYAML) {
		if _, err := h.objects.Put(h.ctx, base+"factory/"+p, body); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := catalog.Push(h.ctx, authoring.TemplateUpload{Name: reviewTemplate, Version: 1, Meta: meta, Files: acmeFiles(nilIDYAML)}); !errors.Is(err, authoring.ErrTemplateExists) {
		t.Errorf("push over a partial version = %v, want ErrTemplateExists", err)
	}
}

func TestTemplateCatalog_PushRefusesInvalidTemplates(t *testing.T) {
	t.Parallel()

	meta := "title: Acme review\ncode: none\n"

	tests := []struct {
		name  string
		meta  string
		files map[string][]byte
		want  []string // code@file:line
	}{
		{name: "real-id", meta: meta, files: acmeFiles(acmeYAML), want: []string{"template-id@factory/factory.yaml:5"}},
		{name: "no-title", meta: "summary: x\ncode: none\n", files: acmeFiles(nilIDYAML), want: []string{metaInvalid}},
		{name: "unknown-key", meta: "title: x\ncode: none\nauthor: y\n", files: acmeFiles(nilIDYAML), want: []string{metaInvalid}},
		{name: "no-code", meta: "title: x\n", files: acmeFiles(nilIDYAML), want: []string{metaInvalid}},
		{name: "code-write", meta: "title: x\ncode: write\n", files: acmeFiles(nilIDYAML), want: []string{metaInvalid}},
		{name: "code-yes", meta: "title: x\ncode: yes\n", files: acmeFiles(nilIDYAML), want: []string{metaInvalid}},
		{name: "broken", meta: meta, files: map[string][]byte{yamlFile: []byte(nilIDYAML)}, want: []string{
			"prompt-not-found@factory/factory.yaml:15", "environment-default-missing@factory/environments/default.yaml:0",
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			_, err := authoring.NewTemplateCatalog(h.objects, h.clock).Push(h.ctx,
				authoring.TemplateUpload{Name: tt.name, Version: 1, Meta: []byte(tt.meta), Files: tt.files})

			invalid, ok := errors.AsType[*authoring.TemplateInvalidError](err)
			if !ok {
				t.Fatalf("push = %v, want a TemplateInvalidError", err)
			}

			var got []string
			for _, p := range invalid.Problems {
				got = append(got, fmt.Sprintf("%s@%s:%d", p.Code, p.File, p.Line))
			}

			if !slices.Equal(got, tt.want) {
				t.Errorf("problems = %v, want %v", got, tt.want)
			}

			if objs, _ := h.objects.List(h.ctx, authoring.TemplatesPrefix); len(objs) != 0 {
				t.Errorf("a refused push wrote %d objects", len(objs))
			}
		})
	}
}

func equalSummary(a, b authoring.TemplateSummary) bool {
	return a.Name == b.Name && a.Version == b.Version && a.Title == b.Title && a.Summary == b.Summary &&
		a.Stations == b.Stations && a.Agents == b.Agents && slices.Equal(a.Connectors, b.Connectors) && a.Changed == b.Changed &&
		a.Code == b.Code
}
