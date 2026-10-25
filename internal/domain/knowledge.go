package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
	"unicode/utf8"
)

// The knowledge base (spec 4 §4.8): one per organization, made of sources. An upload source holds
// files people upload (originals in the bucket at KnowledgeKey); a synced source reads a
// connection whose kind can sync (KindSpec.Sync) into documents; a live source is a connection
// whose own tools search, which nothing indexes. A document's text is split into chunks, each
// indexed for keyword search and, with an embedding model, for nearest-neighbour search.

// KnowledgeSourceID is a knowledge source's id (a UUID).
type KnowledgeSourceID string

// KnowledgeDocumentID is a knowledge document's id (a UUID).
type KnowledgeDocumentID string

// KnowledgeSourceKind is how a source gets its documents.
type KnowledgeSourceKind string

// The kinds of knowledge sources.
const (
	SourceUpload KnowledgeSourceKind = "upload"
	SourceSynced KnowledgeSourceKind = "synced"
	SourceLive   KnowledgeSourceKind = "live"
)

// SyncStatus is a source's sync state: idle between syncs, syncing while a goald holds its lease,
// failed after a sync that stopped (LastError says why).
type SyncStatus string

// The sync states of a source.
const (
	SyncIdle    SyncStatus = "idle"
	SyncSyncing SyncStatus = "syncing"
	SyncFailed  SyncStatus = "failed"
)

// DocumentStatus is where a document is in indexing: pending until the next sync of its source
// indexes it, failed when it could not be (Error says why) and skipped when there is nothing to
// index (no text).
type DocumentStatus string

// The states of a document.
const (
	DocumentPending DocumentStatus = "pending"
	DocumentIndexed DocumentStatus = "indexed"
	DocumentFailed  DocumentStatus = "failed"
	DocumentSkipped DocumentStatus = "skipped"
)

// Limits and defaults of the knowledge base (00054's checks).
const (
	// DefaultSyncEvery and MinSyncEvery bound how often a source syncs.
	DefaultSyncEvery = time.Hour
	MinSyncEvery     = 15 * time.Minute
	// MaxSourceTitle is a source title's length in characters; MaxSourceConfig its config's
	// size as JSON.
	MaxSourceTitle  = 120
	MaxSourceConfig = 8192
	// MaxChunkText is a chunk's text in characters.
	MaxChunkText = 8000
	// MaxExternalID is a document's id at its source in bytes: an entry of its unique index
	// must fit a btree page's third (00056).
	MaxExternalID = 2000
	// MaxKnowledgeError is a stored error message in characters.
	MaxKnowledgeError = 500
	// EmbeddingDimensions is every embedding's size: one embedding space per install.
	EmbeddingDimensions = 1024
	// SearchCandidates is how many chunks each ranking of a search contributes before fusion.
	SearchCandidates = 50
	// RRFK is reciprocal rank fusion's constant: a chunk scores 1/(RRFK+rank) in each ranking.
	RRFK = 60
)

var (
	// ErrConnectionCannotSync refuses a synced source on a connection whose kind cannot sync.
	ErrConnectionCannotSync = fmt.Errorf("the connection's kind cannot sync into the knowledge base: %w", ErrInvalid)
	// ErrNotUploadSource refuses uploading to, or deleting a document of, a source that is not
	// an upload source.
	ErrNotUploadSource = errors.New("not an upload source")
	// ErrUnsupportedDocument refuses a document that is not text, Markdown, HTML, JSON or YAML.
	ErrUnsupportedDocument = errors.New("unsupported document type")
)

// KnowledgeSource is one source of an organization's knowledge base.
type KnowledgeSource struct {
	ID    KnowledgeSourceID
	Name  string
	Title string
	Kind  KnowledgeSourceKind
	// Connection is the connection a synced or live source reads; "" for an upload source.
	Connection string
	// Config is the source's settings for its reader (a repository, a branch and path globs; a
	// Confluence space).
	Config     map[string]string
	SyncEvery  time.Duration
	NextSyncAt *time.Time
	// LeaseUntil is when the lease of the goald syncing it ends.
	LeaseUntil   *time.Time
	Status       SyncStatus
	LastSyncedAt *time.Time
	LastError    string
	// EmbedModel is the model the source's chunks were embedded with at its last full sync; ""
	// when they have no embeddings.
	EmbedModel string
	// Documents is how many documents it has.
	Documents int
	CreatedBy Actor
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewKnowledgeSource is a source to create. SyncEvery 0 is DefaultSyncEvery.
type NewKnowledgeSource struct {
	Name, Title string
	Kind        KnowledgeSourceKind
	Connection  string
	Config      map[string]string
	SyncEvery   time.Duration
}

// Check enforces a new source's rules: its name as a connection's, a title, a kind, a connection
// exactly when it is not an upload source, its config's size and the schedule. Errors wrap
// ErrInvalid.
func (n NewKnowledgeSource) Check() error {
	errs := []error{CheckConnectionName(n.Name), checkSourceTitle(n.Title), checkSourceConfig(n.Config), checkSyncEvery(n.SyncEvery)}

	switch n.Kind {
	case SourceUpload:
		if n.Connection != "" {
			errs = append(errs, fmt.Errorf("an upload source reads no connection: %w", ErrInvalid))
		}
	case SourceSynced, SourceLive:
		if n.Connection == "" {
			errs = append(errs, fmt.Errorf("a %s source reads a connection: %w", n.Kind, ErrInvalid))
		}
	default:
		errs = append(errs, fmt.Errorf("source kind %q is not upload, synced or live: %w", n.Kind, ErrInvalid))
	}

	return errors.Join(errs...)
}

// KnowledgeSourcePatch changes a source: Title and SyncEvery when set, Config when not nil.
type KnowledgeSourcePatch struct {
	Title     *string
	Config    map[string]string
	SyncEvery *time.Duration
}

// Check enforces the rules of what p sets. Errors wrap ErrInvalid.
func (p KnowledgeSourcePatch) Check() error {
	var errs []error

	if p.Title != nil {
		errs = append(errs, checkSourceTitle(*p.Title))
	}

	if p.SyncEvery != nil {
		if *p.SyncEvery == 0 {
			errs = append(errs, fmt.Errorf("a source syncs at least every %s: %w", MinSyncEvery, ErrInvalid))
		}

		errs = append(errs, checkSyncEvery(*p.SyncEvery))
	}

	return errors.Join(append(errs, checkSourceConfig(p.Config))...)
}

func checkSourceTitle(t string) error {
	if n := utf8.RuneCountInString(t); n == 0 || n > MaxSourceTitle {
		return fmt.Errorf("a source title has 1 to %d characters: %w", MaxSourceTitle, ErrInvalid)
	}

	return nil
}

func checkSourceConfig(c map[string]string) error {
	b, err := json.Marshal(c)
	if err != nil || len(b) > MaxSourceConfig {
		return fmt.Errorf("a source's config is at most %d bytes as JSON: %w", MaxSourceConfig, ErrInvalid)
	}

	return nil
}

func checkSyncEvery(d time.Duration) error {
	if d != 0 && d < MinSyncEvery {
		return fmt.Errorf("a source syncs at most every %s, not every %s: %w", MinSyncEvery, d, ErrInvalid)
	}

	return nil
}

// SyncResult is how a source's sync ended: Error "" when it went through, EmbedModel the model
// every chunk now has ("" without one), At when it ended. Due is the source's NextSyncAt when
// it was claimed: the next sync is At plus the source's interval unless someone made it due
// since (Sync now, an upload), which then stands.
type SyncResult struct {
	At, Due    time.Time
	Error      string
	EmbedModel string
}

// SourceClaim is a source claimed for one sync, with its organization (a claim reaches every
// organization's sources) and the lease that sync holds.
type SourceClaim struct {
	Org    OrgID
	Source KnowledgeSource
	Lease  SourceLease
}

// SourceLease is one sync's hold on its source. Its token fences the sync's renewals, seen
// marks, deletions and end: once another claim took the source over (the holder stalled past
// its lease), none of them applies any more.
type SourceLease struct {
	Source KnowledgeSourceID
	Token  string
}

// KnowledgeDocument is one document of a source.
type KnowledgeDocument struct {
	ID     KnowledgeDocumentID
	Source KnowledgeSourceID
	// ExternalID is the document's id at its source: a path, a page id, an upload's SHA-256.
	ExternalID string
	// Version changes whenever the content does: a git blob id, a page version, an upload's
	// SHA-256.
	Version string
	Title   string
	// URL links back to the document at its source; "" for uploads.
	URL   string
	MIME  string
	Bytes int64
	// Blob is an upload's content hash, its object at KnowledgeKey; "" otherwise.
	Blob   BlobHash
	Status DocumentStatus
	Error  string
	// SeenAt is when a sync last listed it.
	SeenAt    time.Time
	IndexedAt *time.Time
	// EmbedModel is the model its chunks were embedded with; "" without embeddings.
	EmbedModel string
	CreatedBy  Actor
}

// KnowledgeKey is the bucket key of an organization's uploaded document with content hash h:
// orgs/<org-id>/knowledge/<sha256>.
func KnowledgeKey(org OrgID, h BlobHash) string { return OrgPrefix(org) + "knowledge/" + string(h) }

// Chunk is one indexed piece of a document's text, in document order. Heading is the path of the
// headings it is under ("Install > Docker"); Embedding is nil without an embedding model.
type Chunk struct {
	Ord       int
	Heading   string
	Text      string
	Embedding []float32
}

// KnowledgeQuery is a search: Text ranked by keywords and, with Embedding (Text's embedding by
// Model), by nearest neighbours among the chunks of Model; only chunks of the named Sources are
// ranked. Limit is how many hits at most.
type KnowledgeQuery struct {
	Text      string
	Sources   []string
	Limit     int
	Embedding []float32
	Model     string
}

// KnowledgeHit is one chunk a search found, best first.
type KnowledgeHit struct {
	Chunk    int64
	Document KnowledgeDocumentID
	Source   string
	Title    string
	URL      string
	Heading  string
	Text     string
	Score    float64
}

// SyncSpec is what a source reader reads: the source and, for a synced source, its connection
// opened with its values.
type SyncSpec struct {
	Source     KnowledgeSource
	Connection *ResolvedConnection
}

// SourceItem is a document as its source lists it. MIME is its type when the listing knows it
// ("": the fetch says).
type SourceItem struct {
	ID, Version, Title, URL, MIME string
}

// SourceDoc is a document's content as its source returns it.
type SourceDoc struct {
	SourceItem

	MIME string
	Body []byte
}

// Ranked is a chunk's score after FuseRanks.
type Ranked struct {
	Chunk int64
	Score float64
}

// FuseRanks fuses rankings of chunks (each best first) by reciprocal rank: a chunk scores the
// sum of 1/(RRFK+rank) over the rankings it is in, ranks from 1. The fused ranking is best
// first, ties by chunk id. The postgres adapter's search computes the same in SQL.
func FuseRanks(rankings ...[]int64) []Ranked {
	scores := map[int64]float64{}

	for _, r := range rankings {
		for i, id := range r {
			scores[id] += 1 / float64(RRFK+i+1)
		}
	}

	out := make([]Ranked, 0, len(scores))
	for id, s := range scores {
		out = append(out, Ranked{Chunk: id, Score: s})
	}

	slices.SortFunc(out, func(a, b Ranked) int {
		switch {
		case a.Score > b.Score:
			return -1
		case a.Score < b.Score:
			return 1
		case a.Chunk < b.Chunk:
			return -1
		case a.Chunk > b.Chunk:
			return 1
		default:
			return 0
		}
	})

	return out
}

// Audit actions of the knowledge base (spec 4 §12).
const (
	AuditKnowledgeSourceCreated    = "knowledge_source.created"
	AuditKnowledgeSourceUpdated    = "knowledge_source.updated"
	AuditKnowledgeSourceDeleted    = "knowledge_source.deleted"
	AuditKnowledgeDocumentUploaded = "knowledge_document.uploaded"
	AuditKnowledgeDocumentDeleted  = "knowledge_document.deleted"
)

// Audit target kinds of the knowledge base.
const (
	AuditTargetKnowledgeSource   = "knowledge_source"
	AuditTargetKnowledgeDocument = "knowledge_document"
)
