package postgres

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// checkStateArg is a firing's check state as its column takes it: NULL for none.
func checkStateArg(s domain.CheckState) *string {
	if s == "" {
		return nil
	}

	return new(string(s))
}

// ClaimCheck implements ports.Intake. The slot caps are checked first, under the organization's
// slot lock while a cap is set (checkSlotCaps, which counts running checks with the leases), so a
// check and a station's claim never take the same free slot.
func (in *Intake) ClaimCheck(ctx context.Context, factory domain.FactoryID, holder string, now time.Time, lease time.Duration,
) (domain.TriggerFiring, domain.CheckLease, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return domain.TriggerFiring{}, domain.CheckLease{}, err
	}

	token, err := newID()
	if err != nil {
		return domain.TriggerFiring{}, domain.CheckLease{}, err
	}

	until := now.Add(lease)

	var row db.ClaimTriggerCheckRow

	err = in.s.inTx(ctx, func(q *db.Queries) error {
		if err := checkSlotCaps(ctx, q, fid); err != nil {
			return err
		}

		var err error

		row, err = q.ClaimTriggerCheck(ctx, db.ClaimTriggerCheckParams{Holder: holder, Token: &token, Until: &until, FactoryID: fid})
		if notFound(err) {
			return fmt.Errorf("queued check of factory %s: %w", factory, ports.ErrNotFound)
		}

		return err
	})
	if err != nil {
		return domain.TriggerFiring{}, domain.CheckLease{}, fmt.Errorf("claim check: %w", err)
	}

	f := firingFrom(db.TriggerFiringByKeyRow{
		ID: row.ID, FactoryID: row.FactoryID, Trigger: row.Trigger, Source: row.Source, EventKey: row.EventKey, IdempotencyKey: row.IdempotencyKey,
		Dedupe: row.Dedupe, Outcome: row.Outcome, Reason: row.Reason, ItemID: row.ItemID, Missed: row.Missed, At: row.At,
		CheckState: row.CheckState, RunID: row.RunID, CostUsd: row.CostUsd, Decisions: row.Decisions,
	})

	return f, domain.CheckLease{
		FiringID: f.ID, FactoryID: factory, Trigger: f.Trigger, Holder: holder, Token: token.String(), Until: row.LeaseUntil.UTC(),
	}, nil
}

// checkLeaseIDs are l's firing id and token.
func checkLeaseIDs(l domain.CheckLease) (uuid.UUID, uuid.UUID, error) {
	id, err := parseID("firing", l.FiringID)
	if err != nil {
		return uuid.UUID{}, uuid.UUID{}, err
	}

	token, err := uuid.Parse(l.Token)
	if err != nil {
		return uuid.UUID{}, uuid.UUID{}, fmt.Errorf("check %s: %w", l.FiringID, ports.ErrLeaseLost)
	}

	return id, token, nil
}

// HeartbeatCheck implements ports.Intake.
func (in *Intake) HeartbeatCheck(ctx context.Context, l domain.CheckLease, until time.Time) error {
	id, token, err := checkLeaseIDs(l)
	if err != nil {
		return err
	}

	n, err := fetch(ctx, in.s.inTx, func(q *db.Queries) (int64, error) {
		return q.HeartbeatTriggerCheck(ctx, db.HeartbeatTriggerCheckParams{Until: &until, ID: id, Token: &token})
	})

	switch {
	case err != nil:
		return fmt.Errorf("heartbeat check %s: %w", l.FiringID, err)
	case n == 0:
		return fmt.Errorf("check %s: %w", l.FiringID, ports.ErrLeaseLost)
	}

	return nil
}

// FinishCheck implements ports.Intake: the trigger's advisory lock (LockTriggerFiring, as Fire
// takes it) first, then the firing's row, then the answer's items in id order (applyCheck), so it
// never deadlocks with Fire or DeliverNotes.
func (in *Intake) FinishCheck(ctx context.Context, l domain.CheckLease, r domain.CheckResult) error {
	if err := r.Check(); err != nil {
		return fmt.Errorf("finish check %s: %w", l.FiringID, err)
	}

	id, token, err := checkLeaseIDs(l)
	if err != nil {
		return err
	}

	run, err := optionalID("run", string(r.RunID))
	if err != nil {
		return err
	}

	fid, err := parseID("factory", string(l.FactoryID))
	if err != nil {
		return err
	}

	p := db.FinishTriggerCheckParams{
		State: checkStateArg(r.State), RunID: run, CostUsd: r.CostUSD, Reason: cutText(r.Reason, maxFiringReason), At: &r.At, ID: id, Token: &token,
	}

	err = in.s.inTx(ctx, func(q *db.Queries) error {
		if err := q.LockTriggerFiring(ctx, db.LockTriggerFiringParams{FactoryID: fid, Trigger: l.Trigger}); err != nil {
			return fmt.Errorf("lock the trigger: %w", err)
		}

		row, err := q.RunningTriggerCheck(ctx, db.RunningTriggerCheckParams{ID: id, Token: &token})
		if notFound(err) {
			return fmt.Errorf("check %s: %w", l.FiringID, ports.ErrLeaseLost)
		}

		if err != nil {
			return err
		}

		return finishCheckInTx(ctx, q, row, r, p)
	})
	if err != nil {
		return fmt.Errorf("finish check %s: %w", l.FiringID, ruleError(err, ""))
	}

	return nil
}

// finishCheckInTx ends running check c as r says with p, in q's transaction under the trigger's
// lock: a done r's answer applied (applyCheck) and kept, and its state saved unless c is a try.
func finishCheckInTx(ctx context.Context, q *db.Queries, c db.RunningTriggerCheckRow, r domain.CheckResult, p db.FinishTriggerCheckParams) error {
	if r.State == domain.CheckDone {
		a, err := applyCheck(ctx, q, c, r)
		if err != nil {
			return err
		}

		p.Decisions = r.Decisions
		p.Applied, _ = json.Marshal(a) // plain fields: never fails
	}

	if _, err := q.FinishTriggerCheck(ctx, p); err != nil {
		return err
	}

	if r.State != domain.CheckDone || c.Try || len(r.Saved) == 0 || string(r.Saved) == "null" {
		return nil
	}

	return q.SaveTriggerState(ctx, db.SaveTriggerStateParams{FactoryID: c.FactoryID, Trigger: c.Trigger, State: r.Saved, UpdatedAt: r.At})
}

// applyCheck applies done check c's answer in q's transaction, under the trigger's lock, as
// domain.ApplyCheck decides from the keys it names (their items locked in id order) and the items
// the trigger filed today: it files each item through fileItem and keeps it as its key's, and
// writes each note as an item_note and its key's last note. A try only decides.
func applyCheck(ctx context.Context, q *db.Queries, c db.RunningTriggerCheckRow, r domain.CheckResult) (domain.CheckApplied, error) {
	var a domain.CheckAnswer
	if err := json.Unmarshal(r.Decisions, &a); err != nil {
		return domain.CheckApplied{}, fmt.Errorf("decisions: %w: %w", domain.ErrInvalid, err)
	}

	keys, stations, err := checkKeys(ctx, q, c, a)
	if err != nil {
		return domain.CheckApplied{}, err
	}

	today, err := q.CheckFiledSince(ctx, db.CheckFiledSinceParams{FactoryID: c.FactoryID, Trigger: &c.Trigger, Since: r.At.UTC().Truncate(24 * time.Hour)})
	if err != nil {
		return domain.CheckApplied{}, fmt.Errorf("items filed today: %w", err)
	}

	out := domain.ApplyCheck(a, r.Items, keys, r.Limits, int(today), r.At)
	if c.Try {
		return out, nil
	}

	for i, f := range out.Filed {
		row, err := fileItem(ctx, q, filingOf(f.Item, c.FactoryID))
		if err != nil {
			return domain.CheckApplied{}, fmt.Errorf("file %s: %w", f.Key, err)
		}

		out.Filed[i].Ref = fmt.Sprintf("#%d", row.Number)

		if err := q.KeepTriggerKey(ctx, db.KeepTriggerKeyParams{FactoryID: c.FactoryID, Trigger: c.Trigger, Key: f.Key, ItemID: &row.ID}); err != nil {
			return domain.CheckApplied{}, fmt.Errorf("keep key %s: %w", f.Key, err)
		}
	}

	for _, n := range out.Noted {
		if err := appendEvents(ctx, q, []domain.Event{{
			FactoryID: domain.FactoryID(c.FactoryID.String()), ItemID: n.Item, At: r.At, Kind: domain.EventItemNote, Station: stations[n.Item],
			Actor: domain.ActorEngine, Data: domain.CheckNoteData(c.Trigger, n.Key, r.Agent, r.RunID, n.Text),
		}}); err != nil {
			return domain.CheckApplied{}, fmt.Errorf("note on %s: %w", n.Key, err)
		}

		if err := q.NoteTriggerKey(ctx, db.NoteTriggerKeyParams{At: &r.At, Text: n.Text, FactoryID: c.FactoryID, Trigger: c.Trigger, Key: n.Key}); err != nil {
			return domain.CheckApplied{}, fmt.Errorf("note on key %s: %w", n.Key, err)
		}
	}

	return out, nil
}

// checkKeys are the trigger's keys a names, each with its item as it stands, locked in id order,
// and the station of each item.
func checkKeys(ctx context.Context, q *db.Queries, c db.RunningTriggerCheckRow, a domain.CheckAnswer) (map[string]domain.CheckKey, map[domain.ItemID]string, error) {
	var names []string
	for _, f := range a.File {
		names = append(names, f.Key)
	}

	for _, n := range a.Note {
		names = append(names, n.Key)
	}

	rows, err := q.TriggerKeysOf(ctx, db.TriggerKeysOfParams{FactoryID: c.FactoryID, Trigger: c.Trigger, Keys: names})
	if err != nil {
		return nil, nil, fmt.Errorf("keys: %w", err)
	}

	keys := make(map[string]domain.CheckKey, len(rows))
	byItem := map[uuid.UUID]string{}

	var ids []uuid.UUID

	for _, r := range rows {
		k := domain.CheckKey{Key: r.Key, NoteText: r.LastNoteText}
		if r.LastNoteAt != nil {
			k.NoteAt = r.LastNoteAt.UTC()
		}

		if r.ItemID != nil {
			ids = append(ids, *r.ItemID)
			byItem[*r.ItemID] = r.Key
		}

		keys[r.Key] = k
	}

	items, err := q.LockCheckItems(ctx, ids)
	if err != nil {
		return nil, nil, fmt.Errorf("the keys' items: %w", err)
	}

	stations := make(map[domain.ItemID]string, len(items))

	for _, it := range items {
		k := keys[byItem[it.ID]]
		k.Item, k.Number, k.State = domain.ItemID(it.ID.String()), it.Number, domain.ItemState(it.State)

		if k.Closed() {
			k.ClosedAt, k.Why = it.ClosedAt.UTC(), it.Why
		}

		keys[k.Key] = k
		stations[k.Item] = it.Station
	}

	return keys, stations, nil
}

// TriggerChecks implements ports.Intake.
func (in *Intake) TriggerChecks(ctx context.Context, factory domain.FactoryID, trigger string, limit int) ([]domain.TriggerFiring, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return nil, err
	}

	rows, err := fetch(ctx, in.s.read, func(q *db.Queries) ([]db.TriggerChecksRow, error) {
		return q.TriggerChecks(ctx, db.TriggerChecksParams{FactoryID: fid, Trigger: trigger, MaxRows: int32(min(max(limit, 0), maxInt32))})
	})
	if err != nil {
		return nil, fmt.Errorf("checks of trigger %s: %w", trigger, err)
	}

	out := make([]domain.TriggerFiring, 0, len(rows))
	for _, r := range rows {
		out = append(out, firingFrom(db.TriggerFiringByKeyRow(r)))
	}

	return out, nil
}

// CheckAlerts implements ports.Intake, in one read transaction.
func (in *Intake) CheckAlerts(ctx context.Context, factories []domain.FactoryID, since time.Time) ([]domain.CheckAlert, error) {
	ids := make([]uuid.UUID, 0, len(factories))

	for _, f := range factories {
		id, err := parseID("factory", string(f))
		if err != nil {
			return nil, err
		}

		ids = append(ids, id)
	}

	out := []domain.CheckAlert{}

	err := in.s.read(ctx, func(q *db.Queries) error {
		failing, err := q.FailingTriggerChecks(ctx, ids)
		if err != nil {
			return fmt.Errorf("failing checks: %w", err)
		}

		for _, r := range failing {
			out = append(out, domain.CheckAlert{FactoryID: domain.FactoryID(r.FactoryID.String()), Trigger: r.Trigger, Kind: domain.CheckFailing, At: r.At.UTC(), Reason: r.Reason})
		}

		limited, err := q.LimitedTriggerChecks(ctx, db.LimitedTriggerChecksParams{Factories: ids, Since: &since})
		if err != nil {
			return fmt.Errorf("checks over the limit: %w", err)
		}

		for _, r := range limited {
			out = append(out, domain.CheckAlert{FactoryID: domain.FactoryID(r.FactoryID.String()), Trigger: r.Trigger, Kind: domain.CheckLimited, At: r.At.UTC()})
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("check alerts: %w", err)
	}

	slices.SortFunc(out, func(a, b domain.CheckAlert) int {
		return cmp.Or(cmp.Compare(a.FactoryID, b.FactoryID), cmp.Compare(a.Trigger, b.Trigger), cmp.Compare(a.Kind, b.Kind))
	})

	return out, nil
}

// maxFiringReason is the most characters a firing's reason column keeps.
const maxFiringReason = 500

// cutText is s cut to n characters.
func cutText(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}

	return s
}

// ExpireChecks implements ports.Intake.
func (in *Intake) ExpireChecks(ctx context.Context, factory domain.FactoryID, now time.Time) (int, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return 0, err
	}

	n, err := fetch(ctx, in.s.inTx, func(q *db.Queries) (int64, error) {
		return q.ExpireTriggerChecks(ctx, db.ExpireTriggerChecksParams{Reason: domain.CheckLeaseExpired, Now: &now, FactoryID: fid})
	})
	if err != nil {
		return 0, fmt.Errorf("expire checks of factory %s: %w", factory, err)
	}

	return int(n), nil
}

// CheckInput implements ports.Intake, in one read transaction.
func (in *Intake) CheckInput(ctx context.Context, factory domain.FactoryID, trigger string, now time.Time) (domain.CheckInput, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return domain.CheckInput{}, err
	}

	out := domain.CheckInput{Open: []domain.CheckOpenItem{}, Closed: []domain.CheckClosed{}, LastDecisions: json.RawMessage("null"), State: json.RawMessage("null")}
	since := now.Add(-domain.CheckClosedWindow)

	err = in.s.read(ctx, func(q *db.Queries) error {
		var err error
		if out.Open, out.Closed, err = checkLists(ctx, q, fid, trigger, since, domain.MaxCheckOpenItems, domain.MaxCheckClosed); err != nil {
			return err
		}

		switch d, err := q.LastCheckDecisions(ctx, db.LastCheckDecisionsParams{FactoryID: fid, Trigger: trigger}); {
		case err == nil && len(d) > 0:
			out.LastDecisions = d
		case err != nil && !notFound(err):
			return fmt.Errorf("last decisions: %w", err)
		}

		switch s, err := q.TriggerState(ctx, db.TriggerStateParams{FactoryID: fid, Trigger: trigger}); {
		case err == nil:
			out.State = s
		case !notFound(err):
			return fmt.Errorf("saved state: %w", err)
		}

		return nil
	})
	if err != nil {
		return domain.CheckInput{}, fmt.Errorf("input of trigger %s's check: %w", trigger, err)
	}

	return out, nil
}

// checkLists are the trigger's open items, at most maxOpen, and the keys whose items closed at or
// after since, at most maxClosed, each newest first, as a check reads them; empty, never nil.
func checkLists(ctx context.Context, q *db.Queries, fid uuid.UUID, trigger string, since time.Time, maxOpen, maxClosed int32,
) ([]domain.CheckOpenItem, []domain.CheckClosed, error) {
	open, err := q.CheckOpenItems(ctx, db.CheckOpenItemsParams{FactoryID: fid, Trigger: &trigger, MaxItems: maxOpen})
	if err != nil {
		return nil, nil, fmt.Errorf("open items: %w", err)
	}

	openItems := make([]domain.CheckOpenItem, 0, len(open))
	for _, r := range open {
		openItems = append(openItems, domain.CheckOpenItem{Key: r.Key, Ref: fmt.Sprintf("#%d", r.Number), Title: r.Title, State: r.State, LastNote: r.LastNote})
	}

	closed, err := q.CheckClosedItems(ctx, db.CheckClosedItemsParams{FactoryID: fid, Trigger: &trigger, Since: &since, MaxItems: maxClosed})
	if err != nil {
		return nil, nil, fmt.Errorf("closed items: %w", err)
	}

	closedKeys := make([]domain.CheckClosed, 0, len(closed))
	for _, r := range closed {
		closedKeys = append(closedKeys, domain.CheckClosed{Key: r.Key, Ref: fmt.Sprintf("#%d", r.Number), How: r.State, Why: r.Why, At: r.ClosedAt.UTC()})
	}

	return openItems, closedKeys, nil
}

// CheckMemory implements ports.Intake, in one read transaction.
func (in *Intake) CheckMemory(ctx context.Context, factory domain.FactoryID, trigger string, now time.Time) (domain.CheckMemory, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return domain.CheckMemory{}, err
	}

	out := domain.CheckMemory{State: json.RawMessage("null")}
	since := now.Add(-domain.CheckClosedWindow)

	err = in.s.read(ctx, func(q *db.Queries) error {
		var err error
		if out.OpenItems, out.ClosedKeys, err = checkLists(ctx, q, fid, trigger, since, domain.MaxCheckMemoryItems, domain.MaxCheckMemoryItems); err != nil {
			return err
		}

		open, err := q.CheckOpenCount(ctx, db.CheckOpenCountParams{FactoryID: fid, Trigger: &trigger})
		if err != nil {
			return fmt.Errorf("open count: %w", err)
		}

		closed, err := q.CheckClosedCount(ctx, db.CheckClosedCountParams{FactoryID: fid, Trigger: &trigger, Since: &since})
		if err != nil {
			return fmt.Errorf("closed count: %w", err)
		}

		out.Open, out.Closed = int(open), int(closed)

		switch s, err := q.TriggerStateAt(ctx, db.TriggerStateAtParams{FactoryID: fid, Trigger: trigger}); {
		case err == nil:
			out.State, out.CheckedUpTo = s.State, s.UpdatedAt.UTC()
		case !notFound(err):
			return fmt.Errorf("saved state: %w", err)
		}

		return nil
	})
	if err != nil {
		return domain.CheckMemory{}, fmt.Errorf("memory of trigger %s: %w", trigger, err)
	}

	return out, nil
}

// ClearCheckState implements ports.Intake: the trigger's advisory lock first (LockTriggerFiring,
// as Fire and FinishCheck take it), so no check is queued or finishes between the refusal and
// the delete.
func (in *Intake) ClearCheckState(ctx context.Context, factory domain.FactoryID, trigger string) error {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return err
	}

	err = in.s.inTx(ctx, func(q *db.Queries) error {
		if err := q.LockTriggerFiring(ctx, db.LockTriggerFiringParams{FactoryID: fid, Trigger: trigger}); err != nil {
			return fmt.Errorf("lock the trigger: %w", err)
		}

		live, err := q.LiveTriggerCheck(ctx, db.LiveTriggerCheckParams{FactoryID: fid, Trigger: trigger})

		switch {
		case err != nil:
			return fmt.Errorf("live check: %w", err)
		case live:
			return fmt.Errorf("a check is queued or running: %w", ports.ErrConflict)
		}

		return q.ClearTriggerState(ctx, db.ClearTriggerStateParams{FactoryID: fid, Trigger: trigger})
	})
	if err != nil {
		return fmt.Errorf("clear the state of trigger %s: %w", trigger, err)
	}

	return nil
}
