package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.Watches = (*Store)(nil)

// undoesKey is 00100's index of one live undo per proposal.
const undoesKey = "jonah_proposals_undoes_key"

// StationOutcomes implements ports.Watches.
func (s *Store) StationOutcomes(ctx context.Context, factory domain.FactoryID, station string, versions []domain.DefinitionVersionID) ([]domain.StationOutcome, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return nil, domain.ErrNoOrg
	}

	f, err := parseID("factory", string(factory))
	if err != nil {
		return nil, fmt.Errorf("station outcomes: %w", err)
	}

	vs, err := parseIDs("definition version", versions)
	if err != nil {
		return nil, fmt.Errorf("station outcomes: %w", err)
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.StationOutcomesRow, error) {
		return q.StationOutcomes(ctx, db.StationOutcomesParams{FactoryID: f, Station: station, Versions: vs})
	})
	if err != nil {
		return nil, fmt.Errorf("outcomes at %s of factory %s: %w", station, factory, err)
	}

	out := make([]domain.StationOutcome, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.StationOutcome{
			VersionID: domain.DefinitionVersionID(r.VersionID.String()), ItemID: domain.ItemID(r.ItemID.String()), At: r.At.UTC(),
			SentBack: r.SentBack, CostUSD: r.CostUsd, LeadTime: time.Duration(r.LeadSeconds * float64(time.Second)),
		})
	}

	return out, nil
}

// SaveWatch implements ports.Watches.
func (s *Store) SaveWatch(ctx context.Context, w domain.Watch) (bool, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return false, domain.ErrNoOrg
	}

	p, err := upsertWatchParams(w)
	if err != nil {
		return false, fmt.Errorf("save watch: %w", err)
	}

	_, err = fetch(ctx, s.inTx, func(q *db.Queries) (db.Watch, error) { return q.UpsertWatch(ctx, p) })

	switch {
	case notFound(err):
		return false, nil
	case isCheckViolationErr(err):
		return false, fmt.Errorf("save watch of %s at %s: %w: %w", w.VersionID, w.Station, domain.ErrInvalid, err)
	case err != nil:
		return false, fmt.Errorf("save watch of %s at %s: %w", w.VersionID, w.Station, err)
	}

	return true, nil
}

func upsertWatchParams(w domain.Watch) (db.UpsertWatchParams, error) {
	p := db.UpsertWatchParams{
		Station: w.Station, Metric: string(w.Metric), BeforeN: int32(min(w.Before.N, 1<<31-1)), BeforeValue: w.Before.Value, //nolint:gosec // clamped
		AfterN: int32(min(w.After.N, 1<<31-1)), AfterValue: w.After.Value, Verdict: string(w.Verdict), //nolint:gosec // clamped
		OpenedAt: w.OpenedAt, UpdatedAt: w.UpdatedAt,
	}

	var err error

	if p.ID, err = newID(); err != nil {
		return p, err
	}

	if p.FactoryID, err = parseID("factory", string(w.FactoryID)); err != nil {
		return p, err
	}

	if p.VersionID, err = parseID("definition version", string(w.VersionID)); err != nil {
		return p, err
	}

	if p.ProposalID, err = optionalID("proposal", string(w.ProposalID)); err != nil {
		return p, err
	}

	if w.Interval != nil {
		p.IntervalLow, p.IntervalHigh = &w.Interval.Low, &w.Interval.High
	}

	if !w.ClosedAt.IsZero() {
		p.ClosedAt = &w.ClosedAt
	}

	return p, nil
}

// VersionWatches implements ports.Watches.
func (s *Store) VersionWatches(ctx context.Context, factory domain.FactoryID, versions []domain.DefinitionVersionID) ([]domain.Watch, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return nil, domain.ErrNoOrg
	}

	f, err := parseID("factory", string(factory))
	if err != nil {
		return nil, fmt.Errorf("version watches: %w", err)
	}

	vs, err := parseIDs("definition version", versions)
	if err != nil {
		return nil, fmt.Errorf("version watches: %w", err)
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.Watch, error) {
		return q.VersionWatches(ctx, db.VersionWatchesParams{FactoryID: f, Versions: vs})
	})
	if err != nil {
		return nil, fmt.Errorf("watches of factory %s: %w", factory, err)
	}

	return watchesFrom(rows, 0), nil
}

// ProposalWatches implements ports.Watches.
func (s *Store) ProposalWatches(ctx context.Context, proposals []domain.ProposalID) ([]domain.Watch, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return nil, domain.ErrNoOrg
	}

	ps, err := parseIDs("proposal", proposals)
	if err != nil {
		return nil, fmt.Errorf("proposal watches: %w", err)
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.Watch, error) { return q.ProposalWatches(ctx, ps) })
	if err != nil {
		return nil, fmt.Errorf("proposal watches: %w", err)
	}

	return watchesFrom(rows, 0), nil
}

// WorseProposalWatches implements ports.Watches.
func (s *Store) WorseProposalWatches(ctx context.Context, limit int) ([]domain.Watch, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return nil, domain.ErrNoOrg
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.WorseProposalWatchesRow, error) {
		return q.WorseProposalWatches(ctx, clampLimit(limit))
	})
	if err != nil {
		return nil, fmt.Errorf("worse proposal watches: %w", err)
	}

	out := make([]domain.Watch, 0, len(rows))
	for _, r := range rows {
		out = append(out, watchFrom(db.Watch{
			ID: r.ID, OrgID: r.OrgID, FactoryID: r.FactoryID, VersionID: r.VersionID, ProposalID: r.ProposalID, Station: r.Station,
			Metric: r.Metric, BeforeN: r.BeforeN, BeforeValue: r.BeforeValue, AfterN: r.AfterN, AfterValue: r.AfterValue,
			IntervalLow: r.IntervalLow, IntervalHigh: r.IntervalHigh, Verdict: r.Verdict, OpenedAt: r.OpenedAt, ClosedAt: r.ClosedAt,
			UpdatedAt: r.UpdatedAt,
		}, r.ProposalNumber))
	}

	return out, nil
}

func watchesFrom(rows []db.Watch, number int64) []domain.Watch {
	out := make([]domain.Watch, 0, len(rows))
	for _, r := range rows {
		out = append(out, watchFrom(r, number))
	}

	return out
}

func watchFrom(r db.Watch, number int64) domain.Watch {
	w := domain.Watch{
		FactoryID: domain.FactoryID(r.FactoryID.String()), VersionID: domain.DefinitionVersionID(r.VersionID.String()),
		Station: r.Station, Metric: domain.ProposalMetric(r.Metric), Verdict: domain.WatchVerdict(r.Verdict), ProposalNumber: number,
		Before: domain.WatchSide{N: int(r.BeforeN), Value: r.BeforeValue}, After: domain.WatchSide{N: int(r.AfterN), Value: r.AfterValue},
		OpenedAt: r.OpenedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
	}

	if r.ProposalID != nil {
		w.ProposalID = domain.ProposalID(r.ProposalID.String())
	}

	if r.IntervalLow != nil && r.IntervalHigh != nil {
		w.Interval = &domain.WatchInterval{Low: *r.IntervalLow, High: *r.IntervalHigh}
	}

	if r.ClosedAt != nil {
		w.ClosedAt = r.ClosedAt.UTC()
	}

	return w
}

// UndoProposal implements ports.JonahStore: the factory's next number, in one transaction.
func (s *Store) UndoProposal(ctx context.Context, factory domain.FactoryID, undoes domain.ProposalID, version domain.DefinitionVersionID,
	n domain.NewProposal, at time.Time,
) (domain.Proposal, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.Proposal{}, domain.ErrNoOrg
	}

	if err := n.Check(); err != nil {
		return domain.Proposal{}, fmt.Errorf("undo proposal: %w", err)
	}

	p, err := undoProposalParams(factory, undoes, version, n, at)
	if err != nil {
		return domain.Proposal{}, fmt.Errorf("undo proposal: %w", err)
	}

	row, err := fetch(ctx, s.inTx, func(q *db.Queries) (db.JonahProposal, error) {
		last, err := q.LastProposalNumber(ctx, p.FactoryID)
		if err != nil {
			return db.JonahProposal{}, fmt.Errorf("last proposal number: %w", err)
		}

		p.Number = last + 1

		row, err := q.InsertUndoProposal(ctx, p)
		if err != nil {
			return db.JonahProposal{}, err
		}

		return row, supersede(ctx, q, p.FactoryID, n.Supersedes, at)
	})

	switch {
	case violates(err, undoesKey):
		return domain.Proposal{}, fmt.Errorf("undo proposal of %s: one is open: %w", undoes, ports.ErrConflict)
	case isForeignKeyViolation(err):
		return domain.Proposal{}, fmt.Errorf("undo proposal of %s: %w: %w", undoes, ports.ErrNotFound, err)
	case isCheckViolationErr(err):
		return domain.Proposal{}, fmt.Errorf("undo proposal of %s: %w: %w", undoes, domain.ErrInvalid, err)
	case err != nil:
		return domain.Proposal{}, fmt.Errorf("undo proposal of %s: %w", undoes, err)
	}

	return proposalFrom(row)
}

func undoProposalParams(factory domain.FactoryID, undoes domain.ProposalID, version domain.DefinitionVersionID, n domain.NewProposal,
	at time.Time,
) (db.InsertUndoProposalParams, error) {
	p := db.InsertUndoProposalParams{Title: n.Title, Summary: n.Summary, Paths: n.Paths(), CreatedAt: at}

	var err error

	if p.ID, err = newID(); err != nil {
		return p, err
	}

	if p.FactoryID, err = parseID("factory", string(factory)); err != nil {
		return p, err
	}

	if p.VersionID, err = parseID("definition version", string(version)); err != nil {
		return p, err
	}

	if p.Undoes, err = optionalID("proposal", string(undoes)); err != nil || p.Undoes == nil {
		return p, fmt.Errorf("no proposal %q to undo: %w", undoes, ports.ErrNotFound)
	}

	if p.Edits, err = json.Marshal(n.Edits); err != nil {
		return p, fmt.Errorf("edits: %w", err)
	}

	return p, nil
}

// parseIDs parses ids of kind.
func parseIDs[T ~string](kind string, ids []T) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(ids))

	for _, id := range ids {
		u, err := parseID(kind, string(id))
		if err != nil {
			return nil, err
		}

		out = append(out, u)
	}

	return out, nil
}
