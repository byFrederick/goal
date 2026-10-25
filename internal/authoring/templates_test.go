package authoring_test

import (
	"errors"
	"fmt"
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
		"connectors: [github]\nchanged: \"2026-10-09\"\n"
	h.putTemplate(t, reviewTemplate, "1", meta, acmeFiles(acmeYAML))
	h.putTemplate(t, reviewTemplate, "2", meta, acmeFiles(acmeYAML))
	h.putTemplate(t, "acme-source", "1", "title: Source\n", acmeFiles(credentialYAML))

	list, err := h.svc.Templates(h.ctx)
	if err != nil {
		t.Fatal(err)
	}

	want := authoring.TemplateSummary{
		Name: reviewTemplate, Version: 2, Title: "Acme review", Summary: "Build and review",
		Stations: 4, Agents: 1, Connectors: []string{"github"}, Changed: "2026-10-09",
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

// reviewTemplate is the template the tests read and push.
const reviewTemplate = "acme-review"

// nilIDYAML is acmeYAML with a template's id.
var nilIDYAML = strings.Replace(acmeYAML, "0b6f1c9e-5d0a-4c39-9a51-6f1f3f0e2a10", "00000000-0000-0000-0000-000000000000", 1)

func TestTemplateCatalog_Push(t *testing.T) {
	t.Parallel()

	meta := []byte("title: Acme review\nsummary: Build and review\n")
	h := newHarness(t)
	catalog := authoring.NewTemplateCatalog(h.objects, h.clock)

	if err := catalog.Push(h.ctx, authoring.TemplateUpload{Name: reviewTemplate, Version: 1, Meta: meta, Files: acmeFiles(nilIDYAML)}); err != nil {
		t.Fatalf("push: %v", err)
	}

	// goald's index lists what the operator pushed.
	got, err := h.svc.Template(h.ctx, reviewTemplate, 0)
	if err != nil || got.Version != 1 || got.Title != "Acme review" || got.Stations != 4 || got.Agents != 1 {
		t.Fatalf("pushed template = %+v, %v", got, err)
	}

	again := catalog.Push(h.ctx, authoring.TemplateUpload{Name: reviewTemplate, Version: 1, Meta: meta, Files: acmeFiles(nilIDYAML)})
	if !errors.Is(again, authoring.ErrTemplateExists) {
		t.Errorf("push of an existing version = %v, want ErrTemplateExists", again)
	}

	badName := catalog.Push(h.ctx, authoring.TemplateUpload{Name: "Acme Review", Version: 1, Meta: meta, Files: acmeFiles(nilIDYAML)})
	if !errors.Is(badName, authoring.ErrInvalidRequest) {
		t.Errorf("push of a bad name = %v, want ErrInvalidRequest", badName)
	}
}

func TestTemplateCatalog_PushRefusesInvalidTemplates(t *testing.T) {
	t.Parallel()

	meta := "title: Acme review\n"

	tests := []struct {
		name  string
		meta  string
		files map[string][]byte
		want  []string // code@file:line
	}{
		{name: "real-id", meta: meta, files: acmeFiles(acmeYAML), want: []string{"template-id@factory/factory.yaml:5"}},
		{name: "no-title", meta: "summary: x\n", files: acmeFiles(nilIDYAML), want: []string{"template-meta-invalid@template.yaml:0"}},
		{name: "unknown-key", meta: "title: x\nauthor: y\n", files: acmeFiles(nilIDYAML), want: []string{"template-meta-invalid@template.yaml:0"}},
		{name: "broken", meta: meta, files: map[string][]byte{yamlFile: []byte(nilIDYAML)}, want: []string{"prompt-not-found@factory/factory.yaml:15"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			err := authoring.NewTemplateCatalog(h.objects, h.clock).Push(h.ctx,
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
		a.Stations == b.Stations && a.Agents == b.Agents && slices.Equal(a.Connectors, b.Connectors) && a.Changed == b.Changed
}
