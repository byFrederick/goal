package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// openAndonKey is the unique index that keeps one open pull per factory (migration 00001).
const openAndonKey = "andon_pulls_open_key"

// PullAndon implements ports.Store: the pull and its audit entry in one transaction, whose commit
// notifies the stream (the andon_pulls trigger of migration 00032).
func (s *Store) PullAndon(ctx context.Context, p domain.AndonPull, by domain.Actor) (domain.AndonPull, error) {
	factory, err := parseID("factory", string(p.FactoryID))
	if err != nil {
		return domain.AndonPull{}, err
	}

	data := p.Data
	if data == nil {
		data = map[string]any{}
	}

	raw, err := json.Marshal(data)
	if err != nil {
		return domain.AndonPull{}, fmt.Errorf("andon data: %w", err)
	}

	var out domain.AndonPull

	err = s.inTx(ctx, func(q *db.Queries) error {
		// The open-pull index spans organizations: only a factory of this one may meet it.
		if _, err := q.AndonFactory(ctx, factory); notFound(err) {
			return fmt.Errorf("factory %s: %w", p.FactoryID, ports.ErrNotFound)
		} else if err != nil {
			return err
		}

		row, err := q.InsertAndonPull(ctx, db.InsertAndonPullParams{
			FactoryID: factory, Kind: string(p.Kind), Station: optional(p.Station), Reason: p.Reason,
			PulledBy: by.Ref(), PulledAt: p.PulledAt, Data: raw,
		})

		switch {
		case violates(err, openAndonKey):
			return fmt.Errorf("the line is already stopped: %w", ports.ErrConflict)
		case err != nil:
			return err
		}

		if out, err = andonFrom(row); err != nil {
			return err
		}

		return appendAudit(ctx, q, andonAuditEntry(domain.AuditAndonPulled, out, by, out.PulledAt))
	})
	if err != nil {
		return domain.AndonPull{}, fmt.Errorf("pull the andon of factory %s: %w", p.FactoryID, err)
	}

	return out, nil
}

// ClearAndon implements ports.Store.
func (s *Store) ClearAndon(ctx context.Context, factory domain.FactoryID, by domain.Actor, note string, at time.Time) (domain.AndonPull, error) {
	id, err := parseID("factory", string(factory))
	if err != nil {
		return domain.AndonPull{}, err
	}

	var out domain.AndonPull

	err = s.inTx(ctx, func(q *db.Queries) error {
		row, err := q.ClearOpenAndon(ctx, db.ClearOpenAndonParams{FactoryID: id, ClearedAt: &at, ClearedBy: new(by.Ref()), Note: optional(note)})
		if notFound(err) {
			return fmt.Errorf("the line is not stopped: %w", ports.ErrNotFound)
		}

		if err != nil {
			return err
		}

		if out, err = andonFrom(row); err != nil {
			return err
		}

		return appendAudit(ctx, q, andonAuditEntry(domain.AuditAndonCleared, out, by, at))
	})
	if err != nil {
		return domain.AndonPull{}, fmt.Errorf("clear the andon of factory %s: %w", factory, err)
	}

	return out, nil
}

// OpenAndon implements ports.Store.
func (s *Store) OpenAndon(ctx context.Context, factory domain.FactoryID) (*domain.AndonPull, error) {
	id, err := parseID("factory", string(factory))
	if err != nil {
		return nil, err
	}

	row, err := fetch(ctx, s.read, func(q *db.Queries) (db.AndonPull, error) { return q.OpenAndon(ctx, id) })
	if notFound(err) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("open andon of factory %s: %w", factory, err)
	}

	p, err := andonFrom(row)
	if err != nil {
		return nil, err
	}

	return &p, nil
}

// AndonPulls implements ports.Store.
func (s *Store) AndonPulls(ctx context.Context, factory domain.FactoryID, limit int) ([]domain.AndonPull, error) {
	id, err := parseID("factory", string(factory))
	if err != nil {
		return nil, err
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.AndonPull, error) {
		return q.AndonPulls(ctx, db.AndonPullsParams{FactoryID: id, MaxPulls: int64(limit)})
	})
	if err != nil {
		return nil, fmt.Errorf("andon pulls of factory %s: %w", factory, err)
	}

	return convertAll(rows, andonFrom)
}

// GateDecisions implements ports.FlowStats.
func (s *Store) GateDecisions(ctx context.Context, factory domain.FactoryID, station string, after time.Time, n int) (domain.GateTally, error) {
	id, err := parseID("factory", string(factory))
	if err != nil {
		return domain.GateTally{}, err
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.GateVerdictsRow, error) {
		return q.GateVerdicts(ctx, db.GateVerdictsParams{FactoryID: id, Station: &station, MaxVerdicts: int64(n), After: after})
	})
	if err != nil {
		return domain.GateTally{}, fmt.Errorf("verdicts of %s at factory %s: %w", station, factory, err)
	}

	verdicts := make([]domain.GateVerdict, 0, len(rows))
	for _, r := range rows {
		verdicts = append(verdicts, domain.GateVerdict{
			Rejected: r.Kind == string(domain.EventGateRejected), Category: r.Category, Reason: r.Reason, At: r.At.UTC(),
		})
	}

	return domain.TallyGate(station, verdicts), nil
}

func andonFrom(r db.AndonPull) (domain.AndonPull, error) {
	data := map[string]any{}
	if err := json.Unmarshal(r.Data, &data); err != nil {
		return domain.AndonPull{}, fmt.Errorf("andon pull %d: data: %w", r.ID, err)
	}

	p := domain.AndonPull{
		ID: r.ID, FactoryID: domain.FactoryID(r.FactoryID.String()), Kind: domain.AndonKind(r.Kind), Station: deref(r.Station),
		Reason: r.Reason, PulledBy: r.PulledBy, PulledAt: r.PulledAt.UTC(), ClearedBy: deref(r.ClearedBy), Note: deref(r.Note),
		Data: data,
	}

	if r.ClearedAt != nil {
		p.ClearedAt = r.ClearedAt.UTC()
	}

	return p, nil
}

// andonAuditEntry is the audit entry of a pull or a clear: the pull's id, kind and reason, and
// the clearing note.
func andonAuditEntry(action string, p domain.AndonPull, by domain.Actor, at time.Time) domain.AuditEntry {
	data := map[string]any{"pull": strconv.FormatInt(p.ID, 10), "kind": string(p.Kind), "reason": p.Reason}
	if !p.Open() {
		data["note"] = p.Note
	}

	return domain.AuditEntry{
		At: at, Actor: by, Action: action, TargetKind: domain.AuditTargetFactory, TargetID: string(p.FactoryID),
		FactoryID: p.FactoryID, Data: data,
	}
}
