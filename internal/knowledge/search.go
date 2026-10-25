package knowledge

import (
	"context"
	"log/slog"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Searcher answers searches of a knowledge base: by keywords and, with an embedding model, by
// nearest neighbours of the query's embedding too, over the sources the caller names only.
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
