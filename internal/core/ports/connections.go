package ports

import (
	"context"
	"errors"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
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
	// ErrNameTaken when a new kind takes a platform kind's name; ErrConflict when the change makes
	// a field secret that one of its connections holds as a plain value (cleared there first).
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
	// in use, ErrNoMasterKey for secrets to seal without a master key. A secret value that is
	// exactly {{name}} is stored as a reference to credential name in SecretRefs, never sealed;
	// one that holds {{…}} anywhere else, or a non-secret field with one, is domain.ErrInvalid
	// (domain.ConnectionSecrets). The credential need not exist: a run fails without it.
	Create(ctx context.Context, c domain.NewConnection, by domain.Actor) (domain.Connection, error)
	// Update renames a connection and changes its metadata and fields (connection.updated); Secrets
	// replaces the named secret fields only (connection.secret_replaced), each one moving between
	// sealed and a reference as its new value is one or the other, as Create. ErrNotFound when
	// there is none, ErrNameTaken when another connection has the new name. ToolModes replaces
	// every tool's mode (connection.updated); an unknown mode or a malformed tool name is
	// domain.ErrInvalid.
	Update(ctx context.Context, name string, p domain.ConnectionPatch, by domain.Actor) (domain.Connection, error)
	// Delete deletes a connection (connection.deleted) and revokes its active standing rules by
	// the same actor (standing_rule.revoked each). ErrNotFound when there is none; ErrConflict
	// while a knowledge source uses it.
	Delete(ctx context.Context, name string, by domain.Actor) error
	// RecordTest records a test's outcome (connection.tested), its Tools sorted and without
	// repeats; the tool modes stay. ErrNotFound when there is none.
	RecordTest(ctx context.Context, name string, r domain.ConnectionTest, by domain.Actor) error

	// StandingRules are the organization's standing rules (ADR 0031 decision 7), oldest first:
	// the active ones, and the revoked ones too with includeRevoked.
	StandingRules(ctx context.Context, includeRevoked bool) ([]domain.StandingRule, error)
	// CreateStandingRule creates a standing rule for the connection named n.Connection
	// (standing_rule.created). When its key (connection, tool, agent) has an active rule already,
	// it returns that rule and audits nothing. ErrNotFound for an unknown connection;
	// domain.ErrInvalid for a malformed n (NewStandingRule.Check).
	CreateStandingRule(ctx context.Context, n domain.NewStandingRule, by domain.Actor) (domain.StandingRule, error)
	// RevokeStandingRule revokes rule id (standing_rule.revoked). ErrNotFound when there is none;
	// ErrConflict when it is revoked already.
	RevokeStandingRule(ctx context.Context, id domain.StandingRuleID, by domain.Actor) (domain.StandingRule, error)

	// Resolve opens the named connections for use (a run, a merge, a sync), audits each as
	// connection.used by use's actor with what it is for, and sets last_used_at. ErrNotFound
	// names the first unknown connection. References to credentials come back unopened in
	// SecretRefs, for the engine to open.
	Resolve(ctx context.Context, names []string, use domain.ConnectionUse) ([]domain.ResolvedConnection, error)
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
