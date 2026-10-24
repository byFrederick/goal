package ports

import (
	"context"
	"time"

	"github.com/byFrederick/goal/internal/domain"
)

// Platform keeps install-level identity: the platform admins, the security log and the clean-up
// of what signing in leaves behind (phase 3). It belongs to no organization.
type Platform interface {
	// PlatformAdmins lists the platform admins, earliest first.
	PlatformAdmins(ctx context.Context) ([]domain.PlatformAdmin, error)
	// AddPlatformAdmin makes the user with email (case-insensitive) a platform admin, by by, and
	// records platform_admin.added; adding one twice changes nothing. ErrNotFound when no user
	// has that email.
	AddPlatformAdmin(ctx context.Context, by domain.Actor, email string) (domain.PlatformAdmin, error)
	// RemovePlatformAdmin removes user from the platform admins, by by, and records
	// platform_admin.removed. ErrNotFound when they are not one; ErrConflict, changing nothing,
	// when they are the last one.
	RemovePlatformAdmin(ctx context.Context, by domain.Actor, user domain.UserID) error
	// AuthEvents is one page of the security log, newest first.
	AuthEvents(ctx context.Context, q AuthEventQuery) ([]domain.AuthEvent, error)
	// CleanUpAuth deletes, as of now, sessions expired over a day ago, expired verification
	// values and device codes, rate-limit counters idle for a day and security-log events older
	// than 90 days. One instance cleans up at a time: while another does, it does nothing and
	// says so (AuthCleanUp.Skipped).
	CleanUpAuth(ctx context.Context, now time.Time) (AuthCleanUp, error)
}

// AuthEventQuery selects a page of the security log: up to Limit events (1 to 500) with an id
// below Before (0: the newest), of Kind when it is set.
type AuthEventQuery struct {
	Before int64
	Limit  int
	Kind   string
}

// AuthCleanUp counts what one clean-up deleted.
type AuthCleanUp struct {
	// Skipped: another instance was cleaning up; nothing was deleted.
	Skipped                                                      bool
	Sessions, Verifications, DeviceCodes, RateLimits, AuthEvents int64
}
