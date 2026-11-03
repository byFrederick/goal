package ports

import (
	"context"
	"iter"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// Knowledge is an organization's knowledge base (spec 4 §4.8, §5.6) for the organization in ctx:
// its sources, their snapshots, current files and read counts, and the files uploaded to upload
// sources (ADR 0038). Every change people make is audited in its transaction. Syncs are
// claimed with leases, so any goald may sync any source and one that dies leaves it to the
// next once its lease ends.
type Knowledge interface {
	// Sources are the organization's sources, by name.
	Sources(ctx context.Context) ([]domain.KnowledgeSource, error)
	// Source is the source name. ErrNotFound when there is none.
	Source(ctx context.Context, name string) (domain.KnowledgeSource, error)
	// CreateSource creates a source, due to sync at once (knowledge_source.created). Its rules
	// are NewKnowledgeSource.Check's (domain.ErrInvalid); ErrNameTaken for a name in use,
	// ErrNotFound for an unknown connection or secret, domain.ErrConnectionCannotSync for a synced
	// source whose connection's kind cannot sync. A source refers to its secret by id: renaming
	// the secret keeps it, and the secret cannot be deleted while a source reads with it
	// (OrgSecrets.Delete: ErrConflict naming the sources).
	CreateSource(ctx context.Context, s domain.NewKnowledgeSource, by domain.Actor) (domain.KnowledgeSource, error)
	// UpdateSource changes a source's title, config, schedule or secret (knowledge_source.updated),
	// with KnowledgeSourcePatch.Check's and CheckOn's rules (domain.ErrInvalid). ErrNotFound when
	// there is no such source or secret.
	UpdateSource(ctx context.Context, name string, p domain.KnowledgeSourcePatch, by domain.Actor) (domain.KnowledgeSource, error)
	// DeleteSource deletes a source with its documents and chunks (knowledge_source.deleted) and
	// calls drop, before the deletion commits, with the uploads' content hashes no document of
	// the organization holds any more (DropBlobs). ErrNotFound when there is none.
	DeleteSource(ctx context.Context, name string, by domain.Actor, drop DropBlobs) error
	// ConvertSource moves a synced source that reads with a secret over to kind, a repository or
	// Confluence source (ADR 0038 decision 16; KnowledgeSource.CheckConvert): it clears its
	// connection, drops its reader from its config and keeps the other keys and the secret, makes
	// it due at once and audits knowledge_source.updated with the new kind. domain.ErrInvalid for
	// any other source or kind, ErrNotFound when there is none.
	ConvertSource(ctx context.Context, name string, kind domain.KnowledgeSourceKind, by domain.Actor) (domain.KnowledgeSource, error)
	// SyncNow makes a source due at once. ErrNotFound when there is none; a live source never
	// syncs (domain.ErrInvalid).
	SyncNow(ctx context.Context, name string) error

	// ClaimSource leases from now to until, as syncing, the source due first in any live
	// organization (ctx needs none), so every organization's sources take their turn in one
	// order; nil when none is: a source not live whose next sync is due and whose lease ended. The
	// other lease methods take the claim's lease, under its organization in ctx.
	ClaimSource(ctx context.Context, now, until time.Time) (*domain.SourceClaim, error)
	// RenewSource extends a lease held at now until until. ErrLeaseLost when it ended, another
	// claim took the source, or the source is not syncing any more.
	RenewSource(ctx context.Context, l domain.SourceLease, now, until time.Time) error
	// FinishSync ends a sync: idle or failed with r.Error, its lease released and the next sync
	// due r.At plus the source's interval, unless it was made due since its claim (r.Due); a
	// failure adds one to the source's SyncFailures, a success resets them. ErrLeaseLost, changing nothing, once another
	// claim took the source.
	FinishSync(ctx context.Context, l domain.SourceLease, r domain.SyncResult) error

	// PutSnapshot records a sync's snapshot s of the lease's source with files, every file it
	// holds, and makes it the source's current snapshot, in one transaction: files replace the
	// current files, and the snapshots before stay until retention deletes them. It returns s as
	// stored, with its zip's key (domain.KnowledgeSnapshotKey). Its rules are
	// KnowledgeSnapshot.Check's (domain.ErrInvalid), and s.Source is the lease's; ErrLeaseLost,
	// changing nothing, once another claim took the source.
	PutSnapshot(ctx context.Context, l domain.SourceLease, s domain.KnowledgeSnapshot, files []domain.KnowledgeFile) (domain.KnowledgeSnapshot, error)
	// Snapshots are the source name's snapshots, newest first. ErrNotFound for an unknown source.
	Snapshots(ctx context.Context, source string) ([]domain.KnowledgeSnapshot, error)
	// Files are one page of a source's current files (KnowledgeFileQuery): in path order, after
	// q.After, at most q.Limit up to domain.MaxFilePage, and with q.Prefix those whose path, or
	// title in any case, starts with it. ErrNotFound for an unknown source.
	Files(ctx context.Context, q domain.KnowledgeFileQuery) ([]domain.KnowledgeFile, error)
	// AddReads adds each count to the reads and searches of its source, path, factory and day
	// (domain.KnowledgeDay), one row for each, in one transaction. Its rules are
	// KnowledgeFileReads.Check's (domain.ErrInvalid); ErrNotFound, adding nothing, for an unknown
	// source.
	AddReads(ctx context.Context, reads []domain.KnowledgeFileReads) error
	// FileReads are the source name's counts of the days from since (domain.KnowledgeDay) on, by
	// day, path and factory. ErrNotFound for an unknown source.
	FileReads(ctx context.Context, source string, since time.Time) ([]domain.KnowledgeFileReads, error)

	// AddUpload adds an uploaded file to the upload source named source (knowledge_document.uploaded);
	// the caller then stores its original and makes the source due. u.Blob is its content's hash.
	// A file of the same name in the source is replaced, calling drop with the content it held when
	// no other upload holds it, and uploading the same content under the same name again returns
	// the file it is. It waits for a deletion dropping that content (DropBlobs). ErrNotFound for an unknown source, domain.ErrNotUploadSource for one of
	// another kind.
	AddUpload(ctx context.Context, source string, u domain.KnowledgeUpload, by domain.Actor, drop DropBlobs) (domain.KnowledgeUpload, error)
	// DeleteUpload deletes the file named name of an upload source (knowledge_document.deleted) and
	// calls drop, before the deletion commits, with its content hash when no upload of the
	// organization holds it any more (DropBlobs). ErrNotFound when there is none;
	// domain.ErrNotUploadSource for a source of another kind.
	DeleteUpload(ctx context.Context, source, name string, by domain.Actor, drop DropBlobs) error
	// Uploads are the files of an upload source, by name. ErrNotFound for an unknown source,
	// domain.ErrNotUploadSource for one of another kind.
	Uploads(ctx context.Context, source string) ([]domain.KnowledgeUpload, error)
}

// DropBlobs deletes the objects of uploads' contents no document holds any more. A deletion calls
// it before it commits, holding those contents against AddUpload, so an upload of the same
// content meanwhile never ends up with its object gone; its error undoes the deletion. nil drops
// nothing.
type DropBlobs func(ctx context.Context, unheld []domain.BlobHash) error

// SourceReader reads the items of a source, one reader per kind (ADR 0038 decision 2): an upload
// source's from the bucket, a repository's from its code host, Confluence's pages, and until the
// cut-over a synced source's from its connection.
type SourceReader interface {
	// List yields every item of the source with its version, then stops; an error ends it.
	List(ctx context.Context, s domain.SyncSpec) iter.Seq2[domain.SourceItem, error]
	// Fetch returns one listed item's content.
	Fetch(ctx context.Context, s domain.SyncSpec, id string) (domain.SourceDoc, error)
}

// ArchiveReader is a SourceReader that can also read many listed items of one listing from a
// single download, such as a repository's archive (ADR 0038 decision 2). A sync that changes
// many items uses it instead of fetching them one by one.
type ArchiveReader interface {
	SourceReader
	// Archive yields, one at a time, the content of each of items whose archived content is the
	// version listed. Items it never yields are the caller's to fetch one by one. An error ends it
	// and means the archive failed as a whole.
	Archive(ctx context.Context, s domain.SyncSpec, items []domain.SourceItem) iter.Seq2[domain.SourceDoc, error]
}

// Converter turns an item that is neither Markdown nor text into Markdown, so every file of a
// source is one an agent can read (ADR 0038 decision 2): HTML built in, PDF and Office files
// through a conversion service. name is the item's file name, mediaType its media type.
type Converter interface {
	// Convert returns body as Markdown; domain.ErrUnsupportedDocument for a media type it does not
	// convert.
	Convert(ctx context.Context, name, mediaType string, body []byte) ([]byte, error)
}
