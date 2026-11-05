package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
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
