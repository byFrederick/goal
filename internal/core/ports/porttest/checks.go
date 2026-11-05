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

// checkAlertsOf are the alerts of k's finished checks fs, newest first: failing when the newest
// domain.CheckFailuresShown failed, and limited from the first one at or after since over the limit.
func checkAlertsOf(k stateKey, fs []domain.TriggerFiring, since time.Time) []domain.CheckAlert {
	var out []domain.CheckAlert

	failed := func(f domain.TriggerFiring) bool { return f.CheckState == domain.CheckFailed }
	if len(fs) >= domain.CheckFailuresShown && !slices.ContainsFunc(fs[:domain.CheckFailuresShown], func(f domain.TriggerFiring) bool { return !failed(f) }) {
		out = append(out, domain.CheckAlert{FactoryID: k.factory, Trigger: k.trigger, Kind: domain.CheckFailing, At: fs[0].CheckedAt, Reason: fs[0].Reason})
	}

	var first *domain.TriggerFiring

	for i := range fs {
		if f := &fs[i]; f.Applied != nil && f.Applied.OverLimit() && !f.CheckedAt.Before(since) {
			first = f
		}
	}

	if first != nil {
		out = append(out, domain.CheckAlert{FactoryID: k.factory, Trigger: k.trigger, Kind: domain.CheckLimited, At: first.CheckedAt})
	}

	return out
}

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

// FinishCheck implements ports.Intake: fires is the trigger's lock, as in Fire.
func (in *Intake) FinishCheck(ctx context.Context, l domain.CheckLease, res domain.CheckResult) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}

	if err := res.Check(); err != nil {
		return fmt.Errorf("finish check %s: %w", l.FiringID, err)
	}

	in.d.fires.Lock()
	defer in.d.fires.Unlock()

	in.d.mu.Lock()
	r, err := in.runningLocked(org, l)

	var f domain.TriggerFiring
	if r != nil {
		f = r.f
	}
	in.d.mu.Unlock()

	if err != nil {
		return err
	}

	var applied *domain.CheckApplied

	if res.State == domain.CheckDone {
		a, err := in.applyCheck(ctx, org, f, res)
		if err != nil {
			return fmt.Errorf("finish check %s: %w", l.FiringID, err)
		}

		applied = &a
	}

	in.d.mu.Lock()

	if r, err = in.runningLocked(org, l); err != nil {
		in.d.mu.Unlock()
		return err
	}

	r.f.CheckState, r.f.RunID, r.f.CostUSD, r.f.CheckedAt, r.lease = res.State, res.RunID, res.CostUSD, res.At.UTC().Truncate(time.Microsecond), domain.CheckLease{}
	if res.State == domain.CheckDone {
		r.f.Decisions, r.f.Applied = string(res.Decisions), applied
	} else {
		r.f.Reason = truncateRunes(res.Reason, maxReason)
	}

	if res.State == domain.CheckDone && !f.Try && len(res.Saved) > 0 && string(res.Saved) != "null" {
		in.d.states[stateKey{org: org, factory: r.f.FactoryID, trigger: r.f.Trigger}] = savedState{state: slices.Clone(res.Saved), at: r.f.CheckedAt}
	}

	in.d.mu.Unlock()
	in.store.freeCheckSlot(l.FiringID)

	return nil
}

// applyCheck applies a done check's answer as domain.ApplyCheck decides: it files its items and
// writes its notes, and keeps each key's last item and note; for a try it only decides.
func (in *Intake) applyCheck(ctx context.Context, org domain.OrgID, f domain.TriggerFiring, res domain.CheckResult) (domain.CheckApplied, error) {
	var a domain.CheckAnswer
	if err := json.Unmarshal(res.Decisions, &a); err != nil {
		return domain.CheckApplied{}, fmt.Errorf("decisions: %w: %w", domain.ErrInvalid, err)
	}

	sk := stateKey{org: org, factory: f.FactoryID, trigger: f.Trigger}
	keys, today := in.checkKeys(sk, res.At)
	out := domain.ApplyCheck(a, res.Items, keys, res.Limits, today, res.At)

	if f.Try {
		return out, nil
	}

	for i := range out.Filed {
		it, err := in.store.CreateItem(ctx, out.Filed[i].Item)
		if err != nil {
			return domain.CheckApplied{}, fmt.Errorf("file %s: %w", out.Filed[i].Key, err)
		}

		out.Filed[i].Ref = fmt.Sprintf("#%d", it.Number)

		in.d.mu.Lock()
		in.d.keys[triggerKey{stateKey: sk, key: out.Filed[i].Key}] = keyRow{item: it.ID}
		in.d.mu.Unlock()
	}

	for _, n := range out.Noted {
		it, err := in.store.Item(ctx, n.Item)
		if err != nil {
			return domain.CheckApplied{}, fmt.Errorf("note on %s: %w", n.Key, err)
		}

		if err := in.store.AppendEvents(ctx, domain.Event{
			FactoryID: f.FactoryID, ItemID: n.Item, At: res.At, Kind: domain.EventItemNote, Station: it.Station, Actor: domain.ActorEngine,
			Data: domain.CheckNoteData(f.Trigger, n.Key, res.Agent, res.RunID, n.Text),
		}); err != nil {
			return domain.CheckApplied{}, fmt.Errorf("note on %s: %w", n.Key, err)
		}

		in.d.mu.Lock()
		k := triggerKey{stateKey: sk, key: n.Key}
		row := in.d.keys[k]
		row.noteAt, row.noteText = res.At.UTC().Truncate(time.Microsecond), n.Text
		in.d.keys[k] = row
		in.d.mu.Unlock()
	}

	return out, nil
}

// checkKeys are sk's keys with their items as ApplyCheck reads them, and how many items the
// trigger filed since the UTC day of at began.
func (in *Intake) checkKeys(sk stateKey, at time.Time) (map[string]domain.CheckKey, int) {
	in.d.mu.Lock()
	rows := map[string]keyRow{}

	for k, r := range in.d.keys {
		if k.stateKey == sk {
			rows[k.key] = r
		}
	}
	in.d.mu.Unlock()

	in.store.mu.Lock()
	defer in.store.mu.Unlock()

	keys := map[string]domain.CheckKey{}

	for key, r := range rows {
		k := domain.CheckKey{Key: key, NoteAt: r.noteAt, NoteText: r.noteText}

		if it, ok := in.store.items[r.item]; ok && it.OrgID == sk.org {
			k.Item, k.Number, k.State = it.ID, it.Number, it.State
			if k.Closed() {
				k.ClosedAt, k.Why = cmp.Or(it.DoneAt, it.UpdatedAt).UTC(), in.lastTextLocked(it.ID, "reason", domain.EventDone, domain.EventCancelled)
			}
		}

		keys[key] = k
	}

	today, day := 0, at.UTC().Truncate(24*time.Hour)

	for _, it := range in.store.items {
		if it.OrgID == sk.org && it.FactoryID == sk.factory && it.Trigger == sk.trigger && it.Origin == domain.OriginTrigger && !it.CreatedAt.Before(day) {
			today++
		}
	}

	return keys, today
}

// TriggerChecks implements ports.Intake.
func (in *Intake) TriggerChecks(ctx context.Context, factory domain.FactoryID, trigger string, limit int) ([]domain.TriggerFiring, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	out := []domain.TriggerFiring{}

	for _, r := range in.d.firings {
		if r.org == org && r.f.FactoryID == factory && r.f.Trigger == trigger && r.f.Outcome == domain.FiringChecked {
			out = append(out, r.f)
		}
	}

	slices.SortFunc(out, func(a, b domain.TriggerFiring) int { return cmp.Or(b.At.Compare(a.At), cmp.Compare(b.ID, a.ID)) })

	return out[:min(len(out), max(limit, 0))], nil
}

// CheckAlerts implements ports.Intake.
func (in *Intake) CheckAlerts(ctx context.Context, factories []domain.FactoryID, since time.Time) ([]domain.CheckAlert, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	finished := map[stateKey][]domain.TriggerFiring{}

	for _, r := range in.d.firings {
		done := r.f.CheckState == domain.CheckDone || r.f.CheckState == domain.CheckFailed
		if r.org == org && slices.Contains(factories, r.f.FactoryID) && done && !r.f.Try {
			k := stateKey{org: org, factory: r.f.FactoryID, trigger: r.f.Trigger}
			finished[k] = append(finished[k], r.f)
		}
	}

	out := []domain.CheckAlert{}

	for k, fs := range finished {
		slices.SortFunc(fs, func(a, b domain.TriggerFiring) int {
			return cmp.Or(b.CheckedAt.Compare(a.CheckedAt), cmp.Compare(b.ID, a.ID))
		})

		out = append(out, checkAlertsOf(k, fs, since)...)
	}

	slices.SortFunc(out, func(a, b domain.CheckAlert) int {
		return cmp.Or(cmp.Compare(a.FactoryID, b.FactoryID), cmp.Compare(a.Trigger, b.Trigger), cmp.Compare(a.Kind, b.Kind))
	})

	return out, nil
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

		r.f.CheckState, r.f.Reason, r.f.CheckedAt, r.lease = domain.CheckFailed, domain.CheckLeaseExpired, now.UTC().Truncate(time.Microsecond), domain.CheckLease{}
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
	keys := in.checkFiringsInput(org, factory, trigger, &out)

	in.store.mu.Lock()
	defer in.store.mu.Unlock()

	for _, it := range in.triggerItemsLocked(org, factory, trigger) {
		in.addItemLocked(&out, it, keys[it.ID], now)
	}

	slices.SortStableFunc(out.Closed, func(a, b domain.CheckClosed) int { return b.At.Compare(a.At) })

	return out, nil
}

// triggerItemsLocked are the trigger's items, newest first. Callers hold in.store.mu.
func (in *Intake) triggerItemsLocked(org domain.OrgID, factory domain.FactoryID, trigger string) []domain.Item {
	var items []domain.Item

	for _, it := range in.store.items {
		if it.OrgID == org && it.FactoryID == factory && it.Trigger == trigger {
			items = append(items, *it)
		}
	}

	slices.SortFunc(items, func(a, b domain.Item) int { return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), cmp.Compare(b.ID, a.ID)) })

	return items
}

// CheckMemory implements ports.Intake.
func (in *Intake) CheckMemory(ctx context.Context, factory domain.FactoryID, trigger string, now time.Time) (domain.CheckMemory, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.CheckMemory{}, err
	}

	input := domain.CheckInput{State: json.RawMessage("null")}
	keys := in.checkFiringsInput(org, factory, trigger, &input)
	out := domain.CheckMemory{OpenItems: []domain.CheckOpenItem{}, ClosedKeys: []domain.CheckClosed{}, State: input.State}

	in.d.mu.Lock()
	out.CheckedUpTo = in.d.states[stateKey{org: org, factory: factory, trigger: trigger}].at
	in.d.mu.Unlock()

	in.store.mu.Lock()
	defer in.store.mu.Unlock()

	for _, it := range in.triggerItemsLocked(org, factory, trigger) {
		switch open, closed, ok := in.checkEntryLocked(it, keys[it.ID], now); {
		case !ok:
		case open != nil:
			out.Open++
			if len(out.OpenItems) < domain.MaxCheckMemoryItems {
				out.OpenItems = append(out.OpenItems, *open)
			}
		default:
			out.Closed++
			out.ClosedKeys = append(out.ClosedKeys, *closed)
		}
	}

	slices.SortStableFunc(out.ClosedKeys, func(a, b domain.CheckClosed) int { return b.At.Compare(a.At) })
	out.ClosedKeys = out.ClosedKeys[:min(len(out.ClosedKeys), domain.MaxCheckMemoryItems)]

	return out, nil
}

// ClearCheckState implements ports.Intake: fires is the trigger's lock, as in Fire.
func (in *Intake) ClearCheckState(ctx context.Context, factory domain.FactoryID, trigger string) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}

	in.d.fires.Lock()
	defer in.d.fires.Unlock()

	if live, err := in.liveCheck(ctx, factory, trigger); err != nil || live {
		return cmp.Or(err, fmt.Errorf("clear the state of trigger %s: %w", trigger, ports.ErrConflict))
	}

	in.d.mu.Lock()
	delete(in.d.states, stateKey{org: org, factory: factory, trigger: trigger})
	in.d.mu.Unlock()

	return nil
}

// checkFiringsInput sets out's last decisions and saved state from the trigger's firings and
// returns the key of each item they filed: its firing's dedupe value.
func (in *Intake) checkFiringsInput(org domain.OrgID, factory domain.FactoryID, trigger string, out *domain.CheckInput) map[domain.ItemID]string {
	keys := map[domain.ItemID]string{}

	in.d.mu.Lock()
	defer in.d.mu.Unlock()

	var last domain.TriggerFiring

	for _, r := range in.d.firings {
		if r.org != org || r.f.FactoryID != factory || r.f.Trigger != trigger {
			continue
		}

		if r.f.ItemID != "" {
			keys[r.f.ItemID] = r.f.Dedupe
		}

		if r.f.CheckState == domain.CheckDone && !r.f.Try && (last.ID == "" || r.f.ID > last.ID) {
			last = r.f
		}
	}

	if last.ID != "" {
		out.LastDecisions = json.RawMessage(last.Decisions)
	}

	if s, ok := in.d.states[stateKey{org: org, factory: factory, trigger: trigger}]; ok {
		out.State = slices.Clone(s.state)
	}

	in.keyedItemsLocked(stateKey{org: org, factory: factory, trigger: trigger}, keys)

	return keys
}

// keyedItemsLocked sets keys' key of each item sk keeps a key for, which wins over its firing's
// dedupe value. Callers hold in.d.mu.
func (in *Intake) keyedItemsLocked(sk stateKey, keys map[domain.ItemID]string) {
	for k, r := range in.d.keys {
		if k.stateKey == sk && r.item != "" {
			keys[r.item] = k.key
		}
	}
}

// addItemLocked adds it to out: to the closed keys when it closed within the window, to the open
// items while there are fewer than the most. Callers hold in.store.mu.
func (in *Intake) addItemLocked(out *domain.CheckInput, it domain.Item, key string, now time.Time) {
	switch open, closed, ok := in.checkEntryLocked(it, key, now); {
	case !ok:
	case open == nil && len(out.Closed) < domain.MaxCheckClosed:
		out.Closed = append(out.Closed, *closed)
	case open != nil && len(out.Open) < domain.MaxCheckOpenItems:
		out.Open = append(out.Open, *open)
	}
}

// checkEntryLocked is it as a check reads it: an open item, or a closed key when it closed within
// the window; false for an item closed before it. Callers hold in.store.mu.
func (in *Intake) checkEntryLocked(it domain.Item, key string, now time.Time) (*domain.CheckOpenItem, *domain.CheckClosed, bool) {
	ref := fmt.Sprintf("#%d", it.Number)

	if closedAt := cmp.Or(it.DoneAt, it.UpdatedAt); it.State == domain.StateDone || it.State == domain.StateCancelled {
		if closedAt.Before(now.Add(-domain.CheckClosedWindow)) {
			return nil, nil, false
		}

		return nil, &domain.CheckClosed{
			Key: key, Ref: ref, How: string(it.State), Why: in.lastTextLocked(it.ID, "reason", domain.EventDone, domain.EventCancelled),
			At: closedAt.UTC(),
		}, true
	}

	return &domain.CheckOpenItem{
		Key: key, Ref: ref, Title: it.Title, State: string(it.State),
		LastNote: in.lastTextLocked(it.ID, "text", domain.EventNote, domain.EventItemNote),
	}, nil, true
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
