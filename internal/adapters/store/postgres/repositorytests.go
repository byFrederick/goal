package postgres

import (
	"context"
	"fmt"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.RepositoryTests = (*RepositoryTests)(nil)

// RepositoryTests implements ports.RepositoryTests over 00131's repository_tests, as goal_app
// under row-level security: every statement sees the organization in ctx. A test upserts the one
// row of its factory and url.
type RepositoryTests struct {
	s *Store
}

// NewRepositoryTests returns the repository tests on s's pool.
func NewRepositoryTests(s *Store) *RepositoryTests { return &RepositoryTests{s: s} }

// Record implements ports.RepositoryTests.
func (r *RepositoryTests) Record(ctx context.Context, t domain.RepositoryTest) error {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.ErrNoOrg
	}

	if err := t.Check(); err != nil {
		return fmt.Errorf("record repository test: %w", err)
	}

	factory, err := parseID("factory", string(t.Factory))
	if err != nil {
		return err
	}

	err = r.s.inTx(ctx, func(q *db.Queries) error {
		_, err := q.UpsertRepositoryTest(ctx, db.UpsertRepositoryTestParams{
			FactoryID: factory, Url: t.URL, Access: t.Access, Secret: t.Secret, At: t.At.UTC(), Tested: t.Tested, Ok: t.OK,
			Summary: t.Summary, Warning: t.Warning, ReadOk: t.ReadOK, ReadReason: t.ReadReason, PushOk: t.PushOK, PushReason: t.PushReason,
			Readable: int32(min(t.Readable, ports.MaxOwnerRepositories)), Pushable: int32(min(t.Pushable, ports.MaxOwnerRepositories)), //nolint:gosec // bounded above
			Truncated: t.Truncated,
		})

		return err
	})

	switch {
	case isForeignKeyViolation(err):
		return fmt.Errorf("record repository test of factory %s: %w", t.Factory, ports.ErrNotFound)
	case err != nil:
		return fmt.Errorf("record repository test of factory %s: %w", t.Factory, err)
	default:
		return nil
	}
}

// List implements ports.RepositoryTests.
func (r *RepositoryTests) List(ctx context.Context, factory domain.FactoryID) ([]domain.RepositoryTest, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return nil, domain.ErrNoOrg
	}

	id, err := parseID("factory", string(factory))
	if err != nil {
		return nil, err
	}

	rows, err := fetch(ctx, r.s.inTx, func(q *db.Queries) ([]db.RepositoryTest, error) { return q.RepositoryTests(ctx, id) })
	if err != nil {
		return nil, fmt.Errorf("list repository tests of factory %s: %w", factory, err)
	}

	return repositoryTestsOut(rows), nil
}

// BySecret implements ports.RepositoryTests.
func (r *RepositoryTests) BySecret(ctx context.Context, secret string) ([]domain.RepositoryTest, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return nil, domain.ErrNoOrg
	}

	rows, err := fetch(ctx, r.s.inTx, func(q *db.Queries) ([]db.RepositoryTest, error) { return q.RepositoryTestsBySecret(ctx, secret) })
	if err != nil {
		return nil, fmt.Errorf("list repository tests of secret %s: %w", secret, err)
	}

	return repositoryTestsOut(rows), nil
}

// repositoryTestsOut are rows as the domain's tests.
func repositoryTestsOut(rows []db.RepositoryTest) []domain.RepositoryTest {
	out := make([]domain.RepositoryTest, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.RepositoryTest{
			Factory: domain.FactoryID(r.FactoryID.String()), URL: r.Url, Access: r.Access, Secret: r.Secret, At: r.At.UTC(),
			Tested: r.Tested, OK: r.Ok, Summary: r.Summary, Warning: r.Warning, ReadOK: r.ReadOk, ReadReason: r.ReadReason,
			PushOK: r.PushOk, PushReason: r.PushReason, Readable: int(r.Readable), Pushable: int(r.Pushable), Truncated: r.Truncated,
		})
	}

	return out
}
