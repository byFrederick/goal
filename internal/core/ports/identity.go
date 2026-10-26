package ports

import (
	"context"

	"github.com/byFrederick/goal/internal/core/domain"
)

// Identity answers who a request is, per request, from the database (phase 3): Better Auth's
// sessions, Goal's API keys and personal tokens, with the memberships and roles they carry right
// now. Nothing is cached, so a removal, a role change, a revocation or a sign-out holds on the
// next request.
// Failures to resolve a credential wrap domain.ErrUnauthenticated.
type Identity interface {
	// Session resolves a Better Auth session token (the 32-character token, never the signed
	// cookie value) for org ("" outside one). ErrUnauthenticated when unknown or expired. A user
	// who is not a member of a live org gets OrgRole "" and no factory roles.
	Session(ctx context.Context, token string, org domain.OrgID) (domain.Principal, error)
	// APIKey resolves base64url(SHA-256(key)). ErrUnauthenticated when unknown, revoked, expired
	// or of a deleted org. Touches last_used_at at most once a minute.
	APIKey(ctx context.Context, hash string) (domain.Principal, error)
	// PersonalToken resolves base64url(SHA-256(token)) to its owner: a user principal with Token
	// set, Org the token's own, and the owner's CURRENT org role and factory roles there, never
	// platform admin and never a session. ErrUnauthenticated when unknown, revoked, expired, of a
	// removed member or of a deleted org. Touches last_used_at at most once a minute.
	PersonalToken(ctx context.Context, hash string) (domain.Principal, error)
	// OrgsFor lists the live organizations the user belongs to, by name, with the user's role.
	OrgsFor(ctx context.Context, user domain.UserID) ([]domain.OrgMembership, error)
	// DeviceRequest describes a pending CLI sign-in by its user code; ErrNotFound when unknown
	// or expired.
	DeviceRequest(ctx context.Context, userCode string) (domain.DeviceRequest, error)
}

// Access keeps the people and keys of the organization in ctx (spec 3 §5.1): its members and
// pending invitations as the identity service keeps them, Goal's factory roles, the transfer
// of its ownership, and its API keys. Membership itself (inviting, changing an organization
// role, removing) changes only through the identity service, so its owner rules hold. Every
// change appends its audit entry in its transaction; nothing is cached.
type Access interface {
	// APIKeys lists the organization's live keys and those revoked in the last 30 days, live
	// ones first, newest first.
	APIKeys(ctx context.Context) ([]domain.APIKey, error)
	// CreateAPIKey stores a key whose hash and hint the caller computed (api_key.created). A live
	// key of the same name (case ignored) is domain.ErrAPIKeyNameTaken; a factory not of the
	// organization ErrNotFound.
	CreateAPIKey(ctx context.Context, by domain.Actor, k domain.NewAPIKey) (domain.APIKey, error)
	// RevokeAPIKey revokes a key at once (api_key.revoked): it stops resolving on the next
	// request. Revoking a revoked key changes nothing; an unknown one is ErrNotFound.
	RevokeAPIKey(ctx context.Context, by domain.Actor, id domain.APIKeyID) error
	// Members lists the organization's members by name, with their factory roles and last
	// activity.
	Members(ctx context.Context) ([]domain.Member, error)
	// Invitations lists the organization's pending, unexpired invitations, oldest first.
	Invitations(ctx context.Context) ([]domain.Invitation, error)
	// SetFactoryRole gives member user role on factory f; "" removes it (nothing to remove is no
	// change). ErrNotFound when user is not a member or f not a factory of the organization;
	// domain.ErrRoleFromOrg for an owner or an admin, who are editors everywhere;
	// domain.ErrInvalid for an unknown role. Audit: factory_role.granted, changed or removed.
	SetFactoryRole(ctx context.Context, by domain.Actor, user domain.UserID, f domain.FactoryID, role domain.FactoryRole) error
	// TransferOwnership makes the admin to an owner and the owner from an admin, at once.
	// domain.ErrNotAnAdmin when to is not an admin, domain.ErrNotAnOwner when from is not an
	// owner; nothing changes then. Audit: org.ownership_transferred (and both role changes).
	TransferOwnership(ctx context.Context, by domain.Actor, from, to domain.UserID) error
	// PersonalTokens lists the organization's live personal tokens and those revoked in the last
	// 30 days, live ones first, newest first: owner's only, or every member's when owner is nil
	// (the admins' listing). The owner filter is part of the statement.
	PersonalTokens(ctx context.Context, owner *domain.UserID) ([]domain.PersonalToken, error)
	// CreatePersonalToken stores a token of member t.Owner whose hash and hint the caller
	// computed (personal_token.created). A live token of the owner's with the same name (case
	// ignored) is domain.ErrPersonalTokenNameTaken; an owner who is not a member ErrNotFound.
	CreatePersonalToken(ctx context.Context, by domain.Actor, t domain.NewPersonalToken) (domain.PersonalToken, error)
	// RevokePersonalToken revokes a token at once (personal_token.revoked): it stops resolving on
	// the next request. owner nil reaches any member's token, else only owner's: another's is
	// ErrNotFound, as is an unknown one, decided in the statement. Revoking a revoked token
	// changes nothing.
	RevokePersonalToken(ctx context.Context, by domain.Actor, id domain.PersonalTokenID, owner *domain.UserID) error
}

// AccessNotices streams the access changes committed by anyone (PostgreSQL LISTEN goal_access):
// a membership ended or its role changed, a session ended, a factory role granted, changed or
// removed, an organization deleted. Any role that may connect can NOTIFY, so a notice is a claim:
// it only ends what it names (a stream, which reconnects and is checked again) and never grants
// anything.
type AccessNotices interface {
	// Listen calls deliver for every access notice until ctx ends, then returns nil. Each time
	// it starts listening (the first time, and after a lost connection, which misses notices) it
	// first delivers a notice with Resync set, after which listeners must check everything again.
	Listen(ctx context.Context, deliver func(AccessNotice)) error
}

// Kinds of access notices (00043's goal_notify_access, 00046's api_key, 00047's personal_token).
const (
	AccessMember      = "member"       // OrgID, UserID: a role changed or the membership ended
	AccessSession     = "session"      // UserID, SessionID: signed out or cleaned up
	AccessFactoryRole = "factory_role" // OrgID, UserID, FactoryID
	AccessOrg         = "org"          // OrgID: soft-deleted
	AccessAPIKey      = "api_key"      // OrgID, KeyID: revoked
	// AccessPersonalToken: OrgID, UserID, TokenID: revoked (a removal sends member).
	AccessPersonalToken = "personal_token"
)

// AccessNotice is one access change as its notification claims it.
type AccessNotice struct {
	Resync    bool
	Kind      string
	OrgID     domain.OrgID
	UserID    domain.UserID
	SessionID string
	FactoryID domain.FactoryID
	KeyID     domain.APIKeyID
	TokenID   domain.PersonalTokenID
}
