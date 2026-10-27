package knowledge_test

import (
	"errors"
	"slices"
	"testing"
	"time"

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

// RecordSearch records one search use per document among the hits, its chunks in hit order and
// its source's id; a hit of a source the organization does not have is skipped.
func TestSearcher_RecordSearchOneUsePerDocument(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.ctx()

	leave, err := w.uploads.Upload(ctx, srcHandbook, "leave.md", []byte("# Leave\n\nAsk your lead."), admin)
	if err != nil {
		t.Fatal(err)
	}

	deploy, err := w.uploads.Upload(ctx, srcHandbook, "deploy.md", []byte("# Deploy\n\nOn Tuesdays."), admin)
	if err != nil {
		t.Fatal(err)
	}

	w.syncDue(w.ingestor(nil, nil))

	s := knowledge.NewSearcher(w.kb, nil, nil)
	use := domain.KnowledgeUse{Factory: "factory-1", Item: "item-1", Run: "run-1", At: time.Now()}
	hits := []domain.KnowledgeHit{
		{Chunk: 7, Document: leave.ID, Source: srcHandbook},
		{Chunk: 3, Document: deploy.ID, Source: srcHandbook},
		{Chunk: 5, Document: leave.ID, Source: srcHandbook},
		{Chunk: 9, Document: leave.ID, Source: "gone"},
	}

	if err := s.RecordSearch(ctx, use, hits); err != nil {
		t.Fatal(err)
	}

	uses, err := s.RunUses(ctx, "run-1")
	if err != nil {
		t.Fatal(err)
	}

	got := map[domain.KnowledgeDocumentID]domain.KnowledgeUse{}
	for _, u := range uses {
		got[u.Document] = u
	}

	if u := got[leave.ID]; len(uses) != 2 || u.Kind != domain.KnowledgeSearched || u.Source != w.source(srcHandbook).ID || !slices.Equal(u.Chunks, []int64{5, 7}) {
		t.Errorf("uses = %+v; want one of leave.md with chunks 5 and 7, and one of deploy.md", uses)
	}

	if u := got[deploy.ID]; u.Source != w.source(srcHandbook).ID || !slices.Equal(u.Chunks, []int64{3}) {
		t.Errorf("use of deploy.md = %+v; want chunk 3 of handbook", u)
	}
}

// Verdict gives a verdict on a document of the named sources only.
func TestSearcher_VerdictOnlyOnTheNamedSources(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.ctx()

	d, err := w.uploads.Upload(ctx, srcHandbook, "leave.md", []byte("# Leave\n\nAsk your lead."), admin)
	if err != nil {
		t.Fatal(err)
	}

	w.syncDue(w.ingestor(nil, nil))

	s := knowledge.NewSearcher(w.kb, nil, nil)
	v := domain.KnowledgeVerdict{Document: d.ID, Run: "run-1", Item: "item-1", Verdict: domain.KnowledgeHelped, By: domain.Actor{Kind: domain.ActorAgent, ID: "engineer"}}

	if _, err := s.Verdict(ctx, v, []string{srcRunbooks}); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Verdict on an unlisted source's document = %v, want not found", err)
	}

	got, err := s.Verdict(ctx, v, []string{srcHandbook})
	if err != nil || got.Source != w.source(srcHandbook).ID || got.Verdict != domain.KnowledgeHelped || got.At.IsZero() {
		t.Errorf("Verdict = %+v, %v; want it stored on handbook's document", got, err)
	}
}
