package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Where knowledge was used and what people said of it (ADR 0027), until the cut-over (ADR 0038
// decision 16): a source's usage, the runs that used a document and a run's documents need
// knowledge:read (a run's, factory:read). Verdicts are no longer given (decision 9).

// Limits of a document's uses.
const (
	defaultDocumentUses = 50
	maxDocumentUses     = 100
)

func (s *server) GetKnowledgeUsage(ctx context.Context, req gen.GetKnowledgeUsageRequestObject) (gen.GetKnowledgeUsageResponseObject, error) {
	days := domain.DefaultUsageDays
	if req.Params.Days != nil {
		days = *req.Params.Days
	}

	if err := domain.CheckUsageDays(days); err != nil {
		return nil, fmt.Errorf("%w: %w", errSourceInvalid, err)
	}

	usage, err := s.knowledge.SourceUsage(ctx, req.Params.Source, days)
	if err != nil {
		return nil, sourceError(err, req.Params.Source)
	}

	out := gen.GetKnowledgeUsage200JSONResponse{Source: req.Params.Source, Days: days, Documents: make([]gen.KnowledgeDocumentUsage, 0, len(usage))}
	for _, u := range usage {
		out.Documents = append(out.Documents, gen.KnowledgeDocumentUsage{
			Id: string(u.Document), Title: u.Title, Source: req.Params.Source, Runs: u.Runs,
			AgentsHelped: u.AgentsHelped, AgentsMisled: u.AgentsMisled, PeopleHelped: u.PeopleHelped, PeopleMisled: u.PeopleMisled,
		})
	}

	return out, nil
}

// ListKnowledgeDocumentUses names each run's item and factory, leaving out the runs of factories
// the caller does not see; next_after follows the store's page, so a page may hold fewer.
func (s *server) ListKnowledgeDocumentUses(ctx context.Context, req gen.ListKnowledgeDocumentUsesRequestObject) (gen.ListKnowledgeDocumentUsesResponseObject, error) {
	limit := defaultDocumentUses
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}

	if limit < 1 || limit > maxDocumentUses {
		return nil, fmt.Errorf("%w: limit must be in [1, %d]", errInvalid, maxDocumentUses)
	}

	// One more than the page says whether there is a next one.
	uses, err := s.knowledge.DocumentUses(ctx, domain.KnowledgeDocumentID(req.Document), domain.RunID(deref(req.Params.After)), limit+1)
	if err != nil {
		return nil, documentError(err, req.Document)
	}

	out := gen.ListKnowledgeDocumentUses200JSONResponse{Uses: []gen.KnowledgeDocumentUse{}}

	if len(uses) > limit {
		uses = uses[:limit]
		out.NextAfter = optional(string(uses[limit-1].Run))
	}

	byID := map[domain.FactoryID]*config.Factory{}
	for _, f := range s.visibleFactories(ctx) {
		byID[domain.FactoryID(f.Metadata.ID)] = f
	}

	for _, u := range uses {
		f, ok := byID[u.Factory]
		if !ok {
			continue
		}

		it, err := s.items.Item(ctx, u.Item)
		if errors.Is(err, ports.ErrNotFound) {
			continue
		}

		if err != nil {
			return nil, fmt.Errorf("knowledge document %s uses: item %s: %w", req.Document, u.Item, err)
		}

		kinds := make([]gen.KnowledgeUseKind, 0, len(u.Kinds))
		for _, k := range u.Kinds {
			kinds = append(kinds, gen.KnowledgeUseKind(k))
		}

		out.Uses = append(out.Uses, gen.KnowledgeDocumentUse{
			RunId: string(u.Run), Factory: factoryHit(f), Kinds: kinds, At: u.At,
			Item: gen.RunItem{Id: string(it.ID), Ref: domain.ItemRef{Factory: f.Metadata.Name, Number: it.Number}.String(), Title: it.Title},
		})
	}

	return out, nil
}

func (s *server) ListRunKnowledge(ctx context.Context, req gen.ListRunKnowledgeRequestObject) (gen.ListRunKnowledgeResponseObject, error) {
	r, err := s.run(ctx, req.Run)
	if err != nil {
		return nil, err
	}

	uses, err := s.knowledge.RunUses(ctx, r.ID)
	if err != nil {
		return nil, fmt.Errorf("run %s knowledge uses: %w", r.ID, err)
	}

	verdicts, err := s.knowledge.RunVerdicts(ctx, r.ID)
	if err != nil {
		return nil, fmt.Errorf("run %s knowledge verdicts: %w", r.ID, err)
	}

	names, err := s.sourceNames(ctx)
	if err != nil {
		return nil, err
	}

	out := gen.ListRunKnowledge200JSONResponse{
		Uses: make([]gen.RunKnowledgeUse, 0, len(uses)), Verdicts: make([]gen.KnowledgeVerdict, 0, len(verdicts)),
	}

	for _, u := range uses {
		out.Uses = append(out.Uses, gen.RunKnowledgeUse{
			Document: string(u.Document), Source: names[u.Source], Kind: gen.KnowledgeUseKind(u.Kind),
			Chunks: append([]int64{}, u.Chunks...), At: u.At,
		})
	}

	for _, v := range verdicts {
		out.Verdicts = append(out.Verdicts, s.verdictOut(v, names))
	}

	return out, nil
}

// verdictOut is a verdict as the contract has it, its source named by names.
func (s *server) verdictOut(v domain.KnowledgeVerdict, names map[domain.KnowledgeSourceID]string) gen.KnowledgeVerdict {
	return gen.KnowledgeVerdict{
		Document: string(v.Document), Source: names[v.Source], Verdict: gen.KnowledgeVerdictVerdict(v.Verdict),
		RunId: optional(string(v.Run)), ItemId: optional(string(v.Item)), Note: optional(v.Note), By: s.actorOut(v.By), At: v.At,
	}
}
