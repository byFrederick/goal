package postgres

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/byFrederick/goal/internal/adapters/postgres/db"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

var _ ports.FlowStats = (*Store)(nil)

// ItemSpans implements ports.FlowStats. The spans are item_spans rows, which the work_items
// trigger of migration 00030 keeps.
func (s *Store) ItemSpans(ctx context.Context, id domain.ItemID) ([]domain.Span, error) {
	uid, err := parseID("item", string(id))
	if err != nil {
		return nil, err
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.ItemSpansRow, error) { return q.ItemSpans(ctx, uid) })
	if err != nil {
		return nil, fmt.Errorf("spans of item %s: %w", id, err)
	}

	out := make([]domain.Span, 0, len(rows))
	for _, r := range rows {
		sp := domain.Span{
			Station: r.Station, State: domain.ItemState(r.State), ReworkRound: int(r.ReworkRound),
			ReworkBy: deref(r.ReworkBy), EnteredAt: r.EnteredAt.UTC(),
		}
		if r.LeftAt != nil {
			sp.LeftAt = r.LeftAt.UTC()
		}

		out = append(out, sp)
	}

	return out, nil
}

// CurrentRun implements ports.FlowStats.
func (s *Store) CurrentRun(ctx context.Context, id domain.ItemID) (*domain.CurrentRun, error) {
	uid, err := parseID("item", string(id))
	if err != nil {
		return nil, err
	}

	r, err := fetch(ctx, s.read, func(q *db.Queries) (db.CurrentRunRow, error) {
		return q.CurrentRun(ctx, db.CurrentRunParams{ItemID: uid, UsageKind: string(domain.EventUsage)})
	})
	if notFound(err) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("current run of item %s: %w", id, err)
	}

	run := domain.Run{
		ID: domain.RunID(r.ID.String()), FactoryID: domain.FactoryID(r.FactoryID.String()), ItemID: id,
		Station: r.Station, Agent: r.Agent, Model: r.Model, StartedAt: r.StartedAt.UTC(),
		Result: domain.RunResult{
			Status: domain.RunStatus(r.Status), CostUSD: r.CostUsd, Turns: int(r.Turns), InputTokens: r.InputTokens,
			OutputTokens: r.OutputTokens, SessionID: deref(r.SessionID), Error: deref(r.Error),
		},
	}
	if r.DefinitionVersionID != nil {
		run.DefinitionVersionID = domain.DefinitionVersionID(r.DefinitionVersionID.String())
	}

	return &domain.CurrentRun{Run: run, Slot: int(r.LeaseSlot), Turns: int(r.UsageTurns)}, nil
}

// LiveTurns implements ports.FlowStats.
func (s *Store) LiveTurns(ctx context.Context, factory domain.FactoryID) (map[domain.ItemID]int, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return nil, err
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.LiveTurnsRow, error) {
		return q.LiveTurns(ctx, db.LiveTurnsParams{FactoryID: fid, UsageKind: string(domain.EventUsage)})
	})
	if err != nil {
		return nil, fmt.Errorf("live turns of factory %s: %w", factory, err)
	}

	out := make(map[domain.ItemID]int, len(rows))
	for _, r := range rows {
		out[domain.ItemID(r.ItemID.String())] = int(r.UsageTurns)
	}

	return out, nil
}

// StationTimes implements ports.FlowStats. Its reads share one snapshot.
func (s *Store) StationTimes(ctx context.Context, factory domain.FactoryID, since, until time.Time) ([]domain.StationTimes, time.Time, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return nil, time.Time{}, err
	}

	tally := stationTally{}

	var history time.Time

	err = s.orgTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(q *db.Queries) error {
		if err := tally.read(ctx, q, fid, since, until); err != nil {
			return err
		}

		first, err := q.LeaseHistoryStart(ctx, fid)

		switch {
		case notFound(err):
		case err != nil:
			return fmt.Errorf("lease history: %w", err)
		default:
			history = first.UTC()
		}

		return nil
	})
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("station times of factory %s: %w", factory, err)
	}

	return tally.sorted(), history, nil
}

// stationTally collects the times of a factory's stations, by station.
type stationTally map[string]*domain.StationTimes

func (t stationTally) at(station string) *domain.StationTimes {
	st, ok := t[station]
	if !ok {
		st = &domain.StationTimes{Station: station}
		t[station] = st
	}

	return st
}

// read adds the lease times, the waiting items and the gate verdicts of factory fid over
// [since, until).
func (t stationTally) read(ctx context.Context, q *db.Queries, fid uuid.UUID, since, until time.Time) error {
	held, err := q.StationLeaseTimes(ctx, db.StationLeaseTimesParams{FactoryID: fid, Since: since, Until: until})
	if err != nil {
		return fmt.Errorf("lease times: %w", err)
	}

	for _, r := range held {
		st := t.at(r.Station)
		st.Leases, st.Busy = int(r.Leases), seconds(r.BusySeconds)
	}

	waiting, err := q.StationWaiting(ctx, db.StationWaitingParams{FactoryID: fid, Until: until, DayAgo: until.Add(-24 * time.Hour)})
	if err != nil {
		return fmt.Errorf("waiting: %w", err)
	}

	for _, r := range waiting {
		st := t.at(r.Station)
		st.WaitingNow, st.WaitingDayAgo = int(r.WaitingNow), int(r.WaitingDayAgo)

		if st.WaitingNow > 0 {
			st.OldestWaiting = r.Oldest.UTC()
		}
	}

	waits, err := q.StationWaitTimes(ctx, db.StationWaitTimesParams{FactoryID: fid, Since: since, Until: until})
	if err != nil {
		return fmt.Errorf("wait times: %w", err)
	}

	for _, r := range waits {
		st := t.at(r.Station)
		st.Waiting, st.Waiters = seconds(r.WaitingSeconds), int(r.Waiters)
	}

	verdicts, err := q.StationVerdicts(ctx, db.StationVerdictsParams{FactoryID: fid, Since: since, Until: until})
	if err != nil {
		return fmt.Errorf("verdicts: %w", err)
	}

	for _, r := range verdicts {
		st := t.at(deref(r.Station))
		st.Verdicts, st.Rejected = int(r.Verdicts), int(r.Rejected)
	}

	return nil
}

func (t stationTally) sorted() []domain.StationTimes {
	out := make([]domain.StationTimes, 0, len(t))
	for _, name := range slices.Sorted(maps.Keys(t)) {
		out = append(out, *t[name])
	}

	return out
}
