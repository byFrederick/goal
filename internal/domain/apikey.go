package domain

import (
	"fmt"
	"time"
)

// ErrAPIKeyNameTaken means another live key of the organization has the name (case ignored). It
// wraps ErrInvalid; a revoked key's name may be used again.
var ErrAPIKeyNameTaken = fmt.Errorf("api key name taken: %w", ErrInvalid)

// API key limits (spec 3 §4.3): every key expires, after at most MaxAPIKeyDays.
const (
	MaxAPIKeyDays     = 90
	DefaultAPIKeyDays = 90
	MaxAPIKeyName     = 80
	MaxAPIKeyScopes   = 64
)

// APIKey is an organization's service account as goald lists it: never its secret or hash. A key
// acts for the organization with its scopes, at most as its role ceiling on each factory it
// reaches (Factories nil: every factory, now and later).
type APIKey struct {
	ID APIKeyID
	// Name is how the audit log shows its actions ("<name> (API key)"); Hint is goal_sk_ and the
	// secret's first four characters.
	Name, Hint string
	Scopes     []Permission
	Ceiling    FactoryRole
	Factories  []FactoryID
	CreatedBy  Actor
	CreatedAt  time.Time
	ExpiresAt  time.Time
	// LastUsedAt is nil until the key is first used; it is recorded at most once a minute.
	LastUsedAt *time.Time
	// RevokedAt and RevokedBy are nil while the key is live.
	RevokedAt *time.Time
	RevokedBy *Actor
}

// NewAPIKey is a key to create. The caller makes the secret and passes only its Hash,
// base64url(SHA-256(secret)), and Hint; the store dates it and counts ExpiresInDays from then.
type NewAPIKey struct {
	Name, Hint, Hash string
	Scopes           []Permission
	Ceiling          FactoryRole
	// Factories nil is every factory of the organization.
	Factories     []FactoryID
	ExpiresInDays int
}

// Audit actions of API keys.
const (
	AuditAPIKeyCreated = "api_key.created"
	AuditAPIKeyRevoked = "api_key.revoked"
)
