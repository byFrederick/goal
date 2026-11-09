//go:build integration

package postgres

import (
	"context"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/byFrederick/goal/internal/core/ports"
)

// outsideDB is a migrated database and a store whose connections all carry application_name
// app, so pg_stat_activity tells them apart from every other test's; admin reads it.
type outsideDB struct {
	app   string
	pool  *pgxpool.Pool
	s     *Store
	admin *pgxpool.Pool
}

func newOutsideDB(t *testing.T) outsideDB {
	t.Helper()

	db := newTestDB(t, OwnerRole)
	migrate(t, db.owner)

	app := "goal_test_outside_" + randomHex(t)
	pool := openPool(t, db.app+"&application_name="+app)
	s := New(pool)
	t.Cleanup(s.Close)

	return outsideDB{app: app, pool: pool, s: s, admin: openPool(t, db.admin)}
}

// outside counts the store's backends that are not the pool's.
func (d outsideDB) outside(t *testing.T) int {
	t.Helper()

	var backends int
	if err := d.admin.QueryRow(t.Context(), "SELECT count(*) FROM pg_stat_activity WHERE application_name = $1", d.app).Scan(&backends); err != nil {
		t.Fatal(err)
	}

	return backends - int(d.pool.Stat().TotalConns())
}

// advisoryLocks counts the advisory locks held in the store's database, and by how many sessions.
func (d outsideDB) advisoryLocks(t *testing.T) (locks, sessions int) {
	t.Helper()

	if err := d.admin.QueryRow(t.Context(), `SELECT count(*), count(DISTINCT pid) FROM pg_locks
		WHERE locktype = 'advisory' AND granted AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`).Scan(&locks, &sessions); err != nil {
		t.Fatal(err)
	}

	return locks, sessions
}

// terminate ends the store's backends that are not the pool's: the listening and lock connections.
func (d outsideDB) terminate(t *testing.T) {
	t.Helper()

	if _, err := d.admin.Exec(t.Context(), `SELECT pg_terminate_backend(pid) FROM pg_stat_activity
		WHERE application_name = $1 AND backend_type = 'client backend' AND state <> 'active'`, d.app); err != nil {
		t.Fatal(err)
	}
}

// listenAll starts every listener goald starts (the definition cache, and the feed's item,
// definition and access notices) and waits for each one's first resync.
func listenAll(t *testing.T, s *Store) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	resyncs := make(chan struct{}, 16)

	var wg sync.WaitGroup

	listen := func(run func(ctx context.Context) error) {
		wg.Go(func() {
			if err := run(ctx); err != nil {
				t.Errorf("Listen = %v; want nil", err)
			}
		})
	}

	listen(func(ctx context.Context) error {
		return NewDefinitionNotices(s, nil).Listen(ctx, func(n ports.DefinitionNotice) {
			if n.Resync {
				resyncs <- struct{}{}
			}
		})
	})
	listen(func(ctx context.Context) error {
		return NewNotices(s, nil).Listen(ctx, func(n ports.ItemNotice) {
			if n.Resync {
				resyncs <- struct{}{}
			}
		})
	})
	listen(func(ctx context.Context) error {
		return NewDefinitionNotices(s, nil).Listen(ctx, func(n ports.DefinitionNotice) {
			if n.Resync {
				resyncs <- struct{}{}
			}
		})
	})
	listen(func(ctx context.Context) error {
		return NewAccessNotices(s, nil).Listen(ctx, func(n ports.AccessNotice) {
			if n.Resync {
				resyncs <- struct{}{}
			}
		})
	})

	t.Cleanup(func() {
		cancel()
		wg.Wait()
	})

	for range 4 {
		select {
		case <-resyncs:
		case <-t.Context().Done():
			t.Fatal("a listener never resynced")
		}
	}
}

// With every listener goald starts listening and both minute locks held, goald has two
// connections outside its pool: one listening, one holding the locks.
func TestStore_AtMostTwoConnectionsOutsideThePool(t *testing.T) {
	t.Parallel()

	d := newOutsideDB(t)
	listenAll(t, d.s)

	var during, reported int

	ran, err := NewIntake(d.s).WithScheduleLock(t.Context(), func(ctx context.Context) error {
		ran, err := NewDigests(d.s).WithDigestLock(ctx, func(context.Context) error {
			during, reported = d.outside(t), d.s.OutsideConns()

			return nil
		})
		if !ran {
			t.Error("the digest lock was refused while the schedule lock was held")
		}

		return err
	})
	if err != nil || !ran {
		t.Fatalf("WithScheduleLock = %v, %v; want it run", ran, err)
	}

	if during > 2 {
		t.Errorf("%d connections outside the pool; want at most 2", during)
	}

	if reported != 2 || reported != during {
		t.Errorf("OutsideConns = %d with %d counted in pg_stat_activity; want 2 and 2", reported, during)
	}
}

// The schedule lock and the digest lock are held at the same time by one session, the lock
// connection, and both are freed after.
func TestLockConn_HoldsBothLocksOnOneConnection(t *testing.T) {
	t.Parallel()

	d := newOutsideDB(t)

	var locks, sessions int

	ran, err := NewIntake(d.s).WithScheduleLock(t.Context(), func(ctx context.Context) error {
		_, err := NewDigests(d.s).WithDigestLock(ctx, func(context.Context) error {
			locks, sessions = d.advisoryLocks(t)

			return nil
		})

		return err
	})
	if err != nil || !ran {
		t.Fatalf("WithScheduleLock = %v, %v; want it run", ran, err)
	}

	if locks != 2 || sessions != 1 {
		t.Errorf("%d advisory locks held by %d sessions; want 2 by 1", locks, sessions)
	}

	if locks, _ := d.advisoryLocks(t); locks != 0 {
		t.Errorf("%d advisory locks held after both returned; want none", locks)
	}

	if n := d.s.OutsideConns(); n != 1 {
		t.Errorf("OutsideConns = %d after the locks; want the lock connection kept open", n)
	}
}

// When the lock connection is lost while a loop holds its lock, the loop ends without an error
// (PostgreSQL freed the lock with the session), the next lock redials, and the lock this goald
// held is no longer counted as held.
func TestLockConn_RedialsWhenTheConnectionIsLost(t *testing.T) {
	t.Parallel()

	d := newOutsideDB(t)
	in := NewIntake(d.s)

	var digestRan bool

	ran, err := in.WithScheduleLock(t.Context(), func(ctx context.Context) error {
		d.terminate(t)

		var err error
		digestRan, err = NewDigests(d.s).WithDigestLock(ctx, func(context.Context) error { return nil })

		return err
	})
	if err != nil || !ran {
		t.Fatalf("WithScheduleLock = %v, %v; want it run and no error from the lost unlock", ran, err)
	}

	if !digestRan {
		t.Error("the digest lock was not taken after the connection was lost; want it redialled")
	}

	if ran, err := in.WithScheduleLock(t.Context(), func(context.Context) error { return nil }); !ran || err != nil {
		t.Errorf("WithScheduleLock after the loss = %v, %v; want it run", ran, err)
	}
}
