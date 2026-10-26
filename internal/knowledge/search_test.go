package knowledge_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/knowledge"
)

// The world's sources.
const (
	srcHandbook = "handbook"
	srcRunbooks = "runbooks"
)

// SourceNames are what a search may name: the organization's sources, which the definitions check
// holds knowledge.sources to.
func TestSearcher_SourceNames(t *testing.T) {
	t.Parallel()

	w := newWorld(t)

	names, err := knowledge.NewSearcher(w.kb, nil, nil).SourceNames(w.ctx())
	if err != nil || !slices.Equal(names, []string{srcHandbook, srcRunbooks}) {
		t.Errorf("SourceNames = %v, %v; want handbook and runbooks", names, err)
	}
}

// Read returns a document of the named sources only: one of a source the caller may not search,
// or of another organization, is not found, as one that does not exist.
func TestSearcher_ReadsOnlyDocumentsOfTheNamedSources(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.ctx()

	d, err := w.uploads.Upload(ctx, srcHandbook, "leave.md", []byte("# Leave\n\nAsk your lead."), admin)
	if err != nil {
		t.Fatal(err)
	}

	w.syncDue(w.ingestor(nil, nil))

	s := knowledge.NewSearcher(w.kb, nil, nil)

	got, text, err := s.Read(ctx, d.ID, []string{srcRunbooks, srcHandbook})
	if err != nil || got.ID != d.ID || text != "# Leave\n\nAsk your lead." {
		t.Fatalf("Read of a listed source's document = %+v, %q, %v", got, text, err)
	}

	if _, _, err := s.Read(ctx, d.ID, []string{srcRunbooks}); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Read of an unlisted source's document = %v, want not found", err)
	}

	if _, _, err := s.Read(ctx, d.ID, nil); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Read with no sources = %v, want not found", err)
	}

	other, err := w.tenancy.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: "example-labs", Name: "Example Labs"})
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := s.Read(domain.WithOrg(t.Context(), other.ID), d.ID, []string{srcHandbook}); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Read from another organization = %v, want not found", err)
	}
}
