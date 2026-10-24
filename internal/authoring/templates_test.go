package authoring_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/authoring"
)

func TestTemplates_LatestVersionPerNameAndRefresh(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	meta := "title: Acme review\nsummary: Build and review\ndescription: One engineer, one review.\n" +
		"connectors: [github]\nchanged: \"2026-10-09\"\n"
	h.putTemplate(t, "acme-review", "1", meta, acmeFiles(acmeYAML))
	h.putTemplate(t, "acme-review", "2", meta, acmeFiles(acmeYAML))
	h.putTemplate(t, "acme-source", "1", "title: Source\n", acmeFiles(credentialYAML))

	list, err := h.svc.Templates(h.ctx)
	if err != nil {
		t.Fatal(err)
	}

	want := authoring.TemplateSummary{
		Name: "acme-review", Version: 2, Title: "Acme review", Summary: "Build and review",
		Stations: 4, Agents: 1, Connectors: []string{"github"}, Changed: "2026-10-09",
	}
	if len(list) != 2 || !equalSummary(list[0], want) || list[1].Name != "acme-source" ||
		!slices.Equal(list[1].Connectors, []string{"github"}) {
		t.Fatalf("templates = %+v", list)
	}

	detail, err := h.svc.Template(h.ctx, "acme-review", 1)
	if err != nil || detail.Version != 1 || detail.Description != "One engineer, one review." || len(detail.Model.Line) == 0 {
		t.Errorf("template v1 = %+v, %v", detail, err)
	}

	if _, err := h.svc.Template(h.ctx, "acme-review", 7); !errors.Is(err, authoring.ErrTemplateNotFound) {
		t.Errorf("missing version = %v", err)
	}

	// A version pushed later shows once the index is older than five minutes.
	h.putTemplate(t, "acme-review", "3", meta, acmeFiles(acmeYAML))

	if list, _ := h.svc.Templates(h.ctx); list[0].Version != 2 {
		t.Errorf("the index refreshed early: v%d", list[0].Version)
	}

	h.clock.Advance(6 * time.Minute)

	if list, _ := h.svc.Templates(h.ctx); list[0].Version != 3 {
		t.Errorf("the index did not refresh: v%d", list[0].Version)
	}
}

func equalSummary(a, b authoring.TemplateSummary) bool {
	return a.Name == b.Name && a.Version == b.Version && a.Title == b.Title && a.Summary == b.Summary &&
		a.Stations == b.Stations && a.Agents == b.Agents && slices.Equal(a.Connectors, b.Connectors) && a.Changed == b.Changed
}
