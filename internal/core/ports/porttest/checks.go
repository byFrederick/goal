package porttest

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// liveCheck reports whether the factory's trigger has a check queued or running.
func (in *Intake) liveCheck(ctx context.Context, factory domain.FactoryID, trigger string) (bool, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return false, err
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	return slices.ContainsFunc(in.d.firings, func(r firingRow) bool {
		return r.org == org && r.f.FactoryID == factory && r.f.Trigger == trigger && r.f.CheckState.Live()
	}), nil
}

// ClaimCheck implements ports.Intake. The slot is held in the store (holdCheckSlot), so the
// store's claims count it as a lease.
func (in *Intake) ClaimCheck(ctx context.Context, factory domain.FactoryID, holder string, now time.Time, lease time.Duration,
) (domain.TriggerFiring, domain.CheckLease, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.TriggerFiring{}, domain.CheckLease{}, err
	}

	in.d.fires.Lock()
	defer in.d.fires.Unlock()

	in.d.mu.Lock()
	i := in.oldestQueuedLocked(org, factory)
	in.d.mu.Unlock()

	if i < 0 {
		return domain.TriggerFiring{}, domain.CheckLease{}, fmt.Errorf("queued check of factory %s: %w", factory, ports.ErrNotFound)
	}

	in.d.mu.Lock()
	id := in.d.firings[i].f.ID
	in.d.mu.Unlock()

	if err := in.store.holdCheckSlot(id, org, factory); err != nil {
		return domain.TriggerFiring{}, domain.CheckLease{}, err
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	r := &in.d.firings[i]
	r.f.CheckState = domain.CheckRunning
	r.lease = domain.CheckLease{
		FiringID: r.f.ID, FactoryID: factory, Trigger: r.f.Trigger, Holder: holder, Token: uuid.NewString(),
		Until: now.Add(lease).UTC().Truncate(time.Microsecond),
	}

	return r.f, r.lease, nil
}

// oldestQueuedLocked is the index of org's factory's oldest queued check, -1 for none. Callers
// hold in.d.mu.
func (in *Intake) oldestQueuedLocked(org domain.OrgID, factory domain.FactoryID) int {
	best := -1

	for i, r := range in.d.firings {
		if r.org != org || r.f.FactoryID != factory || r.f.CheckState != domain.CheckQueued {
			continue
		}

		if best < 0 || cmp.Or(r.f.At.Compare(in.d.firings[best].f.At), cmp.Compare(r.f.ID, in.d.firings[best].f.ID)) < 0 {
			best = i
		}
	}

	return best
}

// runningLocked is l's check while it runs under l's token, or ErrLeaseLost. Callers hold in.d.mu.
func (in *Intake) runningLocked(org domain.OrgID, l domain.CheckLease) (*firingRow, error) {
	for i := range in.d.firings {
		r := &in.d.firings[i]
		if r.org == org && r.f.ID == l.FiringID && r.f.CheckState == domain.CheckRunning && r.lease.Token == l.Token {
			return r, nil
		}
	}

	return nil, fmt.Errorf("check %s: %w", l.FiringID, ports.ErrLeaseLost)
}

// HeartbeatCheck implements ports.Intake.
func (in *Intake) HeartbeatCheck(ctx context.Context, l domain.CheckLease, until time.Time) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	r, err := in.runningLocked(org, l)
	if err != nil {
		return err
	}

	r.lease.Until = until.UTC().Truncate(time.Microsecond)

	return nil
}

// FinishCheck implements ports.Intake.
func (in *Intake) FinishCheck(ctx context.Context, l domain.CheckLease, res domain.CheckResult) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}

	if err := res.Check(); err != nil {
		return fmt.Errorf("finish check %s: %w", l.FiringID, err)
	}

	in.d.mu.Lock()

	r, err := in.runningLocked(org, l)
	if err != nil {
		in.d.mu.Unlock()
		return err
	}

	r.f.CheckState, r.f.RunID, r.f.CostUSD, r.lease = res.State, res.RunID, res.CostUSD, domain.CheckLease{}
	if res.State == domain.CheckDone {
		r.f.Decisions = string(res.Decisions)
	} else {
		r.f.Reason = truncateRunes(res.Reason, maxReason)
	}

	if res.State == domain.CheckDone && len(res.Saved) > 0 && string(res.Saved) != "null" {
		in.d.states[stateKey{org: org, factory: r.f.FactoryID, trigger: r.f.Trigger}] = slices.Clone(res.Saved)
	}

	in.d.mu.Unlock()
	in.store.freeCheckSlot(l.FiringID)

	return nil
}

// maxReason is the most characters a firing's reason keeps, as the column allows.
const maxReason = 500

// ExpireChecks implements ports.Intake.
func (in *Intake) ExpireChecks(ctx context.Context, factory domain.FactoryID, now time.Time) (int, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return 0, err
	}

	var freed []string

	in.d.mu.Lock()

	for i := range in.d.firings {
		r := &in.d.firings[i]
		if r.org != org || r.f.FactoryID != factory || r.f.CheckState != domain.CheckRunning || r.lease.Until.After(now) {
			continue
		}

		r.f.CheckState, r.f.Reason, r.lease = domain.CheckFailed, domain.CheckLeaseExpired, domain.CheckLease{}
		freed = append(freed, r.f.ID)
	}

	in.d.mu.Unlock()

	for _, id := range freed {
		in.store.freeCheckSlot(id)
	}

	return len(freed), nil
}

// CheckInput implements ports.Intake.
func (in *Intake) CheckInput(ctx context.Context, factory domain.FactoryID, trigger string, now time.Time) (domain.CheckInput, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.CheckInput{}, err
	}

	out := domain.CheckInput{Open: []domain.CheckOpenItem{}, Closed: []domain.CheckClosed{}, LastDecisions: json.RawMessage("null"), State: json.RawMessage("null")}
	keys := map[domain.ItemID]string{}

	in.d.mu.Lock()

	var last domain.TriggerFiring

	for _, r := range in.d.firings {
		if r.org != org || r.f.FactoryID != factory || r.f.Trigger != trigger {
			continue
		}

		if r.f.ItemID != "" {
			keys[r.f.ItemID] = r.f.Dedupe
		}

		if r.f.CheckState == domain.CheckDone && (last.ID == "" || r.f.ID > last.ID) {
			last = r.f
		}
	}

	if last.ID != "" {
		out.LastDecisions = json.RawMessage(last.Decisions)
	}

	if s, ok := in.d.states[stateKey{org: org, factory: factory, trigger: trigger}]; ok {
		out.State = slices.Clone(s)
	}

	in.d.mu.Unlock()

	in.store.mu.Lock()
	defer in.store.mu.Unlock()

	var items []domain.Item

	for _, it := range in.store.items {
		if it.OrgID == org && it.FactoryID == factory && it.Trigger == trigger {
			items = append(items, *it)
		}
	}

	slices.SortFunc(items, func(a, b domain.Item) int { return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), cmp.Compare(b.ID, a.ID)) })

	for _, it := range items {
		ref := fmt.Sprintf("#%d", it.Number)

		switch closedAt := cmp.Or(it.DoneAt, it.UpdatedAt); {
		case it.State == domain.StateDone || it.State == domain.StateCancelled:
			if closedAt.Before(now.Add(-domain.CheckClosedWindow)) || len(out.Closed) >= domain.MaxCheckClosed {
				continue
			}

			out.Closed = append(out.Closed, domain.CheckClosed{
				Key: keys[it.ID], Ref: ref, How: string(it.State), Why: in.lastTextLocked(it.ID, "reason", domain.EventDone, domain.EventCancelled),
				At: closedAt.UTC(),
			})
		case len(out.Open) < domain.MaxCheckOpenItems:
			out.Open = append(out.Open, domain.CheckOpenItem{
				Key: keys[it.ID], Ref: ref, Title: it.Title, State: string(it.State),
				LastNote: in.lastTextLocked(it.ID, "text", domain.EventNote, domain.EventItemNote),
			})
		}
	}

	slices.SortStableFunc(out.Closed, func(a, b domain.CheckClosed) int { return b.At.Compare(a.At) })

	return out, nil
}

// lastTextLocked is field of the item's newest event of one of kinds, "" for none. Callers hold
// in.store.mu.
func (in *Intake) lastTextLocked(item domain.ItemID, field string, kinds ...domain.EventKind) string {
	text := ""

	for _, e := range in.store.events {
		if e.ItemID == item && slices.Contains(kinds, e.Kind) {
			text, _ = e.Data[field].(string)
		}
	}

	return text
}
