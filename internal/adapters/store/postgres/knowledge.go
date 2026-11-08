package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.Knowledge = (*Knowledge)(nil)

// Knowledge implements ports.Knowledge over 00054's knowledge_sources, 00099's knowledge_uploads
// and 00095's knowledge_snapshots, knowledge_files and knowledge_reads, as goal_app under
// row-level security: every statement sees the organization in ctx only (a claim alone reads
// across organizations, through 00055's definer), and every change people make appends its audit
// entry in its transaction. Knowledge is files (ADR 0038): an agent reads a source's files
// through its snapshots, and nothing embeds or ranks them here.
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
		secret, err := sourceSecret(ctx, q, n.Secret)
		if err != nil {
			return err
		}

		err = q.InsertKnowledgeSource(ctx, db.InsertKnowledgeSourceParams{
			ID: id, Name: n.Name, Title: n.Title, Kind: string(n.Kind), SecretID: secret, Config: config, ReadWhen: n.ReadWhen,
			SyncEverySeconds: wholeSeconds(n.Every()), CreatedBy: by.Ref(),
		})
		if violates(err, sourceNameKey) {
			return fmt.Errorf("knowledge source %q: %w", n.Name, ports.ErrNameTaken)
		}

		if err != nil {
			return ruleError(err, "")
		}

		if err := sourceAudit(ctx, q, by, domain.AuditKnowledgeSourceCreated, id,
			createdAudit(n)); err != nil {
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

// createdAudit is the data of a new source's created entry: its name, kind and what it reads.
func createdAudit(n domain.NewKnowledgeSource) map[string]any {
	out := map[string]any{auditName: n.Name, "kind": string(n.Kind)}
	if n.Secret != "" {
		out["secret"] = n.Secret
	}

	return out
}

// sourceSecret is the id of the secret name a source reads with, nil for none.
func sourceSecret(ctx context.Context, q *db.Queries, name string) (*uuid.UUID, error) {
	if name == "" {
		return nil, nil //nolint:nilnil // a source without a secret
	}

	id, err := q.KnowledgeSecret(ctx, name)
	if notFound(err) {
		return nil, fmt.Errorf("secret %q: %w", name, ports.ErrNotFound)
	}

	if err != nil {
		return nil, fmt.Errorf("secret %q: %w", name, err)
	}

	return &id, nil
}

// UpdateSource implements ports.Knowledge.
func (k *Knowledge) UpdateSource(ctx context.Context, name string, p domain.KnowledgeSourcePatch, by domain.Actor) (domain.KnowledgeSource, error) {
	if err := p.Check(); err != nil {
		return domain.KnowledgeSource{}, fmt.Errorf("update knowledge source: %w", err)
	}

	params := db.UpdateKnowledgeSourceParams{Name: name, Title: p.Title, ReadWhen: p.ReadWhen}

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
		src, err := oneSource(ctx, q, db.KnowledgeSourcesParams{Name: &name}, "knowledge source "+name)
		if err != nil {
			return err
		}

		if err := p.CheckOn(src); err != nil {
			return err
		}

		if p.Secret != nil {
			if params.SecretID, err = sourceSecret(ctx, q, *p.Secret); err != nil {
				return err
			}
		}

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

// ConvertSource implements ports.Knowledge: the source is locked while its rules are checked.
func (k *Knowledge) ConvertSource(ctx context.Context, name string, kind domain.KnowledgeSourceKind, by domain.Actor) (domain.KnowledgeSource, error) {
	var out domain.KnowledgeSource

	err := k.s.inTx(ctx, func(q *db.Queries) error {
		locked, err := q.LockKnowledgeSource(ctx, name)
		if notFound(err) {
			return fmt.Errorf("knowledge source %q: %w", name, ports.ErrNotFound)
		}

		if err != nil {
			return err
		}

		what := "knowledge source " + name

		src, err := oneSource(ctx, q, db.KnowledgeSourcesParams{ID: &locked.ID}, what)
		if err != nil {
			return err
		}

		if err := src.CheckConvert(kind); err != nil {
			return err
		}

		raw, err := json.Marshal(src.ConvertedConfig())
		if err != nil {
			return fmt.Errorf("config: %w", err)
		}

		if err := q.ConvertKnowledgeSource(ctx, db.ConvertKnowledgeSourceParams{Kind: string(kind), Config: raw, ID: locked.ID}); err != nil {
			return ruleError(err, "")
		}

		if err := sourceAudit(ctx, q, by, domain.AuditKnowledgeSourceUpdated, locked.ID,
			map[string]any{auditName: name, auditKind: string(kind)}); err != nil {
			return err
		}

		out, err = oneSource(ctx, q, db.KnowledgeSourcesParams{ID: &locked.ID}, what)

		return err
	})
	if err != nil {
		return domain.KnowledgeSource{}, fmt.Errorf("convert knowledge source: %w", err)
	}

	return out, nil
}

// DeleteSource implements ports.Knowledge: the source's upload contents are held
// (LockKnowledgeBlobs) from before the deletion until drop has run and the transaction ends.
func (k *Knowledge) DeleteSource(ctx context.Context, name string, by domain.Actor, drop ports.DropBlobs) error {
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

		if err := q.LockKnowledgeBlobs(ctx, blobs); err != nil {
			return err
		}

		if err := q.DeleteKnowledgeSource(ctx, src.ID); err != nil {
			return err
		}

		if err := sourceAudit(ctx, q, by, domain.AuditKnowledgeSourceDeleted, src.ID,
			map[string]any{auditName: name, "kind": src.Kind}); err != nil {
			return err
		}

		return dropUnheld(ctx, q, blobs, drop)
	})
	if err != nil {
		return fmt.Errorf("delete knowledge source: %w", err)
	}

	return nil
}

// dropUnheld calls drop with those of blobs, held by the transaction, that no document of the
// organization holds any more.
func dropUnheld(ctx context.Context, q *db.Queries, blobs []string, drop ports.DropBlobs) error {
	if len(blobs) == 0 || drop == nil {
		return nil
	}

	gone, err := q.UnheldBlobs(ctx, blobs)
	if err != nil || len(gone) == 0 {
		return err
	}

	out := make([]domain.BlobHash, 0, len(gone))
	for _, b := range gone {
		out = append(out, domain.BlobHash(b))
	}

	return drop(ctx, out)
}

// SyncNow implements ports.Knowledge.
func (k *Knowledge) SyncNow(ctx context.Context, name string) error {
	n, err := fetch(ctx, k.s.inTx, func(q *db.Queries) (int64, error) { return q.SyncKnowledgeSourceNow(ctx, name) })
	switch {
	case err != nil:
		return fmt.Errorf("sync knowledge source %q: %w", name, err)
	case n == 0:
		return fmt.Errorf("knowledge source %q: %w", name, ports.ErrNotFound)
	default:
		return nil
	}
}

// ClaimSource implements ports.Knowledge: goal_claim_knowledge_source claims across
// organizations, then the transaction binds to the claim's to read the source; the claim
// notifies the stream event knowledge (syncing).
func (k *Knowledge) ClaimSource(ctx context.Context, now, until time.Time) (*domain.SourceClaim, error) {
	var out *domain.SourceClaim

	err := k.s.systemTx(ctx, func(q *db.Queries) error {
		c, err := q.ClaimKnowledgeSource(ctx, db.ClaimKnowledgeSourceParams{Until: until, Now: now})
		if notFound(err) {
			return nil
		}

		if err != nil {
			return err
		}

		if err := q.SetOrg(ctx, c.OrgID.String()); err != nil {
			return fmt.Errorf("set org: %w", err)
		}

		s, err := oneSource(ctx, q, db.KnowledgeSourcesParams{ID: &c.ID}, "knowledge source "+c.ID.String())
		if err != nil {
			return err
		}

		out = &domain.SourceClaim{
			Org: domain.OrgID(c.OrgID.String()), Source: s, Lease: domain.SourceLease{Source: s.ID, Token: c.LeaseToken.String()},
		}

		return q.NotifyKnowledgeSync(ctx, c.ID)
	})
	if err != nil {
		return nil, fmt.Errorf("claim knowledge source: %w", err)
	}

	return out, nil
}

// lease parses l. A malformed one holds nothing: ErrLeaseLost.
func lease(l domain.SourceLease) (source, token uuid.UUID, err error) {
	source, err = uuid.Parse(string(l.Source))
	if err == nil {
		token, err = uuid.Parse(l.Token)
	}

	if err != nil {
		return source, token, fmt.Errorf("knowledge source %s: %w", l.Source, ports.ErrLeaseLost)
	}

	return source, token, nil
}

// held locks l's source while l is its lease (HoldKnowledgeLease); ErrLeaseLost when it is not.
func held(ctx context.Context, q *db.Queries, l domain.SourceLease, source, token uuid.UUID) error {
	_, err := q.HoldKnowledgeLease(ctx, db.HoldKnowledgeLeaseParams{ID: source, LeaseToken: token})
	if notFound(err) {
		return fmt.Errorf("knowledge source %s: %w", l.Source, ports.ErrLeaseLost)
	}

	return err
}

// RenewSource implements ports.Knowledge.
func (k *Knowledge) RenewSource(ctx context.Context, l domain.SourceLease, now, until time.Time) error {
	sid, token, err := lease(l)
	if err != nil {
		return err
	}

	n, err := fetch(ctx, k.s.inTx, func(q *db.Queries) (int64, error) {
		return q.RenewKnowledgeSource(ctx, db.RenewKnowledgeSourceParams{Until: until, ID: sid, LeaseToken: token, Now: now})
	})
	if err != nil {
		return fmt.Errorf("renew knowledge source: %w", err)
	}

	if n == 0 {
		return fmt.Errorf("knowledge source %s: %w", l.Source, ports.ErrLeaseLost)
	}

	return nil
}

// FinishSync implements ports.Knowledge; the end notifies the stream event knowledge (idle or failed).
func (k *Knowledge) FinishSync(ctx context.Context, l domain.SourceLease, r domain.SyncResult) error {
	sid, token, err := lease(l)
	if err != nil {
		return err
	}

	n, err := fetch(ctx, k.s.inTx, func(q *db.Queries) (int64, error) {
		n, err := q.FinishKnowledgeSync(ctx, db.FinishKnowledgeSyncParams{
			Error: truncate(r.Error, domain.MaxKnowledgeError), At: r.At, Due: r.Due, ID: sid, LeaseToken: token,
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
		return fmt.Errorf("knowledge source %s: %w", l.Source, ports.ErrLeaseLost)
	}

	return nil
}

// maxDocumentTitle bounds a document's title (00054's check).
const maxDocumentTitle = 1024

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
		Secret: deref(r.Secret), Config: config, ReadWhen: r.ReadWhen,
		CurrentSnapshot: snapshotID(r.CurrentSnapshotID), SyncEvery: time.Duration(r.SyncEverySeconds) * time.Second,
		NextSyncAt: utc(r.NextSyncAt), LeaseUntil: utc(r.LeaseUntil), Status: domain.SyncStatus(r.Status),
		LastSyncedAt: utc(r.LastSyncedAt), LastError: deref(r.LastError), SyncFailures: int(r.SyncFailures),
		CreatedBy: namedActor(r.CreatedBy, ""), CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
	}, nil
}

func nonNilConfig(c map[string]string) map[string]string {
	if c == nil {
		return map[string]string{}
	}

	return c
}

func wholeSeconds(d time.Duration) int64 { return int64(d / time.Second) }

// truncate cuts s to at most n characters of valid UTF-8 (an invalid byte becomes U+FFFD:
// PostgreSQL stores no other).
func truncate(s string, n int) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	if utf8.RuneCountInString(s) <= n {
		return s
	}

	r := []rune(s)

	return string(r[:n])
}

// AddUpload implements ports.Knowledge: the content hash is held (LockKnowledgeBlobs) while the
// file is written, and the content a replaced file held is dropped when no other upload holds it.
func (k *Knowledge) AddUpload(ctx context.Context, source string, u domain.KnowledgeUpload, by domain.Actor, drop ports.DropBlobs) (domain.KnowledgeUpload, error) {
	if !u.Blob.Valid() {
		return domain.KnowledgeUpload{}, fmt.Errorf("upload %q: its content hash %q: %w", u.Name, u.Blob, domain.ErrInvalid)
	}

	id, err := newID()
	if err != nil {
		return domain.KnowledgeUpload{}, err
	}

	var out domain.KnowledgeUpload

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

		blobs, err := lockUploadBlobs(ctx, q, src.ID, u)
		if err != nil {
			return err
		}

		added, err := q.UpsertKnowledgeUpload(ctx, db.UpsertKnowledgeUploadParams{
			ID: id, SourceID: src.ID, Name: truncate(u.Name, maxDocumentTitle), Mime: u.MIME, Bytes: u.Bytes,
			BlobSha256: string(u.Blob), CreatedBy: by.Ref(),
		})
		if err != nil {
			return ruleError(err, "")
		}

		if len(added) > 0 {
			if err := appendAudit(ctx, q, domain.AuditEntry{
				Actor: by, Action: domain.AuditKnowledgeDocumentUploaded, TargetKind: domain.AuditTargetKnowledgeDocument,
				TargetID: added[0].String(), Data: map[string]any{auditSource: source, auditTitle: u.Name, "bytes": u.Bytes},
			}); err != nil {
				return err
			}

			if err := dropUnheld(ctx, q, blobs[1:], drop); err != nil {
				return err
			}
		}

		out, err = uploadNamed(ctx, q, src.ID, truncate(u.Name, maxDocumentTitle))

		return err
	})
	if err != nil {
		return domain.KnowledgeUpload{}, fmt.Errorf("add upload: %w", err)
	}

	return out, nil
}

// lockUploadBlobs holds the content of u and of the file it replaces, and returns them, u's first.
func lockUploadBlobs(ctx context.Context, q *db.Queries, source uuid.UUID, u domain.KnowledgeUpload) ([]string, error) {
	blobs := []string{string(u.Blob)}

	old, err := q.KnowledgeUploadNamed(ctx, db.KnowledgeUploadNamedParams{SourceID: source, Name: u.Name})
	if err != nil && !notFound(err) {
		return nil, err
	}

	if err == nil {
		blobs = append(blobs, old.BlobSha256)
	}

	return blobs, q.LockKnowledgeBlobs(ctx, blobs)
}

// uploadNamed is the file named name of the source with id.
func uploadNamed(ctx context.Context, q *db.Queries, source uuid.UUID, name string) (domain.KnowledgeUpload, error) {
	files, err := q.KnowledgeUploads(ctx, source)
	if err != nil {
		return domain.KnowledgeUpload{}, err
	}

	for _, f := range files {
		if f.Name == name {
			return uploadFrom(f), nil
		}
	}

	return domain.KnowledgeUpload{}, fmt.Errorf("knowledge upload %q: %w", name, ports.ErrNotFound)
}

// DeleteUpload implements ports.Knowledge, holding the file's content as DeleteSource does.
func (k *Knowledge) DeleteUpload(ctx context.Context, source, name string, by domain.Actor, drop ports.DropBlobs) error {
	err := k.s.inTx(ctx, func(q *db.Queries) error {
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

		u, err := q.KnowledgeUploadNamed(ctx, db.KnowledgeUploadNamedParams{SourceID: src.ID, Name: name})
		if notFound(err) {
			return fmt.Errorf("knowledge source %q has no upload %q: %w", source, name, ports.ErrNotFound)
		}

		if err != nil {
			return err
		}

		blobs := []string{u.BlobSha256}
		if err := q.LockKnowledgeBlobs(ctx, blobs); err != nil {
			return err
		}

		if err := q.DeleteKnowledgeUpload(ctx, u.ID); err != nil {
			return err
		}

		if err := appendAudit(ctx, q, domain.AuditEntry{
			Actor: by, Action: domain.AuditKnowledgeDocumentDeleted, TargetKind: domain.AuditTargetKnowledgeDocument,
			TargetID: u.ID.String(), Data: map[string]any{auditSource: source, auditTitle: name},
		}); err != nil {
			return err
		}

		return dropUnheld(ctx, q, blobs, drop)
	})
	if err != nil {
		return fmt.Errorf("delete knowledge upload: %w", err)
	}

	return nil
}

// Uploads implements ports.Knowledge.
func (k *Knowledge) Uploads(ctx context.Context, source string) ([]domain.KnowledgeUpload, error) {
	var out []domain.KnowledgeUpload

	err := k.s.inTx(ctx, func(q *db.Queries) error {
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

		rows, err := q.KnowledgeUploads(ctx, src.ID)
		if err != nil {
			return err
		}

		out = make([]domain.KnowledgeUpload, 0, len(rows))
		for _, r := range rows {
			out = append(out, uploadFrom(r))
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("knowledge uploads: %w", err)
	}

	return out, nil
}

func uploadFrom(r db.KnowledgeUploadsRow) domain.KnowledgeUpload {
	return domain.KnowledgeUpload{
		ID: domain.KnowledgeUploadID(r.ID.String()), Source: domain.KnowledgeSourceID(r.SourceID.String()), Name: r.Name, MIME: r.Mime,
		Bytes: r.Bytes, Blob: domain.BlobHash(r.BlobSha256), CreatedBy: namedActor(r.CreatedBy, ""), CreatedAt: r.CreatedAt.UTC(),
	}
}
