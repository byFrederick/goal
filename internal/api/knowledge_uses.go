package api

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Where knowledge was used and what people said of it (ADR 0027): a source's usage, the runs that
// used a document and a run's documents need knowledge:read (a run's, factory:read); a person's
// verdict needs knowledge:verdict. ports.Knowledge audits the verdict and sends the stream's
// knowledge event.

// Limits of a document's uses.
const (
	defaultDocumentUses = 50
	maxDocumentUses     = 100
)

// Errors of verdicts, answered by knowledgeProblem: a run with no use of the document (422), and
// a verdict the rules refuse (400).
var (
	errDocumentNotUsed = errors.New("the run did not use the knowledge document")
	errVerdictInvalid  = errors.New("knowledge verdict invalid")
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

// GiveKnowledgeVerdict stores the caller's verdict; with a run_id, of a run the caller sees that
// used the document, and of its item.
func (s *server) GiveKnowledgeVerdict(ctx context.Context, req gen.GiveKnowledgeVerdictRequestObject) (gen.GiveKnowledgeVerdictResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("give knowledge verdict: the request has no actor")
	}

	if req.Body == nil {
		return nil, fmt.Errorf("%w: a verdict needs a body", errInvalid)
	}

	doc := domain.KnowledgeDocumentID(req.Document)
	v := domain.KnowledgeVerdict{Document: doc, Verdict: domain.KnowledgeVerdictKind(req.Body.Verdict), Note: deref(req.Body.Note), By: by}

	if err := v.Check(); err != nil {
		return nil, fmt.Errorf("%w: %w", errVerdictInvalid, err)
	}

	if id := deref(req.Body.RunId); id != "" {
		r, err := s.run(ctx, id)
		if err != nil {
			return nil, err
		}

		if err := s.usedBy(ctx, doc, r.ID); err != nil {
			return nil, err
		}

		v.Run, v.Item = r.ID, r.ItemID
	}

	stored, err := s.knowledge.GiveVerdict(ctx, v)
	if errors.Is(err, domain.ErrInvalid) {
		return nil, fmt.Errorf("%w: %w", errVerdictInvalid, err)
	}

	if err != nil {
		return nil, documentError(err, req.Document)
	}

	names, err := s.sourceNames(ctx)
	if err != nil {
		return nil, err
	}

	return gen.GiveKnowledgeVerdict200JSONResponse(s.verdictOut(stored, names)), nil
}

// usedBy is nil when run used the document, errDocumentNotUsed when the document exists but
// run did not use it, and errDocumentNotFound for no such document.
func (s *server) usedBy(ctx context.Context, doc domain.KnowledgeDocumentID, run domain.RunID) error {
	uses, err := s.knowledge.RunUses(ctx, run)
	if err != nil {
		return fmt.Errorf("run %s knowledge uses: %w", run, err)
	}

	if slices.ContainsFunc(uses, func(u domain.KnowledgeUse) bool { return u.Document == doc }) {
		return nil
	}

	if _, _, err := s.knowledge.Document(ctx, doc); err != nil {
		return documentError(err, string(doc))
	}

	return fmt.Errorf("%w: run %s, document %s", errDocumentNotUsed, run, doc)
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
