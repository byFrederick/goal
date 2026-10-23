package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/byFrederick/goal/internal/adapters/postgres/db"
	"github.com/byFrederick/goal/internal/domain"
)

// slotKey is the unique (factory_id, station, slot) index on leases.
const slotKey = "leases_factory_id_station_slot_key"

// uniqueViolation is PostgreSQL's SQLSTATE for a unique index rejecting a row.
const uniqueViolation = "23505"

func violates(err error, constraint string) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == constraint
}

func itemFrom(w db.WorkItem) (domain.Item, error) {
	outputs := map[string]json.RawMessage{}
	if err := json.Unmarshal(w.Outputs, &outputs); err != nil {
		return domain.Item{}, fmt.Errorf("item %s: outputs: %w", w.ID, err)
	}

	it := domain.Item{
		ID: domain.ItemID(w.ID.String()), FactoryID: domain.FactoryID(w.FactoryID.String()), Number: w.Number,
		Type: w.Type, Lane: deref(w.Lane), Title: w.Title, Body: w.Body, Station: w.Station,
		State: domain.ItemState(w.State), Class: domain.ClassOfService(w.ClassOfService), Priority: int(w.Priority),
		ReworkCount: int(w.ReworkCount), CostUSD: w.CostUsd, Outputs: outputs,
		CreatedAt: w.CreatedAt.UTC(), UpdatedAt: w.UpdatedAt.UTC(),
	}

	if w.ParentID != nil {
		it.ParentID = domain.ItemID(w.ParentID.String())
	}

	if w.DoneAt != nil {
		it.DoneAt = w.DoneAt.UTC()
	}

	return it, nil
}

func leaseFrom(l db.Lease) domain.Lease {
	return domain.Lease{
		ItemID: domain.ItemID(l.ItemID.String()), FactoryID: domain.FactoryID(l.FactoryID.String()),
		Station: l.Station, Agent: l.Agent, Slot: int(l.Slot),
		AcquiredAt: l.AcquiredAt.UTC(), HeartbeatAt: l.HeartbeatAt.UTC(), ExpiresAt: l.ExpiresAt.UTC(),
	}
}

// appendEvents inserts events in order; their ids come from the table's identity column.
func appendEvents(ctx context.Context, q *db.Queries, events []domain.Event) error {
	if len(events) == 0 {
		return nil
	}

	rows := make([]db.InsertEventsParams, 0, len(events))

	for _, e := range events {
		row, err := eventRow(e)
		if err != nil {
			return err
		}

		rows = append(rows, row)
	}

	if _, err := q.InsertEvents(ctx, rows); err != nil {
		return fmt.Errorf("append events: %w", err)
	}

	return nil
}

func eventRow(e domain.Event) (db.InsertEventsParams, error) {
	factory, err := parseID("factory", string(e.FactoryID))
	if err != nil {
		return db.InsertEventsParams{}, fmt.Errorf("event %s: %w", e.Kind, err)
	}

	item, err := parseID("item", string(e.ItemID))
	if err != nil {
		return db.InsertEventsParams{}, fmt.Errorf("event %s: %w", e.Kind, err)
	}

	var run *uuid.UUID

	if e.RunID != "" {
		id, err := parseID("run", string(e.RunID))
		if err != nil {
			return db.InsertEventsParams{}, fmt.Errorf("event %s: %w", e.Kind, err)
		}

		run = &id
	}

	data := []byte("{}")
	if e.Data != nil {
		if data, err = json.Marshal(e.Data); err != nil {
			return db.InsertEventsParams{}, fmt.Errorf("event %s: data: %w", e.Kind, err)
		}
	}

	return db.InsertEventsParams{
		FactoryID: factory, ItemID: item, RunID: run, At: e.At, Kind: string(e.Kind),
		Station: optional(e.Station), Actor: e.Actor, Data: data,
	}, nil
}

// optional maps "" to NULL.
func optional(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}

// optionalTime maps the zero time to NULL.
func optionalTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}

	return &t
}
