package postgres

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.BuildCaches = (*Store)(nil)

// FindBuildCache implements ports.BuildCaches: one UPDATE marks the ready row used and returns it.
func (s *Store) FindBuildCache(ctx context.Context, repositoryKey, cacheKey string, at time.Time) (domain.BuildCache, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.BuildCache{}, domain.ErrNoOrg
	}

	var row db.BuildCache

	err := s.inTx(ctx, func(q *db.Queries) error {
		var err error

		row, err = q.FindBuildCache(ctx, db.FindBuildCacheParams{At: at, RepositoryKey: repositoryKey, CacheKey: cacheKey})

		return err
	})

	switch {
	case notFound(err):
		return domain.BuildCache{}, fmt.Errorf("find build cache %s: %w", cacheKey, ports.ErrNotFound)
	case err != nil:
		return domain.BuildCache{}, fmt.Errorf("find build cache %s: %w", cacheKey, err)
	}

	return buildCacheFrom(row), nil
}

// ReserveBuildCache implements ports.BuildCaches: one INSERT ... ON CONFLICT DO UPDATE inserts the
// row or takes a stale reservation over, so concurrent reserves of a key have one winner.
func (s *Store) ReserveBuildCache(ctx context.Context, n domain.NewBuildCache) (bool, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return false, domain.ErrNoOrg
	}

	if err := n.Check(); err != nil {
		return false, fmt.Errorf("reserve build cache: %w", err)
	}

	var rows int64

	err := s.inTx(ctx, func(q *db.Queries) error {
		var err error

		rows, err = q.ReserveBuildCache(ctx, db.ReserveBuildCacheParams{
			RepositoryKey: n.RepositoryKey, CacheKey: n.CacheKey, ObjectKey: n.ObjectKey, At: n.At,
		})

		return err
	})
	if err != nil {
		return false, fmt.Errorf("reserve build cache %s: %w", n.CacheKey, err)
	}

	return rows == 1, nil
}

// CommitBuildCache implements ports.BuildCaches.
func (s *Store) CommitBuildCache(ctx context.Context, repositoryKey, cacheKey, objectKey string, sizeBytes int64, at time.Time) error {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.ErrNoOrg
	}

	if sizeBytes < 0 {
		return fmt.Errorf("commit build cache %s: size %d: %w", cacheKey, sizeBytes, domain.ErrInvalid)
	}

	var rows int64

	err := s.inTx(ctx, func(q *db.Queries) error {
		var err error

		rows, err = q.CommitBuildCache(ctx, db.CommitBuildCacheParams{
			SizeBytes: &sizeBytes, At: at, RepositoryKey: repositoryKey, CacheKey: cacheKey, ObjectKey: objectKey,
		})

		return err
	})
	if err != nil {
		return fmt.Errorf("commit build cache %s: %w", cacheKey, err)
	}

	if rows == 0 {
		return fmt.Errorf("commit build cache %s: %w", cacheKey, ports.ErrNotFound)
	}

	return nil
}

// PruneBuildCaches implements ports.BuildCaches: one DELETE removes the rows and returns their
// object keys.
func (s *Store) PruneBuildCaches(ctx context.Context, repositoryKey string, keep int, at time.Time) ([]string, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return nil, domain.ErrNoOrg
	}

	if keep < 0 {
		return nil, fmt.Errorf("prune build caches: keep %d: %w", keep, domain.ErrInvalid)
	}

	var deleted []string

	err := s.inTx(ctx, func(q *db.Queries) error {
		var err error

		deleted, err = q.PruneBuildCaches(ctx, db.PruneBuildCachesParams{
			RepositoryKey: repositoryKey, At: at, Keep: int32(min(keep, math.MaxInt32)), //nolint:gosec // bounded by min
		})

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("prune build caches of %s: %w", repositoryKey, err)
	}

	if deleted == nil {
		deleted = []string{}
	}

	return deleted, nil
}

// buildCacheFrom is a build_caches row as the domain's cache.
func buildCacheFrom(r db.BuildCache) domain.BuildCache {
	c := domain.BuildCache{
		RepositoryKey: r.RepositoryKey, CacheKey: r.CacheKey, ObjectKey: r.ObjectKey,
		State: domain.BuildCacheState(r.State), CreatedAt: r.CreatedAt.UTC(), UsedAt: r.UsedAt.UTC(),
	}

	if r.SizeBytes != nil {
		c.SizeBytes = *r.SizeBytes
	}

	return c
}
