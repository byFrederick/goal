package domain

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
)

// Errors of identity (phase 3).
var (
	// ErrUnauthenticated means a request has no credential, or a malformed, unknown, expired or
	// revoked one. The API answers 401 with one body, whatever the path.
	ErrUnauthenticated = errors.New("unauthenticated")
	// ErrReauthRequired means a sensitive operation (fresh: true in api/permissions.yaml) came
	// with a session signed in longer ago than FreshFor. The API answers 403 reauth-required.
	ErrReauthRequired = errors.New("a recent sign-in is required")
)

// Errors of the people of an organization (phase 3). Each wraps ErrInvalid: the API answers 409
// with the rule's code.
var (
	// ErrLastOwner means a change would leave an organization without an owner.
	ErrLastOwner = fmt.Errorf("an organization keeps at least one owner: %w", ErrInvalid)
	// ErrNotAnAdmin means ownership was transferred to someone who is not an admin of the
	// organization: only an admin becomes its owner.
	ErrNotAnAdmin = fmt.Errorf("only an admin of the organization can become its owner: %w", ErrInvalid)
	// ErrNotAnOwner means ownership was transferred away from someone who is not an owner.
	ErrNotAnOwner = fmt.Errorf("only an owner can transfer the organization: %w", ErrInvalid)
	// ErrRoleFromOrg means a factory role was given to an owner or an admin, who are editors in
	// every factory by their organization role.
	ErrRoleFromOrg = fmt.Errorf("owners and admins are editors in every factory: %w", ErrInvalid)
)

// FreshFor is how long after signing in a session may make sensitive changes.
const FreshFor = 10 * time.Minute

// UserID is a Better Auth user's id (a UUID).
type UserID string

// APIKeyID is an API key's id (a UUIDv7).
type APIKeyID string

// OrgRole is a person's role in an organization (Better Auth's organization roles).
type OrgRole string

// Organization roles. Owners hold every permission of the organization; admins every one but
// deleting and transferring it; members read it, and each factory as their factory role says.
const (
	OrgOwner  OrgRole = "owner"
	OrgAdmin  OrgRole = "admin"
	OrgMember OrgRole = "member"
)

// FactoryRole is a member's role on one factory. The roles are a ladder: each includes the one
// before it.
type FactoryRole string

// Factory roles, lowest first.
const (
	FactoryViewer   FactoryRole = "viewer"
	FactoryApprover FactoryRole = "approver"
	FactoryEditor   FactoryRole = "editor"
)

// factoryLadder is the factory roles, lowest first (api/permissions.yaml factory_roles).
var factoryLadder = []FactoryRole{FactoryViewer, FactoryApprover, FactoryEditor}

// Includes reports whether r grants everything other grants (editor ⊇ approver ⊇ viewer). An
// unknown or empty role includes nothing and is included by nothing.
func (r FactoryRole) Includes(other FactoryRole) bool {
	ri, oi := slices.Index(factoryLadder, r), slices.Index(factoryLadder, other)

	return ri >= 0 && oi >= 0 && ri >= oi
}

// Permission is one permission of api/permissions.yaml, such as "org:read" or "item:decide".
type Permission string

// PrincipalKind is what a principal is.
type PrincipalKind string

// Principal kinds: a person, an API key (a service account of one organization), an agent and
// the system. The API makes users and services; agents and jobs carry an Actor only.
const (
	PrincipalUser    PrincipalKind = "user"
	PrincipalService PrincipalKind = "service"
	PrincipalAgent   PrincipalKind = "agent"
	PrincipalSystem  PrincipalKind = "system"
)

// Principal is who a request acts for and what the database says about them right now. The API
// builds it per request, for the organization of the path; nothing caches it.
type Principal struct {
	Kind PrincipalKind
	User UserID   // user
	Key  APIKeyID // service
	// Name is the person's or the key's name. Email is a user's only, and never logged.
	Name, Email string
	// PlatformAdmin is never set for a personal token's principal.
	PlatformAdmin bool
	// Session is the user's session; nil for keys, personal tokens and under GOAL_AUTH=none.
	Session *Session
	// Token is the personal token a user acts through (spec 3 §4.5); nil otherwise. Its scopes
	// limit the user's current roles below.
	Token *TokenGrant
	// Org is the organization the role fields describe ("" outside one). A key's and a personal
	// token's Org is their own.
	Org OrgID
	// OrgRole is "" when the user is not a member of Org; for a personal token, the owner's
	// current role.
	OrgRole      OrgRole
	FactoryRoles map[FactoryID]FactoryRole
	// Scopes, KeyFactories and KeyCeiling are an API key's grants. KeyFactories nil is every
	// factory of Org. KeyExpiresAt is when the key stops working (its streams end then).
	Scopes       []Permission
	KeyFactories []FactoryID
	KeyCeiling   FactoryRole
	KeyExpiresAt time.Time
}

// Session is a user's sign-in. Bearer: the session came as an Authorization bearer value (the
// CLI's goal login), not the browser's cookie.
type Session struct {
	ID                   string
	CreatedAt, ExpiresAt time.Time
	Bearer               bool
}

// LocalPrincipal is the one person of GOAL_AUTH=none: its Actor is LocalUser.
var LocalPrincipal = Principal{Kind: PrincipalUser, User: UserID(LocalUser.ID), Name: LocalUser.Name}

// Actor is how the principal is recorded: user:<id> with the user's name (through a personal
// token too, which Via records), service:<key id> as "<key name> (API key)".
func (p Principal) Actor() Actor {
	switch p.Kind {
	case PrincipalUser:
		if p.User == UserID(LocalUser.ID) {
			return LocalUser
		}

		return Actor{Kind: ActorUser, ID: string(p.User), Name: p.Name}
	case PrincipalService:
		return Actor{Kind: ActorService, ID: string(p.Key), Name: p.Name + " (API key)"}
	case PrincipalAgent:
		return Actor{Kind: ActorAgent, ID: p.Name, Name: p.Name}
	case PrincipalSystem:
		return Actor{Kind: ActorSystem, ID: p.Name, Name: p.Name}
	default:
		return Actor{}
	}
}

// RoleOn is the factory role the principal acts with on factory f of Org: editor for owners and
// admins, the member's own role, a key's ceiling when the key may reach f; "" when f is invisible
// to the principal, which then answers as if f did not exist.
func (p Principal) RoleOn(f FactoryID) FactoryRole {
	switch p.Kind {
	case PrincipalUser:
		switch p.OrgRole {
		case OrgOwner, OrgAdmin:
			return FactoryEditor
		case OrgMember:
			return p.FactoryRoles[f]
		default:
			return ""
		}
	case PrincipalService:
		if p.KeyFactories != nil && !slices.Contains(p.KeyFactories, f) {
			return ""
		}

		return p.KeyCeiling
	case PrincipalAgent, PrincipalSystem:
		return ""
	default:
		return ""
	}
}

// OrgMembership is one organization a user belongs to, with the user's role there and how many
// of its factories the user sees (all of them for owners and admins).
type OrgMembership struct {
	Org          Org
	Role         OrgRole
	FactoryCount int
}

// FactoryGrant is a role on one factory.
type FactoryGrant struct {
	Factory FactoryID
	Role    FactoryRole
}

// Member is a person's membership of an organization: their role there, their factory roles (by
// factory id), when they joined and when they were last active (zero: never signed in).
type Member struct {
	// ID is the membership's own id (Better Auth's member id), which the identity service's
	// role changes and removals name.
	ID           string
	User         User
	Role         OrgRole
	FactoryRoles []FactoryGrant
	JoinedAt     time.Time
	LastActiveAt time.Time
}

// Invitation is a pending invitation to an organization: who it is for, the role and factory
// roles joining gives, who sent it (Inviter's ID and Name) and until when it can be used.
type Invitation struct {
	ID                   string
	Email                string
	Role                 OrgRole
	FactoryRoles         []FactoryGrant
	Inviter              User
	CreatedAt, ExpiresAt time.Time
}

// Audit actions of goald's changes to an organization's people (the identity service's
// membership changes are audited by the database's triggers on its rows, with the same target).
const (
	AuditFactoryRoleGranted = "factory_role.granted"
	AuditFactoryRoleChanged = "factory_role.changed"
	AuditFactoryRoleRemoved = "factory_role.removed"
	// AuditOrgOwnershipTransferred is written by the database's goal_transfer_ownership.
	AuditOrgOwnershipTransferred = "org.ownership_transferred"
	// AuditTargetMember is a member, by their user id.
	AuditTargetMember = "member"
)

// Valid reports whether r is one of the factory roles.
func (r FactoryRole) Valid() bool { return slices.Contains(factoryLadder, r) }

// DeviceRequest is a pending CLI sign-in (the device authorization flow), as the person who
// approves it sees it.
type DeviceRequest struct {
	ClientID, ClientName   string
	RequestedAt, ExpiresAt time.Time
	// IP and UserAgent are the requesting client's.
	IP, UserAgent string
}

type principalKey struct{}

// WithPrincipal returns ctx carrying the request's principal. The API sets it once per request;
// handlers read it with PrincipalFrom.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom returns the principal ctx carries.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)

	return p, ok
}
