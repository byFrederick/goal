package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.PastTries = (*Store)(nil)

// QueuePastTries implements ports.PastTries: the proposal is locked, so two sweeps queue its tries
// once, and every try is inserted in the same transaction.
func (s *Store) QueuePastTries(ctx context.Context, factory domain.FactoryID, proposal domain.ProposalID, tries []domain.NewPastTry,
	at time.Time,
) ([]domain.PastTry, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return nil, domain.ErrNoOrg
	}

	for _, n := range tries {
		if err := n.Check(); err != nil {
			return nil, fmt.Errorf("queue past tries: %w", err)
		}
	}

	f, err := parseID("factory", string(factory))
	if err != nil {
		return nil, err
	}

	p, err := parseID("proposal", string(proposal))
	if err != nil {
		return nil, err
	}

	var out []domain.PastTry

	err = s.inTx(ctx, func(q *db.Queries) error {
		row, err := q.LockProposal(ctx, db.LockProposalParams{FactoryID: f, ID: p})
		if notFound(err) {
			return fmt.Errorf("proposal %s: %w", proposal, ports.ErrNotFound)
		}

		if err != nil {
			return fmt.Errorf("lock proposal: %w", err)
		}

		has, err := q.ProposalHasTries(ctx, p)
		if err != nil {
			return fmt.Errorf("read tries: %w", err)
		}

		if has {
			return fmt.Errorf("proposal %s has tries: %w", proposal, ports.ErrConflict)
		}

		out = make([]domain.PastTry, 0, len(tries))

		for _, n := range tries {
			t, err := insertTry(ctx, q, f, p, n, at)
			if err != nil {
				return err
			}

			out = append(out, pastTryFrom(t, row.Number))
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("queue past tries: %w", err)
	}

	return out, nil
}

// insertTry inserts one try of proposal p: ErrNotFound for an item or run that is not the
// organization's, ErrConflict for an item tried twice.
func insertTry(ctx context.Context, q *db.Queries, f, p uuid.UUID, n domain.NewPastTry, at time.Time) (db.Try, error) {
	item, err := parseID("item", string(n.ItemID))
	if err != nil {
		return db.Try{}, err
	}

	run, err := parseID("run", string(n.OriginalRunID))
	if err != nil {
		return db.Try{}, err
	}

	id, err := newID()
	if err != nil {
		return db.Try{}, err
	}

	t, err := q.InsertTry(ctx, db.InsertTryParams{
		ID: id, FactoryID: f, ProposalID: p, ItemID: item, Attempt: clampLimit(n.TryAttempt()), Station: n.Station, OriginalRunID: run, Model: n.Model,
		CreatedAt: at,
	})

	var pg *pgconn.PgError

	switch {
	case isForeignKeyViolation(err):
		return db.Try{}, fmt.Errorf("item %s or run %s: %w", n.ItemID, n.OriginalRunID, ports.ErrNotFound)
	case errors.As(err, &pg) && pg.Code == uniqueViolation:
		return db.Try{}, fmt.Errorf("item %s attempt %d twice: %w", n.ItemID, n.TryAttempt(), ports.ErrConflict)
	case err != nil:
		return db.Try{}, fmt.Errorf("insert try: %w", err)
	}

	return t, nil
}

// ProposalPastTries implements ports.PastTries.
func (s *Store) ProposalPastTries(ctx context.Context, proposal domain.ProposalID) ([]domain.PastTry, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return nil, domain.ErrNoOrg
	}

	p, err := parseID("proposal", string(proposal))
	if err != nil {
		return nil, err
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.ProposalTriesRow, error) { return q.ProposalTries(ctx, p) })
	if err != nil {
		return nil, fmt.Errorf("tries of proposal %s: %w", proposal, err)
	}

	out := make([]domain.PastTry, len(rows))
	for i, r := range rows {
		out[i] = pastTryFrom(r.Try, r.ProposalNumber)
	}

	return out, nil
}

// PastTry implements ports.PastTries.
func (s *Store) PastTry(ctx context.Context, id domain.PastTryID) (domain.PastTry, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.PastTry{}, domain.ErrNoOrg
	}

	uid, err := parseID("past try", string(id))
	if err != nil {
		return domain.PastTry{}, err
	}

	return s.pastTry(ctx, s.read, uid)
}

func (s *Store) pastTry(ctx context.Context, tx func(context.Context, func(*db.Queries) error) error, id uuid.UUID) (domain.PastTry, error) {
	row, err := fetch(ctx, tx, func(q *db.Queries) (db.TryByIDRow, error) { return q.TryByID(ctx, id) })

	switch {
	case notFound(err):
		return domain.PastTry{}, fmt.Errorf("past try %s: %w", id, ports.ErrNotFound)
	case err != nil:
		return domain.PastTry{}, fmt.Errorf("past try %s: %w", id, err)
	}

	return pastTryFrom(row.Try, row.ProposalNumber), nil
}

// ClaimPastTry implements ports.PastTries: the factory's claims are serialized by an advisory
// lock for the transaction, so the cap holds under concurrent claims.
func (s *Store) ClaimPastTry(ctx context.Context, factory domain.FactoryID, now time.Time, lease time.Duration, limit int) (domain.PastTry, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.PastTry{}, domain.ErrNoOrg
	}

	f, err := parseID("factory", string(factory))
	if err != nil {
		return domain.PastTry{}, fmt.Errorf("claim past try: %w", err)
	}

	var out domain.PastTry

	err = s.inTx(ctx, func(q *db.Queries) error {
		if err := q.LockFactoryTries(ctx, f); err != nil {
			return fmt.Errorf("lock the factory's tries: %w", err)
		}

		id, err := q.ClaimTry(ctx, db.ClaimTryParams{Until: now.Add(lease), FactoryID: f, Now: now, MaxRunning: int64(limit)})
		if notFound(err) {
			return ports.ErrNotFound
		}

		if err != nil {
			return err
		}

		row, err := q.TryByID(ctx, id)
		if err != nil {
			return err
		}

		out = pastTryFrom(row.Try, row.ProposalNumber)

		return nil
	})
	if err != nil {
		return domain.PastTry{}, fmt.Errorf("claim past try of factory %s: %w", factory, err)
	}

	return out, nil
}

// FinishPastTry implements ports.PastTries: an update fenced by the claim's lease.
func (s *Store) FinishPastTry(ctx context.Context, claimed domain.PastTry, r domain.PastTryResult) (domain.PastTry, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.PastTry{}, domain.ErrNoOrg
	}

	if err := r.Check(); err != nil {
		return domain.PastTry{}, fmt.Errorf("finish past try %s: %w", claimed.ID, err)
	}

	id, err := uuid.Parse(string(claimed.ID))
	if err != nil || claimed.LeaseUntil.IsZero() {
		return domain.PastTry{}, fmt.Errorf("finish past try %s: %w", claimed.ID, ports.ErrLeaseLost)
	}

	run, err := optionalID("run", string(r.TryRunID))
	if err != nil {
		return domain.PastTry{}, fmt.Errorf("finish past try %s: %w", claimed.ID, err)
	}

	var verdict *string

	if r.Verdict != "" {
		v := string(r.Verdict)
		verdict = &v
	}

	var out domain.PastTry

	err = s.inTx(ctx, func(q *db.Queries) error {
		_, err := q.FinishTry(ctx, db.FinishTryParams{
			Verdict: verdict, Error: r.Error, Reason: r.Reason, Diff: r.Diff, CostUsd: r.CostUSD, TryRunID: run, FinishedAt: r.At, ID: id,
			LeaseUntil: claimed.LeaseUntil,
		})
		if notFound(err) {
			return ports.ErrLeaseLost
		}

		if err != nil {
			return err
		}

		row, err := q.TryByID(ctx, id)
		if err != nil {
			return err
		}

		out = pastTryFrom(row.Try, row.ProposalNumber)

		return nil
	})
	if err != nil {
		return domain.PastTry{}, fmt.Errorf("finish past try %s: %w", claimed.ID, err)
	}

	return out, nil
}

// PastTryTarget implements ports.PastTries.
func (s *Store) PastTryTarget(ctx context.Context, item domain.ItemID, station string) (domain.Run, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.Run{}, domain.ErrNoOrg
	}

	uid, err := parseID("item", string(item))
	if err != nil {
		return domain.Run{}, err
	}

	row, err := fetch(ctx, s.read, func(q *db.Queries) (db.AgentRun, error) {
		return q.TryTarget(ctx, db.TryTargetParams{ItemID: &uid, Station: station})
	})

	switch {
	case notFound(err):
		return domain.Run{}, fmt.Errorf("past try target of item %s at %s: %w", item, station, ports.ErrNotFound)
	case err != nil:
		return domain.Run{}, fmt.Errorf("past try target of item %s at %s: %w", item, station, err)
	}

	return runFrom(row), nil
}

// AgentDoneItems implements ports.PastTries.
func (s *Store) AgentDoneItems(ctx context.Context, factory domain.FactoryID, agent string, limit int) ([]domain.AgentItem, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return nil, domain.ErrNoOrg
	}

	f, err := parseID("factory", string(factory))
	if err != nil {
		return nil, err
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.AgentDoneItemsRow, error) {
		return q.AgentDoneItems(ctx, db.AgentDoneItemsParams{FactoryID: f, Agent: agent, MaxItems: clampLimit(limit)})
	})
	if err != nil {
		return nil, fmt.Errorf("done items of agent %s: %w", agent, err)
	}

	out := make([]domain.AgentItem, len(rows))
	for i, r := range rows {
		run := runFrom(r.AgentRun)
		out[i] = domain.AgentItem{
			ItemID: run.ItemID, ItemNumber: r.ItemNumber, ItemTitle: r.ItemTitle, Run: run, SentBackAt: r.SentBackAt,
			CostUSD: r.ItemCostUsd, DoneAt: r.DoneAt.UTC(),
		}

		if len(out[i].SentBackAt) == 0 {
			out[i].SentBackAt = nil
		}
	}

	return out, nil
}

// PastTryInput implements ports.PastTries.
func (s *Store) PastTryInput(ctx context.Context, run domain.RunID) (domain.PastTryInput, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.PastTryInput{}, domain.ErrNoOrg
	}

	uid, err := parseID("run", string(run))
	if err != nil {
		return domain.PastTryInput{}, err
	}

	var out domain.PastTryInput

	err = s.read(ctx, func(q *db.Queries) error {
		r, err := q.RunByID(ctx, uid)
		if notFound(err) || (err == nil && r.ItemID == nil) {
			return fmt.Errorf("run %s: %w", run, ports.ErrNotFound)
		}

		if err != nil {
			return err
		}

		rows, err := q.OutputsBefore(ctx, db.OutputsBeforeParams{ItemID: r.ItemID, Before: r.StartedAt})
		if err != nil {
			return fmt.Errorf("outputs before run %s: %w", run, err)
		}

		out = domain.PastTryInput{Run: runFrom(r), Outputs: make(map[string]json.RawMessage, len(rows))}
		for _, o := range rows {
			out.Outputs[o.Station] = rawJSON(o.Output)
		}

		return nil
	})
	if err != nil {
		return domain.PastTryInput{}, fmt.Errorf("past try input: %w", err)
	}

	return out, nil
}

func pastTryFrom(t db.Try, number int64) domain.PastTry {
	out := domain.PastTry{
		ID: domain.PastTryID(t.ID.String()), FactoryID: domain.FactoryID(t.FactoryID.String()), ProposalID: domain.ProposalID(t.ProposalID.String()),
		ProposalNumber: number, ItemID: domain.ItemID(t.ItemID.String()), Attempt: int(t.Attempt), Station: t.Station, OriginalRunID: domain.RunID(t.OriginalRunID.String()),
		Model: t.Model, State: domain.PastTryState(t.State), Verdict: domain.PastTryVerdict(deref(t.Verdict)), Error: t.Error, Reason: t.Reason, Diff: t.Diff,
		CostUSD: t.CostUsd, Attempts: int(t.Attempts), CreatedAt: t.CreatedAt.UTC(),
	}

	if t.TryRunID != nil {
		out.TryRunID = domain.RunID(t.TryRunID.String())
	}

	if t.LeaseUntil != nil {
		out.LeaseUntil = t.LeaseUntil.UTC()
	}

	if t.FinishedAt != nil {
		out.FinishedAt = t.FinishedAt.UTC()
	}

	return out
}
