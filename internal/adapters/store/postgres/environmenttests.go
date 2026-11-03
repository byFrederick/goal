package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.EnvironmentTests = (*EnvironmentTests)(nil)

// EnvironmentTests implements ports.EnvironmentTests over 00087's environment_tests, as goal_app
// under row-level security: every statement sees the organization in ctx. A test upserts the one
// row of its factory, environment and server.
type EnvironmentTests struct {
	s *Store
}

// NewEnvironmentTests returns the environment tests on s's pool.
func NewEnvironmentTests(s *Store) *EnvironmentTests { return &EnvironmentTests{s: s} }

// Record implements ports.EnvironmentTests.
func (e *EnvironmentTests) Record(ctx context.Context, t domain.EnvironmentTest) error {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.ErrNoOrg
	}

	if err := t.Check(); err != nil {
		return fmt.Errorf("record environment test: %w", err)
	}

	factory, err := parseID("factory", string(t.Factory))
	if err != nil {
		return err
	}

	tools := t.Tools
	if tools == nil {
		tools = []string{}
	}

	err = e.s.inTx(ctx, func(q *db.Queries) error {
		_, err := q.UpsertEnvironmentTest(ctx, db.UpsertEnvironmentTestParams{
			FactoryID: factory, Environment: t.Environment, Server: t.Server, At: t.At.UTC(), Ok: t.OK, Error: t.Error, Tools: tools,
		})

		return err
	})

	switch {
	case isForeignKeyViolation(err):
		return fmt.Errorf("record environment test of factory %s: %w", t.Factory, ports.ErrNotFound)
	case err != nil:
		return fmt.Errorf("record environment test of factory %s: %w", t.Factory, err)
	default:
		return nil
	}
}

// List implements ports.EnvironmentTests.
func (e *EnvironmentTests) List(ctx context.Context, factory domain.FactoryID) ([]domain.EnvironmentTest, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return nil, domain.ErrNoOrg
	}

	var id *uuid.UUID

	if factory != "" {
		f, err := parseID("factory", string(factory))
		if err != nil {
			return nil, err
		}

		id = &f
	}

	rows, err := fetch(ctx, e.s.inTx, func(q *db.Queries) ([]db.EnvironmentTest, error) { return q.EnvironmentTests(ctx, id) })
	if err != nil {
		return nil, fmt.Errorf("list environment tests: %w", err)
	}

	out := make([]domain.EnvironmentTest, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.EnvironmentTest{
			Factory: domain.FactoryID(r.FactoryID.String()), Environment: r.Environment, Server: r.Server,
			At: r.At.UTC(), OK: r.Ok, Error: r.Error, Tools: r.Tools,
		})
	}

	return out, nil
}
