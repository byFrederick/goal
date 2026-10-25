package ports

import (
	"context"
	"iter"
	"time"

	"github.com/byFrederick/goal/internal/domain"
)

// Knowledge is an organization's knowledge base (spec 4 §4.8, §5.6) for the organization in ctx:
// its sources, their documents and the documents' chunks, searched by keywords and nearest
// neighbours in one ranking. Every change people make is audited in its transaction. Syncs are
// claimed with leases, so any goald may sync any source and one that dies leaves it to the
// next once its lease ends.
type Knowledge interface {
	// Sources are the organization's sources, by name.
	Sources(ctx context.Context) ([]domain.KnowledgeSource, error)
	// Source is the source name. ErrNotFound when there is none.
	Source(ctx context.Context, name string) (domain.KnowledgeSource, error)
	// CreateSource creates a source, due to sync at once (knowledge_source.created). Its rules
	// are NewKnowledgeSource.Check's (domain.ErrInvalid); ErrNameTaken for a name in use,
	// ErrNotFound for an unknown connection, domain.ErrConnectionCannotSync for a synced source
	// whose connection's kind cannot sync.
	CreateSource(ctx context.Context, s domain.NewKnowledgeSource, by domain.Actor) (domain.KnowledgeSource, error)
	// UpdateSource changes a source's title, config or schedule (knowledge_source.updated).
	// ErrNotFound when there is none.
	UpdateSource(ctx context.Context, name string, p domain.KnowledgeSourcePatch, by domain.Actor) (domain.KnowledgeSource, error)
	// DeleteSource deletes a source with its documents and chunks (knowledge_source.deleted) and
	// calls drop, before the deletion commits, with the uploads' content hashes no document of
	// the organization holds any more (DropBlobs). ErrNotFound when there is none.
	DeleteSource(ctx context.Context, name string, by domain.Actor, drop DropBlobs) error
	// SyncNow makes a source due at once. ErrNotFound when there is none; a live source never
	// syncs (domain.ErrInvalid).
	SyncNow(ctx context.Context, name string) error

	// ClaimSource leases from now to until, as syncing, the source due first in any live
	// organization (ctx needs none), so every organization's sources take their turn in one
	// order; nil when none is: a source not live whose next sync is due or, when model is not "",
	// whose chunks are of another model (unless its last sync failed), and whose lease ended. The
	// other lease methods take the claim's lease, under its organization in ctx.
	ClaimSource(ctx context.Context, now, until time.Time, model string) (*domain.SourceClaim, error)
	// RenewSource extends a lease held at now until until. ErrLeaseLost when it ended, another
	// claim took the source, or the source is not syncing any more.
	RenewSource(ctx context.Context, l domain.SourceLease, now, until time.Time) error
	// FinishSync ends a sync: idle or failed with r.Error, its lease released and the next sync
	// due r.At plus the source's interval, unless it was made due since its claim (r.Due); on
	// success its model becomes r.EmbedModel. ErrLeaseLost, changing nothing, once another
	// claim took the source.
	FinishSync(ctx context.Context, l domain.SourceLease, r domain.SyncResult) error

	// Versions are the versions of a source's documents by external id, those pending excluded:
	// what a sync need not fetch again.
	Versions(ctx context.Context, source domain.KnowledgeSourceID) (map[string]string, error)
	// PutDocument creates or replaces the document of d.Source with d.ExternalID and replaces
	// its chunks, in one transaction. A document's seen time never goes back.
	PutDocument(ctx context.Context, d domain.KnowledgeDocument, chunks []domain.Chunk) error
	// MarkSeen sets the seen time of the lease's source's documents with the given external ids
	// to at, unless they were seen later. ErrLeaseLost, changing nothing, once another claim
	// took the source.
	MarkSeen(ctx context.Context, l domain.SourceLease, externalIDs []string, at time.Time) error
	// DeleteUnseen deletes the lease's source's documents last seen before (a sync's start) and
	// returns how many. ErrLeaseLost, deleting nothing, once another claim took the source.
	DeleteUnseen(ctx context.Context, l domain.SourceLease, before time.Time) (int, error)

	// AddUpload adds an uploaded document to the upload source named source, pending
	// (knowledge_document.uploaded); the caller then stores its content and makes the source
	// due. d.Blob is its content's hash, which is its external id and version too: uploading the
	// same content again returns the document it is. It waits for a deletion dropping that
	// content (DropBlobs). ErrNotFound for an unknown source, domain.ErrNotUploadSource for one of
	// another kind.
	AddUpload(ctx context.Context, source string, d domain.KnowledgeDocument, by domain.Actor) (domain.KnowledgeDocument, error)
	// DeleteDocument deletes an uploaded document (knowledge_document.deleted) and calls drop,
	// before the deletion commits, with its content hash when no document of the organization
	// holds it any more (DropBlobs). ErrNotFound when there is none; domain.ErrNotUploadSource for
	// a document of another kind.
	DeleteDocument(ctx context.Context, id domain.KnowledgeDocumentID, by domain.Actor, drop DropBlobs) error
	// Documents are one page of a source's documents in id order, after the id after ("" from
	// the first). ErrNotFound for an unknown source.
	Documents(ctx context.Context, source string, after domain.KnowledgeDocumentID, limit int) ([]domain.KnowledgeDocument, error)
	// Document is a document with its indexed text, its chunks joined in order. ErrNotFound when
	// there is none.
	Document(ctx context.Context, id domain.KnowledgeDocumentID) (domain.KnowledgeDocument, string, error)

	// Search ranks the chunks of q.Sources only, by reciprocal rank fusion of their keyword
	// ranking and, with q.Embedding, their nearest neighbours among the chunks of q.Model
	// (domain.FuseRanks); there is no unfiltered variant.
	Search(ctx context.Context, q domain.KnowledgeQuery) ([]domain.KnowledgeHit, error)
}

// DropBlobs deletes the objects of uploads' contents no document holds any more. A deletion calls
// it before it commits, holding those contents against AddUpload, so an upload of the same
// content meanwhile never ends up with its object gone; its error undoes the deletion. nil drops
// nothing.
type DropBlobs func(ctx context.Context, unheld []domain.BlobHash) error

// Embedder turns texts into embeddings of domain.EmbeddingDimensions, one per text in order.
// Model names the embedding space; chunks are searched only against queries of their model.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Model() string
}

// SourceReader reads the documents of a source: an upload source's from the bucket, a synced
// source's from its connection.
type SourceReader interface {
	// List yields every document of the source with its version, then stops; an error ends it.
	List(ctx context.Context, s domain.SyncSpec) iter.Seq2[domain.SourceItem, error]
	// Fetch returns a listed document's content.
	Fetch(ctx context.Context, s domain.SyncSpec, id string) (domain.SourceDoc, error)
}
