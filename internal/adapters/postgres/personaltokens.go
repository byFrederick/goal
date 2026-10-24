package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/byFrederick/goal/internal/adapters/postgres/db"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// personalTokenNameKey keeps a live token's name unique among its owner's (case ignored).
const personalTokenNameKey = "personal_tokens_name_key"

// auditTargetPersonalToken is the audit target kind of a personal token's entries.
const auditTargetPersonalToken = "personal_token"

// personalTokenRow is goal_personal_token's jsonb.
type personalTokenRow struct {
	ID           string    `json:"id"`
	OrgID        string    `json:"org_id"`
	UserID       string    `json:"user_id"`
	Name         string    `json:"name"`
	Email        string    `json:"email"`
	TokenName    string    `json:"token_name"`
	Scopes       []string  `json:"scopes"`
	ExpiresAt    time.Time `json:"expires_at"`
	OrgRole      string    `json:"org_role"`
	FactoryRoles []struct {
		FactoryID string `json:"factory_id"`
		Role      string `json:"role"`
	} `json:"factory_roles"`
}

// PersonalToken implements ports.Identity through 00047's goal_personal_token: the owner as a
// user principal of the token's organization, with the token's grant and the owner's current
// roles there; never a session, never platform admin.
func (i *Identity) PersonalToken(ctx context.Context, hash string) (domain.Principal, error) {
	raw, err := i.q.PersonalToken(ctx, hash)
	if err != nil {
		return domain.Principal{}, fmt.Errorf("personal token: %w", err)
	}

	if raw == nil {
		return domain.Principal{}, fmt.Errorf("personal token: %w", domain.ErrUnauthenticated)
	}

	var r personalTokenRow
	if err := json.Unmarshal(raw, &r); err != nil {
		return domain.Principal{}, fmt.Errorf("personal token: decode: %w", err)
	}

	p := domain.Principal{
		Kind: domain.PrincipalUser, User: domain.UserID(r.UserID), Name: r.Name, Email: r.Email, Org: domain.OrgID(r.OrgID),
		OrgRole: domain.OrgRole(r.OrgRole), FactoryRoles: make(map[domain.FactoryID]domain.FactoryRole, len(r.FactoryRoles)),
		Token: &domain.TokenGrant{
			ID: domain.PersonalTokenID(r.ID), Name: r.TokenName, Scopes: permissions(r.Scopes), ExpiresAt: r.ExpiresAt.UTC(),
		},
	}

	for _, fr := range r.FactoryRoles {
		p.FactoryRoles[domain.FactoryID(fr.FactoryID)] = domain.FactoryRole(fr.Role)
	}

	return p, nil
}

// PersonalTokens implements ports.Access.
func (a *Access) PersonalTokens(ctx context.Context, owner *domain.UserID) ([]domain.PersonalToken, error) {
	who, ok := ownerFilter(owner)
	if !ok {
		return []domain.PersonalToken{}, nil
	}

	var rows []db.PersonalTokensRow

	err := a.s.read(ctx, func(q *db.Queries) error {
		var err error

		rows, err = q.PersonalTokens(ctx, who)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("personal tokens: %w", err)
	}

	out := make([]domain.PersonalToken, 0, len(rows))

	for _, r := range rows {
		t := domain.PersonalToken{
			ID: domain.PersonalTokenID(r.ID.String()), Name: r.Name, Hint: r.Hint, Scopes: permissions(r.Scopes),
			Owner:     domain.User{ID: domain.UserID(r.UserID.String()), Name: r.OwnerName, Email: r.OwnerEmail},
			CreatedAt: r.CreatedAt.UTC(), ExpiresAt: r.ExpiresAt.UTC(), LastUsedAt: utc(r.LastUsedAt), RevokedAt: utc(r.RevokedAt),
		}

		if r.RevokedBy != nil {
			by := namedActor(*r.RevokedBy, r.RevokedByName)
			t.RevokedBy = &by
		}

		out = append(out, t)
	}

	return out, nil
}

// CreatePersonalToken implements ports.Access.
func (a *Access) CreatePersonalToken(ctx context.Context, by domain.Actor, t domain.NewPersonalToken) (domain.PersonalToken, error) {
	if t.ExpiresInDays < 1 || t.ExpiresInDays > domain.MaxPersonalTokenDays {
		return domain.PersonalToken{}, fmt.Errorf("create personal token: expires in %d days, not 1 to %d: %w",
			t.ExpiresInDays, domain.MaxPersonalTokenDays, domain.ErrInvalid)
	}

	owner, err := uuid.Parse(string(t.Owner))
	if err != nil {
		return domain.PersonalToken{}, fmt.Errorf("create personal token: owner %q: %w", t.Owner, ports.ErrNotFound)
	}

	id, err := newID()
	if err != nil {
		return domain.PersonalToken{}, err
	}

	scopes := make([]string, 0, len(t.Scopes))
	for _, p := range t.Scopes {
		scopes = append(scopes, string(p))
	}

	out := domain.PersonalToken{
		ID: domain.PersonalTokenID(id.String()), Name: t.Name, Hint: t.Hint, Scopes: slices.Clone(t.Scopes),
		Owner: domain.User{ID: t.Owner},
	}

	err = a.s.inTx(ctx, func(q *db.Queries) error {
		row, err := q.InsertPersonalToken(ctx, db.InsertPersonalTokenParams{
			ID: id, UserID: owner, Name: t.Name, Hint: t.Hint, Hash: t.Hash, Scopes: scopes,
			ExpiresInDays: int32(min(max(t.ExpiresInDays, 1), domain.MaxPersonalTokenDays)),
		})
		if err != nil {
			return err
		}

		out.CreatedAt, out.ExpiresAt = row.CreatedAt.UTC(), row.ExpiresAt.UTC()

		return appendAudit(ctx, q, domain.AuditEntry{
			Actor: by, Action: domain.AuditPersonalTokenCreated, TargetKind: auditTargetPersonalToken, TargetID: id.String(),
			Data: map[string]any{
				"token_id": id.String(), auditName: t.Name, "user_id": owner.String(), "scopes": scopes, "expires_at": out.ExpiresAt,
			},
		})
	})
	if err != nil {
		return domain.PersonalToken{}, fmt.Errorf("create personal token: %w", personalTokenError(err))
	}

	return out, nil
}

// RevokePersonalToken implements ports.Access.
func (a *Access) RevokePersonalToken(ctx context.Context, by domain.Actor, id domain.PersonalTokenID, owner *domain.UserID) error {
	tid, err := uuid.Parse(string(id))
	if err != nil {
		return fmt.Errorf("revoke personal token %q: %w", id, ports.ErrNotFound)
	}

	who, ok := ownerFilter(owner)
	if !ok {
		return fmt.Errorf("revoke personal token %q: %w", id, ports.ErrNotFound)
	}

	err = a.s.inTx(ctx, func(q *db.Queries) error {
		ref := by.Ref()

		row, err := q.RevokePersonalToken(ctx, db.RevokePersonalTokenParams{ID: tid, Owner: who, RevokedBy: &ref})
		if notFound(err) {
			return ports.ErrNotFound
		}

		if err != nil || !row.Revoked {
			return err
		}

		return appendAudit(ctx, q, domain.AuditEntry{
			Actor: by, Action: domain.AuditPersonalTokenRevoked, TargetKind: auditTargetPersonalToken, TargetID: tid.String(),
			Data: map[string]any{"token_id": tid.String(), auditName: row.Name, "user_id": row.UserID.String()},
		})
	})
	if err != nil {
		return fmt.Errorf("revoke personal token %s: %w", id, err)
	}

	return nil
}

// ownerFilter is an owner filter as the statements take it: nil for every member's; ok false for
// an owner that is not a user id, who has no tokens.
func ownerFilter(owner *domain.UserID) (*uuid.UUID, bool) {
	if owner == nil {
		return nil, true
	}

	id, err := uuid.Parse(string(*owner))
	if err != nil {
		return nil, false
	}

	return &id, true
}

// personalTokenError names the rules a new token broke: a live token's name, an owner who is not
// a member.
func personalTokenError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}

	switch {
	case pgErr.Code == uniqueViolation && pgErr.ConstraintName == personalTokenNameKey:
		return domain.ErrPersonalTokenNameTaken
	case pgErr.Code == foreignKeyViolation:
		return fmt.Errorf("%s: %w", pgErr.ConstraintName, ports.ErrNotFound)
	default:
		return ruleError(err, "")
	}
}
