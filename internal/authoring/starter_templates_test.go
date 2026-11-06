package authoring_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// starterTemplates is where the install's starter templates are committed (ADR 0042).
const starterTemplates = "../../deploy/catalog/templates"

// starterStation is a station of a starter template's line as the test expects it.
type starterStation struct {
	name  string
	kind  string
	merge bool
}

// TestStarterTemplates_PushCleanly pushes every committed starter template into an in-memory
// catalog: none may be refused or warned about, and each lists as onboarding shows it.
func TestStarterTemplates_PushCleanly(t *testing.T) {
	t.Parallel()

	want := map[string]struct {
		title, summary, code string
		work                 []starterStation
	}{
		"software-delivery": {
			title: "Software delivery", summary: "Requests become small pull requests a reviewer merges.", code: authoring.CodeRead,
			work: []starterStation{{"Spec", "agent", false}, {"Build", "agent", false}, {"Review", "human", false}},
		},
		"incident-response": {
			title: "Incident response", summary: "Alerts become investigations, fixes and postmortems.", code: authoring.CodeNone,
			work: []starterStation{{"Triage", "agent", false}, {"Investigate", "agent", false}, {"Fix", "agent", false}, {"Review", "human", false}},
		},
		"support-replies": {
			title: "Support replies", summary: "Tickets get drafted answers a person approves.", code: authoring.CodeNone,
			work: []starterStation{{"Triage", "agent", false}, {"Draft", "agent", false}, {"Approve", "human", false}, {"Send", "agent", false}},
		},
	}

	catalog := authoring.NewTemplateCatalog(porttest.NewObjectStore(), porttest.NewClock(time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)))
	pushed := map[string]*config.Factory{}

	names, err := os.ReadDir(starterTemplates)
	if err != nil {
		t.Fatal(err)
	}

	for _, n := range names {
		versions, err := os.ReadDir(filepath.Join(starterTemplates, n.Name()))
		if err != nil {
			t.Fatal(err)
		}

		for _, v := range versions {
			dir := filepath.Join(starterTemplates, n.Name(), v.Name())
			pushed[n.Name()] = pushStarter(t, catalog, dir, n.Name(), v.Name())
		}
	}

	list, err := catalog.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if len(list) != len(want) {
		t.Fatalf("catalog lists %d templates, want %d: %+v", len(list), len(want), list)
	}

	for _, s := range list {
		w, ok := want[s.Name]
		if !ok {
			t.Errorf("unexpected template %s", s.Name)

			continue
		}

		if s.Title != w.title || s.Summary != w.summary || s.Code != w.code {
			t.Errorf("%s lists as %q / %q / code %s, want %q / %q / code %s", s.Name, s.Title, s.Summary, s.Code, w.title, w.summary, w.code)
		}

		if got := lineOf(pushed[s.Name]); !slices.Equal(got, wantLine(w.work)) {
			t.Errorf("%s's line = %v, want %v", s.Name, got, wantLine(w.work))
		}
	}
}

// pushStarter pushes one template folder and returns its checked definition, failing on any
// problem config.Check reports, warnings included.
func pushStarter(t *testing.T, catalog *authoring.TemplateCatalog, dir, name, version string) *config.Factory {
	t.Helper()

	v, err := strconv.Atoi(version)
	if err != nil {
		t.Fatalf("%s: a version folder is a number: %v", dir, err)
	}

	meta, err := os.ReadFile(filepath.Join(dir, "template.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	factory := os.DirFS(filepath.Join(dir, "factory"))
	files := map[string][]byte{}

	err = fs.WalkDir(factory, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		files[p], err = fs.ReadFile(factory, p)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	f, problems := config.Check(factory)
	for _, p := range problems {
		t.Errorf("%s: %s %s:%d: %s (%s)", dir, p.Severity, p.File, p.Line, p.Message, p.Code)
	}

	if f == nil {
		t.Fatalf("%s: the definition does not load", dir)
	}

	if err := catalog.Push(t.Context(), authoring.TemplateUpload{Name: name, Version: v, Meta: meta, Files: files}); err != nil {
		if invalid, ok := errors.AsType[*authoring.TemplateInvalidError](err); ok {
			for _, p := range invalid.Problems {
				t.Errorf("%s: %s: %s (%s)", dir, p.File, p.Message, p.Code)
			}
		}

		t.Fatalf("push %s: %v", dir, err)
	}

	return f
}

// lineOf describes a definition's stations as name/kind, with /merge on one that merges.
func lineOf(f *config.Factory) []string {
	out := []string{}

	for _, s := range f.Spec.Board.Stations {
		d := s.Name + "/" + s.Kind
		if s.Action == config.ActionMerge {
			d += "/merge"
		}

		out = append(out, d)
	}

	return out
}

// wantLine is an Inbox queue, the work stations and Done, described as lineOf does.
func wantLine(work []starterStation) []string {
	out := []string{"Inbox/queue"}

	for _, s := range work {
		d := s.name + "/" + s.kind
		if s.merge {
			d += "/merge"
		}

		out = append(out, d)
	}

	return append(out, "Done/done")
}
