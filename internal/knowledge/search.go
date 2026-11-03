package knowledge

import (
	"context"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Searcher answers searches of a knowledge base by keywords, over the sources the caller names only.
type Searcher struct {
	kb ports.Knowledge
}

// NewSearcher returns a searcher of kb.
func NewSearcher(kb ports.Knowledge) *Searcher {
	return &Searcher{kb: kb}
}

// Search ranks the chunks of sources for text, at most limit, for the organization in ctx.
func (s *Searcher) Search(ctx context.Context, text string, sources []string, limit int) ([]domain.KnowledgeHit, error) {
	return s.kb.Search(ctx, domain.KnowledgeQuery{Text: text, Sources: sources, Limit: limit})
}
