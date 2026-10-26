package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.Access = (*Access)(nil)

// Access implements ports.Access: the organization's API keys over 00046's api_keys and
// api_key_factories, as goal_app under row-level security (every statement sees the organization
// in ctx only, and every change appends its audit entry in its transaction), and its people
// (members.go).
type Access struct {
	s *Store
}

// NewAccess returns the organization's people and keys on s's pool.
func NewAccess(s *Store) *Access { return &Access{s: s} }

// auditFactories is the audit data key of a key's factories.
const auditFactories = "factories"

// apiKeyNameKey keeps a live key's name unique in its organization (case ignored).
const apiKeyNameKey = "api_keys_name_key" //nolint:gosec // an index name, not a credential

// APIKeys implements ports.Access.
func (a *Access) APIKeys(ctx context.Context) ([]domain.APIKey, error) {
	var rows []db.APIKeysRow

	err := a.s.read(ctx, func(q *db.Queries) error {
		var err error

		rows, err = q.APIKeys(ctx)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("api keys: %w", err)
	}

	out := make([]domain.APIKey, 0, len(rows))

	for _, r := range rows {
		k := domain.APIKey{
			ID: domain.APIKeyID(r.ID.String()), Name: r.Name, Hint: r.Hint, Scopes: permissions(r.Scopes),
			Ceiling: domain.FactoryRole(r.RoleCeiling), CreatedBy: namedActor(r.CreatedBy, r.CreatedByName),
			CreatedAt: r.CreatedAt.UTC(), ExpiresAt: r.ExpiresAt.UTC(), LastUsedAt: utc(r.LastUsedAt), RevokedAt: utc(r.RevokedAt),
		}

		if !r.AllFactories {
			k.Factories = make([]domain.FactoryID, 0, len(r.FactoryIds))
			for _, f := range r.FactoryIds {
				k.Factories = append(k.Factories, domain.FactoryID(f.String()))
			}
		}

		if r.RevokedBy != nil {
			by := namedActor(*r.RevokedBy, r.RevokedByName)
			k.RevokedBy = &by
		}

		out = append(out, k)
	}

	return out, nil
}

// CreateAPIKey implements ports.Access.
func (a *Access) CreateAPIKey(ctx context.Context, by domain.Actor, k domain.NewAPIKey) (domain.APIKey, error) {
	if k.ExpiresInDays < 1 || k.ExpiresInDays > domain.MaxAPIKeyDays {
		return domain.APIKey{}, fmt.Errorf("create api key: expires in %d days, not 1 to %d: %w", k.ExpiresInDays, domain.MaxAPIKeyDays, domain.ErrInvalid)
	}

	id, err := newID()
	if err != nil {
		return domain.APIKey{}, err
	}

	factories := make([]uuid.UUID, 0, len(k.Factories))

	for _, f := range k.Factories {
		fid, err := uuid.Parse(string(f))
		if err != nil {
			return domain.APIKey{}, fmt.Errorf("create api key: factory %q: %w", f, ports.ErrNotFound)
		}

		factories = append(factories, fid)
	}

	scopes := make([]string, 0, len(k.Scopes))
	for _, p := range k.Scopes {
		scopes = append(scopes, string(p))
	}

	out := domain.APIKey{
		ID: domain.APIKeyID(id.String()), Name: k.Name, Hint: k.Hint, Scopes: slices.Clone(k.Scopes), Ceiling: k.Ceiling,
		Factories: slices.Clone(k.Factories), CreatedBy: by,
	}

	err = a.s.inTx(ctx, func(q *db.Queries) error {
		row, err := q.InsertAPIKey(ctx, db.InsertAPIKeyParams{
			ID: id, Name: k.Name, Hint: k.Hint, Hash: k.Hash, Scopes: scopes, RoleCeiling: string(k.Ceiling),
			AllFactories: k.Factories == nil, CreatedBy: by.Ref(), ExpiresInDays: int32(min(max(k.ExpiresInDays, 1), domain.MaxAPIKeyDays)),
		})
		if err != nil {
			return err
		}

		out.CreatedAt, out.ExpiresAt = row.CreatedAt.UTC(), row.ExpiresAt.UTC()

		if k.Factories != nil {
			if err := q.InsertAPIKeyFactories(ctx, db.InsertAPIKeyFactoriesParams{KeyID: id, FactoryIds: factories}); err != nil {
				return err
			}
		}

		return appendAudit(ctx, q, domain.AuditEntry{
			Actor: by, Action: domain.AuditAPIKeyCreated, TargetKind: "api_key", TargetID: id.String(),
			Data: map[string]any{
				"key_id": id.String(), "name": k.Name, "scopes": scopes, "role_ceiling": string(k.Ceiling),
				auditFactories: factoryData(k.Factories), "expires_at": out.ExpiresAt,
			},
		})
	})
	if err != nil {
		return domain.APIKey{}, fmt.Errorf("create api key: %w", keyError(err))
	}

	return out, nil
}

// RevokeAPIKey implements ports.Access.
func (a *Access) RevokeAPIKey(ctx context.Context, by domain.Actor, id domain.APIKeyID) error {
	kid, err := uuid.Parse(string(id))
	if err != nil {
		return fmt.Errorf("revoke api key %q: %w", id, ports.ErrNotFound)
	}

	err = a.s.inTx(ctx, func(q *db.Queries) error {
		ref := by.Ref()

		row, err := q.RevokeAPIKey(ctx, db.RevokeAPIKeyParams{ID: kid, RevokedBy: &ref})
		if notFound(err) {
			return ports.ErrNotFound
		}

		if err != nil || !row.Revoked {
			return err
		}

		return appendAudit(ctx, q, domain.AuditEntry{
			Actor: by, Action: domain.AuditAPIKeyRevoked, TargetKind: "api_key", TargetID: kid.String(),
			Data: map[string]any{"key_id": kid.String(), "name": row.Name},
		})
	})
	if err != nil {
		return fmt.Errorf("revoke api key %s: %w", id, err)
	}

	return nil
}

// keyError names the rules a new key broke: a live key's name, a factory of no such id.
func keyError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}

	switch {
	case pgErr.Code == uniqueViolation && pgErr.ConstraintName == apiKeyNameKey:
		return domain.ErrAPIKeyNameTaken
	case pgErr.Code == foreignKeyViolation:
		return fmt.Errorf("%s: %w", pgErr.ConstraintName, ports.ErrNotFound)
	default:
		return ruleError(err, "")
	}
}

// factoryData is a key's factories as its audit entry records them: null for every factory.
func factoryData(fs []domain.FactoryID) any {
	if fs == nil {
		return nil
	}

	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, string(f))
	}

	return out
}

func permissions(ps []string) []domain.Permission {
	out := make([]domain.Permission, 0, len(ps))
	for _, p := range ps {
		out = append(out, domain.Permission(p))
	}

	return out
}

// namedActor is the actor ref names, called name when its audit entry gave one.
func namedActor(ref, name string) domain.Actor {
	a, err := domain.ActorFromRef(ref)
	if err != nil {
		return domain.Actor{Kind: domain.ActorSystem, ID: ref, Name: ref}
	}

	if name != "" {
		a.Name = name
	}

	return a
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}

	u := t.UTC()

	return &u
}
