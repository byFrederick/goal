package knowledge

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Searcher answers searches of a knowledge base: by keywords and, with an embedding model, by
// nearest neighbours of the query's embedding too, over the sources the caller names only; and
// reads their documents, again of the named sources only.
type Searcher struct {
	kb       ports.Knowledge
	embedder ports.Embedder
	log      *slog.Logger
}

// NewSearcher returns a searcher of kb; embedder nil searches by keywords only. log may be nil.
func NewSearcher(kb ports.Knowledge, embedder ports.Embedder, log *slog.Logger) *Searcher {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &Searcher{kb: kb, embedder: embedder, log: log}
}

// Search ranks the chunks of sources for text, at most limit, for the organization in ctx. When
// the embedding model fails the search goes on by keywords alone, and says so in the log.
func (s *Searcher) Search(ctx context.Context, text string, sources []string, limit int) ([]domain.KnowledgeHit, error) {
	q := domain.KnowledgeQuery{Text: text, Sources: sources, Limit: limit}

	if s.embedder != nil && len(sources) > 0 && limit > 0 {
		vecs, err := s.embedder.Embed(ctx, []string{text})

		switch {
		case err == nil && len(vecs) == 1:
			q.Embedding, q.Model = vecs[0], s.embedder.Model()
		case ctx.Err() != nil:
			return nil, ctx.Err()
		default:
			s.log.WarnContext(ctx, "knowledge search by keywords only: the embedding model failed", slog.Any("error", err))
		}
	}

	return s.kb.Search(ctx, q)
}

// SourceNames are the names of the sources of the organization in ctx, which a search may name
// (authoring.KnowledgeSources: the definitions check holds knowledge.sources to them).
func (s *Searcher) SourceNames(ctx context.Context) ([]string, error) {
	sources, err := s.kb.Sources(ctx)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(sources))
	for _, src := range sources {
		names = append(names, src.Name)
	}

	return names, nil
}

// Read returns a document of one of sources with its indexed text, for the organization in ctx.
// A document of another source is ports.ErrNotFound, as one that does not exist: the caller
// learns nothing of what it may not search.
func (s *Searcher) Read(ctx context.Context, id domain.KnowledgeDocumentID, sources []string) (domain.KnowledgeDocument, string, error) {
	d, text, err := s.kb.Document(ctx, id)
	if err != nil {
		return domain.KnowledgeDocument{}, "", err
	}

	all, err := s.kb.Sources(ctx)
	if err != nil {
		return domain.KnowledgeDocument{}, "", err
	}

	listed := slices.ContainsFunc(all, func(src domain.KnowledgeSource) bool {
		return src.ID == d.Source && slices.Contains(sources, src.Name)
	})
	if !listed {
		return domain.KnowledgeDocument{}, "", fmt.Errorf("knowledge document %s: %w", id, ports.ErrNotFound)
	}

	return d, text, nil
}
