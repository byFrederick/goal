package ports

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/byFrederick/goal/internal/domain"
)

// Errors of DefinitionStore, besides ErrNotFound and ErrConflict.
var (
	// ErrNameTaken means another factory of the organization has the name, compared without
	// case.
	ErrNameTaken = errors.New("name taken")
	// ErrBaseMoved means the factory's current version is no longer the one the caller expected:
	// someone published or restored meanwhile.
	ErrBaseMoved = errors.New("current version moved")
	// ErrUnknownBlob means a manifest names a hash the organization has not stored (PutBlobs).
	ErrUnknownBlob = errors.New("unknown blob")
	// ErrBlobCorrupt means a blob's content no longer matches its hash.
	ErrBlobCorrupt = errors.New("blob does not match its hash")
	// ErrItemsAtRemovedStation means a publish or restore would drop stations that open items
	// still sit at; the error is an *ItemsAtRemovedStationError.
	ErrItemsAtRemovedStation = errors.New("items at a removed station")
	// ErrFactoryRunning means a factory must be paused first: archiving a running factory.
	ErrFactoryRunning = errors.New("factory running")
	// ErrFactoryBusy means one of the factory's items holds a lease: a run is still working it.
	// The error is a *FactoryBusyError.
	ErrFactoryBusy = errors.New("factory busy")
)

// DefinitionStore keeps factory definitions: file content as immutable blobs in the bucket
// (domain.BlobKey, written once), versions (drafts and published manifests) and each factory's
// current-version pointer in PostgreSQL. Every method acts in the organization of ctx and fails
// with an error wrapping domain.ErrNoOrg without one; other organizations' factories, versions
// and blobs do not exist for it (ErrNotFound). Every change is one transaction that appends its
// audit entry (the domain.Audit* actions) and is announced by DefinitionNotices once committed.
type DefinitionStore interface {
	// CreateFactory creates a factory and its first draft (revision 1, no base) from n. It
	// returns ErrNameTaken when the organization has a factory of that name, ErrConflict when
	// the id is taken (in any organization) and ErrUnknownBlob when the manifest names a hash
	// the organization lacks. Audit: factory.created.
	CreateFactory(ctx context.Context, n NewFactory) (FactoryRecord, domain.DefinitionVersion, error)
	// FactoryRecord returns a factory, or ErrNotFound.
	FactoryRecord(ctx context.Context, id domain.FactoryID) (FactoryRecord, error)
	// FactoryRecords returns the organization's factories by name (without case), then id.
	FactoryRecords(ctx context.Context) ([]FactoryRecord, error)
	// DeleteUnpublished deletes a factory that was never published, with its drafts. It returns
	// ErrConflict once a version was published. Audit: factory.deleted.
	DeleteUnpublished(ctx context.Context, id domain.FactoryID, by domain.Actor) error
	// SetPaused pauses or resumes a factory: a paused factory takes no work. Setting the state
	// it already has changes nothing. Audit: factory.paused or factory.resumed.
	SetPaused(ctx context.Context, id domain.FactoryID, paused bool, by domain.Actor) (FactoryRecord, error)
	// ArchiveFactory retires a factory: it stays readable, with its items where they are, and is
	// still listed (FactoryRecord.Archived). Only a paused factory none of whose items holds a
	// lease is archived: ErrFactoryRunning otherwise, or a *FactoryBusyError. Archiving an archived
	// factory changes nothing. Audit: factory.archived.
	ArchiveFactory(ctx context.Context, id domain.FactoryID, by domain.Actor) (FactoryRecord, error)
	// RestoreArchivedFactory undoes ArchiveFactory; the factory stays paused, its items where
	// they were. Restoring a factory that is not archived changes nothing. Audit:
	// factory.restored.
	RestoreArchivedFactory(ctx context.Context, id domain.FactoryID, by domain.Actor) (FactoryRecord, error)

	// PutBlobs stores files by their content hash (only blobs the organization lacks are
	// uploaded) and returns their manifest. A file over domain.MaxBlobSize is refused with an
	// error wrapping domain.ErrInvalid, and then nothing is stored.
	PutBlobs(ctx context.Context, files map[string][]byte) (domain.Manifest, error)
	// Blob returns a blob's content, verified against its hash (ErrBlobCorrupt otherwise). It
	// returns ErrNotFound when the organization has no such blob.
	Blob(ctx context.Context, h domain.BlobHash) ([]byte, error)
	// BlobSizes returns the size in bytes of each of hashes the organization has; unknown
	// hashes are left out.
	BlobSizes(ctx context.Context, hashes []domain.BlobHash) (map[domain.BlobHash]int64, error)

	// Version returns a version in any state, or ErrNotFound.
	Version(ctx context.Context, id domain.DefinitionVersionID) (domain.DefinitionVersion, error)
	// VersionByNumber returns a factory's published version n, or ErrNotFound.
	VersionByNumber(ctx context.Context, f domain.FactoryID, n int) (domain.DefinitionVersion, error)
	// Draft returns a factory's open draft, or ErrNotFound when it has none.
	Draft(ctx context.Context, f domain.FactoryID) (domain.DefinitionVersion, error)
	// Versions returns up to limit of a factory's published versions numbered below before
	// (every one when before is 0), newest first.
	Versions(ctx context.Context, f domain.FactoryID, before, limit int) ([]domain.DefinitionVersion, error)
	// CurrentVersions returns the current version of every factory of the organization that
	// has one, by factory id.
	CurrentVersions(ctx context.Context) ([]domain.DefinitionVersion, error)

	// OpenDraft opens a factory's draft on a published version (d.Base, or the current one;
	// an empty draft when the factory has none). When a draft is open already it is returned
	// with created false and nothing changes, unless d.Replace archives it first (audited as
	// definition.draft_discarded). A base that is not a published version of the factory is
	// ErrNotFound. Concurrent calls open one draft: the others get it with created false.
	OpenDraft(ctx context.Context, d OpenDraft) (draft domain.DefinitionVersion, created bool, err error)
	// SaveDraft applies c to the open draft and adds one to its revision, compare and set on
	// c.ExpectedRevision: a stale revision is ErrConflict and changes nothing. It returns
	// ErrNotFound when the factory has no open draft and ErrUnknownBlob for a hash the
	// organization lacks.
	SaveDraft(ctx context.Context, c DraftChange) (domain.DefinitionVersion, error)
	// DiscardDraft archives the open draft at revision (ErrConflict when it moved, ErrNotFound
	// when there is none). Audit: definition.draft_discarded.
	DiscardDraft(ctx context.Context, f domain.FactoryID, revision int64, by domain.Actor) error
	// Publish makes the open draft the factory's next version and points the factory at it, in
	// one transaction (see Publish). Audit: definition.published.
	Publish(ctx context.Context, p Publish) (domain.DefinitionVersion, error)
	// Restore points the factory at an older published version (see Restore); numbers are not
	// reused and an open draft stays open. Audit: definition.restored.
	Restore(ctx context.Context, r Restore) (FactoryRecord, error)
}

// NewFactory is a factory to create with its first draft.
type NewFactory struct {
	// ID is the metadata.id of the draft's factory.yaml.
	ID          domain.FactoryID
	Name, Title string
	Manifest    domain.Manifest
	Source      domain.VersionSource
	By          domain.Actor
}

// FactoryRecord is a factory as the definition store keeps it.
type FactoryRecord struct {
	ID    domain.FactoryID
	OrgID domain.OrgID
	// Name and Title are those of the current version, or of the draft before the first
	// publish.
	Name, Title string
	// Current is the version that runs; nil before the first publish.
	Current *VersionRef
	// Draft is the open draft; nil when there is none.
	Draft *VersionRef
	// LastVersionNumber is the highest number published so far: the open draft is "Draft of
	// v(LastVersionNumber+1)". After a restore it is above Current's number.
	LastVersionNumber int
	// PausedAt is when the factory was paused, and PausedBy by whom; zero while it runs.
	PausedAt time.Time
	PausedBy domain.Actor
	// ArchivedAt is when the factory was archived, and ArchivedBy by whom; zero unless it is.
	ArchivedAt time.Time
	ArchivedBy domain.Actor
	CreatedAt  time.Time
	// CreatedBy is the zero Actor for a factory registered before versions existed.
	CreatedBy domain.Actor
}

// Paused reports whether the factory takes no work.
func (r FactoryRecord) Paused() bool { return !r.PausedAt.IsZero() }

// Archived reports whether the factory is retired (DefinitionStore.ArchiveFactory).
func (r FactoryRecord) Archived() bool { return !r.ArchivedAt.IsZero() }

// VersionRef names a version in listings, without its manifest.
type VersionRef struct {
	ID       domain.DefinitionVersionID
	Number   int
	Revision int64
	// At and By are when and by whom a published version was published, and a draft last saved.
	At time.Time
	By domain.Actor
}

// OpenDraft asks for a factory's draft.
type OpenDraft struct {
	Factory domain.FactoryID
	// Base is the published version the draft starts from; empty means the current one.
	Base domain.DefinitionVersionID
	// Replace archives an open draft first (goal factory push --replace-draft).
	Replace bool
	By      domain.Actor
}

// DraftChange is one save of a draft's file tree.
type DraftChange struct {
	Factory          domain.FactoryID
	ExpectedRevision int64
	// Set adds or replaces paths; Remove then deletes paths (a path the draft lacks is ignored).
	Set    domain.Manifest
	Remove []string
	// Replace makes Set the whole tree (push, import); Remove is then ignored.
	Replace bool
	By      domain.Actor
}

// Publish publishes a factory's open draft. In one transaction the store checks the guards,
// numbers the draft LastVersionNumber+1, records the publisher, message, name and title, points
// the factory at the version and moves the open items of removed stations. It returns
// ErrConflict when the factory has no open draft or the draft is no longer at DraftRevision,
// ErrBaseMoved when the current version is not ExpectedCurrent, ErrUnknownBlob, ErrNameTaken,
// and an *ItemsAtRemovedStationError; nothing changes then.
type Publish struct {
	Factory       domain.FactoryID
	DraftRevision int64
	// ExpectedCurrent is the current version the caller validated against; empty for a first
	// publish.
	ExpectedCurrent domain.DefinitionVersionID
	// Name and Title come from the validated factory.yaml and are copied to the factory.
	Name, Title string
	Message     string
	// RemovedStations are the station ids the new version drops, and StationMoves send the
	// open items at them to a station it keeps.
	RemovedStations []string
	StationMoves    map[string]StationMove
	// Start, on a first publish only, starts the factory; false creates it paused.
	Start bool
	By    domain.Actor
	// At is when the moved items move (their updated_at, and so their spans and moved events),
	// from the caller's clock like every other change of an item; zero means now.
	At time.Time
}

// Restore points a factory back at one of its published versions. It returns ErrNotFound when
// Version is not a published version of Factory, ErrBaseMoved when the current version is not
// ExpectedCurrent, ErrNameTaken and an *ItemsAtRemovedStationError; nothing changes then.
// Restoring the current version changes nothing.
type Restore struct {
	Factory         domain.FactoryID
	Version         domain.DefinitionVersionID
	ExpectedCurrent domain.DefinitionVersionID
	// Name and Title are the restored version's, copied to the factory.
	Name, Title string
	// RemovedStations and StationMoves are as for Publish: the restored version's line may
	// lack stations the current one has.
	RemovedStations []string
	StationMoves    map[string]StationMove
	By              domain.Actor
	// At is as for Publish.
	At time.Time
}

// StationMove sends the open items of a removed station to a station the new version keeps.
// Every moved item gets a moved event with data {from, to, reason}.
type StationMove struct {
	To string
	// State is the state items arrive in at To (domain.Station.Arrival); blocked items stay
	// blocked.
	State domain.ItemState
}

// ItemsAtRemovedStationError lists the items that keep a publish or restore from dropping
// stations: items in progress there, which must finish or be stopped first, and open items
// no StationMove maps.
type ItemsAtRemovedStationError struct {
	InProgress []ItemAt
	Unmapped   []ItemAt
}

// ItemAt is an item at a station.
type ItemAt struct {
	ID      domain.ItemID
	Number  int64
	Station string
	State   domain.ItemState
}

func (e *ItemsAtRemovedStationError) Error() string {
	return fmt.Sprintf("%d items in progress and %d open items unmapped at removed stations",
		len(e.InProgress), len(e.Unmapped))
}

// Unwrap makes the error match ErrItemsAtRemovedStation.
func (e *ItemsAtRemovedStationError) Unwrap() error { return ErrItemsAtRemovedStation }

// FactoryBusyError refuses to archive a factory whose items hold leases: Items is how many,
// counted under the same lock as the refusal.
type FactoryBusyError struct {
	Items int
}

func (e *FactoryBusyError) Error() string {
	return fmt.Sprintf("%d items are being worked", e.Items)
}

// Unwrap makes the error match ErrFactoryBusy.
func (e *FactoryBusyError) Unwrap() error { return ErrFactoryBusy }

// DefinitionNotices delivers a notice of every committed definition change, from any goald
// instance: PostgreSQL LISTEN/NOTIFY on goal_definitions, every organization's.
type DefinitionNotices interface {
	// Listen calls deliver for every change committed while it listens, in commit order, until
	// ctx ends; it returns nil then. Each time it starts listening (the first time, and again
	// after a lost connection, which misses the changes committed meanwhile) it first delivers
	// a notice with Resync set. deliver must not block for long.
	Listen(ctx context.Context, deliver func(DefinitionNotice)) error
}

// Definition notice kinds.
const (
	// NoticeFactory: a factory was created or deleted.
	NoticeFactory = "factory"
	// NoticeCurrent: the factory's current version moved (publish, restore) or
	// it was paused, resumed, archived or restored from the archive. VersionID and Number are the current version's.
	NoticeCurrent = "current"
	// NoticeDraft: a version row changed: a draft opened, saved, discarded or published.
	// VersionID, Number (once published), Revision and By are that version's.
	NoticeDraft = "draft"
	// NoticeOrg: the organization was deleted (soft delete); FactoryID is empty. Its factories
	// stop at once: every goald drops their lines.
	NoticeOrg = "org"
)

// DefinitionNotice is one committed definition change. A Resync notice carries nothing else:
// listeners must re-read what they show. Notices carry no content.
type DefinitionNotice struct {
	Resync    bool
	OrgID     domain.OrgID
	FactoryID domain.FactoryID
	Kind      string
	VersionID domain.DefinitionVersionID
	Number    int
	Revision  int64
	// By is the actor's Ref() on draft notices.
	By string
}
