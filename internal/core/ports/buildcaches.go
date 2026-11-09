package ports

import (
	"context"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// BuildCaches is the index of the organization's per-repository build caches (ADR 0045), for the
// organization in ctx; *postgres.Store and *porttest.Store implement it next to ports.Store. A
// cache is keyed by repository and cache key. One box reserves a key, writes the object and
// commits it; later runs find it until pruning deletes it. The objects themselves are the
// caller's: this index only names them.
type BuildCaches interface {
	// FindBuildCache returns the ready cache of repositoryKey and cacheKey and sets its UsedAt to
	// at. ErrNotFound when there is none or it is still saving.
	FindBuildCache(ctx context.Context, repositoryKey, cacheKey string, at time.Time) (domain.BuildCache, error)
	// ReserveBuildCache stores n as a saving cache and reports whether this caller got it. A
	// saving cache reserved more than domain.BuildCacheStaleAfter before n.At is taken over (its
	// object key and creation set to n's) and reported true; a fresher saving cache or a ready
	// one is left as it is and reported false. Of concurrent reserves of one key at most one
	// gets it. n must pass Check (domain.ErrInvalid).
	ReserveBuildCache(ctx context.Context, n domain.NewBuildCache) (bool, error)
	// CommitBuildCache marks the saving cache of repositoryKey and cacheKey ready with its size,
	// and sets its UsedAt to at. objectKey is the one the caller reserved it with, so a box whose
	// reservation was taken over commits nothing. ErrNotFound when there is no such saving cache;
	// domain.ErrInvalid for a negative size.
	CommitBuildCache(ctx context.Context, repositoryKey, cacheKey, objectKey string, sizeBytes int64, at time.Time) error
	// PruneBuildCaches deletes every ready cache of repositoryKey but the newest keep by
	// creation, and every saving cache of it reserved more than domain.BuildCacheStaleAfter
	// before at, and returns the deleted caches' object keys for the caller to delete. keep
	// below 0 is domain.ErrInvalid.
	PruneBuildCaches(ctx context.Context, repositoryKey string, keep int, at time.Time) ([]string, error)
}
