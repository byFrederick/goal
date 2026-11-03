package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.FactoryConversions = (*FactoryConversions)(nil)

// conversionLock is the session advisory lock key of the conversion, per schema as Migrate's is.
const conversionLock = "SELECT pg_try_advisory_lock(hashtext('goal.moveover.' || current_schema()))"

// FactoryConversions implements ports.FactoryConversions over 00087's factory_conversions, as
// goal_app under row-level security: every statement sees the organization in ctx.
type FactoryConversions struct {
	s *Store
}

// NewFactoryConversions returns the factory conversions on s's pool.
func NewFactoryConversions(s *Store) *FactoryConversions { return &FactoryConversions{s: s} }

// Record implements ports.FactoryConversions.
func (f *FactoryConversions) Record(ctx context.Context, c ports.FactoryConversion) (bool, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return false, domain.ErrNoOrg
	}

	if c.Refused() != (len(c.Problems) > 0) {
		return false, fmt.Errorf("conversion of factory %s: a converted factory has no problems and a refused one has some: %w", c.Factory, domain.ErrInvalid)
	}

	factory, err := parseID("factory", string(c.Factory))
	if err != nil {
		return false, err
	}

	from, err := parseID("version", string(c.From))
	if err != nil {
		return false, err
	}

	var to *uuid.UUID

	if c.To != "" {
		id, err := parseID("version", string(c.To))
		if err != nil {
			return false, err
		}

		to = &id
	}

	names, err := json.Marshal(c.Names)
	if err != nil || c.Names == nil {
		names = []byte("{}")
	}

	problems := c.Problems
	if problems == nil {
		problems = []string{}
	}

	var n int64

	err = f.s.inTx(ctx, func(q *db.Queries) error {
		var err error

		n, err = q.InsertFactoryConversion(ctx, db.InsertFactoryConversionParams{
			FactoryID: factory, FromVersion: from, ToVersion: to, Problems: problems, Names: names, At: c.At.UTC(),
		})

		return err
	})

	switch {
	case isForeignKeyViolation(err):
		return false, fmt.Errorf("record conversion of factory %s: %w", c.Factory, ports.ErrNotFound)
	case err != nil:
		return false, fmt.Errorf("record conversion of factory %s: %w", c.Factory, err)
	default:
		return n == 1, nil
	}
}

// List implements ports.FactoryConversions.
func (f *FactoryConversions) List(ctx context.Context) ([]ports.FactoryConversion, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return nil, domain.ErrNoOrg
	}

	rows, err := fetch(ctx, f.s.inTx, func(q *db.Queries) ([]db.FactoryConversion, error) { return q.FactoryConversions(ctx) })
	if err != nil {
		return nil, fmt.Errorf("list factory conversions: %w", err)
	}

	out := make([]ports.FactoryConversion, 0, len(rows))

	for _, r := range rows {
		c := ports.FactoryConversion{
			Factory: domain.FactoryID(r.FactoryID.String()), From: domain.DefinitionVersionID(r.FromVersion.String()),
			Problems: r.Problems, At: r.At.UTC(),
		}

		if r.ToVersion != nil {
			c.To = domain.DefinitionVersionID(r.ToVersion.String())
		}

		if err := json.Unmarshal(r.Names, &c.Names); err != nil {
			return nil, fmt.Errorf("factory conversion of %s: names: %w", c.Factory, err)
		}

		out = append(out, c)
	}

	return out, nil
}

// WithLock implements ports.FactoryConversions with a session advisory lock on a connection of
// its own (withSessionLock).
func (f *FactoryConversions) WithLock(ctx context.Context, fn func(ctx context.Context) error) (bool, error) {
	return withSessionLock(ctx, f.s, conversionLock, "conversion lock", fn)
}
