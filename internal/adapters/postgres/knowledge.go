package postgres

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/adapters/postgres/db"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

var _ ports.Knowledge = (*Knowledge)(nil)

// Knowledge implements ports.Knowledge over 00054's knowledge_sources, knowledge_documents and
// knowledge_chunks, as goal_app under row-level security: every statement sees the organization
// in ctx only, and every change people make appends its audit entry in its transaction. A search
// is one statement: the nearest chunks by cosine distance (pgvector's HNSW index on halfvec,
// with its iterative scan so filters never starve it) and the best by keywords, both over the
// named sources only, fused by reciprocal rank.
type Knowledge struct {
	s *Store
}

// NewKnowledge returns the knowledge base on s's pool.
func NewKnowledge(s *Store) *Knowledge { return &Knowledge{s: s} }

// sourceNameKey keeps a source's name unique in its organization.
const sourceNameKey = "knowledge_sources_org_id_name_key"

// The audit data keys of the knowledge base.
const (
	auditSource = "source"
	auditTitle  = "title"
)

// Sources implements ports.Knowledge.
func (k *Knowledge) Sources(ctx context.Context) ([]domain.KnowledgeSource, error) {
	rows, err := fetch(ctx, k.s.read, func(q *db.Queries) ([]db.KnowledgeSourcesRow, error) {
		return q.KnowledgeSources(ctx, db.KnowledgeSourcesParams{})
	})
	if err != nil {
		return nil, fmt.Errorf("knowledge sources: %w", err)
	}

	out := make([]domain.KnowledgeSource, 0, len(rows))

	for _, r := range rows {
		s, err := sourceFrom(r)
		if err != nil {
			return nil, err
		}

		out = append(out, s)
	}

	return out, nil
}

// Source implements ports.Knowledge.
func (k *Knowledge) Source(ctx context.Context, name string) (domain.KnowledgeSource, error) {
	var out domain.KnowledgeSource

	err := k.s.read(ctx, func(q *db.Queries) error {
		var err error

		out, err = sourceNamed(ctx, q, name)

		return err
	})

	return out, err
}

// sourceNamed reads the source name; ErrNotFound when there is none.
func sourceNamed(ctx context.Context, q *db.Queries, name string) (domain.KnowledgeSource, error) {
	return oneSource(ctx, q, db.KnowledgeSourcesParams{Name: &name}, fmt.Sprintf("knowledge source %q", name))
}

func oneSource(ctx context.Context, q *db.Queries, p db.KnowledgeSourcesParams, what string) (domain.KnowledgeSource, error) {
	rows, err := q.KnowledgeSources(ctx, p)
	if err != nil {
		return domain.KnowledgeSource{}, fmt.Errorf("%s: %w", what, err)
	}

	if len(rows) == 0 {
		return domain.KnowledgeSource{}, fmt.Errorf("%s: %w", what, ports.ErrNotFound)
	}

	return sourceFrom(rows[0])
}

// CreateSource implements ports.Knowledge.
func (k *Knowledge) CreateSource(ctx context.Context, n domain.NewKnowledgeSource, by domain.Actor) (domain.KnowledgeSource, error) {
	if err := n.Check(); err != nil {
		return domain.KnowledgeSource{}, fmt.Errorf("create knowledge source: %w", err)
	}

	id, err := newID()
	if err != nil {
		return domain.KnowledgeSource{}, err
	}

	config, err := json.Marshal(nonNilConfig(n.Config))
	if err != nil {
		return domain.KnowledgeSource{}, fmt.Errorf("create knowledge source: config: %w", err)
	}

	var out domain.KnowledgeSource

	err = k.s.inTx(ctx, func(q *db.Queries) error {
		conn, err := sourceConnection(ctx, q, n)
		if err != nil {
			return err
		}

		err = q.InsertKnowledgeSource(ctx, db.InsertKnowledgeSourceParams{
			ID: id, Name: n.Name, Title: n.Title, Kind: string(n.Kind), ConnectionID: conn, Config: config,
			SyncEverySeconds: wholeSeconds(cmp.Or(n.SyncEvery, domain.DefaultSyncEvery)), CreatedBy: by.Ref(),
		})
		if violates(err, sourceNameKey) {
			return fmt.Errorf("knowledge source %q: %w", n.Name, ports.ErrNameTaken)
		}

		if err != nil {
			return ruleError(err, "")
		}

		if err := sourceAudit(ctx, q, by, domain.AuditKnowledgeSourceCreated, id,
			map[string]any{auditName: n.Name, "kind": string(n.Kind), "connection": n.Connection}); err != nil {
			return err
		}

		out, err = oneSource(ctx, q, db.KnowledgeSourcesParams{ID: &id}, "knowledge source "+n.Name)

		return err
	})
	if err != nil {
		return domain.KnowledgeSource{}, fmt.Errorf("create knowledge source: %w", err)
	}

	return out, nil
}

// sourceConnection is the id of the connection a new source reads, nil for an upload source.
func sourceConnection(ctx context.Context, q *db.Queries, n domain.NewKnowledgeSource) (*uuid.UUID, error) {
	if n.Kind == domain.SourceUpload {
		return nil, nil //nolint:nilnil // an upload source reads no connection
	}

	c, err := q.KnowledgeConnection(ctx, n.Connection)
	if notFound(err) {
		return nil, fmt.Errorf("connection %q: %w", n.Connection, ports.ErrNotFound)
	}

	if err != nil {
		return nil, fmt.Errorf("connection %q: %w", n.Connection, err)
	}

	if n.Kind == domain.SourceSynced && c.Sync == "" {
		return nil, fmt.Errorf("connection %q: %w", n.Connection, domain.ErrConnectionCannotSync)
	}

	return &c.ID, nil
}

// UpdateSource implements ports.Knowledge.
func (k *Knowledge) UpdateSource(ctx context.Context, name string, p domain.KnowledgeSourcePatch, by domain.Actor) (domain.KnowledgeSource, error) {
	if err := p.Check(); err != nil {
		return domain.KnowledgeSource{}, fmt.Errorf("update knowledge source: %w", err)
	}

	params := db.UpdateKnowledgeSourceParams{Name: name, Title: p.Title}

	if p.Config != nil {
		raw, err := json.Marshal(p.Config)
		if err != nil {
			return domain.KnowledgeSource{}, fmt.Errorf("update knowledge source: config: %w", err)
		}

		params.Config = raw
	}

	if p.SyncEvery != nil {
		s := wholeSeconds(*p.SyncEvery)
		params.SyncEverySeconds = &s
	}

	var out domain.KnowledgeSource

	err := k.s.inTx(ctx, func(q *db.Queries) error {
		id, err := q.UpdateKnowledgeSource(ctx, params)
		if notFound(err) {
			return fmt.Errorf("knowledge source %q: %w", name, ports.ErrNotFound)
		}

		if err != nil {
			return ruleError(err, "")
		}

		if err := sourceAudit(ctx, q, by, domain.AuditKnowledgeSourceUpdated, id, map[string]any{auditName: name}); err != nil {
			return err
		}

		out, err = oneSource(ctx, q, db.KnowledgeSourcesParams{ID: &id}, "knowledge source "+name)

		return err
	})
	if err != nil {
		return domain.KnowledgeSource{}, fmt.Errorf("update knowledge source: %w", err)
	}

	return out, nil
}

// DeleteSource implements ports.Knowledge.
func (k *Knowledge) DeleteSource(ctx context.Context, name string, by domain.Actor) ([]domain.BlobHash, error) {
	var orphans []domain.BlobHash

	err := k.s.inTx(ctx, func(q *db.Queries) error {
		src, err := q.LockKnowledgeSource(ctx, name)
		if notFound(err) {
			return fmt.Errorf("knowledge source %q: %w", name, ports.ErrNotFound)
		}

		if err != nil {
			return err
		}

		blobs, err := q.SourceBlobs(ctx, src.ID)
		if err != nil {
			return err
		}

		if err := q.DeleteKnowledgeSource(ctx, src.ID); err != nil {
			return err
		}

		if err := sourceAudit(ctx, q, by, domain.AuditKnowledgeSourceDeleted, src.ID,
			map[string]any{auditName: name, "kind": src.Kind}); err != nil {
			return err
		}

		orphans, err = unheld(ctx, q, blobs)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("delete knowledge source: %w", err)
	}

	return orphans, nil
}

// unheld is those of blobs no document of the organization holds.
func unheld(ctx context.Context, q *db.Queries, blobs []string) ([]domain.BlobHash, error) {
	if len(blobs) == 0 {
		return nil, nil
	}

	gone, err := q.UnheldBlobs(ctx, blobs)
	if err != nil {
		return nil, err
	}

	out := make([]domain.BlobHash, 0, len(gone))
	for _, b := range gone {
		out = append(out, domain.BlobHash(b))
	}

	return out, nil
}

// SyncNow implements ports.Knowledge.
func (k *Knowledge) SyncNow(ctx context.Context, name string) error {
	kind, err := fetch(ctx, k.s.inTx, func(q *db.Queries) (string, error) { return q.SyncKnowledgeSourceNow(ctx, name) })
	switch {
	case notFound(err):
		return fmt.Errorf("knowledge source %q: %w", name, ports.ErrNotFound)
	case err != nil:
		return fmt.Errorf("sync knowledge source %q: %w", name, err)
	case kind == string(domain.SourceLive):
		return fmt.Errorf("knowledge source %q is live and never syncs: %w", name, domain.ErrInvalid)
	default:
		return nil
	}
}

// ClaimSource implements ports.Knowledge; the claim notifies the stream event knowledge (syncing).
func (k *Knowledge) ClaimSource(ctx context.Context, now, until time.Time, model string) (*domain.KnowledgeSource, error) {
	var out *domain.KnowledgeSource

	err := k.s.inTx(ctx, func(q *db.Queries) error {
		id, err := q.ClaimKnowledgeSource(ctx, db.ClaimKnowledgeSourceParams{Until: until, Now: now, Model: model})
		if notFound(err) {
			return nil
		}

		if err != nil {
			return err
		}

		s, err := oneSource(ctx, q, db.KnowledgeSourcesParams{ID: &id}, "knowledge source "+id.String())
		if err != nil {
			return err
		}

		out = &s

		return q.NotifyKnowledgeSync(ctx, id)
	})
	if err != nil {
		return nil, fmt.Errorf("claim knowledge source: %w", err)
	}

	return out, nil
}

// RenewSource implements ports.Knowledge.
func (k *Knowledge) RenewSource(ctx context.Context, id domain.KnowledgeSourceID, now, until time.Time) error {
	sid, err := parseID("knowledge source", string(id))
	if err != nil {
		return err
	}

	n, err := fetch(ctx, k.s.inTx, func(q *db.Queries) (int64, error) {
		return q.RenewKnowledgeSource(ctx, db.RenewKnowledgeSourceParams{Until: until, ID: sid, Now: now})
	})
	if err != nil {
		return fmt.Errorf("renew knowledge source: %w", err)
	}

	if n == 0 {
		return fmt.Errorf("knowledge source %s: %w", id, ports.ErrLeaseLost)
	}

	return nil
}

// FinishSync implements ports.Knowledge; the end notifies the stream event knowledge (idle or failed).
func (k *Knowledge) FinishSync(ctx context.Context, id domain.KnowledgeSourceID, r domain.SyncResult) error {
	sid, err := parseID("knowledge source", string(id))
	if err != nil {
		return err
	}

	n, err := fetch(ctx, k.s.inTx, func(q *db.Queries) (int64, error) {
		n, err := q.FinishKnowledgeSync(ctx, db.FinishKnowledgeSyncParams{
			Error: truncate(r.Error, domain.MaxKnowledgeError), At: r.At, Model: r.EmbedModel, Due: r.Due, ID: sid,
		})
		if err != nil || n == 0 {
			return n, err
		}

		return n, q.NotifyKnowledgeSync(ctx, sid)
	})
	if err != nil {
		return fmt.Errorf("finish knowledge sync: %w", err)
	}

	if n == 0 {
		return fmt.Errorf("knowledge source %s: %w", id, ports.ErrNotFound)
	}

	return nil
}

// Versions implements ports.Knowledge.
func (k *Knowledge) Versions(ctx context.Context, source domain.KnowledgeSourceID) (map[string]string, error) {
	sid, err := parseID("knowledge source", string(source))
	if err != nil {
		return nil, err
	}

	rows, err := fetch(ctx, k.s.read, func(q *db.Queries) ([]db.KnowledgeVersionsRow, error) { return q.KnowledgeVersions(ctx, sid) })
	if err != nil {
		return nil, fmt.Errorf("knowledge versions: %w", err)
	}

	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.ExternalID] = r.Version
	}

	return out, nil
}

// PutDocument implements ports.Knowledge.
func (k *Knowledge) PutDocument(ctx context.Context, d domain.KnowledgeDocument, chunks []domain.Chunk) error {
	sid, err := parseID("knowledge source", string(d.Source))
	if err != nil {
		return err
	}

	id, err := newID()
	if err != nil {
		return err
	}

	params, err := chunkParams(sid, d.EmbedModel, chunks)
	if err != nil {
		return err
	}

	err = k.s.inTx(ctx, func(q *db.Queries) error {
		docID, err := q.UpsertKnowledgeDocument(ctx, db.UpsertKnowledgeDocumentParams{
			ID: id, SourceID: sid, ExternalID: d.ExternalID, Version: d.Version, Title: truncate(d.Title, maxDocumentTitle),
			Url: optional(d.URL), Mime: d.MIME, Bytes: d.Bytes, BlobSha256: optional(string(d.Blob)), Status: string(d.Status),
			Error: optional(truncate(d.Error, domain.MaxKnowledgeError)), SeenAt: d.SeenAt, IndexedAt: d.IndexedAt,
			EmbedModel: optional(d.EmbedModel), CreatedBy: d.CreatedBy.Ref(),
		})
		if err != nil {
			return ruleError(err, "")
		}

		if err := q.DeleteDocumentChunks(ctx, docID); err != nil {
			return err
		}

		if len(chunks) == 0 {
			return nil
		}

		params.DocumentID = docID

		return ruleError(q.InsertKnowledgeChunks(ctx, params), "")
	})
	if err != nil {
		return fmt.Errorf("put knowledge document %q: %w", d.ExternalID, err)
	}

	return nil
}

// maxDocumentTitle bounds a document's title (00054's check).
const maxDocumentTitle = 1024

// chunkParams are chunks as the arrays InsertKnowledgeChunks takes.
func chunkParams(source uuid.UUID, model string, chunks []domain.Chunk) (db.InsertKnowledgeChunksParams, error) {
	p := db.InsertKnowledgeChunksParams{
		SourceID: source, Model: model, Ords: make([]int32, 0, len(chunks)), Headings: make([]string, 0, len(chunks)),
		Texts: make([]string, 0, len(chunks)), Embeddings: make([]string, 0, len(chunks)),
	}

	for _, c := range chunks {
		if c.Embedding != nil && model == "" {
			return p, fmt.Errorf("chunk %d has an embedding without a model: %w", c.Ord, domain.ErrInvalid)
		}

		vec, err := vectorText(c.Embedding)
		if err != nil {
			return p, fmt.Errorf("chunk %d: %w", c.Ord, err)
		}

		p.Ords = append(p.Ords, int32(c.Ord)) //nolint:gosec // a document has far fewer chunks than 2^31
		p.Headings = append(p.Headings, c.Heading)
		p.Texts = append(p.Texts, c.Text)
		p.Embeddings = append(p.Embeddings, vec)
	}

	return p, nil
}

// vectorText is v in pgvector's text form, "" for nil. It must have domain.EmbeddingDimensions.
func vectorText(v []float32) (string, error) {
	if v == nil {
		return "", nil
	}

	if len(v) != domain.EmbeddingDimensions {
		return "", fmt.Errorf("an embedding has %d dimensions, not %d: %w", len(v), domain.EmbeddingDimensions, domain.ErrInvalid)
	}

	var b strings.Builder

	b.Grow(len(v) * 8)
	b.WriteByte('[')

	for i, x := range v {
		if i > 0 {
			b.WriteByte(',')
		}

		b.WriteString(strconv.FormatFloat(float64(x), 'g', -1, 32))
	}

	b.WriteByte(']')

	return b.String(), nil
}

// MarkSeen implements ports.Knowledge.
func (k *Knowledge) MarkSeen(ctx context.Context, source domain.KnowledgeSourceID, externalIDs []string, at time.Time) error {
	if len(externalIDs) == 0 {
		return nil
	}

	sid, err := parseID("knowledge source", string(source))
	if err != nil {
		return err
	}

	err = k.s.inTx(ctx, func(q *db.Queries) error {
		return q.MarkKnowledgeSeen(ctx, db.MarkKnowledgeSeenParams{At: at, SourceID: sid, ExternalIds: externalIDs})
	})
	if err != nil {
		return fmt.Errorf("mark knowledge documents seen: %w", err)
	}

	return nil
}

// DeleteUnseen implements ports.Knowledge.
func (k *Knowledge) DeleteUnseen(ctx context.Context, source domain.KnowledgeSourceID, before time.Time) (int, error) {
	sid, err := parseID("knowledge source", string(source))
	if err != nil {
		return 0, err
	}

	n, err := fetch(ctx, k.s.inTx, func(q *db.Queries) (int64, error) {
		return q.DeleteUnseenDocuments(ctx, db.DeleteUnseenDocumentsParams{SourceID: sid, Before: before})
	})
	if err != nil {
		return 0, fmt.Errorf("delete unseen knowledge documents: %w", err)
	}

	return int(n), nil
}

// AddUpload implements ports.Knowledge.
func (k *Knowledge) AddUpload(ctx context.Context, source string, d domain.KnowledgeDocument, by domain.Actor) (domain.KnowledgeDocument, error) {
	if !d.Blob.Valid() {
		return domain.KnowledgeDocument{}, fmt.Errorf("upload %q: its content hash %q: %w", d.Title, d.Blob, domain.ErrInvalid)
	}

	id, err := newID()
	if err != nil {
		return domain.KnowledgeDocument{}, err
	}

	var out domain.KnowledgeDocument

	err = k.s.inTx(ctx, func(q *db.Queries) error {
		src, err := q.LockKnowledgeSource(ctx, source)
		if notFound(err) {
			return fmt.Errorf("knowledge source %q: %w", source, ports.ErrNotFound)
		}

		if err != nil {
			return err
		}

		if src.Kind != string(domain.SourceUpload) {
			return fmt.Errorf("knowledge source %q: %w", source, domain.ErrNotUploadSource)
		}

		added, err := q.InsertKnowledgeUpload(ctx, db.InsertKnowledgeUploadParams{
			ID: id, SourceID: src.ID, BlobSha256: string(d.Blob), Title: truncate(d.Title, maxDocumentTitle), Mime: d.MIME,
			Bytes: d.Bytes, CreatedBy: by.Ref(),
		})
		if err != nil {
			return ruleError(err, "")
		}

		if len(added) > 0 {
			if err := q.MakeKnowledgeSourceDue(ctx, src.ID); err != nil {
				return err
			}

			if err := appendAudit(ctx, q, domain.AuditEntry{
				Actor: by, Action: domain.AuditKnowledgeDocumentUploaded, TargetKind: domain.AuditTargetKnowledgeDocument,
				TargetID: added[0].String(), Data: map[string]any{auditSource: source, auditTitle: d.Title, "bytes": d.Bytes},
			}); err != nil {
				return err
			}
		}

		ext := string(d.Blob)

		out, err = oneDocument(ctx, q, db.KnowledgeDocumentsParams{SourceID: &src.ID, ExternalID: &ext})

		return err
	})
	if err != nil {
		return domain.KnowledgeDocument{}, fmt.Errorf("add upload: %w", err)
	}

	return out, nil
}

// DeleteDocument implements ports.Knowledge.
func (k *Knowledge) DeleteDocument(ctx context.Context, id domain.KnowledgeDocumentID, by domain.Actor) (domain.BlobHash, error) {
	did, err := parseID("knowledge document", string(id))
	if err != nil {
		return "", err
	}

	var orphan domain.BlobHash

	err = k.s.inTx(ctx, func(q *db.Queries) error {
		d, err := q.LockKnowledgeDocument(ctx, did)
		if notFound(err) {
			return fmt.Errorf("knowledge document %s: %w", id, ports.ErrNotFound)
		}

		if err != nil {
			return err
		}

		if d.SourceKind != string(domain.SourceUpload) {
			return fmt.Errorf("knowledge document %s: %w", id, domain.ErrNotUploadSource)
		}

		if err := q.DeleteKnowledgeDocument(ctx, did); err != nil {
			return err
		}

		if err := appendAudit(ctx, q, domain.AuditEntry{
			Actor: by, Action: domain.AuditKnowledgeDocumentDeleted, TargetKind: domain.AuditTargetKnowledgeDocument,
			TargetID: did.String(), Data: map[string]any{auditSource: d.SourceName, auditTitle: d.Title},
		}); err != nil {
			return err
		}

		gone, err := unheld(ctx, q, []string{deref(d.BlobSha256)})
		if len(gone) > 0 {
			orphan = gone[0]
		}

		return err
	})
	if err != nil {
		return "", fmt.Errorf("delete knowledge document: %w", err)
	}

	return orphan, nil
}

// Documents implements ports.Knowledge.
func (k *Knowledge) Documents(ctx context.Context, source string, after domain.KnowledgeDocumentID, limit int) ([]domain.KnowledgeDocument, error) {
	params := db.KnowledgeDocumentsParams{MaxRows: int32(min(max(limit, 0), maxDocumentPage))}

	if after != "" {
		a, err := parseID("knowledge document", string(after))
		if err != nil {
			return nil, err
		}

		params.After = &a
	}

	var out []domain.KnowledgeDocument

	err := k.s.read(ctx, func(q *db.Queries) error {
		src, err := sourceNamed(ctx, q, source)
		if err != nil {
			return err
		}

		sid, err := uuid.Parse(string(src.ID))
		if err != nil {
			return err
		}

		params.SourceID = &sid

		rows, err := q.KnowledgeDocuments(ctx, params)
		if err != nil {
			return err
		}

		out = make([]domain.KnowledgeDocument, 0, len(rows))
		for _, r := range rows {
			out = append(out, documentFrom(r))
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("knowledge documents: %w", err)
	}

	return out, nil
}

// maxDocumentPage bounds one page of documents.
const maxDocumentPage = 1000

// Document implements ports.Knowledge.
func (k *Knowledge) Document(ctx context.Context, id domain.KnowledgeDocumentID) (domain.KnowledgeDocument, string, error) {
	did, err := parseID("knowledge document", string(id))
	if err != nil {
		return domain.KnowledgeDocument{}, "", err
	}

	var (
		out  domain.KnowledgeDocument
		text string
	)

	err = k.s.read(ctx, func(q *db.Queries) error {
		var err error

		if out, err = oneDocument(ctx, q, db.KnowledgeDocumentsParams{ID: &did}); err != nil {
			return err
		}

		text, err = q.DocumentText(ctx, did)

		return err
	})
	if err != nil {
		return domain.KnowledgeDocument{}, "", fmt.Errorf("knowledge document: %w", err)
	}

	return out, text, nil
}

func oneDocument(ctx context.Context, q *db.Queries, p db.KnowledgeDocumentsParams) (domain.KnowledgeDocument, error) {
	p.MaxRows = 1

	rows, err := q.KnowledgeDocuments(ctx, p)
	if err != nil {
		return domain.KnowledgeDocument{}, err
	}

	if len(rows) == 0 {
		return domain.KnowledgeDocument{}, fmt.Errorf("knowledge document: %w", ports.ErrNotFound)
	}

	return documentFrom(rows[0]), nil
}

// Search implements ports.Knowledge.
func (k *Knowledge) Search(ctx context.Context, q domain.KnowledgeQuery) ([]domain.KnowledgeHit, error) {
	if len(q.Sources) == 0 || q.Limit <= 0 {
		return nil, nil
	}

	vec, err := vectorText(q.Embedding)
	if err != nil {
		return nil, fmt.Errorf("search knowledge: %w", err)
	}

	if vec != "" && q.Model == "" {
		return nil, fmt.Errorf("search knowledge: an embedding without its model: %w", domain.ErrInvalid)
	}

	var rows []db.SearchKnowledgeRow

	err = k.s.read(ctx, func(dq *db.Queries) error {
		if err := dq.SetSearchScan(ctx); err != nil {
			return err
		}

		var err error

		rows, err = dq.SearchKnowledge(ctx, db.SearchKnowledgeParams{
			MaxHits: int32(min(q.Limit, domain.SearchCandidates)), Sources: q.Sources, //nolint:gosec // bounded by SearchCandidates
			Query: q.Text, Embedding: vec, Model: q.Model,
		})

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("search knowledge: %w", err)
	}

	out := make([]domain.KnowledgeHit, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.KnowledgeHit{
			Chunk: r.ID, Document: domain.KnowledgeDocumentID(r.DocumentID.String()), Source: r.Source, Title: r.Title,
			URL: deref(r.Url), Heading: r.Heading, Text: r.Text, Score: r.Score,
		})
	}

	return out, nil
}

func sourceAudit(ctx context.Context, q *db.Queries, by domain.Actor, action string, id uuid.UUID, data map[string]any) error {
	return appendAudit(ctx, q, domain.AuditEntry{
		Actor: by, Action: action, TargetKind: domain.AuditTargetKnowledgeSource, TargetID: id.String(), Data: data,
	})
}

func sourceFrom(r db.KnowledgeSourcesRow) (domain.KnowledgeSource, error) {
	config := map[string]string{}
	if err := json.Unmarshal(r.Config, &config); err != nil {
		return domain.KnowledgeSource{}, fmt.Errorf("knowledge source %s: config: %w", r.Name, err)
	}

	return domain.KnowledgeSource{
		ID: domain.KnowledgeSourceID(r.ID.String()), Name: r.Name, Title: r.Title, Kind: domain.KnowledgeSourceKind(r.Kind),
		Connection: deref(r.Connection), Config: config, SyncEvery: time.Duration(r.SyncEverySeconds) * time.Second,
		NextSyncAt: utc(r.NextSyncAt), LeaseUntil: utc(r.LeaseUntil), Status: domain.SyncStatus(r.Status),
		LastSyncedAt: utc(r.LastSyncedAt), LastError: deref(r.LastError), EmbedModel: deref(r.EmbedModel),
		Documents: int(r.Documents), CreatedBy: namedActor(r.CreatedBy, ""), CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
	}, nil
}

func documentFrom(r db.KnowledgeDocumentsRow) domain.KnowledgeDocument {
	return domain.KnowledgeDocument{
		ID: domain.KnowledgeDocumentID(r.ID.String()), Source: domain.KnowledgeSourceID(r.SourceID.String()), ExternalID: r.ExternalID,
		Version: r.Version, Title: r.Title, URL: deref(r.Url), MIME: r.Mime, Bytes: r.Bytes, Blob: domain.BlobHash(deref(r.BlobSha256)),
		Status: domain.DocumentStatus(r.Status), Error: deref(r.Error), SeenAt: r.SeenAt.UTC(), IndexedAt: utc(r.IndexedAt),
		EmbedModel: deref(r.EmbedModel), CreatedBy: namedActor(r.CreatedBy, ""),
	}
}

func nonNilConfig(c map[string]string) map[string]string {
	if c == nil {
		return map[string]string{}
	}

	return c
}

func wholeSeconds(d time.Duration) int64 { return int64(d / time.Second) }

// truncate cuts s to at most n characters, keeping it valid UTF-8.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}

	r := []rune(s)

	return string(r[:n])
}
