package ports

import (
	"context"

	"github.com/byFrederick/goal/internal/domain"
)

// Identity answers who a request is, per request, from the database (phase 3): Better Auth's
// sessions and Goal's API keys, with the memberships and roles they carry right now. Nothing is
// cached, so a removal, a role change, a revocation or a sign-out holds on the next request.
// Failures to resolve a credential wrap domain.ErrUnauthenticated.
type Identity interface {
	// Session resolves a Better Auth session token (the 32-character token, never the signed
	// cookie value) for org ("" outside one). ErrUnauthenticated when unknown or expired. A user
	// who is not a member of a live org gets OrgRole "" and no factory roles.
	Session(ctx context.Context, token string, org domain.OrgID) (domain.Principal, error)
	// APIKey resolves base64url(SHA-256(key)). ErrUnauthenticated when unknown, revoked, expired
	// or of a deleted org. Touches last_used_at at most once a minute.
	APIKey(ctx context.Context, hash string) (domain.Principal, error)
	// OrgsFor lists the live organizations the user belongs to, by name, with the user's role.
	OrgsFor(ctx context.Context, user domain.UserID) ([]domain.OrgMembership, error)
	// DeviceRequest describes a pending CLI sign-in by its user code; ErrNotFound when unknown
	// or expired.
	DeviceRequest(ctx context.Context, userCode string) (domain.DeviceRequest, error)
}
