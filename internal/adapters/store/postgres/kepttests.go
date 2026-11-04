package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.KeptTests = (*Store)(nil)

// KeepTest implements ports.KeptTests: the test is inserted only for an item of the factory, and
// read back with the item's number and title in the same transaction.
func (s *Store) KeepTest(ctx context.Context, n domain.NewKeptTest) (domain.KeptTest, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.KeptTest{}, domain.ErrNoOrg
	}

	if err := n.Check(); err != nil {
		return domain.KeptTest{}, fmt.Errorf("keep test: %w", err)
	}

	f, err := parseID("factory", string(n.FactoryID))
	if err != nil {
		return domain.KeptTest{}, err
	}

	item, err := parseID("item", string(n.ItemID))
	if err != nil {
		return domain.KeptTest{}, err
	}

	id, err := newID()
	if err != nil {
		return domain.KeptTest{}, err
	}

	var out domain.KeptTest

	err = s.inTx(ctx, func(q *db.Queries) error {
		_, err := q.InsertKeptTest(ctx, db.InsertKeptTestParams{
			ID: id, Station: n.Station, KeptBy: n.KeptBy.Ref(), KeptAt: n.KeptAt, ItemID: item, FactoryID: f,
		})

		var pg *pgconn.PgError

		switch {
		case notFound(err):
			return fmt.Errorf("item %s: %w", n.ItemID, ports.ErrNotFound)
		case errors.As(err, &pg) && pg.Code == uniqueViolation:
			return fmt.Errorf("item %s at %s: %w", n.ItemID, n.Station, ports.ErrConflict)
		case err != nil:
			return fmt.Errorf("insert kept test: %w", err)
		}

		row, err := q.KeptTestByID(ctx, id)
		if err != nil {
			return fmt.Errorf("read kept test: %w", err)
		}

		out, err = keptTestFrom(row.KeptTest, row.ItemNumber, row.ItemTitle, keptLast{row.LastResult, row.LastAt, row.LastRunID})

		return err
	})
	if err != nil {
		return domain.KeptTest{}, fmt.Errorf("keep test: %w", err)
	}

	return out, nil
}

// KeptTests implements ports.KeptTests.
func (s *Store) KeptTests(ctx context.Context, factory domain.FactoryID, station string) ([]domain.KeptTest, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return nil, domain.ErrNoOrg
	}

	f, err := parseID("factory", string(factory))
	if err != nil {
		return nil, err
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.KeptTestsRow, error) {
		return q.KeptTests(ctx, db.KeptTestsParams{FactoryID: f, Station: station})
	})
	if err != nil {
		return nil, fmt.Errorf("kept tests of factory %s: %w", factory, err)
	}

	out := make([]domain.KeptTest, len(rows))
	for i, r := range rows {
		if out[i], err = keptTestFrom(r.KeptTest, r.ItemNumber, r.ItemTitle, keptLast{r.LastResult, r.LastAt, r.LastRunID}); err != nil {
			return nil, fmt.Errorf("kept tests of factory %s: %w", factory, err)
		}
	}

	return out, nil
}

// RemoveKeptTest implements ports.KeptTests.
func (s *Store) RemoveKeptTest(ctx context.Context, factory domain.FactoryID, id domain.KeptTestID) error {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.ErrNoOrg
	}

	f, err := parseID("factory", string(factory))
	if err != nil {
		return err
	}

	k, err := parseID("kept test", string(id))
	if err != nil {
		return err
	}

	var n int64

	err = s.inTx(ctx, func(q *db.Queries) error {
		n, err = q.DeleteKeptTest(ctx, db.DeleteKeptTestParams{FactoryID: f, ID: k})

		return err
	})
	if err != nil {
		return fmt.Errorf("remove kept test %s: %w", id, err)
	}

	if n == 0 {
		return fmt.Errorf("remove kept test %s: %w", id, ports.ErrNotFound)
	}

	return nil
}

// keptLast is a kept test's newest finished try as the queries read it: its verdict, finish and
// run, each NULL when it has none.
type keptLast struct {
	verdict *string
	at      *time.Time
	run     *uuid.UUID
}

// keptTestFrom is a kept_tests row with its item's number and title and its newest finished try.
func keptTestFrom(k db.KeptTest, number int64, title string, last keptLast) (domain.KeptTest, error) {
	by, err := domain.ActorFromRef(k.KeptBy)
	if err != nil {
		return domain.KeptTest{}, fmt.Errorf("kept test %s: %w", k.ID, err)
	}

	out := domain.KeptTest{
		ID: domain.KeptTestID(k.ID.String()), FactoryID: domain.FactoryID(k.FactoryID.String()), ItemID: domain.ItemID(k.ItemID.String()),
		ItemNumber: number, ItemTitle: title, Station: k.Station, KeptBy: by, KeptAt: k.KeptAt.UTC(),
	}

	if last.at != nil {
		t := domain.PastTry{State: domain.PastTryDone, Verdict: domain.PastTryVerdict(deref(last.verdict))}
		out.LastResult = &domain.KeptTestLast{Result: t.KeptResult(), At: last.at.UTC()}

		if last.run != nil {
			out.LastResult.RunID = domain.RunID(last.run.String())
		}
	}

	return out, nil
}
