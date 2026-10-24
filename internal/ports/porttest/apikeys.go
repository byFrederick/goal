package porttest

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

var _ ports.Access = (*Identity)(nil)

// orgDeleted revokes the keys of a deleted organization, as 00046's retire trigger stamps them.
var orgDeleted = domain.Actor{Kind: domain.ActorSystem, ID: "org-deleted", Name: "org-deleted"}

// keyListedFor is how long a revoked key stays listed.
const keyListedFor = 30 * 24 * time.Hour

// APIKeys implements ports.Access for the organization in ctx.
func (i *Identity) APIKeys(ctx context.Context) ([]domain.APIKey, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, domain.ErrNoOrg
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	now := i.now()
	out := []domain.APIKey{}

	for _, k := range i.keys {
		if k.Org == org && (k.RevokedAt.IsZero() || now.Sub(k.RevokedAt) < keyListedFor) {
			out = append(out, keyOut(k))
		}
	}

	slices.SortFunc(out, func(a, b domain.APIKey) int {
		return cmp.Or(cmp.Compare(boolInt(a.RevokedAt != nil), boolInt(b.RevokedAt != nil)), b.CreatedAt.Compare(a.CreatedAt),
			cmp.Compare(b.ID, a.ID))
	})

	return out, nil
}

// CreateAPIKey implements ports.Access for the organization in ctx, which the fake must know
// (AddOrg), as must each factory (AddFactory).
func (i *Identity) CreateAPIKey(ctx context.Context, by domain.Actor, n domain.NewAPIKey) (domain.APIKey, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.APIKey{}, domain.ErrNoOrg
	}

	if n.ExpiresInDays < 1 || n.ExpiresInDays > domain.MaxAPIKeyDays {
		return domain.APIKey{}, fmt.Errorf("expires in %d days: %w", n.ExpiresInDays, domain.ErrInvalid)
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	o, ok := i.orgs[org]
	if !ok {
		return domain.APIKey{}, fmt.Errorf("organization %s: %w", org, ports.ErrNotFound)
	}

	for _, f := range n.Factories {
		if !slices.Contains(o.factories, f) {
			return domain.APIKey{}, fmt.Errorf("factory %s: %w", f, ports.ErrNotFound)
		}
	}

	for _, k := range i.keys {
		if k.Org == org && k.RevokedAt.IsZero() && strings.EqualFold(k.Name, n.Name) {
			return domain.APIKey{}, domain.ErrAPIKeyNameTaken
		}
	}

	if _, taken := i.keys[n.Hash]; taken {
		return domain.APIKey{}, fmt.Errorf("hash taken: %w", ports.ErrConflict)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return domain.APIKey{}, err
	}

	now := i.now()
	k := &IdentityKey{
		ID: domain.APIKeyID(id.String()), Org: org, Name: n.Name, Hint: n.Hint, Scopes: slices.Clone(n.Scopes),
		Factories: slices.Clone(n.Factories), Ceiling: n.Ceiling, CreatedBy: by, CreatedAt: now,
		ExpiresAt: now.Add(time.Duration(n.ExpiresInDays) * 24 * time.Hour),
	}
	i.keys[n.Hash] = k

	return keyOut(k), nil
}

// RevokeAPIKey implements ports.Access for the organization in ctx.
func (i *Identity) RevokeAPIKey(ctx context.Context, by domain.Actor, id domain.APIKeyID) error {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.ErrNoOrg
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	for _, k := range i.keys {
		if k.ID == id && k.Org == org {
			if k.RevokedAt.IsZero() {
				k.RevokedAt, k.RevokedBy = i.now(), by
			}

			return nil
		}
	}

	return fmt.Errorf("api key %s: %w", id, ports.ErrNotFound)
}

// ExpireKey makes the key with id expired a day ago.
func (i *Identity) ExpireKey(id domain.APIKeyID) {
	i.mu.Lock()
	defer i.mu.Unlock()

	for _, k := range i.keys {
		if k.ID == id {
			k.ExpiresAt = i.now().Add(-24 * time.Hour)
		}
	}
}

func keyOut(k *IdentityKey) domain.APIKey {
	out := domain.APIKey{
		ID: k.ID, Name: k.Name, Hint: k.Hint, Scopes: slices.Clone(k.Scopes), Ceiling: k.Ceiling, Factories: slices.Clone(k.Factories),
		CreatedBy: k.CreatedBy, CreatedAt: k.CreatedAt, ExpiresAt: k.ExpiresAt,
	}

	if !k.LastUsedAt.IsZero() {
		used := k.LastUsedAt
		out.LastUsedAt = &used
	}

	if !k.RevokedAt.IsZero() {
		at, by := k.RevokedAt, k.RevokedBy
		out.RevokedAt, out.RevokedBy = &at, &by
	}

	return out
}

func boolInt(b bool) int {
	if b {
		return 1
	}

	return 0
}
