// Package postgres implements ports.Store on PostgreSQL 18 with pgx and the sqlc-generated
// queries in package db (compiled from db/queries/ against db/migrations/).
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
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/byFrederick/goal/db/migrations"
	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
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

// ErrLegacyImportPending is Migrate's refusal of a database where a live organization still has
// the legacy import of its factory definitions to do: this release no longer has the import
// (ADR 0005 amendment), the previous one runs it at its start.
var ErrLegacyImportPending = errors.New("a legacy import of factory definitions is pending: " +
	"start the previous release of goald once, which imports them, then this one")

// The migrations of the legacy import's record: 00039 made every organization's import pending,
// 00050 drops the record with the import.
const (
	legacyImportPended  = 39
	legacyImportDropped = 50
)

// migrationRoles are the roles the migrations grant to. They are cluster-wide, so goal db init
// creates them (roles.sql), not a migration.
var migrationRoles = []string{AppRole, AuthRole}

// Migrate applies every pending migration from the migrations package. Instances that start
// together take turns on a session advisory lock keyed by the schema, so exactly one applies
// each migration; the lock is freed when its connection closes, even if the process dies.
// While it holds the lock it takes no connection from pool: the lock is held on a connection of
// its own and goose migrates on another, both opened from pool's config and closed on return,
// so any number of callers share a pool of any size. It applies nothing while a role the
// migrations grant to is missing (an install from before goal_auth that was not started with
// goal up); the error wraps ErrRoleMissing. Nor while a live organization still has a legacy
// import to do; the error wraps ErrLegacyImportPending.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	return migrateRequiring(ctx, pool, migrationRoles)
}

// closeTimeout bounds closing migrateRequiring's connections once ctx may be done.
const closeTimeout = 5 * time.Second

func migrateRequiring(ctx context.Context, pool *pgxpool.Pool, roles []string) (err error) {
	if err := requireRoles(ctx, pool, roles); err != nil {
		return fmt.Errorf("postgres: migrate: %w", err)
	}

	defer func() {
		if err != nil {
			err = fmt.Errorf("postgres: migrate: %w", err)
		}
	}()

	cfg := pool.Config().ConnConfig

	lockConn, err := pgx.ConnectConfig(ctx, cfg.Copy())
	if err != nil {
		return fmt.Errorf("connect for the lock: %w", err)
	}

	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), closeTimeout)
		defer cancel()

		err = errors.Join(err, lockConn.Close(closeCtx)) // frees the lock
	}()

	// The previous release takes this key with pg_advisory_xact_lock, which conflicts with the
	// session lock, so mixed-version instances take turns too.
	if _, err := lockConn.Exec(ctx, "SELECT pg_advisory_lock(hashtext('goal.migrate.' || current_schema()))"); err != nil {
		return fmt.Errorf("lock: %w", err)
	}

	// Goose's provider runs on one connection at a time.
	sqlDB := stdlib.OpenDB(*cfg.Copy())
	sqlDB.SetMaxOpenConns(1)

	defer func() { err = errors.Join(err, sqlDB.Close()) }()

	p, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	if err != nil {
		return err
	}

	version, err := p.GetDBVersion(ctx)
	if err != nil {
		return fmt.Errorf("read the version: %w", err)
	}

	if err := refuseLegacyImport(ctx, lockConn, version); err != nil {
		return err
	}

	_, err = p.Up(ctx)

	return err
}

// refuseLegacyImport fails with ErrLegacyImportPending when the database, at migration version,
// still has a legacy import to do: a live organization 00039 recorded pending, or any database
// from before 00039, which would record every organization pending. A new database (version 0)
// and one past 00050 have none. It reads on conn, the lock's connection, outside a transaction,
// so it keeps no lock on the table 00050 drops.
func refuseLegacyImport(ctx context.Context, conn *pgx.Conn, version int64) error {
	switch {
	case version == 0 || version >= legacyImportDropped:
		return nil
	case version < legacyImportPended:
		return fmt.Errorf("the database is at migration %d, before the legacy import ran: %w", version, ErrLegacyImportPending)
	}

	var pending []string

	err := conn.QueryRow(ctx, `
		SELECT coalesce(array_agg(o.slug ORDER BY o.slug), '{}') FROM legacy_import_pending p
		JOIN organizations o ON o.id = p.org_id WHERE o.deleted_at IS NULL`).Scan(&pending)
	if err != nil {
		return fmt.Errorf("read the legacy import: %w", err)
	}

	if len(pending) > 0 {
		return fmt.Errorf("organization %s: %w", strings.Join(pending, ", "), ErrLegacyImportPending)
	}

	return nil
}

// requireRoles fails with ErrRoleMissing naming every role of roles the cluster lacks.
func requireRoles(ctx context.Context, pool *pgxpool.Pool, roles []string) error {
	var missing []string

	err := pool.QueryRow(ctx, `
		SELECT coalesce(array_agg(r ORDER BY r), '{}') FROM unnest($1::text[]) r
		WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r)`, roles).Scan(&missing)
	if err != nil {
		return fmt.Errorf("check roles: %w", err)
	}

	if len(missing) > 0 {
		return fmt.Errorf("role %s is missing: %w", strings.Join(missing, ", "), ErrRoleMissing)
	}

	return nil
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
