package porttest

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.BuildCaches = (*Store)(nil)

// buildCacheRow is one build cache of an organization.
type buildCacheRow struct {
	org domain.OrgID
	row domain.BuildCache
}

// buildCache is the index of org's cache of repo and key in s.buildCaches, or -1. Callers hold
// s.mu.
func (s *Store) buildCache(org domain.OrgID, repo, key string) int {
	return slices.IndexFunc(s.buildCaches, func(r buildCacheRow) bool {
		return r.org == org && r.row.RepositoryKey == repo && r.row.CacheKey == key
	})
}

// FindBuildCache implements ports.BuildCaches.
func (s *Store) FindBuildCache(ctx context.Context, repositoryKey, cacheKey string, at time.Time) (domain.BuildCache, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.BuildCache{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	i := s.buildCache(org, repositoryKey, cacheKey)
	if i < 0 || s.buildCaches[i].row.State != domain.BuildCacheReady {
		return domain.BuildCache{}, fmt.Errorf("find build cache %s: %w", cacheKey, ports.ErrNotFound)
	}

	s.buildCaches[i].row.UsedAt = at

	return s.buildCaches[i].row, nil
}

// ReserveBuildCache implements ports.BuildCaches.
func (s *Store) ReserveBuildCache(ctx context.Context, n domain.NewBuildCache) (bool, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return false, err
	}

	if err := n.Check(); err != nil {
		return false, fmt.Errorf("reserve build cache: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	c := domain.BuildCache{
		RepositoryKey: n.RepositoryKey, CacheKey: n.CacheKey, ObjectKey: n.ObjectKey, State: domain.BuildCacheSaving, CreatedAt: n.At, UsedAt: n.At,
	}

	i := s.buildCache(org, n.RepositoryKey, n.CacheKey)
	switch {
	case i < 0:
		s.buildCaches = append(s.buildCaches, buildCacheRow{org: org, row: c})
	case s.buildCaches[i].row.State == domain.BuildCacheSaving && domain.BuildCacheStale(s.buildCaches[i].row.CreatedAt, n.At):
		s.buildCaches[i].row = c
	default:
		return false, nil
	}

	return true, nil
}

// CommitBuildCache implements ports.BuildCaches.
func (s *Store) CommitBuildCache(ctx context.Context, repositoryKey, cacheKey, objectKey string, sizeBytes int64, at time.Time) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}

	if sizeBytes < 0 {
		return fmt.Errorf("commit build cache %s: size %d: %w", cacheKey, sizeBytes, domain.ErrInvalid)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	i := s.buildCache(org, repositoryKey, cacheKey)
	if i < 0 || s.buildCaches[i].row.State != domain.BuildCacheSaving || s.buildCaches[i].row.ObjectKey != objectKey {
		return fmt.Errorf("commit build cache %s: %w", cacheKey, ports.ErrNotFound)
	}

	r := &s.buildCaches[i].row
	r.State, r.SizeBytes, r.UsedAt = domain.BuildCacheReady, sizeBytes, at

	return nil
}

// PruneBuildCaches implements ports.BuildCaches.
func (s *Store) PruneBuildCaches(ctx context.Context, repositoryKey string, keep int, at time.Time) ([]string, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	if keep < 0 {
		return nil, fmt.Errorf("prune build caches: keep %d: %w", keep, domain.ErrInvalid)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var ready []domain.BuildCache

	for _, r := range s.buildCaches {
		if r.org == org && r.row.RepositoryKey == repositoryKey && r.row.State == domain.BuildCacheReady {
			ready = append(ready, r.row)
		}
	}

	// Newest first, ties by cache key descending, as the adapter orders them.
	slices.SortFunc(ready, func(a, b domain.BuildCache) int {
		return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), cmp.Compare(b.CacheKey, a.CacheKey))
	})

	kept := map[string]bool{}
	for _, c := range ready[:min(keep, len(ready))] {
		kept[c.CacheKey] = true
	}

	deleted := []string{}
	s.buildCaches = slices.DeleteFunc(s.buildCaches, func(r buildCacheRow) bool {
		gone := r.org == org && r.row.RepositoryKey == repositoryKey &&
			(r.row.State == domain.BuildCacheReady && !kept[r.row.CacheKey] ||
				r.row.State == domain.BuildCacheSaving && domain.BuildCacheStale(r.row.CreatedAt, at))
		if gone {
			deleted = append(deleted, r.row.ObjectKey)
		}

		return gone
	})

	return deleted, nil
}
