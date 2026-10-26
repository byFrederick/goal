package porttest

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.Platform = (*Platform)(nil)

// Retention of the security log and grace of expired sessions, as goal_auth_cleanup (00045).
const (
	authEventsKept  = 90 * 24 * time.Hour
	expiredSessions = 24 * time.Hour
	authEventsPage  = 100
	authEventsMax   = 500
)

// Platform keeps the platform admins of an Identity's people and the security log in memory,
// with the database's rules: admins by email (case-insensitive), the last one stays
// (ErrConflict), every change is an event, and the clean-up forgets old sessions and events.
// Admins seeded with Identity.AddUser come first, by id, with no adder.
type Platform struct {
	id     *Identity
	added  map[domain.UserID]addedAdmin
	events []domain.AuthEvent
	lastID int64
}

type addedAdmin struct {
	by domain.Actor
	at time.Time
}

// NewPlatform returns the platform of id's people, sharing its lock and clock.
func NewPlatform(id *Identity) *Platform {
	return &Platform{id: id, added: map[domain.UserID]addedAdmin{}}
}

// PlatformAdmins implements ports.Platform.
func (p *Platform) PlatformAdmins(context.Context) ([]domain.PlatformAdmin, error) {
	p.id.mu.Lock()
	defer p.id.mu.Unlock()

	out := []domain.PlatformAdmin{}

	for _, u := range p.id.users {
		if u.PlatformAdmin {
			a := p.added[u.ID]
			out = append(out, domain.PlatformAdmin{User: domain.User{ID: u.ID, Name: u.Name, Email: u.Email}, AddedBy: a.by, AddedAt: a.at})
		}
	}

	slices.SortFunc(out, func(a, b domain.PlatformAdmin) int {
		return cmp.Or(a.AddedAt.Compare(b.AddedAt), cmp.Compare(a.User.ID, b.User.ID))
	})

	return out, nil
}

// AddPlatformAdmin implements ports.Platform.
func (p *Platform) AddPlatformAdmin(_ context.Context, by domain.Actor, email string) (domain.PlatformAdmin, error) {
	p.id.mu.Lock()
	defer p.id.mu.Unlock()

	for _, id := range slices.Sorted(maps.Keys(p.id.users)) {
		u := p.id.users[id]
		if !strings.EqualFold(u.Email, strings.TrimSpace(email)) {
			continue
		}

		if !u.PlatformAdmin {
			u.PlatformAdmin = true
			p.added[u.ID] = addedAdmin{by: by, at: p.id.now()}
			p.record("platform_admin.added", u.ID, by)
		}

		a := p.added[u.ID]

		return domain.PlatformAdmin{User: domain.User{ID: u.ID, Name: u.Name, Email: u.Email}, AddedBy: a.by, AddedAt: a.at}, nil
	}

	return domain.PlatformAdmin{}, fmt.Errorf("platform admin %q: no such user: %w", email, ports.ErrNotFound)
}

// RemovePlatformAdmin implements ports.Platform.
func (p *Platform) RemovePlatformAdmin(_ context.Context, by domain.Actor, user domain.UserID) error {
	p.id.mu.Lock()
	defer p.id.mu.Unlock()

	u, ok := p.id.users[user]
	if !ok || !u.PlatformAdmin {
		return fmt.Errorf("user %s is not a platform admin: %w", user, ports.ErrNotFound)
	}

	if p.id.platformAdmins() == 1 {
		return fmt.Errorf("user %s is the last platform admin: %w", user, ports.ErrConflict)
	}

	u.PlatformAdmin = false

	delete(p.added, user)
	p.record("platform_admin.removed", user, by)

	return nil
}

// record appends an event about user by by; the caller holds the lock.
func (p *Platform) record(kind string, user domain.UserID, by domain.Actor) {
	p.lastID++
	p.events = append(p.events, domain.AuthEvent{
		ID: p.lastID, At: p.id.now(), Kind: kind, User: user, Data: map[string]any{"by": by.Ref()},
	})
}

// AuthEvents implements ports.Platform.
func (p *Platform) AuthEvents(_ context.Context, q ports.AuthEventQuery) ([]domain.AuthEvent, error) {
	p.id.mu.Lock()
	defer p.id.mu.Unlock()

	limit := q.Limit
	if limit <= 0 {
		limit = authEventsPage
	}

	limit = min(limit, authEventsMax)
	out := []domain.AuthEvent{}

	for _, e := range slices.Backward(p.events) {
		if len(out) == limit {
			break
		}

		if (q.Before == 0 || e.ID < q.Before) && (q.Kind == "" || e.Kind == q.Kind) {
			e.Data = maps.Clone(e.Data)
			out = append(out, e)
		}
	}

	return out, nil
}

// CleanUpAuth implements ports.Platform: the fake keeps no verification values, device codes or
// rate-limit counters, so it forgets sessions and events only.
func (p *Platform) CleanUpAuth(_ context.Context, now time.Time) (ports.AuthCleanUp, error) {
	p.id.mu.Lock()
	defer p.id.mu.Unlock()

	var out ports.AuthCleanUp

	for token, s := range p.id.sessions {
		if s.session.ExpiresAt.Before(now.Add(-expiredSessions)) {
			delete(p.id.sessions, token)

			out.Sessions++
		}
	}

	kept := p.events[:0]

	for _, e := range p.events {
		if e.At.Before(now.Add(-authEventsKept)) {
			out.AuthEvents++
			continue
		}

		kept = append(kept, e)
	}

	p.events = kept

	return out, nil
}
