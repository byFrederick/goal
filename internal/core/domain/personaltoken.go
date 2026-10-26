package domain

import (
	"context"
	"fmt"
	"time"
)

// PersonalTokenID is a personal token's id (a UUIDv7).
type PersonalTokenID string

// ErrPersonalTokenNameTaken means another live token of the same person in the organization has
// the name (case ignored). It wraps ErrInvalid; a revoked token's name may be used again.
var ErrPersonalTokenNameTaken = fmt.Errorf("personal token name taken: %w", ErrInvalid)

// Personal token limits (spec 3 §4.5): every token expires, after DefaultPersonalTokenDays
// unless its creator chose otherwise, after at most MaxPersonalTokenDays.
const (
	MaxPersonalTokenDays     = 90
	DefaultPersonalTokenDays = 30
)

// TokenGrant is what a personal token gives the request carrying it: the token's scopes, within
// its owner's roles at the moment of the request, until it expires (spec 3 §4.5).
type TokenGrant struct {
	ID        PersonalTokenID
	Name      string
	Scopes    []Permission
	ExpiresAt time.Time
}

// Via is how a write made with the principal records the credential it came through, beside
// its actor: pat:<id> for a personal token, "" otherwise (spec 3 §6.7: via in audit and item
// event data).
func (p Principal) Via() string {
	if p.Token == nil {
		return ""
	}

	return "pat:" + string(p.Token.ID)
}

// ViaFrom is the Via of the principal ctx carries, "" without one.
func ViaFrom(ctx context.Context) string {
	p, _ := PrincipalFrom(ctx)

	return p.Via()
}

// PersonalToken is a member's personal token as goald lists it: never its secret or hash.
type PersonalToken struct {
	ID PersonalTokenID
	// Name is the owner's name for it; Hint is goal_pat_ and the secret's first four characters.
	Name, Hint string
	Scopes     []Permission
	// Owner is the member it acts as.
	Owner     User
	CreatedAt time.Time
	ExpiresAt time.Time
	// LastUsedAt is nil until the token is first used; it is recorded at most once a minute.
	LastUsedAt *time.Time
	// RevokedAt and RevokedBy are nil while the token is live.
	RevokedAt *time.Time
	RevokedBy *Actor
}

// NewPersonalToken is a token to create for Owner, who creates it. The caller makes the secret
// and passes only its Hash, base64url(SHA-256(secret)), and Hint; the store dates it and counts
// ExpiresInDays from then.
type NewPersonalToken struct {
	Owner            UserID
	Name, Hint, Hash string
	Scopes           []Permission
	ExpiresInDays    int
}

// Audit actions of personal tokens.
const (
	AuditPersonalTokenCreated = "personal_token.created"
	AuditPersonalTokenRevoked = "personal_token.revoked"
)
