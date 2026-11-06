package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
	"unicode/utf8"
)

// The knowledge base (spec 4 §4.8): one per organization, made of sources. An upload source holds
// files people upload (originals in the bucket at KnowledgeKey); a synced source reads a
// connection whose kind can sync (KindSpec.Sync) into snapshots of its files; a live source is a
// connection whose own tools search, which nothing indexes. Knowledge is files (ADR 0038): agents
// read them as files, with no chunks or embeddings.

// KnowledgeSourceID is a knowledge source's id (a UUID).
type KnowledgeSourceID string

// KnowledgeUploadID is an uploaded file's id (a UUID).
type KnowledgeUploadID string

// KnowledgeSourceKind is how a source gets its documents.
type KnowledgeSourceKind string

// The kinds of knowledge sources. ADR 0038 makes them upload, repository and confluence; synced
// and live stay until the cut-over converts their sources (decision 16).
const (
	SourceUpload     KnowledgeSourceKind = "upload"
	SourceRepository KnowledgeSourceKind = "repository"
	SourceConfluence KnowledgeSourceKind = "confluence"
	SourceSynced     KnowledgeSourceKind = "synced"
	SourceLive       KnowledgeSourceKind = "live"
)

// readsSecret reports whether a source of kind k may read with a secret.
func (k KnowledgeSourceKind) readsSecret() bool {
	return k == SourceSynced || k == SourceRepository || k == SourceConfluence
}

// SyncStatus is a source's sync state: idle between syncs, syncing while a goald holds its lease,
// failed after a sync that stopped (LastError says why).
type SyncStatus string

// The sync states of a source.
const (
	SyncIdle    SyncStatus = "idle"
	SyncSyncing SyncStatus = "syncing"
	SyncFailed  SyncStatus = "failed"
)

// Limits and defaults of the knowledge base (00054's checks).
const (
	// DefaultSyncEvery and MinSyncEvery bound how often a source syncs; SyncOnDemand is a
	// source that syncs only when someone presses Sync (ADR 0038 decision 1).
	SyncOnDemand     time.Duration = 0
	DefaultSyncEvery               = time.Hour
	MinSyncEvery                   = 15 * time.Minute
	// MaxSourceTitle is a source title's length in characters; MaxSourceConfig its config's
	// size as JSON.
	MaxSourceTitle  = 120
	MaxSourceConfig = 8192
	// MaxKnowledgeError is a stored error message in characters.
	MaxKnowledgeError = 500
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
	// Connection is the connection a live source reads, or a synced source not yet on a secret;
	// "" otherwise.
	Connection string
	// Secret is the secret a synced source reads with (ADR 0037 decision 14), opened at each
	// sync; "" for an upload or live source and a synced one still on its connection.
	Secret string
	// Config is the source's settings for its reader (a repository, a branch and path globs; a
	// Confluence space). A synced source on a secret names its reader too (SourceReaderKey).
	Config map[string]string
	// ReadWhen is the line that tells agents when to open the source (ADR 0038 decision 1).
	ReadWhen string
	// CurrentSnapshot is the snapshot runs read; "" before its first.
	CurrentSnapshot KnowledgeSnapshotID
	// SyncEvery is how often the source syncs; SyncOnDemand (0) only when someone presses Sync.
	SyncEvery time.Duration
	// NextSyncAt is when the source is due; nil for a live source, and for an on-demand one
	// between syncs.
	NextSyncAt *time.Time
	// LeaseUntil is when the lease of the goald syncing it ends.
	LeaseUntil   *time.Time
	Status       SyncStatus
	LastSyncedAt *time.Time
	LastError    string
	// SyncFailures is how many syncs failed in a row; a successful one resets it, and three show
	// the source in Needs you (ADR 0038 decision 3).
	SyncFailures int
	CreatedBy    Actor
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// NewKnowledgeSource is a source to create. SyncEvery nil is DefaultSyncEvery and SyncOnDemand
// syncs only when someone presses Sync; a source that syncs is due once when it is created.
type NewKnowledgeSource struct {
	Name, Title string
	Kind        KnowledgeSourceKind
	Connection  string
	Secret      string
	Config      map[string]string
	ReadWhen    string
	SyncEvery   *time.Duration
}

// Every is how often the new source syncs: SyncEvery, or DefaultSyncEvery when nil.
func (n NewKnowledgeSource) Every() time.Duration {
	if n.SyncEvery == nil {
		return DefaultSyncEvery
	}

	return *n.SyncEvery
}

// SourceReaderKey is the config key naming the reader of a synced source on a secret (gitdocs,
// confluence): a source on a connection takes it from the connection's kind.
const SourceReaderKey = "reader"

// Check enforces a new source's rules: its name as a connection's, a title, a kind; an upload
// source reads neither a connection nor a secret, a repository no connection and a secret or
// none, Confluence a secret, a synced one exactly one of a connection and a secret (a secret
// with its reader in its config; a connection until moving over gives it a secret), a live one
// a connection; its config's size, its read-when line and the schedule. Errors wrap ErrInvalid.
func (n NewKnowledgeSource) Check() error {
	errs := []error{
		CheckConnectionName(n.Name), checkSourceTitle(n.Title), checkSourceConfig(n.Config), CheckReadWhen(n.ReadWhen),
	}

	if n.SyncEvery != nil {
		errs = append(errs, checkSyncEvery(*n.SyncEvery))
	}

	if n.Secret != "" {
		errs = append(errs, CheckSecretName(n.Secret))
	}

	return errors.Join(append(errs, n.checkReads())...)
}

// checkReads enforces what a new source of each kind reads: a connection, a secret or neither.
func (n NewKnowledgeSource) checkReads() error {
	conn, secret := n.Connection != "", n.Secret != ""

	rule, ok := map[KnowledgeSourceKind]struct {
		holds bool
		says  string
	}{
		SourceUpload:     {!conn && !secret, "an upload source reads no connection or secret"},
		SourceRepository: {!conn, "a repository source reads with a secret, not a connection"},
		SourceConfluence: {!conn && secret, "a Confluence source reads with a secret and no connection"},
		SourceSynced:     {conn != secret, "a synced source reads with a secret"},
		SourceLive:       {conn && !secret, "a live source reads a connection and no secret"},
	}[n.Kind]

	switch {
	case !ok:
		return fmt.Errorf("source kind %q is not upload, repository, confluence, synced or live: %w", n.Kind, ErrInvalid)
	case !rule.holds:
		return fmt.Errorf("%s: %w", rule.says, ErrInvalid)
	case n.Kind == SourceSynced && secret:
		return checkSourceReader(n.Config)
	}

	return nil
}

// KnowledgeSourcePatch changes a source: Title, ReadWhen, SyncEvery and Secret when set, Config
// when not nil. SyncEvery SyncOnDemand makes the source wait for Sync; an interval makes an
// on-demand source due at once. Secret is a synced, repository or Confluence source's only: it moves a synced
// source on a connection over to the secret.
type KnowledgeSourcePatch struct {
	Title     *string
	ReadWhen  *string
	Config    map[string]string
	SyncEvery *time.Duration
	Secret    *string
}

// Check enforces the rules of what p sets. Errors wrap ErrInvalid.
func (p KnowledgeSourcePatch) Check() error {
	var errs []error

	if p.Title != nil {
		errs = append(errs, checkSourceTitle(*p.Title))
	}

	if p.SyncEvery != nil {
		errs = append(errs, checkSyncEvery(*p.SyncEvery))
	}

	if p.Secret != nil {
		errs = append(errs, CheckSecretName(*p.Secret))
	}

	if p.ReadWhen != nil {
		errs = append(errs, CheckReadWhen(*p.ReadWhen))
	}

	return errors.Join(append(errs, checkSourceConfig(p.Config))...)
}

// CheckOn enforces p's rules on src, the source it changes: a secret only on a synced, repository
// or Confluence source, and a synced source on a secret keeps naming its reader. Errors wrap
// ErrInvalid.
func (p KnowledgeSourcePatch) CheckOn(src KnowledgeSource) error {
	if p.Secret != nil && !src.Kind.readsSecret() {
		return fmt.Errorf("only a synced, repository or Confluence source reads with a secret, and %s is %s: %w", src.Name, src.Kind, ErrInvalid)
	}

	if src.Kind != SourceSynced || (p.Secret == nil && src.Secret == "") {
		return nil
	}

	cfg := src.Config
	if p.Config != nil {
		cfg = p.Config
	}

	return checkSourceReader(cfg)
}

// ConvertsTo is the kind a synced source on a secret becomes when moving over (ADR 0038 decision
// 16): repository for one gitdocs reads, confluence for one confluence reads. It errs, wrapping
// ErrInvalid and saying why, for any other source.
func (s KnowledgeSource) ConvertsTo() (KnowledgeSourceKind, error) {
	if s.Kind != SourceSynced || s.Secret == "" {
		return "", fmt.Errorf("only a synced source that reads with a secret converts, and %s is %s reading %s: %w",
			s.Name, s.Kind, s.reads(), ErrInvalid)
	}

	switch r := s.Config[SourceReaderKey]; r {
	case "gitdocs":
		return SourceRepository, nil
	case "confluence":
		return SourceConfluence, nil
	default:
		return "", fmt.Errorf("source %s's reader %q is neither gitdocs nor confluence: %w", s.Name, r, ErrInvalid)
	}
}

// CheckConvert enforces the rules of converting s into a source of kind to: s converts
// (ConvertsTo), and to is the kind it converts into. Errors wrap ErrInvalid.
func (s KnowledgeSource) CheckConvert(to KnowledgeSourceKind) error {
	want, err := s.ConvertsTo()
	if err != nil {
		return err
	}

	if to != want {
		return fmt.Errorf("source %s converts into a %s source, not %q: %w", s.Name, want, to, ErrInvalid)
	}

	return nil
}

// ConvertedConfig is s's config as its converted source keeps it: without SourceReaderKey, which
// the kind names now.
func (s KnowledgeSource) ConvertedConfig() map[string]string {
	out := make(map[string]string, len(s.Config))

	for k, v := range s.Config {
		if k != SourceReaderKey {
			out[k] = v
		}
	}

	return out
}

// reads names what s reads with, for a refusal.
func (s KnowledgeSource) reads() string {
	switch {
	case s.Secret != "":
		return "secret " + s.Secret
	case s.Connection != "":
		return "connection " + s.Connection
	default:
		return "nothing"
	}
}

// checkSourceReader refuses a synced source on a secret whose config names no reader.
func checkSourceReader(c map[string]string) error {
	if strings.TrimSpace(c[SourceReaderKey]) == "" {
		return fmt.Errorf("a synced source on a secret names its reader in its config (%s: gitdocs or confluence): %w", SourceReaderKey, ErrInvalid)
	}

	return nil
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

// checkSyncEvery allows SyncOnDemand or an interval of at least MinSyncEvery.
func checkSyncEvery(d time.Duration) error {
	if d != SyncOnDemand && d < MinSyncEvery {
		return fmt.Errorf("a source syncs at most every %s, not every %s: %w", MinSyncEvery, d, ErrInvalid)
	}

	return nil
}

// SyncResult is how a source's sync ended: Error "" when it went through, At when it ended. Due is the source's NextSyncAt when
// it was claimed: the next sync is At plus the source's interval, or none for an on-demand
// source, unless someone made it due since (Sync now, an upload), which then stands.
type SyncResult struct {
	At, Due time.Time
	Error   string
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

// KnowledgeUpload is one uploaded file of an upload source (ADR 0038 decision 2): its name, and its
// original, which stays in the bucket at KnowledgeKey under its content hash.
type KnowledgeUpload struct {
	ID     KnowledgeUploadID
	Source KnowledgeSourceID
	// Name is the file's path in the source, unique in it.
	Name  string
	MIME  string
	Bytes int64
	// Blob is the original's content hash, its object at KnowledgeKey and the file's version.
	Blob      BlobHash
	CreatedBy Actor
	CreatedAt time.Time
}

// KnowledgeKey is the bucket key of an organization's uploaded document with content hash h:
// orgs/<org-id>/knowledge/<sha256>.
func KnowledgeKey(org OrgID, h BlobHash) string { return OrgPrefix(org) + "knowledge/" + string(h) }

// SyncSpec is what a source reader reads: the source and, for a synced source, its secret
// opened with its value, or the connection it still reads opened with its values.
type SyncSpec struct {
	Source     KnowledgeSource
	Secret     *ResolvedSecret
	Connection *ResolvedConnection
}

// SourceItem is a document as its source lists it. MIME is its type when the listing knows it
// ("": the fetch says). Folder is the folder its file goes in when its source names one, such as
// a Confluence page's place in its page tree (ENG/runbooks); "": the folder's top.
type SourceItem struct {
	ID, Version, Title, URL, MIME, Folder string
}

// SourceDoc is a document's content as its source returns it.
type SourceDoc struct {
	SourceItem

	Body []byte
}

// Audit actions of the knowledge base (spec 4 §12).
const (
	AuditKnowledgeSourceCreated = "knowledge_source.created"
	AuditKnowledgeSourceUpdated = "knowledge_source.updated"
	AuditKnowledgeSourceDeleted = "knowledge_source.deleted"
	// The audit actions of uploads keep the names of the documents they replaced, so entries
	// written before ADR 0038 stay readable.
	AuditKnowledgeDocumentUploaded = "knowledge_document.uploaded"
	AuditKnowledgeDocumentDeleted  = "knowledge_document.deleted"
)

// Audit target kinds of the knowledge base.
const (
	AuditTargetKnowledgeSource   = "knowledge_source"
	AuditTargetKnowledgeDocument = "knowledge_document"
)

// The document types the knowledge base indexes (spec 4 §4.8; PDF and Office are out of phase 4).
const (
	MediaText     = "text/plain"
	MediaMarkdown = "text/markdown"
	MediaHTML     = "text/html"
	MediaJSON     = "application/json"
	MediaYAML     = "application/yaml"
)

// The types a conversion service turns into Markdown (ADR 0038 decision 2); a goald without one
// leaves such a file out of the snapshot and lists it.
const (
	MediaPDF  = "application/pdf"
	MediaDOCX = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	MediaXLSX = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	MediaPPTX = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	MediaODT  = "application/vnd.oasis.opendocument.text"
	MediaODS  = "application/vnd.oasis.opendocument.spreadsheet"
)

// DefaultMaxUpload is the largest document the knowledge base takes, uploaded or fetched from a
// source (GOAL_UPLOAD_MAX_BYTES).
const DefaultMaxUpload = 25 << 20

// documentExtensions are the file name extensions of each indexed type.
var documentExtensions = map[string]string{
	".txt": MediaText, ".text": MediaText, ".md": MediaMarkdown, ".markdown": MediaMarkdown,
	".html": MediaHTML, ".htm": MediaHTML, ".json": MediaJSON, ".yaml": MediaYAML, ".yml": MediaYAML,
	".pdf": MediaPDF, ".docx": MediaDOCX, ".xlsx": MediaXLSX, ".pptx": MediaPPTX, ".odt": MediaODT, ".ods": MediaODS,
}

// DocumentType is the indexed type of a file named name: by its extension, else by what its
// content looks like (text or HTML). ErrUnsupportedDocument for anything else.
func DocumentType(name string, body []byte) (string, error) {
	if t, ok := documentExtensions[strings.ToLower(path.Ext(name))]; ok {
		return t, nil
	}

	if path.Ext(name) == "" && utf8.Valid(body) && !bytes.ContainsRune(body, 0) {
		if looksLikeHTML(body) {
			return MediaHTML, nil
		}

		return MediaText, nil
	}

	return "", fmt.Errorf("%q is not text, Markdown, HTML, JSON, YAML, PDF or an Office file: %w", name, ErrUnsupportedDocument)
}

func looksLikeHTML(body []byte) bool {
	head := strings.ToLower(strings.TrimSpace(string(body[:min(len(body), 512)])))

	return strings.HasPrefix(head, "<!doctype html") || strings.HasPrefix(head, "<html")
}
