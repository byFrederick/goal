package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/byFrederick/goal/internal/adapters/postgres/db"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

var _ ports.Identity = (*Identity)(nil)

// Identity implements ports.Identity through 00045's definers, which read Better Auth's rows in
// schema auth for goal_app (it has no USAGE there). Each lookup is one statement against the
// database, never cached: a removal, a role change or a sign-out holds on the next request.
type Identity struct {
	q *db.Queries
}

// NewIdentity returns the identity lookups on s's pool.
func NewIdentity(s *Store) *Identity { return &Identity{q: s.q} }

// cliClient is the device authorization client the auth service accepts (apps/auth/src/auth.ts), and
// the name the person approving it is shown.
const (
	cliClient     = "goal-cli"
	cliClientName = "Goal CLI"
)

// principalRow is goal_principal's jsonb.
type principalRow struct {
	UserID        string    `json:"user_id"`
	Name          string    `json:"name"`
	Email         string    `json:"email"`
	SessionID     string    `json:"session_id"`
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	SignInMethod  *string   `json:"sign_in_method"`
	PlatformAdmin bool      `json:"platform_admin"`
	OrgRole       *string   `json:"org_role"`
	FactoryRoles  []struct {
		FactoryID string `json:"factory_id"`
		Role      string `json:"role"`
	} `json:"factory_roles"`
}

// Session implements ports.Identity. An org that is not a UUID names no organization: the
// session resolves without a role.
func (i *Identity) Session(ctx context.Context, token string, org domain.OrgID) (domain.Principal, error) {
	var orgID *uuid.UUID
	if id, err := uuid.Parse(string(org)); err == nil {
		orgID = &id
	}

	raw, err := i.q.Principal(ctx, db.PrincipalParams{Token: token, OrgID: orgID})
	if err != nil {
		return domain.Principal{}, fmt.Errorf("session: %w", err)
	}

	if raw == nil {
		return domain.Principal{}, fmt.Errorf("session: %w", domain.ErrUnauthenticated)
	}

	var r principalRow
	if err := json.Unmarshal(raw, &r); err != nil {
		return domain.Principal{}, fmt.Errorf("session: decode: %w", err)
	}

	p := domain.Principal{
		Kind: domain.PrincipalUser, User: domain.UserID(r.UserID), Name: r.Name, Email: r.Email, PlatformAdmin: r.PlatformAdmin,
		Session: &domain.Session{
			ID: r.SessionID, CreatedAt: r.CreatedAt.UTC(), ExpiresAt: r.ExpiresAt.UTC(), Browser: browserSignIn(r.SignInMethod),
		},
		Org: org,
	}

	if r.OrgRole != nil {
		p.OrgRole = domain.OrgRole(*r.OrgRole)
		p.FactoryRoles = make(map[domain.FactoryID]domain.FactoryRole, len(r.FactoryRoles))

		for _, fr := range r.FactoryRoles {
			p.FactoryRoles[domain.FactoryID(fr.FactoryID)] = domain.FactoryRole(fr.Role)
		}
	}

	return p, nil
}

// browserSignIn is whether a session's "signInMethod" (00048, written by the auth service) is a
// sign-in through the browser: an email code or Google. goal login's device flow, other and an
// untagged session are not.
func browserSignIn(method *string) bool {
	return method != nil && (*method == "email_code" || *method == "google")
}

// apiKeyRow is goal_api_key's jsonb.
type apiKeyRow struct {
	ID          string    `json:"id"`
	OrgID       string    `json:"org_id"`
	Name        string    `json:"name"`
	Scopes      []string  `json:"scopes"`
	RoleCeiling string    `json:"role_ceiling"`
	Factories   *[]string `json:"factories"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// APIKey implements ports.Identity through 00046's goal_api_key: a service principal of the key's
// own organization with its scopes, factories (nil: every one), ceiling and expiry.
func (i *Identity) APIKey(ctx context.Context, hash string) (domain.Principal, error) {
	raw, err := i.q.APIKey(ctx, hash)
	if err != nil {
		return domain.Principal{}, fmt.Errorf("api key: %w", err)
	}

	if raw == nil {
		return domain.Principal{}, fmt.Errorf("api key: %w", domain.ErrUnauthenticated)
	}

	var r apiKeyRow
	if err := json.Unmarshal(raw, &r); err != nil {
		return domain.Principal{}, fmt.Errorf("api key: decode: %w", err)
	}

	p := domain.Principal{
		Kind: domain.PrincipalService, Key: domain.APIKeyID(r.ID), Name: r.Name, Org: domain.OrgID(r.OrgID),
		Scopes: permissions(r.Scopes), KeyCeiling: domain.FactoryRole(r.RoleCeiling), KeyExpiresAt: r.ExpiresAt.UTC(),
	}

	if r.Factories != nil {
		p.KeyFactories = make([]domain.FactoryID, 0, len(*r.Factories))
		for _, f := range *r.Factories {
			p.KeyFactories = append(p.KeyFactories, domain.FactoryID(f))
		}
	}

	return p, nil
}

// orgsForRow is one entry of goal_orgs_for's jsonb.
type orgsForRow struct {
	ID           uuid.UUID `json:"id"`
	Slug         string    `json:"slug"`
	Name         string    `json:"name"`
	Mark         string    `json:"mark"`
	MaxSlots     *int32    `json:"max_slots"`
	CreatedAt    time.Time `json:"created_at"`
	Role         string    `json:"role"`
	FactoryCount int       `json:"factory_count"`
}

// OrgsFor implements ports.Identity.
func (i *Identity) OrgsFor(ctx context.Context, user domain.UserID) ([]domain.OrgMembership, error) {
	id, ok := userUUID(user)
	if !ok {
		return []domain.OrgMembership{}, nil // not a Better Auth user (the local user): in no organization
	}

	raw, err := i.q.OrgsFor(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("orgs for %s: %w", user, err)
	}

	var rows []orgsForRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("orgs for %s: decode: %w", user, err)
	}

	out := make([]domain.OrgMembership, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.OrgMembership{
			Org: orgOf(r.ID, r.Slug, r.Name, r.Mark, r.MaxSlots, r.CreatedAt), Role: domain.OrgRole(r.Role), FactoryCount: r.FactoryCount,
		})
	}

	return out, nil
}

// userUUID is a Better Auth user's id as a UUID; false for any other user (the local user).
func userUUID(u domain.UserID) (uuid.UUID, bool) {
	id, err := uuid.Parse(string(u))

	return id, err == nil
}

// deviceRow is goal_device_request's jsonb.
type deviceRow struct {
	ClientID    string     `json:"client_id"`
	ExpiresAt   time.Time  `json:"expires_at"`
	RequestedAt *time.Time `json:"requested_at"`
	IP          *string    `json:"ip"`
	UserAgent   *string    `json:"user_agent"`
}

// DeviceRequest implements ports.Identity.
func (i *Identity) DeviceRequest(ctx context.Context, userCode string) (domain.DeviceRequest, error) {
	raw, err := i.q.DeviceRequest(ctx, userCode)
	if err != nil {
		return domain.DeviceRequest{}, fmt.Errorf("device request: %w", err)
	}

	if raw == nil {
		return domain.DeviceRequest{}, fmt.Errorf("device request: %w", ports.ErrNotFound)
	}

	var r deviceRow
	if err := json.Unmarshal(raw, &r); err != nil {
		return domain.DeviceRequest{}, fmt.Errorf("device request: decode: %w", err)
	}

	d := domain.DeviceRequest{ClientID: r.ClientID, ClientName: r.ClientID, ExpiresAt: r.ExpiresAt.UTC(), IP: deref(r.IP), UserAgent: deref(r.UserAgent)}
	if r.ClientID == cliClient {
		d.ClientName = cliClientName
	}

	if r.RequestedAt != nil {
		d.RequestedAt = r.RequestedAt.UTC()
	}

	return d, nil
}

// ErrIdentityNotReady is CheckIdentity's refusal: the database lacks what GOAL_AUTH=session needs.
var ErrIdentityNotReady = errors.New("the database is not ready for sign-in")

// CheckIdentity is goald's startup self-check under GOAL_AUTH=session (spec 3 §8.3): Better
// Auth's tables and phase 3's definer functions exist and goal_app cannot use schema auth.
func CheckIdentity(ctx context.Context, s *Store) error {
	ready, err := s.q.IdentityReady(ctx)
	if err != nil {
		return fmt.Errorf("identity check: %w", err)
	}

	if !ready {
		return fmt.Errorf("%w: schema auth, its tables or phase 3's functions are missing, or goal_app may use schema auth", ErrIdentityNotReady)
	}

	return nil
}

// AnyoneSignedIn reports whether the install has its first platform admin, which the bootstrap
// owner becomes at their first sign-in (00044's goal_claim_bootstrap): before it, nobody but the
// bootstrap owner can sign in.
func AnyoneSignedIn(ctx context.Context, s *Store) (bool, error) {
	admins, err := platformAdmins(ctx, s.q)

	return len(admins) > 0, err
}

// UsersExist reports whether anyone has a Better Auth user yet, as goal up asks it before sign-in
// starts. pool connects as the database's administrator (goal db init's), since goal_app cannot
// read schema auth; a database goald has not migrated yet has nobody.
func UsersExist(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	var exist bool

	err := pool.QueryRow(ctx, `SELECT to_regclass('auth."user"') IS NOT NULL`).Scan(&exist)
	if err != nil || !exist {
		return false, err
	}

	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM auth."user")`).Scan(&exist); err != nil {
		return false, fmt.Errorf("users: %w", err)
	}

	return exist, nil
}

// accessChannel is where 00043's goal_notify_access() notifies.
const accessChannel = "goal_access"

var _ ports.AccessNotices = (*AccessNotices)(nil)

// AccessNotices implements ports.AccessNotices with LISTEN goal_access on a connection of its own.
// A notice is not read back: it only makes listeners end what it names, which is safe to do for
// a forged one (the client reconnects and is checked again).
type AccessNotices struct {
	l listener[ports.AccessNotice]
}

// NewAccessNotices listens with the connection config of s's pool. log may be nil.
func NewAccessNotices(s *Store, log *slog.Logger) *AccessNotices {
	return &AccessNotices{l: listener[ports.AccessNotice]{
		cfg: s.pool.Config().ConnConfig, log: discardIfNil(log), channels: []string{accessChannel}, name: "access notices",
		resync: ports.AccessNotice{Resync: true}, decode: accessNoticeFrom,
		readBack: func(_ context.Context, n ports.AccessNotice) (ports.AccessNotice, bool, error) { return n, true, nil },
	}}
}

// Listen implements ports.AccessNotices.
func (n *AccessNotices) Listen(ctx context.Context, deliver func(ports.AccessNotice)) error {
	return n.l.Listen(ctx, deliver)
}

// accessPayload is what goal_notify_access() sends.
type accessPayload struct {
	Kind      string `json:"kind"`
	OrgID     string `json:"org_id"`
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id"`
	FactoryID string `json:"factory_id"`
	KeyID     string `json:"key_id"`
	TokenID   string `json:"token_id"`
}

func accessNoticeFrom(_, raw string) (ports.AccessNotice, error) {
	var p accessPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return ports.AccessNotice{}, fmt.Errorf("decode access notice: %w", err)
	}

	if p.Kind == "" {
		return ports.AccessNotice{}, errors.New("access notice without a kind")
	}

	return ports.AccessNotice{
		Kind: p.Kind, OrgID: domain.OrgID(p.OrgID), UserID: domain.UserID(p.UserID), SessionID: p.SessionID,
		FactoryID: domain.FactoryID(p.FactoryID), KeyID: domain.APIKeyID(p.KeyID), TokenID: domain.PersonalTokenID(p.TokenID),
	}, nil
}
