package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/byFrederick/goal/internal/adapters/postgres/db"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// OpenDraft implements ports.DefinitionStore. The factory's lock makes concurrent calls take
// turns, so only the first opens a draft.
func (d *Definitions) OpenDraft(ctx context.Context, o ports.OpenDraft) (domain.DefinitionVersion, bool, error) {
	var (
		v       domain.DefinitionVersion
		created bool
	)

	err := d.change(ctx, o.Factory, func(q *db.Queries, f db.LockFactoryRow) error {
		open, err := q.LockDraft(ctx, f.ID)
		hasDraft := err == nil

		if err != nil && !notFound(err) {
			return fmt.Errorf("lock draft: %w", err)
		}

		if hasDraft && !o.Replace {
			v, err = versionFrom(open)

			return err
		}

		base, err := draftBase(ctx, q, f, o.Base)
		if err != nil {
			return err
		}

		if hasDraft {
			if err := archive(ctx, q, open, o.By); err != nil {
				return err
			}
		}

		src := domain.VersionSource{Kind: domain.SourceEdit}
		manifest := domain.Manifest{}

		if base != nil {
			src.Version, manifest = domain.DefinitionVersionID(base.ID.String()), nil

			if err := json.Unmarshal(base.Manifest, &manifest); err != nil {
				return fmt.Errorf("base %s: manifest: %w", base.ID, err)
			}
		}

		p, err := newDraft(manifest, src, o.By)
		if err != nil {
			return err
		}

		p.FactoryID = f.ID
		if base != nil {
			p.BaseVersionID = &base.ID
		}

		v, err = insertVersion(ctx, q, p)
		created = err == nil

		return err
	})
	if err != nil {
		return domain.DefinitionVersion{}, false, fmt.Errorf("open draft: %w", err)
	}

	return v, created, nil
}

// draftBase returns the published version a new draft of f starts from: id, or f's current
// version when id is empty (nil when f has none). Anything else is ErrNotFound.
func draftBase(ctx context.Context, q *db.Queries, f db.LockFactoryRow, id domain.DefinitionVersionID) (*db.DefinitionVersion, error) {
	if id != "" {
		return publishedVersion(ctx, q, f, id)
	}

	if f.CurrentVersionID == nil {
		return nil, nil //nolint:nilnil // no base is not an error
	}

	return publishedVersion(ctx, q, f, versionIDOf(f.CurrentVersionID))
}

// publishedVersion returns f's published version id, or ErrNotFound.
func publishedVersion(ctx context.Context, q *db.Queries, f db.LockFactoryRow, id domain.DefinitionVersionID) (*db.DefinitionVersion, error) {
	missing := fmt.Errorf("published version %q of factory %s: %w", id, f.ID, ports.ErrNotFound)

	vid, err := uuid.Parse(string(id))
	if err != nil {
		return nil, missing
	}

	v, err := q.PublishedVersion(ctx, db.PublishedVersionParams{FactoryID: f.ID, ID: vid})
	if notFound(err) {
		return nil, missing
	}

	if err != nil {
		return nil, err
	}

	return &v, nil
}

// SaveDraft implements ports.DefinitionStore.
func (d *Definitions) SaveDraft(ctx context.Context, c ports.DraftChange) (domain.DefinitionVersion, error) {
	var v domain.DefinitionVersion

	err := d.change(ctx, c.Factory, func(q *db.Queries, f db.LockFactoryRow) error {
		open, err := lockDraft(ctx, q, f, c.ExpectedRevision, ports.ErrNotFound)
		if err != nil {
			return err
		}

		next := maps.Clone(c.Set)

		if !c.Replace {
			if err := json.Unmarshal(open.Manifest, &next); err != nil {
				return fmt.Errorf("draft %s: manifest: %w", open.ID, err)
			}

			maps.Copy(next, c.Set)

			for _, path := range c.Remove {
				delete(next, path)
			}
		}

		if err := knownBlobs(ctx, q, next); err != nil {
			return err
		}

		raw, err := manifestJSON(next)
		if err != nil {
			return err
		}

		row, err := q.SaveDraft(ctx, db.SaveDraftParams{ID: open.ID, Manifest: raw, UpdatedBy: c.By.Ref()})
		if err != nil {
			return fmt.Errorf("save draft: %w", err)
		}

		v, err = versionFrom(row)

		return err
	})
	if err != nil {
		return domain.DefinitionVersion{}, fmt.Errorf("save draft: %w", err)
	}

	return v, nil
}

// DiscardDraft implements ports.DefinitionStore.
func (d *Definitions) DiscardDraft(ctx context.Context, factory domain.FactoryID, revision int64, by domain.Actor) error {
	err := d.change(ctx, factory, func(q *db.Queries, f db.LockFactoryRow) error {
		open, err := lockDraft(ctx, q, f, revision, ports.ErrNotFound)
		if err != nil {
			return err
		}

		return archive(ctx, q, open, by)
	})
	if err != nil {
		return fmt.Errorf("discard draft: %w", err)
	}

	return nil
}

// Publish implements ports.DefinitionStore. The version's number is the factory's
// last_version_number plus one, under the factory's lock, so numbers have no gaps.
func (d *Definitions) Publish(ctx context.Context, p ports.Publish) (domain.DefinitionVersion, error) {
	var v domain.DefinitionVersion

	err := d.change(ctx, p.Factory, func(q *db.Queries, f db.LockFactoryRow) error {
		open, err := lockDraft(ctx, q, f, p.DraftRevision, ports.ErrConflict)
		if err != nil {
			return err
		}

		if current := versionIDOf(f.CurrentVersionID); current != p.ExpectedCurrent {
			return fmt.Errorf("factory %s runs %q, not %q: %w", p.Factory, current, p.ExpectedCurrent, ports.ErrBaseMoved)
		}

		if err := nameFree(ctx, q, f.ID, p.Name); err != nil {
			return err
		}

		var manifest domain.Manifest
		if err := json.Unmarshal(open.Manifest, &manifest); err != nil {
			return fmt.Errorf("draft %s: manifest: %w", open.ID, err)
		}

		if err := knownBlobs(ctx, q, manifest); err != nil {
			return err
		}

		number := int(f.LastVersionNumber) + 1

		row, err := q.PublishDraft(ctx, db.PublishDraftParams{
			ID: open.ID, Number: int32ptr(number), Message: p.Message, PublishedBy: p.By.Ref(),
		})
		if err != nil {
			return versionRuleError(err)
		}

		if v, err = versionFrom(row); err != nil {
			return err
		}

		moved, err := moveOffStations(ctx, q, v, p.RemovedStations, p.StationMoves, p.By)
		if err != nil {
			return err
		}

		err = pointFactory(ctx, q, db.PointFactoryParams{
			ID: f.ID, VersionID: &row.ID, Number: int32(number), Name: p.Name, Title: optional(p.Title), //nolint:gosec // a version number
			Pause: f.CurrentVersionID == nil && !p.Start, PausedBy: p.By.Ref(),
		})
		if err != nil {
			return err
		}

		return appendAudit(ctx, q, versionEntry(p.By, domain.AuditDefinitionPublished, v,
			map[string]any{"number": number, "message": p.Message, "moved": moved}))
	})
	if err != nil {
		return domain.DefinitionVersion{}, fmt.Errorf("publish: %w", err)
	}

	return v, nil
}

// Restore implements ports.DefinitionStore.
func (d *Definitions) Restore(ctx context.Context, r ports.Restore) (ports.FactoryRecord, error) {
	var rec ports.FactoryRecord

	err := d.change(ctx, r.Factory, func(q *db.Queries, f db.LockFactoryRow) error {
		target, err := publishedVersion(ctx, q, f, r.Version)
		if err != nil {
			return err
		}

		if current := versionIDOf(f.CurrentVersionID); current != r.ExpectedCurrent {
			return fmt.Errorf("factory %s runs %q, not %q: %w", r.Factory, current, r.ExpectedCurrent, ports.ErrBaseMoved)
		}

		if f.CurrentVersionID != nil && *f.CurrentVersionID != target.ID {
			if err := d.restore(ctx, q, f, target, r); err != nil {
				return err
			}
		}

		rec, err = factoryRecord(ctx, q, f.ID)

		return err
	})
	if err != nil {
		return ports.FactoryRecord{}, fmt.Errorf("restore: %w", err)
	}

	return rec, nil
}

func (d *Definitions) restore(ctx context.Context, q *db.Queries, f db.LockFactoryRow, target *db.DefinitionVersion, r ports.Restore) error {
	if err := nameFree(ctx, q, f.ID, r.Name); err != nil {
		return err
	}

	v, err := versionFrom(*target)
	if err != nil {
		return err
	}

	from, err := q.GetVersion(ctx, *f.CurrentVersionID)
	if err != nil {
		return fmt.Errorf("current version: %w", err)
	}

	if _, err := moveOffStations(ctx, q, v, r.RemovedStations, r.StationMoves, r.By); err != nil {
		return err
	}

	err = pointFactory(ctx, q, db.PointFactoryParams{
		ID: f.ID, VersionID: &target.ID, Number: deref32(target.Number), Name: r.Name, Title: optional(r.Title),
	})
	if err != nil {
		return err
	}

	return appendAudit(ctx, q, versionEntry(r.By, domain.AuditDefinitionRestored, v,
		map[string]any{"from": deref32(from.Number), "to": v.Number}))
}

// ImportLegacy implements ports.DefinitionStore. A factory row of another organization makes
// the insert fail on its primary key: ErrConflict.
func (d *Definitions) ImportLegacy(ctx context.Context, i ports.LegacyImport) (domain.DefinitionVersion, bool, error) {
	id, err := uuid.Parse(string(i.ID))
	if err != nil {
		return domain.DefinitionVersion{}, false, orgFirst(ctx, fmt.Errorf("import legacy: id %q: %w", i.ID, domain.ErrInvalid))
	}

	p, err := importedVersion(i, id)
	if err != nil {
		return domain.DefinitionVersion{}, false, fmt.Errorf("import legacy %s: %w", i.ID, err)
	}

	var v domain.DefinitionVersion

	err = d.store.inTx(ctx, func(q *db.Queries) error {
		f, err := q.LockFactory(ctx, id)
		exists := err == nil

		switch {
		case err != nil && !notFound(err):
			return fmt.Errorf("lock factory: %w", err)
		case exists:
			if has, err := q.HasVersions(ctx, id); err != nil || has {
				return err
			}
		}

		v, err = importLegacy(ctx, q, i, p, exists, f)

		return err
	})
	if err != nil {
		return domain.DefinitionVersion{}, false, fmt.Errorf("import legacy %s: %w", i.ID, err)
	}

	return v, v.ID != "", nil
}

// LegacyImportPending implements ports.DefinitionStore: the organization has a row in
// legacy_import_pending (migration 00039).
func (d *Definitions) LegacyImportPending(ctx context.Context) (bool, error) {
	var pending bool

	err := d.store.inTx(ctx, func(q *db.Queries) error {
		var err error

		pending, err = q.LegacyImportPending(ctx)

		return err
	})
	if err != nil {
		return false, fmt.Errorf("read the legacy import: %w", err)
	}

	return pending, nil
}

// EndLegacyImport implements ports.DefinitionStore.
func (d *Definitions) EndLegacyImport(ctx context.Context) error {
	if err := d.store.inTx(ctx, func(q *db.Queries) error { return q.EndLegacyImport(ctx) }); err != nil {
		return fmt.Errorf("end the legacy import: %w", err)
	}

	return nil
}

// importLegacy imports i, as p, into a factory that has no version yet.
func importLegacy(ctx context.Context, q *db.Queries, i ports.LegacyImport, p db.InsertVersionParams, exists bool,
	f db.LockFactoryRow,
) (domain.DefinitionVersion, error) {
	if err := nameFree(ctx, q, p.FactoryID, i.Name); err != nil {
		return domain.DefinitionVersion{}, err
	}

	if err := knownBlobs(ctx, q, i.Manifest); err != nil {
		return domain.DefinitionVersion{}, err
	}

	if err := importFactory(ctx, q, i, p.FactoryID, exists, f); err != nil {
		return domain.DefinitionVersion{}, err
	}

	v, err := insertVersion(ctx, q, p)
	if err != nil || !i.Publish {
		return v, err
	}

	err = pointFactory(ctx, q, db.PointFactoryParams{ID: p.FactoryID, VersionID: &p.ID, Number: 1, Name: i.Name, Title: optional(i.Title)})
	if err != nil {
		return domain.DefinitionVersion{}, err
	}

	return v, appendAudit(ctx, q, versionEntry(i.By, domain.AuditDefinitionPublished, v,
		map[string]any{"number": 1, "message": i.Message, "moved": 0}))
}

// importedVersion is the insert of an import: version 1, or a draft when it does not publish.
func importedVersion(i ports.LegacyImport, id uuid.UUID) (db.InsertVersionParams, error) {
	p, err := newDraft(i.Manifest, i.Source, i.By)
	if err != nil {
		return db.InsertVersionParams{}, err
	}

	p.FactoryID = id

	if i.Publish {
		by := i.By.Ref()
		p.State, p.Number, p.Message, p.PublishedBy = string(domain.VersionPublished), int32ptr(1), i.Message, &by
	}

	return p, nil
}

// importFactory creates the imported factory's row, or gives the row goald registered before
// versions existed the import's name and title (it keeps no creator: nobody recorded one).
func importFactory(ctx context.Context, q *db.Queries, i ports.LegacyImport, id uuid.UUID, exists bool, f db.LockFactoryRow) error {
	if exists {
		// A draft import points nowhere yet; PointFactory with no version only renames.
		return pointFactory(ctx, q, db.PointFactoryParams{
			ID: id, VersionID: f.CurrentVersionID, Number: f.LastVersionNumber, Name: i.Name, Title: optional(i.Title),
		})
	}

	by := i.By.Ref()

	if err := q.InsertFactory(ctx, db.InsertFactoryParams{ID: id, Name: i.Name, Title: optional(i.Title), CreatedBy: &by}); err != nil {
		return factoryRuleError(err, i.ID, i.Name)
	}

	return appendAudit(ctx, q, factoryEntry(i.By, domain.AuditFactoryCreated, i.ID,
		map[string]any{auditName: i.Name, "source": string(i.Source.Kind)}))
}

// lockDraft locks f's open draft and checks it is at revision. No draft is noDraft, another
// revision ErrConflict.
func lockDraft(ctx context.Context, q *db.Queries, f db.LockFactoryRow, revision int64, noDraft error) (db.DefinitionVersion, error) {
	open, err := q.LockDraft(ctx, f.ID)
	if notFound(err) {
		return db.DefinitionVersion{}, fmt.Errorf("draft of factory %s: %w", f.ID, noDraft)
	}

	if err != nil {
		return db.DefinitionVersion{}, fmt.Errorf("lock draft: %w", err)
	}

	if open.Revision != revision {
		return db.DefinitionVersion{}, fmt.Errorf("draft of factory %s is at revision %d, not %d: %w",
			f.ID, open.Revision, revision, ports.ErrConflict)
	}

	return open, nil
}

// archive discards an open draft, recording who did.
func archive(ctx context.Context, q *db.Queries, open db.DefinitionVersion, by domain.Actor) error {
	row, err := q.ArchiveDraft(ctx, db.ArchiveDraftParams{ID: open.ID, UpdatedBy: by.Ref()})
	if err != nil {
		return fmt.Errorf("archive draft: %w", err)
	}

	v, err := versionFrom(row)
	if err != nil {
		return err
	}

	return appendAudit(ctx, q, versionEntry(by, domain.AuditDraftDiscarded, v, map[string]any{"revision": v.Revision}))
}

func nameFree(ctx context.Context, q *db.Queries, id uuid.UUID, name string) error {
	taken, err := q.NameTaken(ctx, db.NameTakenParams{ID: id, Name: name})
	if err != nil {
		return fmt.Errorf("check name: %w", err)
	}

	if taken {
		return fmt.Errorf("factory name %q: %w", name, ports.ErrNameTaken)
	}

	return nil
}

func pointFactory(ctx context.Context, q *db.Queries, p db.PointFactoryParams) error {
	if err := q.PointFactory(ctx, p); err != nil {
		return factoryRuleError(err, domain.FactoryID(p.ID.String()), p.Name)
	}

	return nil
}

// moveOffStations moves the open items of v's factory off the removed stations as moves say,
// with a moved event each ({from, to, reason: "definition v<number>"}), and returns how many
// moved. When any is in progress or unmapped it moves nothing and returns an
// *ports.ItemsAtRemovedStationError.
func moveOffStations(ctx context.Context, q *db.Queries, v domain.DefinitionVersion, removed []string,
	moves map[string]ports.StationMove, by domain.Actor,
) (int, error) {
	if len(removed) == 0 {
		return 0, nil
	}

	factory, err := uuid.Parse(string(v.FactoryID))
	if err != nil {
		return 0, fmt.Errorf("factory %q: %w", v.FactoryID, err)
	}

	open, err := q.OpenItemsAt(ctx, db.OpenItemsAtParams{FactoryID: factory, Stations: removed})
	if err != nil {
		return 0, fmt.Errorf("open items at removed stations: %w", err)
	}

	if err := refuseUnmoved(open, removed, moves); err != nil {
		return 0, err
	}

	reason := fmt.Sprintf("definition v%d", v.Number)

	var events []domain.Event

	for _, from := range slices.Compact(slices.Sorted(slices.Values(removed))) {
		to, ok := moves[from]
		if !ok {
			continue
		}

		rows, err := q.MoveOffStation(ctx, db.MoveOffStationParams{
			FactoryID: factory, FromStation: from, ToStation: to.To, State: string(to.State),
		})
		if err != nil {
			return 0, fmt.Errorf("move items off %s: %w", from, err)
		}

		for _, r := range rows {
			events = append(events, domain.Event{
				FactoryID: v.FactoryID, ItemID: domain.ItemID(r.ID.String()), At: r.UpdatedAt, Kind: domain.EventMoved,
				Station: to.To, Actor: by.Ref(), Data: map[string]any{"from": from, "to": to.To, "reason": reason},
			})
		}
	}

	return len(events), appendEvents(ctx, q, events)
}

// newDraft is the insert of a draft at revision 1 with a new id; the caller sets FactoryID.
func newDraft(m domain.Manifest, src domain.VersionSource, by domain.Actor) (db.InsertVersionParams, error) {
	id, err := newID()
	if err != nil {
		return db.InsertVersionParams{}, err
	}

	manifest, err := manifestJSON(m)
	if err != nil {
		return db.InsertVersionParams{}, err
	}

	source, err := json.Marshal(sourceDoc{
		Kind: src.Kind, Template: src.Template, Factory: src.Factory, Version: string(src.Version),
		Manifest: src.Manifest, LegacyVersions: src.LegacyVersions,
	})
	if err != nil {
		return db.InsertVersionParams{}, fmt.Errorf("source: %w", err)
	}

	return db.InsertVersionParams{
		ID: id, State: string(domain.VersionDraft), Manifest: manifest, Source: source, CreatedBy: by.Ref(),
	}, nil
}

func insertVersion(ctx context.Context, q *db.Queries, p db.InsertVersionParams) (domain.DefinitionVersion, error) {
	row, err := q.InsertVersion(ctx, p)
	if err != nil {
		return domain.DefinitionVersion{}, versionRuleError(err)
	}

	return versionFrom(row)
}

// versionRuleError maps a version row a CHECK refused (a message over 200 characters) to
// domain.ErrInvalid.
func versionRuleError(err error) error {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == checkViolation {
		return fmt.Errorf("definition version: %s: %w", pgErr.Message, domain.ErrInvalid)
	}

	return fmt.Errorf("definition version: %w", err)
}

// manifestJSON encodes m as a JSON object, {} when it is empty.
func manifestJSON(m domain.Manifest) ([]byte, error) {
	if m == nil {
		m = domain.Manifest{}
	}

	raw, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}

	return raw, nil
}

func versionIDOf(id *uuid.UUID) domain.DefinitionVersionID {
	if id == nil {
		return ""
	}

	return domain.DefinitionVersionID(id.String())
}

func deref32(n *int32) int32 {
	if n == nil {
		return 0
	}

	return *n
}

// refuseUnmoved returns an *ports.ItemsAtRemovedStationError listing the open items at removed
// stations that cannot move: those in progress and those no move maps to a kept station.
func refuseUnmoved(open []db.OpenItemsAtRow, removed []string, moves map[string]ports.StationMove) error {
	var refused ports.ItemsAtRemovedStationError

	for _, r := range open {
		to, mapped := moves[r.Station]

		switch {
		case domain.ItemState(r.State) == domain.StateInProgress:
			refused.InProgress = append(refused.InProgress, itemAt(r))
		case !mapped || to.To == "" || slices.Contains(removed, to.To):
			refused.Unmapped = append(refused.Unmapped, itemAt(r))
		}
	}

	if len(refused.InProgress)+len(refused.Unmapped) == 0 {
		return nil
	}

	slices.SortFunc(refused.InProgress, byNumber)
	slices.SortFunc(refused.Unmapped, byNumber)

	return &refused
}
