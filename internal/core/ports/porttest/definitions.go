package porttest

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.DefinitionStore = (*Definitions)(nil)

// auditName is the audit data key of a factory's name.
const auditName = "name"

// Definitions keeps factories, definition versions and the blob index in memory behind one
// mutex, so every method is atomic like the adapter's transaction, with the database's rules:
// one open draft per factory, compare-and-set revisions, immutable published versions, the
// current pointer only on a published version, names unique per organization, and row-level
// isolation by the organization in ctx. Blob content goes to an in-memory bucket, written once.
type Definitions struct {
	mu sync.Mutex
	// notifyMu keeps notices in commit order without holding mu while they are delivered.
	notifyMu sync.Mutex

	objects *ObjectStore
	items   *Store
	audit   ports.Audit
	notices *DefinitionNotices
	now     func() time.Time

	factories map[domain.FactoryID]*factoryRow
	versions  map[domain.DefinitionVersionID]*domain.DefinitionVersion
	blobs     map[domain.OrgID]map[domain.BlobHash]int64
	ids       int
}

// DefinitionsDeps is what Definitions works with. Only Objects is required.
type DefinitionsDeps struct {
	// Objects is the bucket blobs are written to.
	Objects *ObjectStore
	// Items, when set, is the item store of the same "database": factories created here are
	// registered there, and publish and restore move its items off removed stations.
	Items *Store
	// Audit, when set, receives every change's audit entry; its organizations must exist.
	Audit ports.Audit
	// Notices, when set, gets a notice of every committed change.
	Notices *DefinitionNotices
	// Now stamps changes; time.Now when nil.
	Now func() time.Time
}

type factoryRow struct {
	org         domain.OrgID
	id          domain.FactoryID
	name, title string
	current     domain.DefinitionVersionID
	draft       domain.DefinitionVersionID
	last        int
	pausedAt    time.Time
	pausedBy    domain.Actor
	archivedAt  time.Time
	archivedBy  domain.Actor
	createdAt   time.Time
	createdBy   domain.Actor
}

// NewDefinitions returns a definition store with no factories.
func NewDefinitions(d DefinitionsDeps) *Definitions {
	if d.Now == nil {
		d.Now = time.Now
	}

	return &Definitions{
		objects: d.Objects, items: d.Items, audit: d.Audit, notices: d.Notices, now: d.Now,
		factories: map[domain.FactoryID]*factoryRow{},
		versions:  map[domain.DefinitionVersionID]*domain.DefinitionVersion{},
		blobs:     map[domain.OrgID]map[domain.BlobHash]int64{},
	}
}

// change runs fn under the lock and then delivers the notices it returns, in commit order.
func change[T any](d *Definitions, fn func() (T, []ports.DefinitionNotice, error)) (T, error) {
	d.mu.Lock()
	out, notices, err := fn()

	// Take the delivery lock before releasing the state lock, so notices leave in commit order.
	d.notifyMu.Lock()
	d.mu.Unlock()

	defer d.notifyMu.Unlock()

	if err == nil && d.notices != nil {
		d.notices.Publish(notices...)
	}

	return out, err
}

// CreateFactory implements ports.DefinitionStore.
func (d *Definitions) CreateFactory(ctx context.Context, n ports.NewFactory) (ports.FactoryRecord, domain.DefinitionVersion, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return ports.FactoryRecord{}, domain.DefinitionVersion{}, err
	}

	type created struct {
		r ports.FactoryRecord
		v domain.DefinitionVersion
	}

	out, err := change(d, func() (created, []ports.DefinitionNotice, error) {
		if _, ok := d.factories[n.ID]; ok {
			return created{}, nil, fmt.Errorf("factory %s exists: %w", n.ID, ports.ErrConflict)
		}

		if err := d.checkLocked(org, n.ID, n.Name, n.Manifest); err != nil {
			return created{}, nil, err
		}

		err := d.auditLocked(ctx, n.By, domain.AuditFactoryCreated, domain.AuditTargetFactory, string(n.ID), n.ID,
			map[string]any{auditName: n.Name, "source": string(n.Source.Kind)})
		if err != nil {
			return created{}, nil, err
		}

		f := &factoryRow{org: org, id: n.ID, name: n.Name, title: n.Title, createdAt: d.stamp(), createdBy: stored(n.By)}
		d.factories[n.ID] = f
		v := d.newDraftLocked(f, "", n.Manifest, n.Source, n.By)

		if d.items != nil {
			d.items.AddFactory(org, n.ID)
		}

		return created{d.recordLocked(f), cloneVersion(v)}, []ports.DefinitionNotice{factoryNotice(f), draftNotice(v)}, nil
	})

	return out.r, out.v, err
}

// FactoryRecord implements ports.DefinitionStore.
func (d *Definitions) FactoryRecord(ctx context.Context, id domain.FactoryID) (ports.FactoryRecord, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return ports.FactoryRecord{}, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	f, err := d.factoryLocked(org, id)
	if err != nil {
		return ports.FactoryRecord{}, err
	}

	return d.recordLocked(f), nil
}

// FactoryRecords implements ports.DefinitionStore.
func (d *Definitions) FactoryRecords(ctx context.Context) ([]ports.FactoryRecord, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	out := []ports.FactoryRecord{}

	for _, f := range d.factories {
		if f.org == org {
			out = append(out, d.recordLocked(f))
		}
	}

	slices.SortFunc(out, func(a, b ports.FactoryRecord) int {
		return cmp.Or(cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), cmp.Compare(a.ID, b.ID))
	})

	return out, nil
}

// DeleteUnpublished implements ports.DefinitionStore.
func (d *Definitions) DeleteUnpublished(ctx context.Context, id domain.FactoryID, by domain.Actor) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}

	_, err = change(d, func() (struct{}, []ports.DefinitionNotice, error) {
		f, err := d.factoryLocked(org, id)
		if err != nil {
			return struct{}{}, nil, err
		}

		if f.last > 0 {
			return struct{}{}, nil, fmt.Errorf("factory %s was published: %w", id, ports.ErrConflict)
		}

		err = d.auditLocked(ctx, by, domain.AuditFactoryDeleted, domain.AuditTargetFactory, string(id), id,
			map[string]any{auditName: f.name})
		if err != nil {
			return struct{}{}, nil, err
		}

		maps.DeleteFunc(d.versions, func(_ domain.DefinitionVersionID, v *domain.DefinitionVersion) bool {
			return v.FactoryID == id
		})
		delete(d.factories, id)

		if d.items != nil {
			d.items.forgetFactory(id)
		}

		return struct{}{}, []ports.DefinitionNotice{factoryNotice(f)}, nil
	})

	return err
}

// SetPaused implements ports.DefinitionStore.
func (d *Definitions) SetPaused(ctx context.Context, id domain.FactoryID, paused bool, by domain.Actor) (ports.FactoryRecord, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return ports.FactoryRecord{}, err
	}

	return change(d, func() (ports.FactoryRecord, []ports.DefinitionNotice, error) {
		f, err := d.factoryLocked(org, id)
		if err != nil {
			return ports.FactoryRecord{}, nil, err
		}

		if paused == !f.pausedAt.IsZero() {
			return d.recordLocked(f), nil, nil
		}

		action := domain.AuditFactoryResumed
		if paused {
			action = domain.AuditFactoryPaused
		}

		if err := d.auditLocked(ctx, by, action, domain.AuditTargetFactory, string(id), id, nil); err != nil {
			return ports.FactoryRecord{}, nil, err
		}

		f.pausedAt, f.pausedBy = time.Time{}, domain.Actor{}
		if paused {
			f.pausedAt, f.pausedBy = d.stamp(), stored(by)
		}

		return d.recordLocked(f), []ports.DefinitionNotice{d.currentNoticeLocked(f)}, nil
	})
}

// ArchiveFactory implements ports.DefinitionStore. Leases are checked in Items under this
// store's mutex, as the adapter checks them under the factory's row lock.
func (d *Definitions) ArchiveFactory(ctx context.Context, id domain.FactoryID, by domain.Actor) (ports.FactoryRecord, error) {
	return d.setArchived(ctx, id, true, by)
}

// RestoreArchivedFactory implements ports.DefinitionStore.
func (d *Definitions) RestoreArchivedFactory(ctx context.Context, id domain.FactoryID, by domain.Actor) (ports.FactoryRecord, error) {
	return d.setArchived(ctx, id, false, by)
}

func (d *Definitions) setArchived(ctx context.Context, id domain.FactoryID, archive bool, by domain.Actor) (ports.FactoryRecord, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return ports.FactoryRecord{}, err
	}

	return change(d, func() (ports.FactoryRecord, []ports.DefinitionNotice, error) {
		f, err := d.factoryLocked(org, id)
		if err != nil {
			return ports.FactoryRecord{}, nil, err
		}

		if archive == !f.archivedAt.IsZero() {
			return d.recordLocked(f), nil, nil
		}

		action := domain.AuditFactoryRestored

		if archive {
			action = domain.AuditFactoryArchived

			if f.pausedAt.IsZero() {
				return ports.FactoryRecord{}, nil, fmt.Errorf("factory %s: %w", id, ports.ErrFactoryRunning)
			}

			if n := d.items.leased(org, id); n > 0 {
				return ports.FactoryRecord{}, nil, fmt.Errorf("factory %s: %w", id, &ports.FactoryBusyError{Items: n})
			}
		}

		err = d.auditLocked(ctx, by, action, domain.AuditTargetFactory, string(id), id, map[string]any{auditName: f.name})
		if err != nil {
			return ports.FactoryRecord{}, nil, err
		}

		f.archivedAt, f.archivedBy = time.Time{}, domain.Actor{}
		if archive {
			f.archivedAt, f.archivedBy = d.stamp(), stored(by)
		}

		return d.recordLocked(f), []ports.DefinitionNotice{d.currentNoticeLocked(f)}, nil
	})
}

// DeleteArchivedFactory implements ports.DefinitionStore. Leases are checked and the items'
// rows dropped in Items under this store's mutex, as the adapter does both under the factory's
// row lock.
func (d *Definitions) DeleteArchivedFactory(ctx context.Context, id domain.FactoryID, by domain.Actor) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}

	_, err = change(d, func() (struct{}, []ports.DefinitionNotice, error) {
		f, err := d.factoryLocked(org, id)
		if err != nil {
			return struct{}{}, nil, err
		}

		if f.archivedAt.IsZero() {
			return struct{}{}, nil, fmt.Errorf("factory %s: %w", id, ports.ErrFactoryNotArchived)
		}

		if n := d.items.leased(org, id); n > 0 {
			return struct{}{}, nil, fmt.Errorf("factory %s: %w", id, &ports.FactoryBusyError{Items: n})
		}

		err = d.auditLocked(ctx, by, domain.AuditFactoryDeleted, domain.AuditTargetFactory, string(id), id,
			map[string]any{auditName: f.name})
		if err != nil {
			return struct{}{}, nil, err
		}

		maps.DeleteFunc(d.versions, func(_ domain.DefinitionVersionID, v *domain.DefinitionVersion) bool {
			return v.FactoryID == id
		})
		delete(d.factories, id)

		if d.items != nil {
			d.items.deleteFactory(id)
		}

		return struct{}{}, []ports.DefinitionNotice{factoryNotice(f)}, nil
	})

	return err
}

// PutBlobs implements ports.DefinitionStore.
func (d *Definitions) PutBlobs(ctx context.Context, files map[string][]byte) (domain.Manifest, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	for path, content := range files {
		if len(content) > domain.MaxBlobSize {
			return nil, fmt.Errorf("%s has %d bytes, more than %d: %w", path, len(content), domain.MaxBlobSize, domain.ErrInvalid)
		}
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	m := domain.Manifest{}

	for _, path := range slices.Sorted(maps.Keys(files)) {
		h := domain.HashBlob(files[path])
		m[path] = h

		if _, ok := d.blobs[org][h]; ok {
			continue
		}

		key := domain.BlobKey(org, h)

		_, created, err := d.objects.PutIfAbsent(ctx, key, files[path])
		if err != nil {
			return nil, fmt.Errorf("put blob %s: %w", h, err)
		}

		// An object already there that does not hold the blob is replaced, as the adapter does.
		if obj, gerr := d.objects.Get(ctx, key); !created && (gerr != nil || domain.HashBlob(obj.Body) != h) {
			if _, err := d.objects.Put(ctx, key, files[path]); err != nil {
				return nil, fmt.Errorf("repair blob %s: %w", h, err)
			}
		}

		if d.blobs[org] == nil {
			d.blobs[org] = map[domain.BlobHash]int64{}
		}

		d.blobs[org][h] = int64(len(files[path]))
	}

	return m, nil
}

// Blob implements ports.DefinitionStore.
func (d *Definitions) Blob(ctx context.Context, h domain.BlobHash) ([]byte, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	_, ok := d.blobs[org][h]
	d.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("blob %s: %w", h, ports.ErrNotFound)
	}

	obj, err := d.objects.Get(ctx, domain.BlobKey(org, h))
	if err != nil {
		return nil, fmt.Errorf("blob %s: %w", h, err)
	}

	if domain.HashBlob(obj.Body) != h {
		return nil, fmt.Errorf("blob %s: %w", h, ports.ErrBlobCorrupt)
	}

	return obj.Body, nil
}

// BlobSizes implements ports.DefinitionStore.
func (d *Definitions) BlobSizes(ctx context.Context, hashes []domain.BlobHash) (map[domain.BlobHash]int64, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	out := map[domain.BlobHash]int64{}

	for _, h := range hashes {
		if size, ok := d.blobs[org][h]; ok {
			out[h] = size
		}
	}

	return out, nil
}

// Version implements ports.DefinitionStore.
func (d *Definitions) Version(ctx context.Context, id domain.DefinitionVersionID) (domain.DefinitionVersion, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.DefinitionVersion{}, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	v, ok := d.versions[id]
	if !ok || v.OrgID != org {
		return domain.DefinitionVersion{}, fmt.Errorf("definition version %s: %w", id, ports.ErrNotFound)
	}

	return cloneVersion(v), nil
}

// VersionByNumber implements ports.DefinitionStore.
func (d *Definitions) VersionByNumber(ctx context.Context, f domain.FactoryID, n int) (domain.DefinitionVersion, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.DefinitionVersion{}, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	for _, v := range d.publishedLocked(org, f) {
		if v.Number == n {
			return cloneVersion(v), nil
		}
	}

	return domain.DefinitionVersion{}, fmt.Errorf("factory %s version %d: %w", f, n, ports.ErrNotFound)
}

// Draft implements ports.DefinitionStore.
func (d *Definitions) Draft(ctx context.Context, f domain.FactoryID) (domain.DefinitionVersion, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.DefinitionVersion{}, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	row, err := d.factoryLocked(org, f)
	if err != nil {
		return domain.DefinitionVersion{}, err
	}

	if row.draft == "" {
		return domain.DefinitionVersion{}, fmt.Errorf("draft of factory %s: %w", f, ports.ErrNotFound)
	}

	return cloneVersion(d.versions[row.draft]), nil
}

// Versions implements ports.DefinitionStore.
func (d *Definitions) Versions(ctx context.Context, f domain.FactoryID, before, limit int) ([]domain.DefinitionVersion, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	out := []domain.DefinitionVersion{}

	for _, v := range slices.Backward(d.publishedLocked(org, f)) {
		if len(out) == limit {
			break
		}

		if before <= 0 || v.Number < before {
			out = append(out, cloneVersion(v))
		}
	}

	return out, nil
}

// CurrentVersions implements ports.DefinitionStore.
func (d *Definitions) CurrentVersions(ctx context.Context) ([]domain.DefinitionVersion, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	out := []domain.DefinitionVersion{}

	for _, f := range d.factories {
		if f.org == org && f.current != "" {
			out = append(out, cloneVersion(d.versions[f.current]))
		}
	}

	slices.SortFunc(out, func(a, b domain.DefinitionVersion) int { return cmp.Compare(a.FactoryID, b.FactoryID) })

	return out, nil
}

// OpenDraft implements ports.DefinitionStore.
func (d *Definitions) OpenDraft(ctx context.Context, o ports.OpenDraft) (domain.DefinitionVersion, bool, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.DefinitionVersion{}, false, err
	}

	type opened struct {
		v       domain.DefinitionVersion
		created bool
	}

	out, err := change(d, func() (opened, []ports.DefinitionNotice, error) {
		f, err := d.factoryLocked(org, o.Factory)
		if err != nil {
			return opened{}, nil, err
		}

		if f.draft != "" && !o.Replace {
			return opened{v: cloneVersion(d.versions[f.draft])}, nil, nil
		}

		base, err := d.baseLocked(f, o.Base)
		if err != nil {
			return opened{}, nil, err
		}

		var notices []ports.DefinitionNotice

		if f.draft != "" {
			old, err := d.archiveLocked(ctx, f, o.By)
			if err != nil {
				return opened{}, nil, err
			}

			notices = append(notices, draftNotice(old))
		}

		src := domain.VersionSource{Kind: domain.SourceEdit}
		manifest := domain.Manifest{}

		if base != nil {
			src.Version, manifest = base.ID, base.Manifest
		}

		v := d.newDraftLocked(f, src.Version, manifest, src, o.By)

		return opened{cloneVersion(v), true}, append(notices, draftNotice(v)), nil
	})

	return out.v, out.created, err
}

// SaveDraft implements ports.DefinitionStore.
func (d *Definitions) SaveDraft(ctx context.Context, c ports.DraftChange) (domain.DefinitionVersion, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.DefinitionVersion{}, err
	}

	return change(d, func() (domain.DefinitionVersion, []ports.DefinitionNotice, error) {
		v, err := d.openDraftLocked(org, c.Factory, c.ExpectedRevision, ports.ErrNotFound)
		if err != nil {
			return domain.DefinitionVersion{}, nil, err
		}

		next := maps.Clone(c.Set)
		if !c.Replace {
			next = maps.Clone(v.Manifest)
			maps.Copy(next, c.Set)

			for _, path := range c.Remove {
				delete(next, path)
			}
		}

		if next == nil {
			next = domain.Manifest{}
		}

		if err := d.blobsKnownLocked(org, next); err != nil {
			return domain.DefinitionVersion{}, nil, err
		}

		v.Manifest = next
		v.Revision++
		v.UpdatedBy, v.UpdatedAt = stored(c.By), d.stamp()

		return cloneVersion(v), []ports.DefinitionNotice{draftNotice(v)}, nil
	})
}

// DiscardDraft implements ports.DefinitionStore.
func (d *Definitions) DiscardDraft(ctx context.Context, f domain.FactoryID, revision int64, by domain.Actor) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}

	_, err = change(d, func() (struct{}, []ports.DefinitionNotice, error) {
		if _, err := d.openDraftLocked(org, f, revision, ports.ErrNotFound); err != nil {
			return struct{}{}, nil, err
		}

		v, err := d.archiveLocked(ctx, d.factories[f], by)
		if err != nil {
			return struct{}{}, nil, err
		}

		return struct{}{}, []ports.DefinitionNotice{draftNotice(v)}, nil
	})

	return err
}

// Publish implements ports.DefinitionStore.
func (d *Definitions) Publish(ctx context.Context, p ports.Publish) (domain.DefinitionVersion, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.DefinitionVersion{}, err
	}

	return change(d, func() (domain.DefinitionVersion, []ports.DefinitionNotice, error) {
		v, err := d.openDraftLocked(org, p.Factory, p.DraftRevision, ports.ErrConflict)
		if err != nil {
			return domain.DefinitionVersion{}, nil, err
		}

		f := d.factories[p.Factory]
		if f.current != p.ExpectedCurrent {
			return domain.DefinitionVersion{}, nil, fmt.Errorf("factory %s runs %q, not %q: %w",
				f.id, f.current, p.ExpectedCurrent, ports.ErrBaseMoved)
		}

		if err := d.checkLocked(org, f.id, p.Name, v.Manifest); err != nil {
			return domain.DefinitionVersion{}, nil, err
		}

		number := f.last + 1

		moved, err := d.moveItems(org, f.id, p.RemovedStations, p.StationMoves, p.By, number, p.At)
		if err != nil {
			return domain.DefinitionVersion{}, nil, err
		}

		err = d.auditLocked(ctx, p.By, domain.AuditDefinitionPublished, domain.AuditTargetVersion, string(v.ID), f.id,
			map[string]any{"number": number, "message": p.Message, "moved": moved})
		if err != nil {
			return domain.DefinitionVersion{}, nil, err
		}

		now, by := d.stamp(), stored(p.By)
		v.State, v.Number, v.Message = domain.VersionPublished, number, p.Message
		v.PublishedBy, v.PublishedAt, v.UpdatedBy, v.UpdatedAt = by, now, by, now

		if f.current == "" && !p.Start {
			f.pausedAt, f.pausedBy = now, by
		}

		f.current, f.draft, f.last, f.name, f.title = v.ID, "", number, p.Name, p.Title

		return cloneVersion(v), []ports.DefinitionNotice{draftNotice(v), d.currentNoticeLocked(f)}, nil
	})
}

// Restore implements ports.DefinitionStore.
func (d *Definitions) Restore(ctx context.Context, r ports.Restore) (ports.FactoryRecord, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return ports.FactoryRecord{}, err
	}

	return change(d, func() (ports.FactoryRecord, []ports.DefinitionNotice, error) {
		f, err := d.factoryLocked(org, r.Factory)
		if err != nil {
			return ports.FactoryRecord{}, nil, err
		}

		v, ok := d.versions[r.Version]
		if !ok || v.FactoryID != f.id || v.State != domain.VersionPublished {
			return ports.FactoryRecord{}, nil, fmt.Errorf("published version %s of factory %s: %w", r.Version, f.id, ports.ErrNotFound)
		}

		if f.current != r.ExpectedCurrent {
			return ports.FactoryRecord{}, nil, fmt.Errorf("factory %s runs %q, not %q: %w",
				f.id, f.current, r.ExpectedCurrent, ports.ErrBaseMoved)
		}

		if v.ID == f.current {
			return d.recordLocked(f), nil, nil
		}

		if d.nameTakenLocked(org, f.id, r.Name) {
			return ports.FactoryRecord{}, nil, fmt.Errorf("factory name %q: %w", r.Name, ports.ErrNameTaken)
		}

		if _, err := d.moveItems(org, f.id, r.RemovedStations, r.StationMoves, r.By, v.Number, r.At); err != nil {
			return ports.FactoryRecord{}, nil, err
		}

		err = d.auditLocked(ctx, r.By, domain.AuditDefinitionRestored, domain.AuditTargetVersion, string(v.ID), f.id,
			map[string]any{"from": d.versions[f.current].Number, "to": v.Number})
		if err != nil {
			return ports.FactoryRecord{}, nil, err
		}

		f.current, f.name, f.title = v.ID, r.Name, r.Title

		return d.recordLocked(f), []ports.DefinitionNotice{d.currentNoticeLocked(f)}, nil
	})
}

// factoryLocked returns org's factory id, or ErrNotFound.
func (d *Definitions) factoryLocked(org domain.OrgID, id domain.FactoryID) (*factoryRow, error) {
	f, ok := d.factories[id]
	if !ok || f.org != org {
		return nil, fmt.Errorf("factory %s: %w", id, ports.ErrNotFound)
	}

	return f, nil
}

// openDraftLocked returns factory's open draft when it is at revision. A missing factory is
// ErrNotFound, a missing draft noDraft, another revision ErrConflict.
func (d *Definitions) openDraftLocked(org domain.OrgID, factory domain.FactoryID, revision int64, noDraft error) (*domain.DefinitionVersion, error) {
	f, err := d.factoryLocked(org, factory)
	if err != nil {
		return nil, err
	}

	if f.draft == "" {
		return nil, fmt.Errorf("draft of factory %s: %w", factory, noDraft)
	}

	v := d.versions[f.draft]
	if v.Revision != revision {
		return nil, fmt.Errorf("draft of factory %s is at revision %d, not %d: %w", factory, v.Revision, revision, ports.ErrConflict)
	}

	return v, nil
}

// baseLocked returns the published version a new draft of f starts from: id, or the current
// one when id is empty (nil when f has none).
func (d *Definitions) baseLocked(f *factoryRow, id domain.DefinitionVersionID) (*domain.DefinitionVersion, error) {
	if id == "" {
		return d.versions[f.current], nil
	}

	v, ok := d.versions[id]
	if !ok || v.FactoryID != f.id || v.State != domain.VersionPublished {
		return nil, fmt.Errorf("published version %s of factory %s: %w", id, f.id, ports.ErrNotFound)
	}

	return v, nil
}

// archiveLocked archives f's open draft and returns it.
func (d *Definitions) archiveLocked(ctx context.Context, f *factoryRow, by domain.Actor) (*domain.DefinitionVersion, error) {
	v := d.versions[f.draft]

	err := d.auditLocked(ctx, by, domain.AuditDraftDiscarded, domain.AuditTargetVersion, string(v.ID), f.id,
		map[string]any{"revision": v.Revision})
	if err != nil {
		return nil, err
	}

	v.State, v.UpdatedBy, v.UpdatedAt = domain.VersionArchived, stored(by), d.stamp()
	f.draft = ""

	return v, nil
}

func (d *Definitions) newDraftLocked(f *factoryRow, base domain.DefinitionVersionID, m domain.Manifest, src domain.VersionSource, by domain.Actor) *domain.DefinitionVersion {
	now, a := d.stamp(), stored(by)
	v := &domain.DefinitionVersion{
		ID: domain.DefinitionVersionID(d.newIDLocked()), OrgID: f.org, FactoryID: f.id, State: domain.VersionDraft,
		BaseID: base, Revision: 1, Manifest: maps.Clone(m), Source: cloneSource(src),
		CreatedBy: a, UpdatedBy: a, CreatedAt: now, UpdatedAt: now,
	}

	if v.Manifest == nil {
		v.Manifest = domain.Manifest{}
	}

	d.versions[v.ID] = v
	f.draft = v.ID

	return v
}

// checkLocked refuses a name another factory of org has and a manifest naming unknown blobs.
func (d *Definitions) checkLocked(org domain.OrgID, id domain.FactoryID, name string, m domain.Manifest) error {
	if d.nameTakenLocked(org, id, name) {
		return fmt.Errorf("factory name %q: %w", name, ports.ErrNameTaken)
	}

	return d.blobsKnownLocked(org, m)
}

func (d *Definitions) nameTakenLocked(org domain.OrgID, except domain.FactoryID, name string) bool {
	for _, f := range d.factories {
		if f.org == org && f.id != except && strings.EqualFold(f.name, name) {
			return true
		}
	}

	return false
}

func (d *Definitions) blobsKnownLocked(org domain.OrgID, m domain.Manifest) error {
	for _, h := range m.Hashes() {
		if _, ok := d.blobs[org][h]; !ok {
			return fmt.Errorf("blob %s: %w", h, ports.ErrUnknownBlob)
		}
	}

	return nil
}

// publishedLocked returns org's published versions of factory, oldest first.
func (d *Definitions) publishedLocked(org domain.OrgID, factory domain.FactoryID) []*domain.DefinitionVersion {
	var out []*domain.DefinitionVersion

	for _, v := range d.versions {
		if v.OrgID == org && v.FactoryID == factory && v.State == domain.VersionPublished {
			out = append(out, v)
		}
	}

	slices.SortFunc(out, func(a, b *domain.DefinitionVersion) int { return cmp.Compare(a.Number, b.Number) })

	return out
}

// moveItems moves the open items of removed stations as moves say, for version number, at the
// time given (now when zero).
func (d *Definitions) moveItems(org domain.OrgID, factory domain.FactoryID, removed []string, moves map[string]ports.StationMove,
	by domain.Actor, number int, at time.Time,
) (int, error) {
	if d.items == nil || len(removed) == 0 {
		return 0, nil
	}

	if at.IsZero() {
		at = d.stamp()
	}

	return d.items.moveOffStations(org, factory, stationMoves{
		removed: removed, moves: moves, at: at, actor: by.Ref(), reason: fmt.Sprintf("definition v%d", number),
	})
}

func (d *Definitions) auditLocked(ctx context.Context, by domain.Actor, action, kind, target string, factory domain.FactoryID, data map[string]any) error {
	if d.audit == nil {
		return nil
	}

	err := d.audit.Append(ctx, domain.AuditEntry{
		Actor: by, Action: action, TargetKind: kind, TargetID: target, FactoryID: factory, Data: data,
	})
	if err != nil {
		return fmt.Errorf("audit %s: %w", action, err)
	}

	return nil
}

func (d *Definitions) recordLocked(f *factoryRow) ports.FactoryRecord {
	r := ports.FactoryRecord{
		ID: f.id, OrgID: f.org, Name: f.name, Title: f.title, LastVersionNumber: f.last,
		PausedAt: f.pausedAt, PausedBy: f.pausedBy, ArchivedAt: f.archivedAt, ArchivedBy: f.archivedBy,
		CreatedAt: f.createdAt, CreatedBy: f.createdBy,
	}

	if v, ok := d.versions[f.current]; ok {
		r.Current = &ports.VersionRef{ID: v.ID, Number: v.Number, Revision: v.Revision, At: v.PublishedAt, By: v.PublishedBy}
	}

	if v, ok := d.versions[f.draft]; ok {
		r.Draft = &ports.VersionRef{ID: v.ID, Revision: v.Revision, At: v.UpdatedAt, By: v.UpdatedBy}
	}

	return r
}

func (d *Definitions) currentNoticeLocked(f *factoryRow) ports.DefinitionNotice {
	n := ports.DefinitionNotice{OrgID: f.org, FactoryID: f.id, Kind: ports.NoticeCurrent, VersionID: f.current}
	if v, ok := d.versions[f.current]; ok {
		n.Number = v.Number
	}

	return n
}

// stamp is the time of a change as the database keeps it: UTC microseconds.
func (d *Definitions) stamp() time.Time { return d.now().UTC().Truncate(time.Microsecond) }

// newIDLocked returns a deterministic UUIDv7-shaped id.
func (d *Definitions) newIDLocked() string {
	d.ids++

	return fmt.Sprintf("00000000-0000-7000-9000-%012d", d.ids)
}

func factoryNotice(f *factoryRow) ports.DefinitionNotice {
	return ports.DefinitionNotice{OrgID: f.org, FactoryID: f.id, Kind: ports.NoticeFactory}
}

func draftNotice(v *domain.DefinitionVersion) ports.DefinitionNotice {
	return ports.DefinitionNotice{
		OrgID: v.OrgID, FactoryID: v.FactoryID, Kind: ports.NoticeDraft, VersionID: v.ID,
		Number: v.Number, Revision: v.Revision, By: v.UpdatedBy.Ref(),
	}
}

// stored returns a as it reads back from a `_by` column: through its Ref.
func stored(a domain.Actor) domain.Actor {
	back, err := domain.ActorFromRef(a.Ref())
	if err != nil {
		return domain.Actor{}
	}

	return back
}

func cloneVersion(v *domain.DefinitionVersion) domain.DefinitionVersion {
	c := *v
	c.Manifest = maps.Clone(v.Manifest)
	c.Source = cloneSource(v.Source)

	return c
}

func cloneSource(s domain.VersionSource) domain.VersionSource {
	s.Manifest = maps.Clone(s.Manifest)

	return s
}

// stationMoves is what moveOffStations does: which stations go, where their items move, and
// the moved events' time, actor and reason.
type stationMoves struct {
	removed []string
	moves   map[string]ports.StationMove
	at      time.Time
	actor   string
	reason  string
}

// moveOffStations moves org's open items of factory off the removed stations in one step, or,
// when any is in progress or unmapped, moves nothing and returns an
// *ports.ItemsAtRemovedStationError. It returns how many items moved.
func (s *Store) moveOffStations(org domain.OrgID, factory domain.FactoryID, m stationMoves) (int, error) {
	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	var (
		blocked ports.ItemsAtRemovedStationError
		move    []*domain.Item
	)

	for _, it := range s.items {
		if it.OrgID != org || it.FactoryID != factory || !slices.Contains(m.removed, it.Station) || closed(it.State) {
			continue
		}

		at := ports.ItemAt{ID: it.ID, Number: it.Number, Station: it.Station, State: it.State}
		to, mapped := m.moves[it.Station]

		switch {
		case it.State == domain.StateInProgress:
			blocked.InProgress = append(blocked.InProgress, at)
		case !mapped || to.To == "" || slices.Contains(m.removed, to.To):
			blocked.Unmapped = append(blocked.Unmapped, at)
		default:
			move = append(move, it)
		}
	}

	if len(blocked.InProgress)+len(blocked.Unmapped) > 0 {
		byNumber := func(a, b ports.ItemAt) int { return cmp.Compare(a.Number, b.Number) }
		slices.SortFunc(blocked.InProgress, byNumber)
		slices.SortFunc(blocked.Unmapped, byNumber)

		return 0, &blocked
	}

	slices.SortFunc(move, func(a, b *domain.Item) int { return cmp.Compare(a.Number, b.Number) })

	for _, it := range move {
		from, to, before := it.Station, m.moves[it.Station], *it
		it.Station, it.UpdatedAt = to.To, m.at

		if it.State != domain.StateBlocked {
			it.State = to.State
		}

		s.trackSpan(it, &before, m.at)

		s.append(domain.Event{
			FactoryID: factory, ItemID: it.ID, At: m.at, Kind: domain.EventMoved, Station: to.To, Actor: m.actor,
			Data: map[string]any{"from": from, "to": to.To, "reason": m.reason},
		})
	}

	return len(move), nil
}

// leased is how many of org's items of factory hold a lease; 0 without an item store.
func (s *Store) leased(org domain.OrgID, factory domain.FactoryID) int {
	if s == nil {
		return 0
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	n := 0

	for _, l := range s.leases {
		if _, ok := s.visible(l.ItemID, org); ok && l.FactoryID == factory {
			n++
		}
	}

	return n
}

// forgetFactory drops a deleted factory's registration.
func (s *Store) forgetFactory(id domain.FactoryID) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.factories, id)
}

// deleteFactory drops a deleted factory's registration and every row of it: items, leases, runs,
// events and their touch labels, spans, andon pulls and the needs of its items.
func (s *Store) deleteFactory(id domain.FactoryID) {
	s.mu.Lock()
	defer s.mu.Unlock()

	gone := map[domain.ItemID]bool{}

	maps.DeleteFunc(s.items, func(item domain.ItemID, it *domain.Item) bool {
		gone[item] = it.FactoryID == id
		return gone[item]
	})
	maps.DeleteFunc(s.leases, func(item domain.ItemID, _ domain.Lease) bool { return gone[item] })
	maps.DeleteFunc(s.spans, func(item domain.ItemID, _ []domain.Span) bool { return gone[item] })
	maps.DeleteFunc(s.leaseLog, func(item domain.ItemID, _ []domain.LeaseSpan) bool { return gone[item] })
	maps.DeleteFunc(s.runs, func(_ domain.RunID, r *domain.Run) bool { return r.FactoryID == id })
	s.events = slices.DeleteFunc(s.events, func(e domain.Event) bool {
		if e.FactoryID == id {
			delete(s.labels, e.ID)
		}

		return e.FactoryID == id
	})
	s.andon = slices.DeleteFunc(s.andon, func(a andonRecord) bool { return a.pull.FactoryID == id })
	s.edges = slices.DeleteFunc(s.edges, func(e domain.Edge) bool { return gone[e.Item] || gone[e.Needs] })
	maps.DeleteFunc(s.views, func(_ domain.SavedViewID, v savedViewRecord) bool { return v.view.Factory == id })

	if org, ok := s.factories[id]; ok {
		s.purgeJonah(org, id)
	}

	delete(s.factories, id)
	delete(s.numbers, id)
	delete(s.factCaps, id)
}

func closed(st domain.ItemState) bool { return st == domain.StateDone || st == domain.StateCancelled }
