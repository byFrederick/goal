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

// JoinIntegrationRepositories implements ports.Store.
func (s *Store) JoinIntegrationRepositories(ctx context.Context, root domain.ItemID, repos []string, branch string, at time.Time) ([]string, error) {
	id, err := parseID("item", string(root))
	if err != nil {
		return nil, fmt.Errorf("integration repositories of %s: %w", root, err)
	}

	if branch == "" {
		return nil, fmt.Errorf("integration repositories of %s: a branch is required: %w", root, domain.ErrInvalid)
	}

	var left []string

	err = s.inTx(ctx, func(q *db.Queries) error {
		if _, err := q.LockItem(ctx, id); notFound(err) {
			return ports.ErrNotFound
		} else if err != nil {
			return fmt.Errorf("lock assembly: %w", err)
		}

		left, err = joinIntegration(ctx, q, id, repos, branch, at)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("integration repositories of %s: %w", root, err)
	}

	return left, nil
}

// joinIntegration gives the assembly root, whose row the caller has locked, a row for each of
// repos it lacks, in ordinal order (domain.JoinIntegration), and sets branch on each of repos'
// rows that has no integration branch. It returns the repositories left out past the cap.
func joinIntegration(ctx context.Context, q *db.Queries, root uuid.UUID, repos []string, branch string, at time.Time) ([]string, error) {
	rows, err := q.ItemRepositories(ctx, root)
	if err != nil {
		return nil, fmt.Errorf("repositories of assembly %s: %w", root, err)
	}

	have := make([]domain.ItemRepository, len(rows))
	for i, r := range rows {
		have[i] = domain.ItemRepository{Repo: r.Repo, Joined: domain.RepositoryJoin(r.Joined), Ordinal: int(r.Ordinal)}
	}

	joins, left := domain.JoinIntegration(have, repos, at)
	for _, j := range joins {
		if err := q.JoinItemRepository(ctx, db.JoinItemRepositoryParams{
			ItemID: root, Repo: j.Repo, Joined: string(j.Joined), JoinedAt: j.JoinedAt, Ordinal: int32(min(max(j.Ordinal, 0), maxInt32)),
		}); err != nil {
			return nil, fmt.Errorf("repository %s joins assembly %s: %w", j.Repo, root, err)
		}
	}

	if err := q.SetIntegrationBranchOfRepositories(ctx, db.SetIntegrationBranchOfRepositoriesParams{
		ItemID: root, Repos: repos, IntegrationBranch: branch,
	}); err != nil {
		return nil, fmt.Errorf("integration branch of assembly %s's repositories: %w", root, err)
	}

	return left, nil
}
