package postgres

import (
	"context"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
)

// sessionLocks holds the session advisory locks of goald's minute loops (the schedules, Jonah's
// sweeps, the watches and the digest) on one long-lived connection, outside the pool and
// straight to PostgreSQL (ADR 0026 decision 4: a pooler that drops session locks must not sit
// between). Closing the connection frees every lock it holds, even if goald dies while a loop
// runs. It dials on first use (goald dials it at start, OpenLockConn) and again after the
// connection is lost.
//
// A pgx.Conn is not safe for concurrent use, so mu serializes every lock and unlock query; a
// loop runs without it, so one loop holding its lock does not stop another taking its own. A
// session lock is re-entrant on its own session, so held keeps the keys this store holds and a
// key already held is refused here, as another session refuses it in PostgreSQL.
type sessionLocks struct {
	cfg *pgx.ConnConfig

	mu   sync.Mutex
	conn *pgx.Conn
	// gen counts dials, so an unlock after the connection was lost knows its lock went with it.
	gen uint64
	// held maps each key held to the gen of the connection that holds it.
	held map[string]uint64
}

func newSessionLocks(cfg *pgx.ConnConfig) *sessionLocks {
	return &sessionLocks{cfg: cfg, held: map[string]uint64{}}
}

// open dials the connection unless it is open.
func (l *sessionLocks) open(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.connect(ctx)
}

// opened reports whether the lock connection is open: 0 or 1.
func (l *sessionLocks) opened() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.conn != nil && !l.conn.IsClosed() {
		return 1
	}

	return 0
}

// close closes the connection, freeing every lock it holds.
func (l *sessionLocks) close() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.drop()
}

// with runs fn when it takes the lock of key (a bigint SQL expression for pg_try_advisory_lock),
// and reports whether it did. The unlock after fn never fails the call: a lock it cannot free
// is freed by closing the connection.
func (l *sessionLocks) with(ctx context.Context, key, name string, fn func(ctx context.Context) error) (bool, error) {
	gen, ok, err := l.take(ctx, key)
	if err != nil {
		return false, fmt.Errorf("%s: %w", name, err)
	}

	if !ok {
		return false, nil
	}

	defer l.release(ctx, key, gen)

	return true, fn(ctx)
}

// take tries the lock of key once, or twice when the first try finds the connection lost.
func (l *sessionLocks) take(ctx context.Context, key string) (uint64, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if _, held := l.held[key]; held {
		return 0, false, nil
	}

	var err error

	for range 2 {
		if err = l.connect(ctx); err != nil {
			return 0, false, err
		}

		var held bool

		err = l.conn.QueryRow(ctx, "SELECT pg_try_advisory_lock("+key+")").Scan(&held)
		if err == nil {
			if held {
				l.held[key] = l.gen
			}

			return l.gen, held, nil
		}

		// Whether the lock was taken is unknown, so close the connection, which frees it (and
		// every other lock the connection held: their loops lose them as on a lost connection).
		l.drop()

		if ctx.Err() != nil {
			break
		}
	}

	return 0, false, err
}

// release unlocks key unless the connection that took it (gen) is gone, which freed it.
func (l *sessionLocks) release(ctx context.Context, key string, gen uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if got, held := l.held[key]; !held || got != gen || l.gen != gen || l.conn == nil {
		return
	}

	delete(l.held, key)

	unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), closeTimeout)
	defer cancel()

	var freed bool
	if err := l.conn.QueryRow(unlockCtx, "SELECT pg_advisory_unlock("+key+")").Scan(&freed); err != nil || !freed {
		l.drop()
	}
}

// connect dials unless the connection is open; a lost one is closed first. Called with mu held.
func (l *sessionLocks) connect(ctx context.Context) error {
	if l.conn != nil && !l.conn.IsClosed() {
		return nil
	}

	l.drop()

	conn, err := pgx.ConnectConfig(ctx, l.cfg.Copy())
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	l.conn = conn
	l.gen++

	return nil
}

// drop closes the connection and forgets the keys it held. Called with mu held.
func (l *sessionLocks) drop() {
	if l.conn == nil {
		return
	}

	closeCtx, cancel := context.WithTimeout(context.Background(), closeTimeout)
	defer cancel()

	_ = l.conn.Close(closeCtx)
	l.conn = nil
	clear(l.held)
}

// OpenLockConn dials the store's lock connection, so goald fails at start rather than at its
// first minute loop when it cannot hold one.
func (s *Store) OpenLockConn(ctx context.Context) error {
	if err := s.locks.open(ctx); err != nil {
		return fmt.Errorf("postgres: lock connection: %w", err)
	}

	return nil
}

// OutsideConns reports how many connections the store holds outside its pool: its listening
// connection and its lock connection, 0 to 2.
func (s *Store) OutsideConns() int { return s.listen.opened() + s.locks.opened() }

// Close closes the store's lock connection, freeing its locks. The listening connection closes
// when its last listener's context ends; the caller closes the pool.
func (s *Store) Close() { s.locks.close() }
