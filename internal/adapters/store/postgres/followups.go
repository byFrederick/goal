package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
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

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.ItemFollowUpsRow, error) { return q.ItemFollowUps(ctx, uid) })
	if err != nil {
		return nil, fmt.Errorf("follow-ups of item %s: %w", item, err)
	}

	out := make([]domain.FollowUp, 0, len(rows))

	for _, r := range rows {
		f, err := followUpFrom(r.FollowUp)
		if err != nil {
			return nil, err
		}

		f.FiledFactory, f.FiledNumber = domain.FactoryID(r.FiledFactoryID), r.FiledNumber
		out = append(out, f)
	}

	return out, nil
}

// ItemNotes implements ports.Store.
func (s *Store) ItemNotes(ctx context.Context, item domain.ItemID, limit int) ([]domain.Event, error) {
	if !isUUID(string(item)) {
		return nil, nil // an id that is not a UUID names no item, which has none
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.ItemEvent, error) {
		return q.ItemNotes(ctx, db.ItemNotesParams{ItemID: uuid.MustParse(string(item)), MaxNotes: int64(limit)})
	})
	if err != nil {
		return nil, fmt.Errorf("notes of item %s: %w", item, err)
	}

	return convertAll(rows, eventFrom)
}

// DeliverNotes implements ports.Store: one transaction per note, which locks its follow_ups row
// (ClaimNote) and then only the target item, so two items noting each other never wait on each
// other's locks.
func (s *Store) DeliverNotes(ctx context.Context, factories []domain.NoteFactory, at time.Time, limit int) ([]domain.FollowUp, error) {
	var out []domain.FollowUp

	for len(out) < limit {
		var (
			done bool
			f    domain.FollowUp
		)

		err := s.inTx(ctx, func(q *db.Queries) error {
			row, err := q.ClaimNote(ctx)
			if notFound(err) {
				done = true

				return nil
			}

			if err != nil {
				return fmt.Errorf("claim note: %w", err)
			}

			f, err = deliverNote(ctx, q, row, factories, at)

			return err
		})
		if err != nil {
			return out, fmt.Errorf("deliver notes: %w", err)
		}

		if done {
			break
		}

		out = append(out, f)
	}

	return out, nil
}

// deliverNote routes one claimed note (domain.RouteNote) and records the outcome: an item_note
// on the locked target and the note delivered, or the note for triage with the reason.
func deliverNote(ctx context.Context, q *db.Queries, row db.ClaimNoteRow, factories []domain.NoteFactory, at time.Time) (domain.FollowUp, error) {
	f, err := followUpFrom(row.FollowUp)
	if err != nil {
		return domain.FollowUp{}, err
	}

	to, target, today, err := noteTarget(ctx, q, f, factories, at)
	if err != nil {
		return domain.FollowUp{}, err
	}

	if reason := domain.RouteNote(f.FactoryID, to, domain.ItemState(target.State), today); reason != "" {
		r, err := q.TriageFollowUp(ctx, db.TriageFollowUpParams{
			ID: row.FollowUp.ID, Reason: optional(domain.NoteTriageReason(*f.For, reason)), At: at,
		})
		if err != nil {
			return domain.FollowUp{}, fmt.Errorf("follow-up %s to triage: %w", f.ID, err)
		}

		return followUpFrom(r)
	}

	name := string(f.FactoryID)
	if nf, ok := domain.NoteFactoryNamed(factories, name); ok && nf.Name != "" {
		name = nf.Name
	}

	if err := appendEvents(ctx, q, []domain.Event{{
		FactoryID: domain.FactoryID(target.FactoryID.String()), ItemID: domain.ItemID(target.ID.String()), At: at,
		Kind: domain.EventItemNote, Station: target.Station, Actor: domain.ActorEngine,
		Data: domain.NoteData(f, domain.ItemRef{Factory: name, Number: row.SourceNumber}),
	}}); err != nil {
		return domain.FollowUp{}, fmt.Errorf("follow-up %s: item_note: %w", f.ID, err)
	}

	r, err := q.DeliverFollowUp(ctx, db.DeliverFollowUpParams{ID: row.FollowUp.ID, At: at})
	if err != nil {
		return domain.FollowUp{}, fmt.Errorf("follow-up %s delivered: %w", f.ID, err)
	}

	return followUpFrom(r)
}

// noteTarget resolves what domain.RouteNote needs of note f's for: the factory it names (nil for
// none), the item, locked, when that factory accepts the sender (zero when it has none), and,
// for another factory's open item, the notes it took from other factories today.
func noteTarget(ctx context.Context, q *db.Queries, f domain.FollowUp, factories []domain.NoteFactory, at time.Time) (
	*domain.NoteFactory, db.WorkItem, int, error,
) {
	nf, ok := domain.NoteFactoryNamed(factories, f.For.Factory)
	if !ok || !isUUID(string(nf.ID)) {
		return nil, db.WorkItem{}, 0, nil
	}

	id := uuid.MustParse(string(nf.ID))
	if nf.ID != f.FactoryID && !slices.Contains(nf.FollowUpsFrom, f.FactoryID) {
		return &nf, db.WorkItem{}, 0, nil
	}

	target, err := q.LockNoteTarget(ctx, db.LockNoteTargetParams{FactoryID: id, Number: f.For.Number})
	if notFound(err) {
		return &nf, db.WorkItem{}, 0, nil
	}

	if err != nil {
		return nil, db.WorkItem{}, 0, fmt.Errorf("lock note target: %w", err)
	}

	if nf.ID == f.FactoryID {
		return &nf, target, 0, nil
	}

	today, err := notesToday(ctx, q, id, at)

	return &nf, target, today, err
}

// notesToday counts the notes from other factories the factory took in the 24 hours before at,
// under the factory's cap lock (LockNoteCap), so concurrent deliveries cannot pass the cap.
func notesToday(ctx context.Context, q *db.Queries, factory uuid.UUID, at time.Time) (int, error) {
	if err := q.LockNoteCap(ctx, factory); err != nil {
		return 0, fmt.Errorf("lock note cap: %w", err)
	}

	n, err := q.NotesFromOtherFactories(ctx, db.NotesFromOtherFactoriesParams{FactoryID: factory, Since: at.Add(-24 * time.Hour)})
	if err != nil {
		return 0, fmt.Errorf("count notes from other factories: %w", err)
	}

	return int(n), nil
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
