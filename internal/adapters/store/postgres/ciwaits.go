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

// OpenCIWait implements ports.Store: the item's lock first, then the poll and the wait.
func (s *Store) OpenCIWait(ctx context.Context, n domain.NewCIWait) (domain.CIWaitRecord, error) {
	if err := n.Check(); err != nil {
		return domain.CIWaitRecord{}, err
	}

	item, err := parseID("item", string(n.Item))
	if err != nil {
		return domain.CIWaitRecord{}, err
	}

	var out domain.CIWaitRecord

	err = s.inTx(ctx, func(q *db.Queries) error {
		if _, err := q.LockItem(ctx, item); notFound(err) {
			return fmt.Errorf("item %s: %w", n.Item, ports.ErrNotFound)
		} else if err != nil {
			return fmt.Errorf("lock item: %w", err)
		}

		out, err = openCIWait(ctx, q, item, n)

		return err
	})
	if err != nil {
		return domain.CIWaitRecord{}, fmt.Errorf("open ci wait of %s on %s: %w", n.Item, n.Repo, err)
	}

	return out, nil
}

// openCIWait creates n's poll when the organization has none and inserts the wait; the caller
// locked the item.
func openCIWait(ctx context.Context, q *db.Queries, item uuid.UUID, n domain.NewCIWait) (domain.CIWaitRecord, error) {
	key := n.PollKey()

	if err := q.UpsertCIPoll(ctx, db.UpsertCIPollParams{Host: key.Host, Repo: key.Repo, Sha: key.SHA, NextPollAt: n.StartedAt}); err != nil {
		return domain.CIWaitRecord{}, fmt.Errorf("upsert ci poll: %w", err)
	}

	id, err := newID()
	if err != nil {
		return domain.CIWaitRecord{}, err
	}

	var held []byte
	if len(n.Held) > 0 {
		held = n.Held
	}

	row, err := q.InsertCIWait(ctx, db.InsertCIWaitParams{
		ID: id, ItemID: item, Station: n.Station, Visit: n.Visit, Host: key.Host, Repo: n.Repo, Sha: n.SHA, PrUrl: optional(n.PRURL),
		Phase: string(n.Phase), Round: int32(n.Round), Held: held, StartedAt: n.StartedAt, //nolint:gosec // Check bounds a round from 1
	})

	var pg *pgconn.PgError

	switch {
	case errors.As(err, &pg) && pg.Code == uniqueViolation:
		return domain.CIWaitRecord{}, fmt.Errorf("open ci wait of the visit: %w", ports.ErrConflict)
	case err != nil:
		return domain.CIWaitRecord{}, fmt.Errorf("insert ci wait: %w", err)
	}

	return ciWaitFrom(row), nil
}

// ReplaceCIWaitSHA implements ports.Store: the item's lock first, as OpenCIWait takes it, then
// the end and the new wait in one transaction, so the partial unique key never sees two open
// waits and the item is never left without one.
func (s *Store) ReplaceCIWaitSHA(ctx context.Context, id domain.CIWaitID, sha, prURL string, at time.Time) (domain.CIWaitRecord, error) {
	wid, err := parseID("ci wait", string(id))
	if err != nil {
		return domain.CIWaitRecord{}, err
	}

	var out domain.CIWaitRecord

	err = s.inTx(ctx, func(q *db.Queries) error {
		item, err := q.CIWaitItem(ctx, wid)
		if notFound(err) {
			return fmt.Errorf("open ci wait %s: %w", id, ports.ErrNotFound)
		} else if err != nil {
			return fmt.Errorf("read ci wait: %w", err)
		}

		if _, err := q.LockItem(ctx, item); err != nil {
			return fmt.Errorf("lock item: %w", err)
		}

		// Under the item's lock the wait may have ended since it was read: then no row.
		old, err := q.EndOpenCIWait(ctx, db.EndOpenCIWaitParams{EndedAt: &at, Outcome: new(string(domain.CISuperseded)), ID: wid})
		if notFound(err) {
			return fmt.Errorf("open ci wait %s: %w", id, ports.ErrNotFound)
		} else if err != nil {
			return fmt.Errorf("end ci wait: %w", err)
		}

		n := ciWaitFrom(old).NewCIWait
		n.SHA, n.StartedAt = sha, at

		if prURL != "" {
			n.PRURL = prURL
		}

		if err := n.Check(); err != nil {
			return err
		}

		out, err = openCIWait(ctx, q, old.ItemID, n)

		return err
	})
	if err != nil {
		return domain.CIWaitRecord{}, fmt.Errorf("replace sha of ci wait %s: %w", id, err)
	}

	return out, nil
}

// maxCIPollLease caps the polls one LeaseDueCIPolls call leases.
const maxCIPollLease = 1000

// LeaseDueCIPolls implements ports.Store.
func (s *Store) LeaseDueCIPolls(ctx context.Context, now time.Time, owner string, lease time.Duration, limit int) ([]domain.CIPoll, error) {
	if owner == "" || lease <= 0 {
		return nil, fmt.Errorf("lease ci polls: an owner and a lease are required: %w", domain.ErrInvalid)
	}

	until := now.Add(lease)

	rows, err := fetch(ctx, s.inTx, func(q *db.Queries) ([]db.CiPoll, error) {
		return q.LeaseDueCIPolls(ctx, db.LeaseDueCIPollsParams{Owner: &owner, LeaseUntil: &until, Now: now, MaxPolls: int32(max(min(limit, maxCIPollLease), 0))})
	})
	if err != nil {
		return nil, fmt.Errorf("lease ci polls: %w", err)
	}

	out := make([]domain.CIPoll, 0, len(rows))
	for _, r := range rows {
		out = append(out, ciPollFrom(r))
	}

	return out, nil
}

// RecordCIPoll implements ports.Store.
func (s *Store) RecordCIPoll(ctx context.Context, key domain.CIPollKey, owner string, r domain.CIPollResult) error {
	if err := r.Check(); err != nil {
		return err
	}

	var checks []byte
	if r.Checks != nil {
		checks = r.Checks
	}

	n, err := fetch(ctx, s.inTx, func(q *db.Queries) (int64, error) {
		return q.RecordCIPoll(ctx, db.RecordCIPollParams{
			Checks: checks, Etag: r.ETag, LastError: r.Error, FirstCheckSeenAt: optionalTime(r.FirstCheckSeenAt), NextPollAt: r.NextPollAt,
			IntervalSeconds: int32(r.Interval / time.Second), Host: //nolint:gosec // Check bounds it to 15..120
			key.Host, Repo:  key.Repo, Sha: key.SHA, Owner: &owner,
		})
	})

	switch {
	case err != nil:
		return fmt.Errorf("record ci poll %s@%s: %w", key.Repo, key.SHA, err)
	case n == 0:
		return fmt.Errorf("ci poll %s@%s: %w", key.Repo, key.SHA, ports.ErrLeaseLost)
	}

	return nil
}

// OpenCIWaits implements ports.Store.
func (s *Store) OpenCIWaits(ctx context.Context, item domain.ItemID) ([]domain.CIWaitRecord, error) {
	if !isUUID(string(item)) {
		return nil, nil // an id that is not a UUID names no item, which has none
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.OpenCIWaitsRow, error) {
		return q.OpenCIWaits(ctx, uuid.MustParse(string(item)))
	})
	if err != nil {
		return nil, fmt.Errorf("open ci waits of %s: %w", item, err)
	}

	out := make([]domain.CIWaitRecord, 0, len(rows))

	for _, r := range rows {
		w := ciWaitFrom(r.CiWait)
		w.Poll = ciPollFrom(r.CiPoll)
		out = append(out, w)
	}

	return out, nil
}

// CIWaits implements ports.Store.
func (s *Store) CIWaits(ctx context.Context, item domain.ItemID, station string) ([]domain.CIWaitRecord, error) {
	if !isUUID(string(item)) {
		return nil, nil // an id that is not a UUID names no item, which has none
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.CIWaitsAtRow, error) {
		return q.CIWaitsAt(ctx, db.CIWaitsAtParams{ItemID: uuid.MustParse(string(item)), Station: station})
	})
	if err != nil {
		return nil, fmt.Errorf("ci waits of %s at %s: %w", item, station, err)
	}

	out := make([]domain.CIWaitRecord, 0, len(rows))

	for _, r := range rows {
		w := ciWaitFrom(r.CiWait)
		w.Poll = ciPollFrom(r.CiPoll)
		out = append(out, w)
	}

	return out, nil
}

// OpenCIWaitsOnPoll implements ports.Store.
func (s *Store) OpenCIWaitsOnPoll(ctx context.Context, key domain.CIPollKey) ([]domain.CIWaitRecord, error) {
	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.CiWait, error) {
		return q.OpenCIWaitsOnPoll(ctx, db.OpenCIWaitsOnPollParams{Host: key.Host, Repo: key.Repo, Sha: key.SHA})
	})
	if err != nil {
		return nil, fmt.Errorf("open ci waits on %s@%s: %w", key.Repo, key.SHA, err)
	}

	out := make([]domain.CIWaitRecord, 0, len(rows))
	for _, r := range rows {
		out = append(out, ciWaitFrom(r))
	}

	return out, nil
}

// EndCIWaits implements ports.Store.
func (s *Store) EndCIWaits(ctx context.Context, ids []domain.CIWaitID, outcome domain.CIOutcome, at time.Time) (int, error) {
	if !outcome.Valid() {
		return 0, fmt.Errorf("ci wait outcome %q: %w", outcome, domain.ErrInvalid)
	}

	n, err := fetch(ctx, s.inTx, func(q *db.Queries) (int64, error) {
		return q.EndCIWaits(ctx, db.EndCIWaitsParams{EndedAt: &at, Outcome: new(string(outcome)), Ids: uuids(ids)})
	})
	if err != nil {
		return 0, fmt.Errorf("end %d ci waits: %w", len(ids), err)
	}

	return int(n), nil
}

// CIRounds implements ports.Store.
func (s *Store) CIRounds(ctx context.Context, item domain.ItemID, station, visit string) (int, error) {
	if !isUUID(string(item)) {
		return 0, nil
	}

	n, err := fetch(ctx, s.read, func(q *db.Queries) (int64, error) {
		return q.CIRounds(ctx, db.CIRoundsParams{ItemID: uuid.MustParse(string(item)), Station: station, Visit: visit})
	})
	if err != nil {
		return 0, fmt.Errorf("ci rounds of %s at %s: %w", item, station, err)
	}

	return int(n), nil
}

// ciWaitFrom is one row of ci_waits.
func ciWaitFrom(r db.CiWait) domain.CIWaitRecord {
	w := domain.CIWaitRecord{
		ID: domain.CIWaitID(r.ID.String()),
		NewCIWait: domain.NewCIWait{
			Item: domain.ItemID(r.ItemID.String()), Station: r.Station, Visit: r.Visit, Repo: r.Repo, SHA: r.Sha, PRURL: deref(r.PrUrl),
			Phase: domain.CIPhase(r.Phase), Round: int(r.Round), Held: json.RawMessage(r.Held), StartedAt: r.StartedAt.UTC(),
		},
		Outcome: domain.CIOutcome(deref(r.Outcome)),
	}

	if r.EndedAt != nil {
		w.EndedAt = r.EndedAt.UTC()
	}

	return w
}

// ciPollFrom is one row of ci_polls.
func ciPollFrom(r db.CiPoll) domain.CIPoll {
	p := domain.CIPoll{
		CIPollKey:  domain.CIPollKey{Host: r.Host, Repo: r.Repo, SHA: r.Sha},
		NextPollAt: r.NextPollAt.UTC(), Interval: time.Duration(r.IntervalSeconds) * time.Second, ETag: r.Etag,
		Checks: json.RawMessage(r.Checks), LastError: r.LastError, LeaseOwner: deref(r.LeaseOwner),
	}

	if r.FirstCheckSeenAt != nil {
		p.FirstCheckSeenAt = r.FirstCheckSeenAt.UTC()
	}

	if r.LeaseUntil != nil {
		p.LeaseUntil = r.LeaseUntil.UTC()
	}

	return p
}
