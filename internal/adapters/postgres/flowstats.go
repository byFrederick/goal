package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math"
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

// LiveRuns implements ports.FlowStats.
func (s *Store) LiveRuns(ctx context.Context, factory domain.FactoryID) (map[domain.ItemID]domain.LiveRun, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return nil, err
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.LiveRunsRow, error) {
		return q.LiveRuns(ctx, db.LiveRunsParams{
			FactoryID: fid, UsageKind: string(domain.EventUsage), ToolCallKind: string(domain.EventToolCall),
			TodoTool: domain.ToolTodoWrite,
		})
	})
	if err != nil {
		return nil, fmt.Errorf("live runs of factory %s: %w", factory, err)
	}

	out := make(map[domain.ItemID]domain.LiveRun, len(rows))

	for _, r := range rows {
		live := domain.LiveRun{Turns: int(r.UsageTurns)}

		if live.LastToolCall, err = eventData(r.LastToolCall); err != nil {
			return nil, fmt.Errorf("live runs of factory %s: last tool call of item %s: %w", factory, r.ItemID, err)
		}

		if live.LastPlan, err = eventData(r.LastPlan); err != nil {
			return nil, fmt.Errorf("live runs of factory %s: last plan of item %s: %w", factory, r.ItemID, err)
		}

		out[domain.ItemID(r.ItemID.String())] = live
	}

	return out, nil
}

// eventData decodes an event's data; nil when there is none (SQL NULL).
func eventData(raw []byte) (map[string]any, error) {
	if raw == nil {
		return nil, nil
	}

	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("decode event data: %w", err)
	}

	return data, nil
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

// AgentStats implements ports.FlowStats: four reads of one snapshot (lease spans, runs,
// send-backs, denied calls), labelled and tallied here as the domain does.
func (s *Store) AgentStats(ctx context.Context, factory domain.FactoryID, agent string, since, until time.Time) (domain.AgentStats, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return domain.AgentStats{}, err
	}

	var out domain.AgentStats

	err = s.orgTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(q *db.Queries) error {
		leases, err := q.AgentLeases(ctx, db.AgentLeasesParams{FactoryID: fid, Agent: agent, Since: since, Until: until})
		if err != nil {
			return fmt.Errorf("leases: %w", err)
		}

		out.TasksFinished, out.Advanced = int(leases.Finished), int(leases.Advanced)
		out.TaskTimeP85 = time.Duration(math.Round(leases.P85Seconds * float64(time.Second)))

		runs, err := q.AgentRuns(ctx, db.AgentRunsParams{FactoryID: fid, Agent: agent, Since: since, Until: until})
		if err != nil {
			return fmt.Errorf("runs: %w", err)
		}

		out.SpendUSD, out.SucceededRuns, out.MedianTurns = runs.SpendUsd, int(runs.Succeeded), runs.MedianTurns

		back, err := q.AgentSentBack(ctx, db.AgentSentBackParams{FactoryID: fid, Agent: agent, Since: since, Until: until})
		if err != nil {
			return fmt.Errorf("send-backs: %w", err)
		}

		labels := make([]string, 0, len(back))
		for _, b := range back {
			labels = append(labels, domain.VerdictLabel(b.Category, b.Reason))
		}

		out.SentBack, out.Reasons = len(back), domain.Tallies(labels)

		tools, err := q.AgentDeniedTools(ctx, db.AgentDeniedToolsParams{
			FactoryID: fid, Agent: agent, Since: since, Until: until, MaxTools: domain.MaxDeniedTools,
		})
		if err != nil {
			return fmt.Errorf("denied tools: %w", err)
		}

		for _, d := range tools {
			out.Denied = append(out.Denied, domain.Tally{Label: d.Tool, Count: int(d.Calls)})
			out.DeniedCalls = int(d.AllCalls)
		}

		return nil
	})
	if err != nil {
		return domain.AgentStats{}, fmt.Errorf("stats of agent %s of factory %s: %w", agent, factory, err)
	}

	return out, nil
}
