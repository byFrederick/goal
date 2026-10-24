package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/byFrederick/goal/internal/adapters/postgres/db"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

var _ ports.Platform = (*Platform)(nil)

// Platform implements ports.Platform through 00045's definers: platform admins and the security
// log live outside every organization, so its transactions bind none.
type Platform struct {
	s *Store
}

// NewPlatform returns the install's platform admins, security log and clean-up on s's pool.
func NewPlatform(s *Store) *Platform { return &Platform{s: s} }

// Error codes the platform functions raise.
const (
	noDataFound      = "P0002"
	keepOneAdmin     = "platform_admins_keep_one"
	authEventsLimit  = 100
	maxAuthEventPage = 500
)

// adminRow is one entry of goal_platform_admins' jsonb.
type adminRow struct {
	UserID  domain.UserID `json:"user_id"`
	Name    string        `json:"name"`
	Email   string        `json:"email"`
	AddedBy string        `json:"added_by"`
	AddedAt time.Time     `json:"added_at"`
}

// PlatformAdmins implements ports.Platform.
func (p *Platform) PlatformAdmins(ctx context.Context) ([]domain.PlatformAdmin, error) {
	return platformAdminsOf(ctx, p.s.q)
}

// platformAdminsOf lists the platform admins, each adder named when they are an admin too.
func platformAdminsOf(ctx context.Context, q *db.Queries) ([]domain.PlatformAdmin, error) {
	raw, err := q.PlatformAdmins(ctx)
	if err != nil {
		return nil, fmt.Errorf("platform admins: %w", err)
	}

	var rows []adminRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("platform admins: decode: %w", err)
	}

	names := make(map[string]string, len(rows))
	for _, r := range rows {
		names[domain.Actor{Kind: domain.ActorUser, ID: string(r.UserID)}.Ref()] = r.Name
	}

	out := make([]domain.PlatformAdmin, 0, len(rows))

	for _, r := range rows {
		by, err := domain.ActorFromRef(r.AddedBy)
		if err != nil {
			return nil, fmt.Errorf("platform admin %s: %w", r.UserID, err)
		}

		if name, ok := names[r.AddedBy]; ok {
			by.Name = name
		}

		out = append(out, domain.PlatformAdmin{
			User: domain.User{ID: r.UserID, Name: r.Name, Email: r.Email}, AddedBy: by, AddedAt: r.AddedAt.UTC(),
		})
	}

	return out, nil
}

// AddPlatformAdmin implements ports.Platform: the admin is added and read back in one
// transaction.
func (p *Platform) AddPlatformAdmin(ctx context.Context, by domain.Actor, email string) (domain.PlatformAdmin, error) {
	var out domain.PlatformAdmin

	err := p.s.systemTx(ctx, func(q *db.Queries) error {
		raw, err := q.AddPlatformAdmin(ctx, db.AddPlatformAdminParams{Email: email, AddedBy: by.Ref()})
		if err != nil {
			return fmt.Errorf("add platform admin: %w", err)
		}

		if raw == nil {
			return fmt.Errorf("add platform admin: no user has that email: %w", ports.ErrNotFound)
		}

		var u struct {
			ID domain.UserID `json:"id"`
		}
		if err := json.Unmarshal(raw, &u); err != nil {
			return fmt.Errorf("add platform admin: decode: %w", err)
		}

		admins, err := platformAdminsOf(ctx, q)
		if err != nil {
			return err
		}

		for _, a := range admins {
			if a.User.ID == u.ID {
				out = a

				return nil
			}
		}

		return fmt.Errorf("add platform admin: %s is not listed after being added", u.ID)
	})

	return out, err
}

// RemovePlatformAdmin implements ports.Platform. The last admin is refused at commit, by
// platform_admins_keep_one, and the whole removal rolls back.
func (p *Platform) RemovePlatformAdmin(ctx context.Context, by domain.Actor, user domain.UserID) error {
	id, ok := userUUID(user)
	if !ok {
		return fmt.Errorf("user %s is not a platform admin: %w", user, ports.ErrNotFound)
	}

	err := p.s.systemTx(ctx, func(q *db.Queries) error {
		return q.RemovePlatformAdmin(ctx, db.RemovePlatformAdminParams{UserID: id, RemovedBy: by.Ref()})
	})

	pgErr, isPg := errors.AsType[*pgconn.PgError](err)

	switch {
	case err == nil:
		return nil
	case isPg && pgErr.Code == noDataFound:
		return fmt.Errorf("user %s is not a platform admin: %w", user, ports.ErrNotFound)
	case isPg && pgErr.Code == checkViolation && pgErr.ConstraintName == keepOneAdmin:
		return fmt.Errorf("user %s is the last platform admin: %w", user, ports.ErrConflict)
	default:
		return fmt.Errorf("remove platform admin: %w", err)
	}
}

// authEventRow is one entry of goal_auth_events' jsonb.
type authEventRow struct {
	ID        int64          `json:"id"`
	At        time.Time      `json:"at"`
	Kind      string         `json:"kind"`
	UserID    *string        `json:"user_id"`
	Subject   *string        `json:"subject"`
	IP        *string        `json:"ip"`
	UserAgent *string        `json:"user_agent"`
	Data      map[string]any `json:"data"`
}

// AuthEvents implements ports.Platform. A limit of 0 is the default page, 100 events.
func (p *Platform) AuthEvents(ctx context.Context, q ports.AuthEventQuery) ([]domain.AuthEvent, error) {
	limit := int32(authEventsLimit)
	if q.Limit > 0 {
		limit = int32(min(q.Limit, maxAuthEventPage))
	}

	params := db.AuthEventsParams{PageLimit: &limit}
	if q.Before > 0 {
		params.Before = &q.Before
	}

	if q.Kind != "" {
		params.Kind = &q.Kind
	}

	raw, err := p.s.q.AuthEvents(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("auth events: %w", err)
	}

	var rows []authEventRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("auth events: decode: %w", err)
	}

	out := make([]domain.AuthEvent, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.AuthEvent{
			ID: r.ID, At: r.At.UTC(), Kind: r.Kind, User: domain.UserID(deref(r.UserID)), Subject: deref(r.Subject),
			IP: deref(r.IP), UserAgent: deref(r.UserAgent), Data: r.Data,
		})
	}

	return out, nil
}

// CleanUpAuth implements ports.Platform: in one transaction holding the clean-up's advisory
// lock, which another instance's clean-up finds taken and skips.
func (p *Platform) CleanUpAuth(ctx context.Context, now time.Time) (ports.AuthCleanUp, error) {
	var out ports.AuthCleanUp

	err := p.s.systemTx(ctx, func(q *db.Queries) error {
		locked, err := q.TryAuthCleanUpLock(ctx)
		if err != nil {
			return err
		}

		if !locked {
			out.Skipped = true

			return nil
		}

		raw, err := q.CleanUpAuth(ctx, now)
		if err != nil {
			return err
		}

		var counts struct {
			Sessions      int64 `json:"sessions"`
			Verifications int64 `json:"verifications"`
			DeviceCodes   int64 `json:"device_codes"`
			RateLimits    int64 `json:"rate_limits"`
			AuthEvents    int64 `json:"auth_events"`
		}
		if err := json.Unmarshal(raw, &counts); err != nil {
			return fmt.Errorf("decode: %w", err)
		}

		out = ports.AuthCleanUp{
			Sessions: counts.Sessions, Verifications: counts.Verifications, DeviceCodes: counts.DeviceCodes,
			RateLimits: counts.RateLimits, AuthEvents: counts.AuthEvents,
		}

		return nil
	})
	if err != nil {
		return ports.AuthCleanUp{}, fmt.Errorf("clean up sign-in: %w", err)
	}

	return out, nil
}
