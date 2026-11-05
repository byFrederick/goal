package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// ItemRepositories implements ports.Store.
func (s *Store) ItemRepositories(ctx context.Context, item domain.ItemID) ([]domain.ItemRepository, error) {
	if !isUUID(string(item)) {
		return nil, nil // an id that is not a UUID names no item, which has none
	}

	uid := uuid.MustParse(string(item))

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.WorkItemRepository, error) { return q.ItemRepositories(ctx, uid) })
	if err != nil {
		return nil, fmt.Errorf("repositories of item %s: %w", item, err)
	}

	var out []domain.ItemRepository

	for _, r := range rows {
		out = append(out, domain.ItemRepository{
			Repo: r.Repo, Joined: domain.RepositoryJoin(r.Joined), JoinedAt: r.JoinedAt.UTC(), Ordinal: int(r.Ordinal),
			BaseCommit: deref(r.BaseCommit), IntegrationBranch: deref(r.IntegrationBranch),
		})
	}

	return out, nil
}

// OpenItemRepositories implements ports.Store.
func (s *Store) OpenItemRepositories(ctx context.Context, factory domain.FactoryID) ([]domain.RepositoryUse, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return nil, err
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.OpenItemRepositoriesRow, error) {
		return q.OpenItemRepositories(ctx, fid)
	})
	if err != nil {
		return nil, fmt.Errorf("repositories of factory %s's open items: %w", factory, err)
	}

	out := make([]domain.RepositoryUse, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.RepositoryUse{Repo: r.Repo, Item: domain.ItemID(r.ItemID.String()), Number: r.Number})
	}

	return out, nil
}

// JoinItemRepositories implements ports.Store.
func (s *Store) JoinItemRepositories(ctx context.Context, item domain.ItemID, joined domain.RepositoryJoin, keys []string, at time.Time) error {
	if !isUUID(string(item)) {
		return fmt.Errorf("item %s: %w", item, ports.ErrNotFound)
	}

	uid := uuid.MustParse(string(item))

	err := s.inTx(ctx, func(q *db.Queries) error {
		_, err := q.LockItem(ctx, uid)
		if notFound(err) {
			return ports.ErrNotFound
		}

		if err != nil {
			return fmt.Errorf("lock item: %w", err)
		}

		return joinItemRepositories(ctx, q, uid, joined, keys, at)
	})
	if err != nil {
		return fmt.Errorf("repositories join item %s: %w", item, err)
	}

	return nil
}

// joinItemRepositories adds to item the repositories of keys it lacks, joined as joined at time
// at with the next ordinals, until it has domain.MaxItemRepositories (domain.AppendRepositories).
// The caller holds the item's lock, which serializes the joins.
func joinItemRepositories(ctx context.Context, q *db.Queries, item uuid.UUID, joined domain.RepositoryJoin, keys []string, at time.Time) error {
	if len(keys) == 0 {
		return nil
	}

	rows, err := q.ItemRepositories(ctx, item)
	if err != nil {
		return fmt.Errorf("repositories of item %s: %w", item, err)
	}

	have := make([]domain.ItemRepository, len(rows))
	for i, r := range rows {
		have[i] = domain.ItemRepository{Repo: r.Repo, Joined: domain.RepositoryJoin(r.Joined), Ordinal: int(r.Ordinal)}
	}

	for _, j := range domain.AppendRepositories(have, joined, keys, at) {
		if err := q.JoinItemRepository(ctx, db.JoinItemRepositoryParams{
			ItemID: item, Repo: j.Repo, Joined: string(j.Joined), JoinedAt: j.JoinedAt, Ordinal: int32(min(max(j.Ordinal, 0), maxInt32)),
		}); err != nil {
			return fmt.Errorf("repository %s joins item %s: %w", j.Repo, item, err)
		}
	}

	return nil
}

// wroteRepositories are the repositories of run's stored rows it wrote to, in the order recorded:
// agent_run_repositories decides wrote (RecordRunRepositoryPushed), and a migrated row without a
// repository is none.
func wroteRepositories(ctx context.Context, q *db.Queries, run uuid.UUID) ([]string, error) {
	rows, err := q.RunRepositories(ctx, []uuid.UUID{run})
	if err != nil {
		return nil, fmt.Errorf("repositories of run %s: %w", run, err)
	}

	var out []string

	for _, r := range rows {
		if r.Wrote && r.Repo != nil {
			out = append(out, *r.Repo)
		}
	}

	return out, nil
}
