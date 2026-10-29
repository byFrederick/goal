package porttest

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var (
	_ ports.Identity = (*Identity)(nil)
	_ ports.Access   = (*Identity)(nil)
)

// keyTouchEvery is how often an API key's last use is recorded at most.
const keyTouchEvery = time.Minute

// Identity keeps people, sessions, memberships, invitations, factory roles and API keys in memory
// behind one mutex, with the database's rules: expired sessions and revoked, expired or
// deleted-org keys do not resolve; a member of a deleted organization has no role there; an
// organization never loses its last owner and the install never its last platform admin
// (ErrConflict). It is the organization's Access too, over the same people, so a change made
// through it holds on the next lookup. Tests set it up with the Add, Set and Invite methods.
type Identity struct {
	mu       sync.Mutex
	now      func() time.Time
	orgs     map[domain.OrgID]*identityOrg
	users    map[domain.UserID]*IdentityUser
	sessions map[string]identitySession
	keys     map[string]*IdentityKey   // by hash
	tokens   map[string]*IdentityToken // by hash
	devices  map[string]domain.DeviceRequest
}

// IdentityUser is a person of the fake.
type IdentityUser struct {
	ID            domain.UserID
	Name, Email   string
	PlatformAdmin bool
}

// IdentityKey is an API key of the fake. LastUsedAt is kept by APIKey.
type IdentityKey struct {
	ID                   domain.APIKeyID
	Org                  domain.OrgID
	Name, Hint           string
	Scopes               []domain.Permission
	Factories            []domain.FactoryID // nil: every factory of Org
	Ceiling              domain.FactoryRole
	CreatedBy            domain.Actor
	CreatedAt            time.Time
	ExpiresAt, RevokedAt time.Time // RevokedAt zero while live
	RevokedBy            domain.Actor
	LastUsedAt           time.Time
}

type identityOrg struct {
	org          domain.Org
	deleted      bool
	factories    []domain.FactoryID
	members      map[domain.UserID]domain.OrgRole
	joined       map[domain.UserID]time.Time
	factoryRoles map[domain.UserID]map[domain.FactoryID]domain.FactoryRole
	invitations  []*identityInvitation
}

type identityInvitation struct {
	invitation domain.Invitation
	status     string
}

type identitySession struct {
	user    domain.UserID
	session domain.Session
}

// NewIdentity returns an identity with nobody in it; now decides what has expired.
func NewIdentity(now func() time.Time) *Identity {
	return &Identity{
		now: now, orgs: map[domain.OrgID]*identityOrg{}, users: map[domain.UserID]*IdentityUser{},
		sessions: map[string]identitySession{}, keys: map[string]*IdentityKey{}, tokens: map[string]*IdentityToken{},
		devices: map[string]domain.DeviceRequest{},
	}
}

// AddOrg adds a live organization.
func (i *Identity) AddOrg(o domain.Org) {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.orgs[o.ID] = &identityOrg{
		org: o, members: map[domain.UserID]domain.OrgRole{}, joined: map[domain.UserID]time.Time{},
		factoryRoles: map[domain.UserID]map[domain.FactoryID]domain.FactoryRole{},
	}
}

// DeleteOrg soft-deletes an organization: its members lose their role there and its keys and
// personal tokens are revoked (by system:org-deleted).
func (i *Identity) DeleteOrg(id domain.OrgID) {
	i.mu.Lock()
	defer i.mu.Unlock()

	if o, ok := i.orgs[id]; ok {
		o.deleted = true
	}

	for _, k := range i.keys {
		if k.Org == id && k.RevokedAt.IsZero() {
			k.RevokedAt, k.RevokedBy = i.now(), orgDeleted
		}
	}

	for _, t := range i.tokens {
		if t.Org == id && t.RevokedAt.IsZero() {
			t.RevokedAt, t.RevokedBy = i.now(), orgDeleted
		}
	}
}

// AddFactory adds a factory to an organization: owners and admins see every one (OrgsFor).
func (i *Identity) AddFactory(org domain.OrgID, f domain.FactoryID) {
	i.mu.Lock()
	defer i.mu.Unlock()

	if o, ok := i.orgs[org]; ok && !slices.Contains(o.factories, f) {
		o.factories = append(o.factories, f)
	}
}

// AddUser adds a person. Making the last platform admin a non-admin is refused.
func (i *Identity) AddUser(u IdentityUser) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	if old, ok := i.users[u.ID]; ok && old.PlatformAdmin && !u.PlatformAdmin && i.platformAdmins() == 1 {
		return fmt.Errorf("user %s is the last platform admin: %w", u.ID, ports.ErrConflict)
	}

	i.users[u.ID] = &u

	return nil
}

// Email is the sign-in email of user, and false for no such user.
func (i *Identity) Email(user domain.UserID) (string, bool) {
	i.mu.Lock()
	defer i.mu.Unlock()

	u, ok := i.users[user]
	if !ok {
		return "", false
	}

	return u.Email, true
}

// AddSession signs user in with token.
func (i *Identity) AddSession(token string, user domain.UserID, s domain.Session) {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.sessions[token] = identitySession{user: user, session: s}
}

// EndSession signs a session out.
func (i *Identity) EndSession(token string) {
	i.mu.Lock()
	defer i.mu.Unlock()

	delete(i.sessions, token)
}

// SetOrgRole makes user a member of org with role, or removes them ("" role) with their factory
// roles and personal tokens there (the membership's cascade). Leaving an organization that has
// owners without one is refused.
func (i *Identity) SetOrgRole(org domain.OrgID, user domain.UserID, role domain.OrgRole) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	o, ok := i.orgs[org]
	if !ok {
		return fmt.Errorf("organization %s: %w", org, ports.ErrNotFound)
	}

	if o.members[user] == domain.OrgOwner && role != domain.OrgOwner && o.owners() == 1 {
		return fmt.Errorf("user %s is the last owner of %s: %w", user, org, ports.ErrConflict)
	}

	if role == "" {
		delete(o.members, user)
		delete(o.joined, user)
		delete(o.factoryRoles, user)
		maps.DeleteFunc(i.tokens, func(_ string, t *IdentityToken) bool { return t.Org == org && t.User == user })

		return nil
	}

	if o.members[user] == "" {
		o.joined[user] = i.now()
	}

	o.members[user] = role

	return nil
}

// GrantFactoryRole gives a member a role on factory f of org, whatever their organization role
// and whether f was added; "" removes it. The Access method SetFactoryRole keeps the rules.
func (i *Identity) GrantFactoryRole(org domain.OrgID, user domain.UserID, f domain.FactoryID, role domain.FactoryRole) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	o, ok := i.orgs[org]
	if !ok || o.members[user] == "" {
		return fmt.Errorf("member %s of %s: %w", user, org, ports.ErrNotFound)
	}

	o.grant(user, f, role)

	return nil
}

// AddKey adds an API key stored under hash, base64url(SHA-256(secret)).
func (i *Identity) AddKey(hash string, k IdentityKey) {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.keys[hash] = &k
}

// RevokeKey revokes the key with id at once.
func (i *Identity) RevokeKey(id domain.APIKeyID) {
	i.mu.Lock()
	defer i.mu.Unlock()

	for _, k := range i.keys {
		if k.ID == id && k.RevokedAt.IsZero() {
			k.RevokedAt = i.now()
		}
	}
}

// Key returns the key stored under hash, as it is now.
func (i *Identity) Key(hash string) (IdentityKey, bool) {
	i.mu.Lock()
	defer i.mu.Unlock()

	k, ok := i.keys[hash]
	if !ok {
		return IdentityKey{}, false
	}

	return *k, true
}

// AddDeviceRequest adds a pending CLI sign-in under its user code (dashes and case ignored).
func (i *Identity) AddDeviceRequest(userCode string, d domain.DeviceRequest) {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.devices[normalUserCode(userCode)] = d
}

// normalUserCode is a CLI sign-in code as the database compares it: its ASCII letters and digits
// only, upper case (ABCD-EFGH and abcdefgh are one code).
func normalUserCode(code string) string {
	return strings.ToUpper(strings.Map(func(r rune) rune {
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return r
		}

		return -1
	}, code))
}

// Session implements ports.Identity.
func (i *Identity) Session(_ context.Context, token string, org domain.OrgID) (domain.Principal, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	s, ok := i.sessions[token]
	u := i.users[s.user]

	if !ok || u == nil || !i.now().Before(s.session.ExpiresAt) {
		return domain.Principal{}, fmt.Errorf("session: %w", domain.ErrUnauthenticated)
	}

	session := s.session
	p := domain.Principal{
		Kind: domain.PrincipalUser, User: u.ID, Name: u.Name, Email: u.Email, PlatformAdmin: u.PlatformAdmin,
		Session: &session, Org: org,
	}

	if o, ok := i.orgs[org]; ok && !o.deleted && o.members[u.ID] != "" {
		p.OrgRole = o.members[u.ID]
		p.FactoryRoles = maps.Clone(o.factoryRoles[u.ID])
	}

	return p, nil
}

// APIKey implements ports.Identity.
func (i *Identity) APIKey(_ context.Context, hash string) (domain.Principal, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	now := i.now()
	k, ok := i.keys[hash]

	if !ok || !k.RevokedAt.IsZero() || !now.Before(k.ExpiresAt) || i.orgs[k.Org] == nil || i.orgs[k.Org].deleted {
		return domain.Principal{}, fmt.Errorf("api key: %w", domain.ErrUnauthenticated)
	}

	if now.Sub(k.LastUsedAt) >= keyTouchEvery {
		k.LastUsedAt = now
	}

	return domain.Principal{
		Kind: domain.PrincipalService, Key: k.ID, Name: k.Name, Org: k.Org,
		Scopes: slices.Clone(k.Scopes), KeyFactories: slices.Clone(k.Factories), KeyCeiling: k.Ceiling, KeyExpiresAt: k.ExpiresAt,
	}, nil
}

// OrgsFor implements ports.Identity.
func (i *Identity) OrgsFor(_ context.Context, user domain.UserID) ([]domain.OrgMembership, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	out := []domain.OrgMembership{}

	for _, o := range i.orgs {
		if role := o.members[user]; role != "" && !o.deleted {
			count := len(o.factoryRoles[user])
			if role == domain.OrgOwner || role == domain.OrgAdmin {
				count = len(o.factories)
			}

			out = append(out, domain.OrgMembership{Org: o.org, Role: role, FactoryCount: count})
		}
	}

	slices.SortFunc(out, func(a, b domain.OrgMembership) int {
		return cmp.Or(cmp.Compare(a.Org.Name, b.Org.Name), cmp.Compare(a.Org.ID, b.Org.ID))
	})

	return out, nil
}

// DeviceRequest implements ports.Identity.
func (i *Identity) DeviceRequest(_ context.Context, userCode string) (domain.DeviceRequest, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	d, ok := i.devices[normalUserCode(userCode)]
	if !ok || !i.now().Before(d.ExpiresAt) {
		return domain.DeviceRequest{}, fmt.Errorf("device request: %w", ports.ErrNotFound)
	}

	return d, nil
}

func (i *Identity) platformAdmins() int {
	n := 0

	for _, u := range i.users {
		if u.PlatformAdmin {
			n++
		}
	}

	return n
}

func (o *identityOrg) owners() int {
	n := 0

	for _, r := range o.members {
		if r == domain.OrgOwner {
			n++
		}
	}

	return n
}
