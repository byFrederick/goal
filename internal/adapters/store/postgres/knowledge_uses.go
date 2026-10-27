package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Knowledge use and verdicts (ADR 0027) over 00070's knowledge_uses and knowledge_verdicts, under
// row-level security like the rest of the knowledge base. Both reference their document with
// its source and organization, so another organization's document fails the key, and both go
// with their document.

// RecordUse implements ports.Knowledge.
func (k *Knowledge) RecordUse(ctx context.Context, u domain.KnowledgeUse) error {
	if err := u.Check(); err != nil {
		return fmt.Errorf("record knowledge use: %w", err)
	}

	p := db.UpsertKnowledgeUseParams{Kind: string(u.Kind), ChunkIds: u.Chunks, At: u.At}
	if p.ChunkIds == nil {
		p.ChunkIds = []int64{}
	}

	for _, id := range []struct {
		dst  *uuid.UUID
		kind string
		id   string
	}{
		{&p.FactoryID, "factory", string(u.Factory)},
		{&p.ItemID, "item", string(u.Item)},
		{&p.RunID, "run", string(u.Run)},
		{&p.SourceID, "knowledge source", string(u.Source)},
		{&p.DocumentID, "knowledge document", string(u.Document)},
	} {
		parsed, err := parseID(id.kind, id.id)
		if err != nil {
			return fmt.Errorf("record knowledge use: %w", err)
		}

		*id.dst = parsed
	}

	n, err := fetch(ctx, k.s.inTx, func(q *db.Queries) (int64, error) { return q.UpsertKnowledgeUse(ctx, p) })

	switch pgErr, ok := errors.AsType[*pgconn.PgError](err); {
	case err == nil && n == 1:
		return nil
	case err == nil, isForeignKeyViolation(err):
		return fmt.Errorf("record knowledge use of %s: %w", u.Document, ports.ErrNotFound)
	case ok && pgErr.Code == checkViolation:
		return fmt.Errorf("record knowledge use of %s: more than %d chunks: %w", u.Document, domain.MaxUseChunks, domain.ErrInvalid)
	default:
		return fmt.Errorf("record knowledge use of %s: %w", u.Document, err)
	}
}

// GiveVerdict implements ports.Knowledge.
func (k *Knowledge) GiveVerdict(ctx context.Context, v domain.KnowledgeVerdict) (domain.KnowledgeVerdict, error) {
	if err := v.Check(); err != nil {
		return domain.KnowledgeVerdict{}, fmt.Errorf("give knowledge verdict: %w", err)
	}

	doc, err := parseID("knowledge document", string(v.Document))
	if err != nil {
		return domain.KnowledgeVerdict{}, fmt.Errorf("give knowledge verdict: %w", err)
	}

	p := db.UpsertKnowledgeVerdictParams{
		DocumentID: doc, Verdict: string(v.Verdict), Note: v.Note, Actor: v.By.Ref(),
	}

	if !v.At.IsZero() {
		at := v.At
		p.At = &at
	}

	if p.RunID, err = optionalID("run", string(v.Run)); err != nil {
		return domain.KnowledgeVerdict{}, fmt.Errorf("give knowledge verdict: %w", err)
	}

	if p.ItemID, err = optionalID("item", string(v.Item)); err != nil {
		return domain.KnowledgeVerdict{}, fmt.Errorf("give knowledge verdict: %w", err)
	}

	row, err := fetch(ctx, k.s.inTx, func(q *db.Queries) (db.UpsertKnowledgeVerdictRow, error) {
		return q.UpsertKnowledgeVerdict(ctx, p)
	})
	if notFound(err) {
		return domain.KnowledgeVerdict{}, fmt.Errorf("give knowledge verdict on %s: %w", v.Document, ports.ErrNotFound)
	}

	if err != nil {
		return domain.KnowledgeVerdict{}, fmt.Errorf("give knowledge verdict on %s: %w", v.Document, err)
	}

	v.Source, v.At = domain.KnowledgeSourceID(row.SourceID.String()), row.At.UTC()

	return v, nil
}

// SourceUsage implements ports.Knowledge.
func (k *Knowledge) SourceUsage(ctx context.Context, source string, days int) ([]domain.DocumentUsage, error) {
	if err := domain.CheckUsageDays(days); err != nil {
		return nil, fmt.Errorf("knowledge usage: %w", err)
	}

	var rows []db.KnowledgeSourceUsageRow

	err := k.s.read(ctx, func(q *db.Queries) error {
		src, err := sourceNamed(ctx, q, source)
		if err != nil {
			return err
		}

		sid, err := uuid.Parse(string(src.ID))
		if err != nil {
			return err
		}

		rows, err = q.KnowledgeSourceUsage(ctx, db.KnowledgeSourceUsageParams{
			SourceID: sid, Days: int32(days), MaxRows: domain.MaxUsageDocuments, //nolint:gosec // bounded by CheckUsageDays
		})

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("knowledge usage: %w", err)
	}

	out := make([]domain.DocumentUsage, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.DocumentUsage{
			Document: domain.KnowledgeDocumentID(r.ID.String()), Title: r.Title, Runs: int(r.Runs),
			AgentsHelped: int(r.AgentsHelped), AgentsMisled: int(r.AgentsMisled),
			PeopleHelped: int(r.PeopleHelped), PeopleMisled: int(r.PeopleMisled),
		})
	}

	return out, nil
}

// DocumentUses implements ports.Knowledge.
func (k *Knowledge) DocumentUses(ctx context.Context, document domain.KnowledgeDocumentID, after domain.RunID, limit int) ([]domain.KnowledgeRunUse, error) {
	did, err := parseID("knowledge document", string(document))
	if err != nil {
		return nil, fmt.Errorf("knowledge document uses: %w", err)
	}

	params := db.KnowledgeDocumentUsesParams{DocumentID: did, MaxRows: int32(min(max(limit, 0), maxDocumentPage))}

	if params.After, err = optionalID("run", string(after)); err != nil {
		return nil, fmt.Errorf("knowledge document uses: %w", err)
	}

	var rows []db.KnowledgeDocumentUsesRow

	err = k.s.read(ctx, func(q *db.Queries) error {
		if _, err := oneDocument(ctx, q, db.KnowledgeDocumentsParams{ID: &did}); err != nil {
			return err
		}

		var err error

		rows, err = q.KnowledgeDocumentUses(ctx, params)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("knowledge document uses: %w", err)
	}

	out := make([]domain.KnowledgeRunUse, 0, len(rows))
	for _, r := range rows {
		kinds := make([]domain.KnowledgeUseKind, 0, len(r.Kinds))
		for _, kind := range r.Kinds {
			kinds = append(kinds, domain.KnowledgeUseKind(kind))
		}

		out = append(out, domain.KnowledgeRunUse{
			Run: domain.RunID(r.RunID.String()), Item: domain.ItemID(r.ItemID.String()),
			Factory: domain.FactoryID(r.FactoryID.String()), Kinds: kinds, At: r.At.UTC(),
		})
	}

	return out, nil
}

// RunUses implements ports.Knowledge.
func (k *Knowledge) RunUses(ctx context.Context, run domain.RunID) ([]domain.KnowledgeUse, error) {
	rid, err := uuid.Parse(string(run))
	if err != nil {
		if _, ok := domain.OrgFrom(ctx); !ok {
			return nil, domain.ErrNoOrg
		}

		return nil, nil // no such run has uses
	}

	rows, err := fetch(ctx, k.s.read, func(q *db.Queries) ([]db.KnowledgeRunUsesRow, error) {
		return q.KnowledgeRunUses(ctx, rid)
	})
	if err != nil {
		return nil, fmt.Errorf("knowledge run uses: %w", err)
	}

	out := make([]domain.KnowledgeUse, 0, len(rows))
	for _, r := range rows {
		out = append(out, useFrom(r))
	}

	return out, nil
}

func useFrom(r db.KnowledgeRunUsesRow) domain.KnowledgeUse {
	return domain.KnowledgeUse{
		Factory: domain.FactoryID(r.FactoryID.String()), Item: domain.ItemID(r.ItemID.String()), Run: domain.RunID(r.RunID.String()),
		Source: domain.KnowledgeSourceID(r.SourceID.String()), Document: domain.KnowledgeDocumentID(r.DocumentID.String()),
		Kind: domain.KnowledgeUseKind(r.Kind), Chunks: r.ChunkIds, At: r.At.UTC(),
	}
}
