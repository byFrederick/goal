// Package postgres implements ports.Store on PostgreSQL 18 with pgx and the sqlc-generated
// queries in package db (compiled from queries/ against migrations/).
//
// Every Store method runs in one transaction bound to the organization in ctx
// (domain.WithOrg): it starts with set_config('goal.org_id', <org>, true), so row-level security
// shows it that organization's rows only, and fails with domain.ErrNoOrg without one. Reads run
// in read-only transactions, because SET LOCAL needs one. A claim takes the next waiting item
// with FOR NO KEY UPDATE SKIP LOCKED, and the unique (factory_id, station, slot) lease index
// keeps WIP at or under capacity across goald instances. A lease's acquired_at fences its
// holder: a heartbeat or transition naming another acquired_at fails with ports.ErrLeaseLost.
// IDs are UUIDv7 generated here, not by column defaults. Locks are taken item first, then its
// lease or runs, so concurrent methods never deadlock.
package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/byFrederick/goal/internal/adapters/postgres/db"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/migrations"
)

// Open connects a pool to the database at url (a postgres:// URL or key=value DSN; pool
// settings such as pool_max_conns go in the URL) and checks that it answers.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("postgres: open: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()

		return nil, fmt.Errorf("postgres: ping: %w", err)
	}

	return pool, nil
}

// Migrate applies every pending migration from the migrations package. Instances that start
// together take turns on a transaction-scoped advisory lock keyed by the schema, so exactly
// one applies each migration and the lock is freed even if the process dies.
// The lock is held on one pool connection while goose migrates on others, so the pool needs at
// least two.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	sqlDB := stdlib.OpenDBFromPool(pool) // closing it leaves the pool open

	p, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	if err != nil {
		return errors.Join(fmt.Errorf("postgres: migrate: %w", err), sqlDB.Close())
	}

	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('goal.migrate.' || current_schema()))"); err != nil {
			return fmt.Errorf("lock: %w", err)
		}

		_, err := p.Up(ctx)

		return err
	})
	if err != nil {
		err = fmt.Errorf("postgres: migrate: %w", err)
	}

	return errors.Join(err, sqlDB.Close())
}

var _ ports.Store = (*Store)(nil)

// Store implements ports.Store. It is safe for concurrent use and keeps no state of its own.
type Store struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

// New returns a store on pool; the caller closes the pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: db.New(pool)}
}

// Factory is a loaded factory definition as the factories table records it.
type Factory struct {
	ID    domain.FactoryID
	Name  string
	Title string
	// Definition is the parsed factory.yaml as JSON; the row keeps its SHA-256.
	Definition json.RawMessage
}

// SaveFactory registers a factory in the organization in ctx, or updates its row when the
// definition changed. Items reference their factory, so goald saves every factory it runs
// before work enters it; CreateItem returns ports.ErrNotFound for a factory that was never
// saved in the item's organization.
func (s *Store) SaveFactory(ctx context.Context, f Factory) error {
	id, err := parseID("factory", string(f.ID))
	if err != nil {
		return err
	}

	sum := sha256.Sum256(f.Definition)

	err = s.inTx(ctx, func(q *db.Queries) error {
		return q.SaveFactory(ctx, db.SaveFactoryParams{
			ID: id, Name: f.Name, Title: optional(f.Title), Definition: f.Definition, DefinitionSha256: hex.EncodeToString(sum[:]),
		})
	})
	if err != nil {
		return fmt.Errorf("save factory %s: %w", f.ID, err)
	}

	return nil
}

// inTx runs fn in a transaction bound to the organization in ctx, committing when it returns
// nil. fn's error is returned as is, so the ports errors it wraps stay visible to errors.Is.
func (s *Store) inTx(ctx context.Context, fn func(q *db.Queries) error) error {
	return s.orgTx(ctx, pgx.TxOptions{}, fn)
}

// read runs fn in a read-only transaction bound to the organization in ctx.
func (s *Store) read(ctx context.Context, fn func(q *db.Queries) error) error {
	return s.orgTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly}, fn)
}

// orgTx binds a transaction to the organization in ctx before fn runs: without one it fails
// with domain.ErrNoOrg and never reaches the database.
func (s *Store) orgTx(ctx context.Context, opts pgx.TxOptions, fn func(q *db.Queries) error) error {
	id, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.ErrNoOrg
	}

	org, err := uuid.Parse(string(id))
	if err != nil {
		return fmt.Errorf("org %q: %w", id, ports.ErrNotFound)
	}

	return pgx.BeginTxFunc(ctx, s.pool, opts, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.SetOrg(ctx, org.String()); err != nil {
			return fmt.Errorf("set org: %w", err)
		}

		return fn(q)
	})
}

// fetch runs one query in a transaction of tx (Store.read or Store.inTx) and returns its result.
func fetch[T any](ctx context.Context, tx func(context.Context, func(*db.Queries) error) error,
	query func(*db.Queries) (T, error),
) (T, error) {
	var out T

	err := tx(ctx, func(q *db.Queries) error {
		var err error

		out, err = query(q)

		return err
	})

	return out, err
}

// systemTx runs fn in a transaction bound to no organization, for the cross-organization
// SECURITY DEFINER functions only; every org-owned row stays out of its reach.
func (s *Store) systemTx(ctx context.Context, fn func(q *db.Queries) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return fn(s.q.WithTx(tx)) })
}

// parseID parses a domain id. A string that is not a UUID names nothing, so it is not found.
func parseID(kind, id string) (uuid.UUID, error) {
	u, err := uuid.Parse(id)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("%s %q: %w", kind, id, ports.ErrNotFound)
	}

	return u, nil
}

// newID returns a UUIDv7: time-ordered, so new rows land at the right edge of the index.
func newID() (uuid.UUID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("new id: %w", err)
	}

	return id, nil
}

func notFound(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
