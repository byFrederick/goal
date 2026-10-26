package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"regexp"
	"slices"
	"time"
)

// DefinitionVersionID identifies one draft, published or archived version of a factory
// definition (a UUIDv7).
type DefinitionVersionID string

// BlobHash is the SHA-256 of one definition file's content in 64 lowercase hex characters. A
// file is stored once per organization under its hash.
type BlobHash string

var blobHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// HashBlob returns the hash content is stored under.
func HashBlob(content []byte) BlobHash {
	sum := sha256.Sum256(content)

	return BlobHash(hex.EncodeToString(sum[:]))
}

// Valid reports whether h is 64 lowercase hex characters.
func (h BlobHash) Valid() bool { return blobHashPattern.MatchString(string(h)) }

// MaxBlobSize is the largest definition file, in bytes (1 MiB).
const MaxBlobSize = 1 << 20

// OrgPrefix is the bucket prefix of everything an organization owns: orgs/<org-id>/.
func OrgPrefix(org OrgID) string { return "orgs/" + string(org) + "/" }

// BlobKey is the bucket key of an organization's blob: orgs/<org-id>/blobs/<sha256>.
func BlobKey(org OrgID, h BlobHash) string { return OrgPrefix(org) + "blobs/" + string(h) }

// Manifest is a definition's file tree: each slash path relative to the factory's root
// ("factory.yaml", "agents/engineer.md") and the hash of its content.
type Manifest map[string]BlobHash

// Hashes returns the distinct hashes of m, sorted.
func (m Manifest) Hashes() []BlobHash {
	return slices.Compact(slices.Sorted(maps.Values(m)))
}

// VersionState is where a version is in its life. Values match definition_versions.state.
type VersionState string

// Version states. A factory has at most one draft; published versions never change; an archived
// version is a discarded draft, kept for audit and compare and never published.
const (
	VersionDraft     VersionState = "draft"
	VersionPublished VersionState = "published"
	VersionArchived  VersionState = "archived"
)

// DefinitionVersion is one version of a factory's definition: its manifest and who made it.
type DefinitionVersion struct {
	ID        DefinitionVersionID
	OrgID     OrgID
	FactoryID FactoryID
	State     VersionState
	// Number is the published version's number (v1, v2, ...), never reused; 0 until published.
	Number int
	// BaseID is the published version a draft started from; empty for a draft that started
	// from nothing, such as a factory's first.
	BaseID DefinitionVersionID
	// Revision counts a draft's saves from 1; it is frozen when the draft is published or
	// archived. Saves compare and set on it.
	Revision int64
	Manifest Manifest
	// Message is the publish note ("Merge step on Approve spec").
	Message string
	Source  VersionSource
	// CreatedBy, UpdatedBy and PublishedBy are stored as Actor.Ref().
	CreatedBy, UpdatedBy, PublishedBy Actor
	CreatedAt, UpdatedAt, PublishedAt time.Time
}

// SourceKind says where a version's content came from.
type SourceKind string

// Source kinds.
const (
	SourceBlank    SourceKind = "blank"
	SourceTemplate SourceKind = "template"
	SourceClone    SourceKind = "clone"
	SourceImport   SourceKind = "import"
	SourceCLI      SourceKind = "cli"
	// SourceLegacy is a factory imported from the bucket's per-factory prefix of before 2.2, by
	// the legacy import of releases before phase 4; versions it made keep it.
	SourceLegacy SourceKind = "legacy"
	// SourceEdit is a draft opened on a published version, its base.
	SourceEdit SourceKind = "edit"
)

// VersionSource is where a version started, so a draft can be compared with its starting tree
// ("Changes from the Cost review template").
type VersionSource struct {
	Kind SourceKind
	// Template is the template and version a factory was created from ("cost-review@2").
	Template string
	// Factory and Version are the factory and version a clone copied, or the base of an edit.
	Factory FactoryID
	Version DefinitionVersionID
	// Manifest is the starting tree when no version holds it (template, import).
	Manifest Manifest
}

// Audit actions of factories and their definitions. Their target is the factory (factory.*)
// or the version (definition.*); FactoryID is always set.
const (
	AuditFactoryCreated      = "factory.created"
	AuditFactoryDeleted      = "factory.deleted"
	AuditFactoryPaused       = "factory.paused"
	AuditFactoryResumed      = "factory.resumed"
	AuditFactoryArchived     = "factory.archived"
	AuditFactoryRestored     = "factory.restored"
	AuditDefinitionPublished = "definition.published"
	AuditDefinitionRestored  = "definition.restored"
	AuditDraftDiscarded      = "definition.draft_discarded"
)

// Audit target kinds of factories and their definitions.
const (
	AuditTargetFactory = "factory"
	AuditTargetVersion = "definition_version"
)
