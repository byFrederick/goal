package ports

import (
	"context"
	"errors"

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
// organization in ctx. Secret values are write-only: only Resolve returns them. Every change and
// test is audited in its transaction; so is each use by a run.
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
	// Update changes metadata and fields (connection.updated); Secrets replaces the named secret
	// fields only (connection.secret_replaced). ErrNotFound when there is none.
	Update(ctx context.Context, name string, p domain.ConnectionPatch, by domain.Actor) (domain.Connection, error)
	// Delete deletes a connection (connection.deleted). ErrNotFound when there is none;
	// ErrConflict while a knowledge source uses it.
	Delete(ctx context.Context, name string, by domain.Actor) error
	// RecordTest records a test's outcome (connection.tested). ErrNotFound when there is none.
	RecordTest(ctx context.Context, name string, r domain.ConnectionTest, by domain.Actor) error

	// Resolve opens the named connections for one run, audits connection.used and sets
	// last_used_at. It is the only method that returns secret values. ErrNotFound names the first
	// unknown connection.
	Resolve(ctx context.Context, names []string, run domain.RunID) ([]domain.ResolvedConnection, error)
}
