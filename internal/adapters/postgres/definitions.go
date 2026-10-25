package postgres

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/byFrederick/goal/internal/adapters/postgres/db"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

var _ ports.DefinitionStore = (*Definitions)(nil)

// Constraints and SQLSTATEs the definition store maps to ports errors.
const (
	factoryIDKey        = "factories_pkey"
	factoryNameKey      = "factories_name_key"
	foreignKeyViolation = "23503"
)

// auditName is the audit data key of a factory's name.
const auditName = "name"

// Definitions implements ports.DefinitionStore: blob content in the bucket through
// ports.ObjectStore (written once with PutIfAbsent, at domain.BlobKey), and the blob index,
// versions and current-version pointers in PostgreSQL (migration 00010). Like Store, every
// method runs in transactions bound to the organization in ctx, so row-level security hides
// other organizations' factories, versions and blobs. Blob uploads happen before and outside
// the transaction that records them; manifests and pointers change in transactions that check
// every hash is a blob of the organization. Each change appends its audit entry in its own
// transaction, and triggers announce it on goal_definitions (DefinitionNotices).
//
// Every write locks the factory row first (FOR NO KEY UPDATE), then its versions, then its
// items, so concurrent writers of one factory take turns and never deadlock with the engine,
// which locks items and never factories. It is safe for concurrent use and keeps no state.
type Definitions struct {
	store   *Store
	objects ports.ObjectStore
	log     *slog.Logger
}

// NewDefinitions returns a definition store on pool, keeping blobs in objects. log may be nil.
func NewDefinitions(pool *pgxpool.Pool, objects ports.ObjectStore, log *slog.Logger) *Definitions {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &Definitions{store: New(pool), objects: objects, log: log}
}

// CreateFactory implements ports.DefinitionStore.
func (d *Definitions) CreateFactory(ctx context.Context, n ports.NewFactory) (ports.FactoryRecord, domain.DefinitionVersion, error) {
	id, err := uuid.Parse(string(n.ID))
	if err != nil {
		return ports.FactoryRecord{}, domain.DefinitionVersion{}, fmt.Errorf("create factory: id %q: %w", n.ID, domain.ErrInvalid)
	}

	draft, err := newDraft(n.Manifest, n.Source, n.By)
	if err != nil {
		return ports.FactoryRecord{}, domain.DefinitionVersion{}, fmt.Errorf("create factory %s: %w", n.ID, err)
	}

	var (
		rec ports.FactoryRecord
		v   domain.DefinitionVersion
	)

	err = d.store.inTx(ctx, func(q *db.Queries) error {
		if err := knownBlobs(ctx, q, n.Manifest); err != nil {
			return err
		}

		by := n.By.Ref()

		err := q.InsertFactory(ctx, db.InsertFactoryParams{ID: id, Name: n.Name, Title: optional(n.Title), CreatedBy: &by})
		if err != nil {
			return factoryRuleError(err, n.ID, n.Name)
		}

		draft.FactoryID = id
		if v, err = insertVersion(ctx, q, draft); err != nil {
			return err
		}

		err = appendAudit(ctx, q, factoryEntry(n.By, domain.AuditFactoryCreated, n.ID,
			map[string]any{auditName: n.Name, "source": string(n.Source.Kind)}))
		if err != nil {
			return err
		}

		rec, err = factoryRecord(ctx, q, id)

		return err
	})
	if err != nil {
		return ports.FactoryRecord{}, domain.DefinitionVersion{}, fmt.Errorf("create factory %s: %w", n.ID, err)
	}

	return rec, v, nil
}

// FactoryRecord implements ports.DefinitionStore.
func (d *Definitions) FactoryRecord(ctx context.Context, id domain.FactoryID) (ports.FactoryRecord, error) {
	fid, err := parseID("factory", string(id))
	if err != nil {
		return ports.FactoryRecord{}, orgFirst(ctx, err)
	}

	rec, err := fetch(ctx, d.store.read, func(q *db.Queries) (ports.FactoryRecord, error) { return factoryRecord(ctx, q, fid) })
	if err != nil {
		return ports.FactoryRecord{}, fmt.Errorf("factory record: %w", err)
	}

	return rec, nil
}

// FactoryRecords implements ports.DefinitionStore.
func (d *Definitions) FactoryRecords(ctx context.Context) ([]ports.FactoryRecord, error) {
	recs, err := fetch(ctx, d.store.read, func(q *db.Queries) ([]ports.FactoryRecord, error) {
		rows, err := q.FactoryRecords(ctx)
		if err != nil {
			return nil, err
		}

		return convertAll(rows, recordFrom)
	})
	if err != nil {
		return nil, fmt.Errorf("factory records: %w", err)
	}

	return recs, nil
}

// DeleteUnpublished implements ports.DefinitionStore. Its drafts go with the factory row (ON
// DELETE CASCADE); a factory that items or runs reference cannot go and is ErrConflict too.
func (d *Definitions) DeleteUnpublished(ctx context.Context, id domain.FactoryID, by domain.Actor) error {
	err := d.change(ctx, id, func(q *db.Queries, f db.LockFactoryRow) error {
		if f.LastVersionNumber > 0 {
			return fmt.Errorf("factory %s was published: %w", id, ports.ErrConflict)
		}

		if err := appendAudit(ctx, q, factoryEntry(by, domain.AuditFactoryDeleted, id, map[string]any{auditName: f.Name})); err != nil {
			return err
		}

		err := q.DeleteFactory(ctx, f.ID)
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == foreignKeyViolation {
			return fmt.Errorf("factory %s has items or runs: %w", id, ports.ErrConflict)
		}

		return err
	})
	if err != nil {
		return fmt.Errorf("delete unpublished factory: %w", err)
	}

	return nil
}

// SetPaused implements ports.DefinitionStore.
func (d *Definitions) SetPaused(ctx context.Context, id domain.FactoryID, paused bool, by domain.Actor) (ports.FactoryRecord, error) {
	var rec ports.FactoryRecord

	err := d.change(ctx, id, func(q *db.Queries, f db.LockFactoryRow) error {
		if paused != (f.PausedAt != nil) {
			action := domain.AuditFactoryResumed
			if paused {
				action = domain.AuditFactoryPaused
			}

			if err := q.SetFactoryPaused(ctx, db.SetFactoryPausedParams{ID: f.ID, Paused: paused, PausedBy: by.Ref()}); err != nil {
				return err
			}

			if err := appendAudit(ctx, q, factoryEntry(by, action, id, nil)); err != nil {
				return err
			}
		}

		var err error

		rec, err = factoryRecord(ctx, q, f.ID)

		return err
	})
	if err != nil {
		return ports.FactoryRecord{}, fmt.Errorf("set paused: %w", err)
	}

	return rec, nil
}

// ArchiveFactory implements ports.DefinitionStore. Paused and leases are checked under the
// factory's row lock (see FactoryLeased for the one claim it cannot see).
func (d *Definitions) ArchiveFactory(ctx context.Context, id domain.FactoryID, by domain.Actor) (ports.FactoryRecord, error) {
	rec, err := d.setArchived(ctx, id, true, by)
	if err != nil {
		return ports.FactoryRecord{}, fmt.Errorf("archive factory: %w", err)
	}

	return rec, nil
}

// RestoreArchivedFactory implements ports.DefinitionStore.
func (d *Definitions) RestoreArchivedFactory(ctx context.Context, id domain.FactoryID, by domain.Actor) (ports.FactoryRecord, error) {
	rec, err := d.setArchived(ctx, id, false, by)
	if err != nil {
		return ports.FactoryRecord{}, fmt.Errorf("restore archived factory: %w", err)
	}

	return rec, nil
}

func (d *Definitions) setArchived(ctx context.Context, id domain.FactoryID, archive bool, by domain.Actor) (ports.FactoryRecord, error) {
	var rec ports.FactoryRecord

	err := d.change(ctx, id, func(q *db.Queries, f db.LockFactoryRow) error {
		if archive != (f.ArchivedAt != nil) {
			action := domain.AuditFactoryRestored

			if archive {
				action = domain.AuditFactoryArchived

				if err := archivable(ctx, q, f); err != nil {
					return fmt.Errorf("factory %s: %w", id, err)
				}
			}

			if err := q.SetFactoryArchived(ctx, db.SetFactoryArchivedParams{ID: f.ID, Archived: archive, ArchivedBy: by.Ref()}); err != nil {
				return err
			}

			if err := appendAudit(ctx, q, factoryEntry(by, action, id, map[string]any{auditName: f.Name})); err != nil {
				return err
			}
		}

		var err error

		rec, err = factoryRecord(ctx, q, f.ID)

		return err
	})

	return rec, err
}

// archivable returns nil when the locked factory f may be archived: it is paused and none of its
// items holds a lease.
func archivable(ctx context.Context, q *db.Queries, f db.LockFactoryRow) error {
	if f.PausedAt == nil {
		return ports.ErrFactoryRunning
	}

	leased, err := q.FactoryLeased(ctx, f.ID)
	if err != nil {
		return fmt.Errorf("leases: %w", err)
	}

	if leased {
		return ports.ErrFactoryBusy
	}

	return nil
}

// PutBlobs implements ports.DefinitionStore. Objects go to the bucket first; their rows are
// recorded after, so a crash in between leaves only an unreferenced object, which the next put
// of the same content adopts.
func (d *Definitions) PutBlobs(ctx context.Context, files map[string][]byte) (domain.Manifest, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, domain.ErrNoOrg
	}

	m := domain.Manifest{}
	content := map[domain.BlobHash][]byte{}

	for _, path := range slices.Sorted(maps.Keys(files)) {
		body := files[path]
		if len(body) > domain.MaxBlobSize {
			return nil, fmt.Errorf("put blobs: %s has %d bytes, more than %d: %w", path, len(body), domain.MaxBlobSize, domain.ErrInvalid)
		}

		h := domain.HashBlob(body)
		m[path], content[h] = h, body
	}

	known, err := fetch(ctx, d.store.read, func(q *db.Queries) (map[domain.BlobHash]int64, error) { return blobSizes(ctx, q, m.Hashes()) })
	if err != nil {
		return nil, fmt.Errorf("put blobs: %w", err)
	}

	var (
		hashes []string
		sizes  []int32
	)

	for _, h := range m.Hashes() {
		if _, ok := known[h]; ok {
			continue
		}

		if err := d.upload(ctx, org, h, content[h]); err != nil {
			return nil, fmt.Errorf("put blobs: %w", err)
		}

		hashes, sizes = append(hashes, string(h)), append(sizes, int32(len(content[h]))) //nolint:gosec // at most MaxBlobSize
	}

	if len(hashes) == 0 {
		return m, nil
	}

	err = d.store.inTx(ctx, func(q *db.Queries) error {
		return q.InsertBlobs(ctx, db.InsertBlobsParams{Hashes: hashes, Sizes: sizes})
	})
	if err != nil {
		return nil, fmt.Errorf("put blobs: record: %w", err)
	}

	return m, nil
}

// upload writes one blob's object unless it is there. An object already there (from a put that
// did not get as far as recording it) is adopted only when it hashes to h; any other object at
// the key (written to the bucket directly) is replaced by body, so a corrupt object is never
// recorded as the blob.
func (d *Definitions) upload(ctx context.Context, org domain.OrgID, h domain.BlobHash, body []byte) error {
	key := domain.BlobKey(org, h)

	_, created, err := d.objects.PutIfAbsent(ctx, key, body)
	if err != nil {
		return fmt.Errorf("blob %s: %w", h, err)
	}

	if created {
		return nil
	}

	if obj, err := d.objects.Get(ctx, key); err == nil && domain.HashBlob(obj.Body) == h {
		return nil
	} else if err != nil && !errors.Is(err, ports.ErrNotFound) {
		return fmt.Errorf("blob %s: read the object there: %w", h, err)
	}

	d.log.WarnContext(ctx, "definition blob object does not match its hash; replaced", slog.String("key", key))

	if _, err := d.objects.Put(ctx, key, body); err != nil {
		return fmt.Errorf("blob %s: replace the object there: %w", h, err)
	}

	return nil
}

// Blob implements ports.DefinitionStore.
func (d *Definitions) Blob(ctx context.Context, h domain.BlobHash) ([]byte, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, domain.ErrNoOrg
	}

	known, err := fetch(ctx, d.store.read, func(q *db.Queries) (map[domain.BlobHash]int64, error) {
		return blobSizes(ctx, q, []domain.BlobHash{h})
	})
	if err != nil {
		return nil, fmt.Errorf("blob %s: %w", h, err)
	}

	if _, ok := known[h]; !ok {
		return nil, fmt.Errorf("blob %s: %w", h, ports.ErrNotFound)
	}

	obj, err := d.objects.Get(ctx, domain.BlobKey(org, h))
	if err != nil {
		return nil, fmt.Errorf("blob %s: %w", h, err)
	}

	if domain.HashBlob(obj.Body) != h {
		d.log.WarnContext(ctx, "definition blob does not match its hash", slog.String("key", obj.Key))

		return nil, fmt.Errorf("blob %s: %w", h, ports.ErrBlobCorrupt)
	}

	return obj.Body, nil
}

// BlobSizes implements ports.DefinitionStore.
func (d *Definitions) BlobSizes(ctx context.Context, hashes []domain.BlobHash) (map[domain.BlobHash]int64, error) {
	sizes, err := fetch(ctx, d.store.read, func(q *db.Queries) (map[domain.BlobHash]int64, error) { return blobSizes(ctx, q, hashes) })
	if err != nil {
		return nil, fmt.Errorf("blob sizes: %w", err)
	}

	return sizes, nil
}

// Version implements ports.DefinitionStore.
func (d *Definitions) Version(ctx context.Context, id domain.DefinitionVersionID) (domain.DefinitionVersion, error) {
	vid, err := parseID("definition version", string(id))
	if err != nil {
		return domain.DefinitionVersion{}, orgFirst(ctx, err)
	}

	return d.readVersion(ctx, "definition version "+string(id), func(q *db.Queries) (db.DefinitionVersion, error) {
		return q.GetVersion(ctx, vid)
	})
}

// VersionByNumber implements ports.DefinitionStore.
func (d *Definitions) VersionByNumber(ctx context.Context, f domain.FactoryID, n int) (domain.DefinitionVersion, error) {
	fid, err := parseID("factory", string(f))
	if err != nil {
		return domain.DefinitionVersion{}, orgFirst(ctx, err)
	}

	return d.readVersion(ctx, fmt.Sprintf("factory %s version %d", f, n), func(q *db.Queries) (db.DefinitionVersion, error) {
		return q.VersionByNumber(ctx, db.VersionByNumberParams{FactoryID: fid, Number: int32ptr(n)})
	})
}

// Draft implements ports.DefinitionStore.
func (d *Definitions) Draft(ctx context.Context, f domain.FactoryID) (domain.DefinitionVersion, error) {
	fid, err := parseID("factory", string(f))
	if err != nil {
		return domain.DefinitionVersion{}, orgFirst(ctx, err)
	}

	return d.readVersion(ctx, "draft of factory "+string(f), func(q *db.Queries) (db.DefinitionVersion, error) {
		return q.Draft(ctx, fid)
	})
}

// Versions implements ports.DefinitionStore.
func (d *Definitions) Versions(ctx context.Context, f domain.FactoryID, before, limit int) ([]domain.DefinitionVersion, error) {
	fid, err := parseID("factory", string(f))
	if err != nil {
		return nil, orgFirst(ctx, err)
	}

	vs, err := fetch(ctx, d.store.read, func(q *db.Queries) ([]domain.DefinitionVersion, error) {
		rows, err := q.ListVersions(ctx, db.ListVersionsParams{
			FactoryID: fid, Before: int32(min(max(before, 0), maxInt32)), MaxVersions: int64(max(limit, 0)),
		})
		if err != nil {
			return nil, err
		}

		return convertAll(rows, versionFrom)
	})
	if err != nil {
		return nil, fmt.Errorf("versions of factory %s: %w", f, err)
	}

	return vs, nil
}

// CurrentVersions implements ports.DefinitionStore.
func (d *Definitions) CurrentVersions(ctx context.Context) ([]domain.DefinitionVersion, error) {
	vs, err := fetch(ctx, d.store.read, func(q *db.Queries) ([]domain.DefinitionVersion, error) {
		rows, err := q.CurrentVersions(ctx)
		if err != nil {
			return nil, err
		}

		return convertAll(rows, versionFrom)
	})
	if err != nil {
		return nil, fmt.Errorf("current versions: %w", err)
	}

	return vs, nil
}

// readVersion reads one version in a read-only transaction; no row is ErrNotFound.
func (d *Definitions) readVersion(ctx context.Context, what string, query func(*db.Queries) (db.DefinitionVersion, error)) (domain.DefinitionVersion, error) {
	row, err := fetch(ctx, d.store.read, query)
	if notFound(err) {
		return domain.DefinitionVersion{}, fmt.Errorf("%s: %w", what, ports.ErrNotFound)
	}

	if err != nil {
		return domain.DefinitionVersion{}, fmt.Errorf("%s: %w", what, err)
	}

	return versionFrom(row)
}

// change runs fn in a transaction of the organization in ctx with factory id locked; a factory
// the organization lacks is ErrNotFound.
func (d *Definitions) change(ctx context.Context, id domain.FactoryID, fn func(*db.Queries, db.LockFactoryRow) error) error {
	fid, err := parseID("factory", string(id))
	if err != nil {
		return orgFirst(ctx, err)
	}

	return d.store.inTx(ctx, func(q *db.Queries) error {
		f, err := q.LockFactory(ctx, fid)
		if notFound(err) {
			return fmt.Errorf("factory %s: %w", id, ports.ErrNotFound)
		}

		if err != nil {
			return fmt.Errorf("lock factory %s: %w", id, err)
		}

		return fn(q, f)
	})
}

// orgFirst reports a missing organization before anything else about a call, as a transaction
// would.
func orgFirst(ctx context.Context, err error) error {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.ErrNoOrg
	}

	return err
}

func factoryRecord(ctx context.Context, q *db.Queries, id uuid.UUID) (ports.FactoryRecord, error) {
	row, err := q.FactoryRecord(ctx, id)
	if notFound(err) {
		return ports.FactoryRecord{}, fmt.Errorf("factory %s: %w", id, ports.ErrNotFound)
	}

	if err != nil {
		return ports.FactoryRecord{}, err
	}

	return recordFrom(row)
}

func blobSizes(ctx context.Context, q *db.Queries, hashes []domain.BlobHash) (map[domain.BlobHash]int64, error) {
	in := make([]string, 0, len(hashes))
	for _, h := range hashes {
		in = append(in, string(h))
	}

	rows, err := q.BlobSizes(ctx, in)
	if err != nil {
		return nil, err
	}

	out := make(map[domain.BlobHash]int64, len(rows))
	for _, r := range rows {
		out[domain.BlobHash(r.Sha256)] = int64(r.Size)
	}

	return out, nil
}

// knownBlobs fails with ErrUnknownBlob unless every hash of m is a blob of the organization.
func knownBlobs(ctx context.Context, q *db.Queries, m domain.Manifest) error {
	hashes := m.Hashes()

	known, err := blobSizes(ctx, q, hashes)
	if err != nil {
		return err
	}

	for _, h := range hashes {
		if _, ok := known[h]; !ok {
			return fmt.Errorf("blob %s: %w", h, ports.ErrUnknownBlob)
		}
	}

	return nil
}

// factoryRuleError maps a rejected factory row: its id taken (in any organization) is
// ErrConflict, its name taken in the organization ErrNameTaken, a broken CHECK ErrInvalid.
func factoryRuleError(err error, id domain.FactoryID, name string) error {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)

	switch {
	case !ok:
		return err
	case pgErr.Code == uniqueViolation && pgErr.ConstraintName == factoryIDKey:
		return fmt.Errorf("factory %s exists: %w", id, ports.ErrConflict)
	case pgErr.Code == uniqueViolation && pgErr.ConstraintName == factoryNameKey:
		return fmt.Errorf("factory name %q: %w", name, ports.ErrNameTaken)
	case pgErr.Code == checkViolation:
		return fmt.Errorf("factory %s: %s: %w", id, pgErr.ConstraintName, domain.ErrInvalid)
	default:
		return err
	}
}

func factoryEntry(by domain.Actor, action string, id domain.FactoryID, data map[string]any) domain.AuditEntry {
	return domain.AuditEntry{Actor: by, Action: action, TargetKind: domain.AuditTargetFactory, TargetID: string(id), FactoryID: id, Data: data}
}

func versionEntry(by domain.Actor, action string, v domain.DefinitionVersion, data map[string]any) domain.AuditEntry {
	return domain.AuditEntry{
		Actor: by, Action: action, TargetKind: domain.AuditTargetVersion, TargetID: string(v.ID), FactoryID: v.FactoryID, Data: data,
	}
}

func recordFrom(r db.FactoryRecord) (ports.FactoryRecord, error) {
	rec := ports.FactoryRecord{
		ID: domain.FactoryID(r.ID.String()), OrgID: domain.OrgID(r.OrgID.String()), Name: r.Name, Title: deref(r.Title),
		LastVersionNumber: int(r.LastVersionNumber), CreatedAt: r.CreatedAt.UTC(),
	}

	var err error

	if rec.CreatedBy, err = actorOrZero(r.CreatedBy); err != nil {
		return ports.FactoryRecord{}, fmt.Errorf("factory %s: created by: %w", r.ID, err)
	}

	if r.PausedAt != nil {
		rec.PausedAt = r.PausedAt.UTC()
		if rec.PausedBy, err = actorOrZero(r.PausedBy); err != nil {
			return ports.FactoryRecord{}, fmt.Errorf("factory %s: paused by: %w", r.ID, err)
		}
	}

	if r.ArchivedAt != nil {
		rec.ArchivedAt = r.ArchivedAt.UTC()
		if rec.ArchivedBy, err = actorOrZero(r.ArchivedBy); err != nil {
			return ports.FactoryRecord{}, fmt.Errorf("factory %s: archived by: %w", r.ID, err)
		}
	}

	if rec.Current, err = versionRef(r.CurrentID, r.CurrentNumber, r.CurrentRevision, r.CurrentAt, r.CurrentBy); err != nil {
		return ports.FactoryRecord{}, fmt.Errorf("factory %s: current version: %w", r.ID, err)
	}

	if rec.Draft, err = versionRef(r.DraftID, nil, r.DraftRevision, r.DraftAt, r.DraftBy); err != nil {
		return ports.FactoryRecord{}, fmt.Errorf("factory %s: draft: %w", r.ID, err)
	}

	return rec, nil
}

// versionRef is the reference to a version a factory record joined, or nil when it joined none.
func versionRef(id *uuid.UUID, number *int32, revision *int64, at *time.Time, by *string) (*ports.VersionRef, error) {
	if id == nil {
		return nil, nil //nolint:nilnil // no version is not an error
	}

	ref := &ports.VersionRef{ID: domain.DefinitionVersionID(id.String())}

	if number != nil {
		ref.Number = int(*number)
	}

	if revision != nil {
		ref.Revision = *revision
	}

	if at != nil {
		ref.At = at.UTC()
	}

	actor, err := actorOrZero(by)
	ref.By = actor

	return ref, err
}

// actorOrZero reads a `_by` column; NULL is the zero Actor.
func actorOrZero(ref *string) (domain.Actor, error) {
	if ref == nil {
		return domain.Actor{}, nil
	}

	return domain.ActorFromRef(*ref)
}

// sourceDoc is definition_versions.source.
type sourceDoc struct {
	Kind     domain.SourceKind `json:"kind,omitempty"`
	Template string            `json:"template,omitempty"`
	Factory  domain.FactoryID  `json:"factory,omitempty"`
	Version  string            `json:"version,omitempty"`
	Manifest domain.Manifest   `json:"manifest,omitempty"`
}

func versionFrom(r db.DefinitionVersion) (domain.DefinitionVersion, error) {
	v := domain.DefinitionVersion{
		ID: domain.DefinitionVersionID(r.ID.String()), OrgID: domain.OrgID(r.OrgID.String()),
		FactoryID: domain.FactoryID(r.FactoryID.String()), State: domain.VersionState(r.State), Revision: r.Revision,
		Message: r.Message, CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
	}

	if r.Number != nil {
		v.Number = int(*r.Number)
	}

	if r.BaseVersionID != nil {
		v.BaseID = domain.DefinitionVersionID(r.BaseVersionID.String())
	}

	if r.PublishedAt != nil {
		v.PublishedAt = r.PublishedAt.UTC()
	}

	var src sourceDoc

	err := errors.Join(json.Unmarshal(r.Manifest, &v.Manifest), json.Unmarshal(r.Source, &src))
	if err != nil {
		return domain.DefinitionVersion{}, fmt.Errorf("definition version %s: %w", r.ID, err)
	}

	v.Source = domain.VersionSource{
		Kind: src.Kind, Template: src.Template, Factory: src.Factory, Version: domain.DefinitionVersionID(src.Version),
		Manifest: src.Manifest,
	}

	var errs [3]error

	v.CreatedBy, errs[0] = domain.ActorFromRef(r.CreatedBy)
	v.UpdatedBy, errs[1] = domain.ActorFromRef(r.UpdatedBy)
	v.PublishedBy, errs[2] = actorOrZero(r.PublishedBy)

	if err := errors.Join(errs[:]...); err != nil {
		return domain.DefinitionVersion{}, fmt.Errorf("definition version %s: %w", r.ID, err)
	}

	return v, nil
}

// itemsAt converts the open items OpenItemsAt locked.
func itemAt(r db.OpenItemsAtRow) ports.ItemAt {
	return ports.ItemAt{ID: domain.ItemID(r.ID.String()), Number: r.Number, Station: r.Station, State: domain.ItemState(r.State)}
}

// byNumber orders items by number.
func byNumber(a, b ports.ItemAt) int { return cmp.Compare(a.Number, b.Number) }

const maxInt32 = 1<<31 - 1

func int32ptr(n int) *int32 {
	v := int32(min(max(n, 0), maxInt32))

	return &v
}
