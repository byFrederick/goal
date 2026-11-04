package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
)

// FollowUps implements ports.Store.
func (s *Store) FollowUps(ctx context.Context, item domain.ItemID) ([]domain.FollowUp, error) {
	if !isUUID(string(item)) {
		return nil, nil // an id that is not a UUID names no item, which has none
	}

	uid := uuid.MustParse(string(item))

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.FollowUp, error) { return q.ItemFollowUps(ctx, uid) })
	if err != nil {
		return nil, fmt.Errorf("follow-ups of item %s: %w", item, err)
	}

	out := make([]domain.FollowUp, 0, len(rows))

	for _, r := range rows {
		f, err := followUpFrom(r)
		if err != nil {
			return nil, err
		}

		out = append(out, f)
	}

	return out, nil
}

// keepFollowUps keeps a transition's follow-ups on the locked item cur at at; a text already kept
// for it is skipped (follow_ups_text_key).
func keepFollowUps(ctx context.Context, q *db.Queries, cur db.WorkItem, fs []domain.FollowUp, at time.Time) error {
	for _, f := range fs {
		p, err := keepParams(cur, f, at)
		if err != nil {
			return err
		}

		if err := q.KeepFollowUp(ctx, p); err != nil {
			return fmt.Errorf("keep follow-up: %w", err)
		}
	}

	return nil
}

func keepParams(cur db.WorkItem, f domain.FollowUp, at time.Time) (db.KeepFollowUpParams, error) {
	id, err := newID()
	if err != nil {
		return db.KeepFollowUpParams{}, err
	}

	run, err := optionalID("run", string(f.RunID))
	if err != nil {
		return db.KeepFollowUpParams{}, err
	}

	p := db.KeepFollowUpParams{
		ID: id, FactoryID: cur.FactoryID, ItemID: cur.ID, RunID: run, Station: f.Station, Agent: f.Agent,
		Text: f.Text, TextKey: domain.FollowUpKey(f.Text), Target: optional(string(f.Target)),
		State: string(f.State), Reason: optional(f.Reason), CreatedAt: at,
	}

	if f.For != nil {
		p.ForFactory, p.ForNumber = &f.For.Factory, &f.For.Number
	}

	if f.Start != nil {
		if p.Start, err = json.Marshal(f.Start); err != nil {
			return db.KeepFollowUpParams{}, fmt.Errorf("follow-up start: %w", err)
		}
	}

	return p, nil
}

func followUpFrom(r db.FollowUp) (domain.FollowUp, error) {
	f := domain.FollowUp{
		ID: domain.FollowUpID(r.ID.String()), FactoryID: domain.FactoryID(r.FactoryID.String()),
		ItemID: domain.ItemID(r.ItemID.String()), Station: r.Station, Agent: r.Agent, Text: r.Text,
		Target: domain.FollowUpTarget(deref(r.Target)), State: domain.FollowUpState(r.State), Reason: deref(r.Reason),
		CreatedAt: r.CreatedAt,
	}

	if r.RunID != nil {
		f.RunID = domain.RunID(r.RunID.String())
	}

	if r.FiledItemID != nil {
		f.FiledItem = domain.ItemID(r.FiledItemID.String())
	}

	if r.ForFactory != nil && r.ForNumber != nil {
		f.For = &domain.ItemRef{Factory: *r.ForFactory, Number: *r.ForNumber}
	}

	if r.Start != nil {
		f.Start = &domain.FollowUpPlan{}
		if err := json.Unmarshal(r.Start, f.Start); err != nil {
			return domain.FollowUp{}, fmt.Errorf("follow-up %s: start: %w", r.ID, err)
		}
	}

	return f, nil
}

func isUUID(s string) bool { return uuid.Validate(s) == nil }
