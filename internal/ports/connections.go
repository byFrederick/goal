package ports

import (
	"context"
	"errors"
	"time"

	"github.com/byFrederick/goal/internal/domain"
)

// ErrNoMasterKey means goald has no master key, so no secret can be sealed or opened: connections
// with secrets are unavailable until one is configured (spec 4 §5.2).
var ErrNoMasterKey = errors.New("connections are unavailable: no master key is configured")

// MasterKey seals and opens small secrets with the platform's master key (AES-256-GCM).
type MasterKey interface {
	// Seal encrypts with the active key and returns its id. Without a key it fails with
	// ErrNoMasterKey.
	Seal(plaintext, aad []byte) (keyID string, nonce, ciphertext []byte, err error)
	// Open decrypts with the named key; an unknown key id or a wrong aad is an error.
	Open(keyID string, nonce, ciphertext, aad []byte) ([]byte, error)
	// Active is the id of the key Seal uses; "" without one.
	Active() string
}

// Connections are an organization's connections and connection kinds (spec 4 §4.2, §4.3), for the
// organization in ctx. Secret values are write-only: only Resolve and Open return them. Every
// change and test is audited in its transaction, and is the organization's stream event
// connection ({connection_id, name, change: created|updated|deleted|tested}, spec 4 §8.3) once it
// commits; each use by a run is audited too.
type Connections interface {
	// Kinds are the platform's kinds and the organization's, by name; an organization's own kind
	// hides a platform kind of the same name.
	Kinds(ctx context.Context) ([]domain.ConnectionKind, error)
	// Kind is the kind name as the organization sees it. ErrNotFound when there is none.
	Kind(ctx context.Context, name string) (domain.ConnectionKind, error)
	// PutKind creates or replaces an organization kind (connection_kind.created or .updated).
	// ErrNameTaken when a new kind takes a platform kind's name.
	PutKind(ctx context.Context, k domain.ConnectionKind, by domain.Actor) error
	// DeleteKind deletes an organization kind. ErrNotFound when the organization has none of that
	// name; ErrConflict while a connection uses it.
	DeleteKind(ctx context.Context, name string, by domain.Actor) error
	// SyncPlatformKinds makes kinds the platform's (goald's start, in no organization): new ones
	// added, changed ones replaced, missing ones deleted unless a connection uses them.
	SyncPlatformKinds(ctx context.Context, kinds []domain.ConnectionKind) error

	// List is the organization's connections, by name.
	List(ctx context.Context) ([]domain.Connection, error)
	// Get is the connection name. ErrNotFound when there is none.
	Get(ctx context.Context, name string) (domain.Connection, error)
	// Create creates a connection (connection.created). Its values must pass its kind's
	// CheckValues (domain.ErrInvalid); ErrNotFound for an unknown kind, ErrNameTaken for a name
	// in use, ErrNoMasterKey for secrets without a master key.
	Create(ctx context.Context, c domain.NewConnection, by domain.Actor) (domain.Connection, error)
	// Update renames a connection and changes its metadata and fields (connection.updated); Secrets
	// replaces the named secret fields only (connection.secret_replaced). ErrNotFound when there is
	// none, ErrNameTaken when another connection has the new name.
	Update(ctx context.Context, name string, p domain.ConnectionPatch, by domain.Actor) (domain.Connection, error)
	// Delete deletes a connection (connection.deleted). ErrNotFound when there is none;
	// ErrConflict while a knowledge source uses it.
	Delete(ctx context.Context, name string, by domain.Actor) error
	// RecordTest records a test's outcome (connection.tested). ErrNotFound when there is none.
	RecordTest(ctx context.Context, name string, r domain.ConnectionTest, by domain.Actor) error

	// Resolve opens the named connections for one run, audits connection.used and sets
	// last_used_at. ErrNotFound names the first unknown connection.
	Resolve(ctx context.Context, names []string, run domain.RunID) ([]domain.ResolvedConnection, error)
	// Open opens one connection for its test, whose outcome RecordTest records: its values, secret
	// ones included, with no use recorded. ErrNotFound when there is none.
	Open(ctx context.Context, name string) (domain.ResolvedConnection, error)
}

// Minter mints short-lived credentials for one run from a connection's stored values (spec 4
// §5.4), so the stored ones never reach a box. A kind's mint names its adapter (aws-sts); the
// minted values fill the kind's {{minted.<name>}} templates.
type Minter interface {
	// Mint returns values that live for about ttl (capped by the provider) for run, made from
	// values (the connection's fields and secrets, its kind's defaults applied), and when they
	// expire. Errors never hold a value.
	Mint(ctx context.Context, values map[string]string, run domain.RunID, ttl time.Duration) (minted map[string]string, expires time.Time, err error)
}
