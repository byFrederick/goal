package porttest

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// IdentityToken is a personal token of the fake: a member's own, deleted with the membership.
// LastUsedAt is kept by PersonalToken.
type IdentityToken struct {
	ID                   domain.PersonalTokenID
	Org                  domain.OrgID
	User                 domain.UserID
	Name, Hint           string
	Scopes               []domain.Permission
	CreatedAt            time.Time
	ExpiresAt, RevokedAt time.Time // RevokedAt zero while live
	RevokedBy            domain.Actor
	LastUsedAt           time.Time
}

// AddToken adds a personal token stored under hash, base64url(SHA-256(secret)). Its owner must
// be a member of its organization, as the database's foreign key requires.
func (i *Identity) AddToken(hash string, t IdentityToken) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	if o, ok := i.orgs[t.Org]; !ok || o.members[t.User] == "" {
		return fmt.Errorf("member %s of %s: %w", t.User, t.Org, ports.ErrNotFound)
	}

	i.tokens[hash] = &t

	return nil
}

// Token returns the personal token stored under hash, as it is now; false once it is gone (its
// owner left the organization).
func (i *Identity) Token(hash string) (IdentityToken, bool) {
	i.mu.Lock()
	defer i.mu.Unlock()

	t, ok := i.tokens[hash]
	if !ok {
		return IdentityToken{}, false
	}

	return *t, true
}

// RevokeToken revokes the personal token with id at once (by system:test).
func (i *Identity) RevokeToken(id domain.PersonalTokenID) {
	i.mu.Lock()
	defer i.mu.Unlock()

	for _, t := range i.tokens {
		if t.ID == id && t.RevokedAt.IsZero() {
			t.RevokedAt, t.RevokedBy = i.now(), domain.Actor{Kind: domain.ActorSystem, ID: "test", Name: "test"}
		}
	}
}

// ExpireToken makes the personal token with id expired a day ago.
func (i *Identity) ExpireToken(id domain.PersonalTokenID) {
	i.mu.Lock()
	defer i.mu.Unlock()

	for _, t := range i.tokens {
		if t.ID == id {
			t.ExpiresAt = i.now().Add(-24 * time.Hour)
		}
	}
}

// PersonalToken implements ports.Identity: the owner with their roles in the token's
// organization now, never platform admin and never a session.
func (i *Identity) PersonalToken(_ context.Context, hash string) (domain.Principal, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	now := i.now()
	t, ok := i.tokens[hash]

	if !ok || !t.RevokedAt.IsZero() || !now.Before(t.ExpiresAt) {
		return domain.Principal{}, fmt.Errorf("personal token: %w", domain.ErrUnauthenticated)
	}

	o, u := i.orgs[t.Org], i.users[t.User]
	if o == nil || o.deleted || o.members[t.User] == "" || u == nil {
		return domain.Principal{}, fmt.Errorf("personal token: %w", domain.ErrUnauthenticated)
	}

	if now.Sub(t.LastUsedAt) >= keyTouchEvery {
		t.LastUsedAt = now
	}

	return domain.Principal{
		Kind: domain.PrincipalUser, User: u.ID, Name: u.Name, Email: u.Email, Org: t.Org,
		OrgRole: o.members[t.User], FactoryRoles: maps.Clone(o.factoryRoles[t.User]),
		Token: &domain.TokenGrant{ID: t.ID, Name: t.Name, Scopes: slices.Clone(t.Scopes), ExpiresAt: t.ExpiresAt},
	}, nil
}

// PersonalTokens implements ports.Access for the organization in ctx.
func (i *Identity) PersonalTokens(ctx context.Context, owner *domain.UserID) ([]domain.PersonalToken, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, domain.ErrNoOrg
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	now := i.now()
	out := []domain.PersonalToken{}

	for _, t := range i.tokens {
		listed := t.RevokedAt.IsZero() || now.Sub(t.RevokedAt) < keyListedFor
		if t.Org == org && listed && (owner == nil || t.User == *owner) {
			out = append(out, i.tokenOut(t))
		}
	}

	slices.SortFunc(out, func(a, b domain.PersonalToken) int {
		return cmp.Or(cmp.Compare(boolInt(a.RevokedAt != nil), boolInt(b.RevokedAt != nil)), b.CreatedAt.Compare(a.CreatedAt),
			cmp.Compare(b.ID, a.ID))
	})

	return out, nil
}

// CreatePersonalToken implements ports.Access for the organization in ctx.
func (i *Identity) CreatePersonalToken(ctx context.Context, _ domain.Actor, n domain.NewPersonalToken) (domain.PersonalToken, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.PersonalToken{}, domain.ErrNoOrg
	}

	if n.ExpiresInDays < 1 || n.ExpiresInDays > domain.MaxPersonalTokenDays {
		return domain.PersonalToken{}, fmt.Errorf("expires in %d days: %w", n.ExpiresInDays, domain.ErrInvalid)
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	if o, ok := i.orgs[org]; !ok || o.members[n.Owner] == "" {
		return domain.PersonalToken{}, fmt.Errorf("member %s: %w", n.Owner, ports.ErrNotFound)
	}

	for _, t := range i.tokens {
		if t.Org == org && t.User == n.Owner && t.RevokedAt.IsZero() && strings.EqualFold(t.Name, n.Name) {
			return domain.PersonalToken{}, domain.ErrPersonalTokenNameTaken
		}
	}

	if _, taken := i.tokens[n.Hash]; taken {
		return domain.PersonalToken{}, fmt.Errorf("hash taken: %w", ports.ErrConflict)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return domain.PersonalToken{}, err
	}

	now := i.now()
	t := &IdentityToken{
		ID: domain.PersonalTokenID(id.String()), Org: org, User: n.Owner, Name: n.Name, Hint: n.Hint, Scopes: slices.Clone(n.Scopes),
		CreatedAt: now, ExpiresAt: now.Add(time.Duration(n.ExpiresInDays) * 24 * time.Hour),
	}
	i.tokens[n.Hash] = t

	return i.tokenOut(t), nil
}

// RevokePersonalToken implements ports.Access for the organization in ctx.
func (i *Identity) RevokePersonalToken(ctx context.Context, by domain.Actor, id domain.PersonalTokenID, owner *domain.UserID) error {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.ErrNoOrg
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	for _, t := range i.tokens {
		if t.ID == id && t.Org == org && (owner == nil || t.User == *owner) {
			if t.RevokedAt.IsZero() {
				t.RevokedAt, t.RevokedBy = i.now(), by
			}

			return nil
		}
	}

	return fmt.Errorf("personal token %s: %w", id, ports.ErrNotFound)
}

// tokenOut is a token as Access lists it, its owner named. Callers hold i.mu.
func (i *Identity) tokenOut(t *IdentityToken) domain.PersonalToken {
	out := domain.PersonalToken{
		ID: t.ID, Name: t.Name, Hint: t.Hint, Scopes: slices.Clone(t.Scopes), Owner: domain.User{ID: t.User},
		CreatedAt: t.CreatedAt, ExpiresAt: t.ExpiresAt,
	}

	if u := i.users[t.User]; u != nil {
		out.Owner.Name, out.Owner.Email = u.Name, u.Email
	}

	if !t.LastUsedAt.IsZero() {
		used := t.LastUsedAt
		out.LastUsedAt = &used
	}

	if !t.RevokedAt.IsZero() {
		at, by := t.RevokedAt, t.RevokedBy
		out.RevokedAt, out.RevokedBy = &at, &by
	}

	return out
}
