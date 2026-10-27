package knowledge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
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

	if err := s.listed(ctx, d, sources); err != nil {
		return domain.KnowledgeDocument{}, "", err
	}

	return d, text, nil
}

// listed is ports.ErrNotFound unless d is a document of one of sources.
func (s *Searcher) listed(ctx context.Context, d domain.KnowledgeDocument, sources []string) error {
	all, err := s.kb.Sources(ctx)
	if err != nil {
		return err
	}

	listed := slices.ContainsFunc(all, func(src domain.KnowledgeSource) bool {
		return src.ID == d.Source && slices.Contains(sources, src.Name)
	})
	if !listed {
		return fmt.Errorf("knowledge document %s: %w", d.ID, ports.ErrNotFound)
	}

	return nil
}

// RecordSearch records a search use (ADR 0027) of each document among hits, for the factory,
// item, run and time of use: one per document, with its chunk ids in hit order and its source's
// id. A hit of a source the organization no longer has is skipped and logged.
func (s *Searcher) RecordSearch(ctx context.Context, use domain.KnowledgeUse, hits []domain.KnowledgeHit) error {
	if len(hits) == 0 {
		return nil
	}

	all, err := s.kb.Sources(ctx)
	if err != nil {
		return fmt.Errorf("record knowledge search: %w", err)
	}

	ids := make(map[string]domain.KnowledgeSourceID, len(all))
	for _, src := range all {
		ids[src.Name] = src.ID
	}

	var (
		order []domain.KnowledgeDocumentID
		uses  = map[domain.KnowledgeDocumentID]*domain.KnowledgeUse{}
	)

	for _, h := range hits {
		id, ok := ids[h.Source]
		if !ok {
			s.log.WarnContext(ctx, "knowledge use skipped: unknown source", slog.String("document", string(h.Document)), slog.String("source", h.Source))

			continue
		}

		u, ok := uses[h.Document]
		if !ok {
			u = &domain.KnowledgeUse{
				Factory: use.Factory, Item: use.Item, Run: use.Run, At: use.At,
				Source: id, Document: h.Document, Kind: domain.KnowledgeSearched,
			}
			uses[h.Document] = u
			order = append(order, h.Document)
		}

		u.Chunks = append(u.Chunks, h.Chunk)
	}

	var errs []error

	for _, doc := range order {
		if err := s.kb.RecordUse(ctx, *uses[doc]); err != nil {
			errs = append(errs, fmt.Errorf("record knowledge search of %s: %w", doc, err))
		}
	}

	return errors.Join(errs...)
}

// RecordUse records one use of a document (ADR 0027), as ports.Knowledge.RecordUse.
func (s *Searcher) RecordUse(ctx context.Context, u domain.KnowledgeUse) error {
	return s.kb.RecordUse(ctx, u)
}

// RunUses are a run's uses of documents, as ports.Knowledge.RunUses.
func (s *Searcher) RunUses(ctx context.Context, run domain.RunID) ([]domain.KnowledgeUse, error) {
	return s.kb.RunUses(ctx, run)
}

// Verdict records v on a document of one of sources and returns it as stored. A document of
// another source is ports.ErrNotFound, as one that does not exist.
func (s *Searcher) Verdict(ctx context.Context, v domain.KnowledgeVerdict, sources []string) (domain.KnowledgeVerdict, error) {
	d, _, err := s.kb.Document(ctx, v.Document)
	if err != nil {
		return domain.KnowledgeVerdict{}, err
	}

	if err := s.listed(ctx, d, sources); err != nil {
		return domain.KnowledgeVerdict{}, err
	}

	return s.kb.GiveVerdict(ctx, v)
}
