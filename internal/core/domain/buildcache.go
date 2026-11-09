package domain

import (
	"fmt"
	"time"
)

// BuildCacheState is where a build cache is in its life (ADR 0045): reserved by one box while it
// saves the object, then ready for later runs to restore.
type BuildCacheState string

const (
	// BuildCacheSaving is a reserved cache whose object one box is writing.
	BuildCacheSaving BuildCacheState = "saving"
	// BuildCacheReady is a cache whose object is written and may be restored.
	BuildCacheReady BuildCacheState = "ready"
)

// BuildCacheStaleAfter is how long a reservation holds: a saving cache older than this may be
// reserved again by another box, and pruning deletes it.
const BuildCacheStaleAfter = time.Hour

// BuildCacheKeep is how many ready caches of a repository pruning keeps by default.
const BuildCacheKeep = 3

// BuildCache is one cache of a repository's build inputs, as the index stores it (ADR 0045).
type BuildCache struct {
	// RepositoryKey names the repository: a hash of its clone URL, so an organization's factories
	// share it.
	RepositoryKey string
	// CacheKey is a SHA-256 of the cache's inputs (lockfiles and toolchain versions), in hex.
	CacheKey string
	// ObjectKey is the bucket key of the cache's object.
	ObjectKey string
	// SizeBytes is the object's size; 0 while saving.
	SizeBytes int64
	State     BuildCacheState
	CreatedAt time.Time
	// UsedAt is when a run last found the cache, or when it was reserved or committed.
	UsedAt time.Time
}

// NewBuildCache is a cache a box reserves before it saves the object at ObjectKey.
type NewBuildCache struct {
	RepositoryKey string
	CacheKey      string
	ObjectKey     string
	// At is when the box reserves it.
	At time.Time
}

// Check reports whether n can be reserved: ErrInvalid without a repository key, an object key or
// a time, or with a cache key that is not 64 lowercase hex characters.
func (n NewBuildCache) Check() error {
	switch {
	case n.RepositoryKey == "":
		return fmt.Errorf("build cache: a repository key is required: %w", ErrInvalid)
	case !ValidBuildCacheKey(n.CacheKey):
		return fmt.Errorf("build cache key %q is not 64 lowercase hex characters: %w", n.CacheKey, ErrInvalid)
	case n.ObjectKey == "":
		return fmt.Errorf("build cache %s: an object key is required: %w", n.CacheKey, ErrInvalid)
	case n.At.IsZero():
		return fmt.Errorf("build cache %s: a time is required: %w", n.CacheKey, ErrInvalid)
	}

	return nil
}

// ValidBuildCacheKey reports whether key is 64 lowercase hex characters, a SHA-256 in hex.
func ValidBuildCacheKey(key string) bool {
	if len(key) != 64 {
		return false
	}

	for _, c := range []byte(key) {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}

	return true
}

// BuildCacheStale reports whether a reservation made at created is stale at: more than
// BuildCacheStaleAfter before it.
func BuildCacheStale(created, at time.Time) bool {
	return created.Before(at.Add(-BuildCacheStaleAfter))
}
