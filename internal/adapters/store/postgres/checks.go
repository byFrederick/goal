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

// FinishCheck implements ports.Intake.
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

	p := db.FinishTriggerCheckParams{
		State: checkStateArg(r.State), RunID: run, CostUsd: r.CostUSD, Reason: cutText(r.Reason, maxFiringReason), At: &r.At, ID: id, Token: &token,
	}
	if r.State == domain.CheckDone {
		p.Decisions = r.Decisions
	}

	err = in.s.inTx(ctx, func(q *db.Queries) error {
		row, err := q.FinishTriggerCheck(ctx, p)
		if notFound(err) {
			return fmt.Errorf("check %s: %w", l.FiringID, ports.ErrLeaseLost)
		}

		if err != nil {
			return err
		}

		if r.State != domain.CheckDone || len(r.Saved) == 0 || string(r.Saved) == "null" {
			return nil
		}

		return q.SaveTriggerState(ctx, db.SaveTriggerStateParams{FactoryID: row.FactoryID, Trigger: row.Trigger, State: r.Saved, UpdatedAt: r.At})
	})
	if err != nil {
		return fmt.Errorf("finish check %s: %w", l.FiringID, ruleError(err, ""))
	}

	return nil
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
		open, err := q.CheckOpenItems(ctx, db.CheckOpenItemsParams{FactoryID: fid, Trigger: &trigger, MaxItems: domain.MaxCheckOpenItems})
		if err != nil {
			return fmt.Errorf("open items: %w", err)
		}

		for _, r := range open {
			out.Open = append(out.Open, domain.CheckOpenItem{Key: r.Key, Ref: fmt.Sprintf("#%d", r.Number), Title: r.Title, State: r.State, LastNote: r.LastNote})
		}

		closed, err := q.CheckClosedItems(ctx, db.CheckClosedItemsParams{FactoryID: fid, Trigger: &trigger, Since: &since, MaxItems: domain.MaxCheckClosed})
		if err != nil {
			return fmt.Errorf("closed items: %w", err)
		}

		for _, r := range closed {
			out.Closed = append(out.Closed, domain.CheckClosed{Key: r.Key, Ref: fmt.Sprintf("#%d", r.Number), How: r.State, Why: r.Why, At: r.ClosedAt.UTC()})
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
