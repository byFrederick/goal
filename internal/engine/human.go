package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// ErrNotBlocked means a retry was asked for an item that is not blocked.
var ErrNotBlocked = errors.New("item is not blocked")

// keyActor is the event data key that records who decided, as an actor ref (user:local).
const keyActor = "actor"

// decision is the event of a human decision: its actor is the decider's name, and data.actor
// their ref, so the decision stays attributable when names change.
func decision(it domain.Item, kind domain.EventKind, at time.Time, by domain.Actor, data map[string]any) domain.Event {
	d := maps.Clone(data)
	if d == nil {
		d = map[string]any{}
	}

	d[keyActor] = by.Ref()

	return event(it, kind, at, by.Name, d)
}

// Approve records a human's approval of an item waiting for one (at a human station, or a
// gate escalation after too many reworks) and moves it to the next station.
func (e *Engine) Approve(ctx context.Context, id domain.ItemID, by domain.Actor) (domain.Item, error) {
	it, l, err := e.waitingHuman(ctx, id)
	if err != nil {
		return domain.Item{}, err
	}

	now := e.clock.Now()
	t := ports.Transition{
		ItemID: it.ID, From: it.Position(), At: now,
		Events: []domain.Event{decision(it, domain.EventApproved, now, by, nil)},
	}

	if err := e.advance(l, it, by.Name, &t); err != nil {
		return domain.Item{}, fmt.Errorf("approve %s: %w", id, err)
	}

	return e.decide(ctx, "approve", t)
}

// Reject records a human's rejection and sends the item back for rework: to the station's
// gate.onReject, or else to the nearest agent station upstream. The reason is required; it is
// what the agent acts on. An item waiting for a human, blocked or in progress can be sent back;
// in progress, its lease is taken and its run stopped.
func (e *Engine) Reject(ctx context.Context, id domain.ItemID, by domain.Actor, reason string) (domain.Item, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return domain.Item{}, fmt.Errorf("reject %s: %w", id, ErrReasonRequired)
	}

	it, l, err := e.itemAndLine(ctx, id)
	if err != nil {
		return domain.Item{}, fmt.Errorf("reject %s: %w", id, err)
	}

	switch it.State {
	case domain.StateWaitingHuman, domain.StateInProgress, domain.StateBlocked:
	case domain.StateQueued, domain.StateReady, domain.StateDone, domain.StateCancelled:
		return domain.Item{}, fmt.Errorf("reject %s: item is %s: %w", id, it.State, ErrCannotSendBack)
	}

	target, err := l.ReworkTarget(it.Station)
	if err != nil {
		return domain.Item{}, fmt.Errorf("reject %s: %w: %w", id, ErrCannotSendBack, err)
	}

	now := e.clock.Now()
	pos, events, doneAt := e.arrive(it, target, by.Name)
	t := ports.Transition{
		ItemID: it.ID, From: it.Position(), To: pos, Rework: true, At: now, DoneAt: doneAt,
		Events: append([]domain.Event{decision(it, domain.EventRejected, now, by, map[string]any{keyReason: reason})}, events...),
		Audit:  itemAudit(it, now, by, domain.AuditItemRejected, map[string]any{keyFrom: it.Station, keyTo: target.ID, keyReason: reason}),
	}

	return e.control(ctx, "reject", it, by, t)
}

// Errors of the human controls of an item (Move, Pause and Reject).
var (
	// ErrCannotSendBack means a send-back of an item that is queued, ready, done or cancelled,
	// or whose station has nowhere upstream to send it.
	ErrCannotSendBack = errors.New("item cannot be sent back now")
	// ErrAlreadyBlocked means a pause of an item that is already blocked.
	ErrAlreadyBlocked = errors.New("item is already blocked")
	// ErrSameStation means a move of an item to the station it is at.
	ErrSameStation = errors.New("item is already at that station")
	// ErrDoneStation means a move to a done station: work finishes by approval or merge.
	ErrDoneStation = errors.New("an item reaches a done station by approval or merge, not by a move")
	// ErrUnknownStation means a move to a station that is not on the item's line.
	ErrUnknownStation = errors.New("station is not on the item's line")
	// ErrReasonRequired means a send-back or an upstream move without a reason.
	ErrReasonRequired = errors.New("a reason is required")
	// ErrItemChanged means the item changed while a person acted on it; nothing changed.
	ErrItemChanged = errors.New("item changed meanwhile; read it again")
)

// errLeaseTaken is the cause of a run this instance stops because a person took its lease.
var errLeaseTaken = errors.New("a person took the item's lease")

// Move puts an item at another station of its line, in that station's arrival state, from any
// open state; in progress, its lease is taken and its run stopped. Moving upstream counts as
// rework and needs a reason; moving to the item's own station or to a done station is refused.
func (e *Engine) Move(ctx context.Context, id domain.ItemID, to string, by domain.Actor, reason string) (domain.Item, error) {
	reason = strings.TrimSpace(reason)

	it, l, err := e.itemAndLine(ctx, id)
	if err != nil {
		return domain.Item{}, fmt.Errorf("move %s: %w", id, err)
	}

	target, ok := l.Station(to)

	switch {
	case closed(it):
		return domain.Item{}, fmt.Errorf("move %s: %w", id, ErrItemClosed)
	case !ok:
		return domain.Item{}, fmt.Errorf("move %s to %q: %w", id, to, ErrUnknownStation)
	case target.ID == it.Station:
		return domain.Item{}, fmt.Errorf("move %s to %s: %w", id, to, ErrSameStation)
	case target.Kind == domain.KindDone:
		return domain.Item{}, fmt.Errorf("move %s to %s: %w", id, to, ErrDoneStation)
	}

	upstream := stationIndex(l, target.ID) < stationIndex(l, it.Station)
	if upstream && reason == "" {
		return domain.Item{}, fmt.Errorf("move %s upstream to %s: %w", id, to, ErrReasonRequired)
	}

	now := e.clock.Now()
	pos, events, doneAt := e.arrive(it, target, by.Name)

	data := map[string]any{keyFrom: it.Station, keyTo: target.ID, "manual": true}
	if reason != "" {
		data[keyReason] = reason
	}

	events[0] = decision(it, domain.EventMoved, now, by, data)

	return e.control(ctx, "move", it, by, ports.Transition{
		ItemID: it.ID, From: it.Position(), To: pos, Rework: upstream, At: now, DoneAt: doneAt, Events: events,
		Audit: itemAudit(it, now, by, domain.AuditItemMoved, withReason(map[string]any{keyFrom: it.Station, keyTo: target.ID}, reason)),
	})
}

// Pause stops an open item where it is: it becomes blocked with a paused event (the reason is
// optional) until Retry resumes it. In progress, its lease is taken and its run stopped.
func (e *Engine) Pause(ctx context.Context, id domain.ItemID, by domain.Actor, reason string) (domain.Item, error) {
	reason = strings.TrimSpace(reason)

	it, _, err := e.itemAndLine(ctx, id)
	if err != nil {
		return domain.Item{}, fmt.Errorf("pause %s: %w", id, err)
	}

	switch {
	case closed(it):
		return domain.Item{}, fmt.Errorf("pause %s: %w", id, ErrItemClosed)
	case it.State == domain.StateBlocked:
		return domain.Item{}, fmt.Errorf("pause %s: %w", id, ErrAlreadyBlocked)
	}

	now := e.clock.Now()

	return e.control(ctx, "pause", it, by, ports.Transition{
		ItemID: it.ID, From: it.Position(), To: domain.Position{Station: it.Station, State: domain.StateBlocked}, At: now,
		Events: []domain.Event{decision(it, domain.EventPaused, now, by, withReason(nil, reason))},
		Audit:  itemAudit(it, now, by, domain.AuditItemPaused, withReason(map[string]any{"station": it.Station}, reason)),
	})
}

// control applies a person's transition of it. An item in progress has its lease taken first
// (lease_taken) and, when this instance runs it, its run stopped at once; another instance
// running it learns through Interrupt or at its next heartbeat.
func (e *Engine) control(ctx context.Context, verb string, it domain.Item, by domain.Actor, t ports.Transition) (domain.Item, error) {
	taking := it.State == domain.StateInProgress
	if taking {
		t.TakeLease, t.Outcome = true, domain.LeaseTaken
		t.Events = append([]domain.Event{decision(it, domain.EventLeaseTaken, t.At, by, map[string]any{"action": verb})}, t.Events...)
	}

	got, err := e.store.Transition(ctx, t)
	if errors.Is(err, ports.ErrConflict) {
		return domain.Item{}, fmt.Errorf("%s %s: %w", verb, it.ID, errors.Join(ErrItemChanged, err))
	}

	if err != nil {
		return domain.Item{}, fmt.Errorf("%s %s: %w", verb, it.ID, err)
	}

	e.log.InfoContext(ctx, "item moved by a person", slog.String("item", string(it.ID)), slog.String("action", verb),
		slog.String(keyFrom, it.Station), slog.String(keyTo, got.Station), slog.String("to_state", string(got.State)),
		slog.String("by", by.Ref()))

	if taking {
		e.stopRun(ctx, it.ID, it.Station, t.At)
	}

	return got, nil
}

// Interrupt is told every committed item notice (goald wires it to the API's feed). A
// lease_taken notice of an item this instance runs at that station, under a lease acquired
// before the notice, stops that run at once; the heartbeat would find out within HeartbeatEvery.
// Other notices, and those of items other instances run, are ignored.
func (e *Engine) Interrupt(n ports.ItemNotice) {
	if n.Resync || n.Org != nil || n.Kind != domain.EventLeaseTaken {
		return
	}

	e.stopRun(context.Background(), n.ItemID, n.Station, n.At)
}

// stopRun cancels this instance's run of item at station whose lease was acquired no later than
// takenAt, so a later claim of the item is never stopped by an old notice.
func (e *Engine) stopRun(ctx context.Context, item domain.ItemID, station string, takenAt time.Time) {
	e.activeMu.Lock()
	r, ok := e.active[item]
	e.activeMu.Unlock()

	if !ok || r.lease.Station != station || r.lease.AcquiredAt.After(takenAt) {
		return
	}

	r.cancel(errLeaseTaken)

	e.log.InfoContext(ctx, "run taken", slog.String("factory", string(r.factory)), slog.String("item", string(item)),
		slog.String("station", station), slog.Int("slot", r.lease.Slot))
}

// itemAndLine reads an item and the line of its factory, which must belong to the organization
// in ctx.
func (e *Engine) itemAndLine(ctx context.Context, id domain.ItemID) (domain.Item, domain.Line, error) {
	it, err := e.store.Item(ctx, id)
	if err != nil {
		return domain.Item{}, domain.Line{}, err
	}

	l, err := e.lineIn(ctx, it.FactoryID)
	if err != nil {
		return domain.Item{}, domain.Line{}, err
	}

	return it, l, nil
}

func closed(it domain.Item) bool {
	return it.State == domain.StateDone || it.State == domain.StateCancelled
}

// stationIndex is where a station is on the line, -1 when it is not.
func stationIndex(l domain.Line, id string) int {
	return slices.IndexFunc(l.Stations, func(s domain.Station) bool { return s.ID == id })
}

// withReason adds a non-empty reason to data.
func withReason(data map[string]any, reason string) map[string]any {
	if reason == "" {
		return data
	}

	d := maps.Clone(data)
	if d == nil {
		d = map[string]any{}
	}

	d[keyReason] = reason

	return d
}

// itemAudit is the audit entry of a person's action on an item.
func itemAudit(it domain.Item, at time.Time, by domain.Actor, action string, data map[string]any) *domain.AuditEntry {
	return &domain.AuditEntry{
		At: at, Actor: by, Action: action, TargetKind: domain.AuditTargetItem, TargetID: string(it.ID),
		FactoryID: it.FactoryID, Data: data,
	}
}

func (e *Engine) waitingHuman(ctx context.Context, id domain.ItemID) (domain.Item, domain.Line, error) {
	it, err := e.store.Item(ctx, id)
	if err != nil {
		return domain.Item{}, domain.Line{}, fmt.Errorf("item %s: %w", id, err)
	}

	if it.State != domain.StateWaitingHuman {
		return domain.Item{}, domain.Line{}, fmt.Errorf("item %s is %s: %w", id, it.State, ErrNotWaitingHuman)
	}

	l, err := e.lineIn(ctx, it.FactoryID)
	if err != nil {
		return domain.Item{}, domain.Line{}, err
	}

	return it, l, nil
}

func (e *Engine) decide(ctx context.Context, verb string, t ports.Transition) (domain.Item, error) {
	it, err := e.store.Transition(ctx, t)
	if errors.Is(err, ports.ErrConflict) {
		return domain.Item{}, fmt.Errorf("%s %s: %w", verb, t.ItemID, errors.Join(ErrNotWaitingHuman, err))
	}

	if err != nil {
		return domain.Item{}, fmt.Errorf("%s %s: %w", verb, t.ItemID, err)
	}

	return it, nil
}

// Retry sends a blocked item back to its station's queue, so a slot pulls it again. Its
// rework count is unchanged: nobody rejected the work.
func (e *Engine) Retry(ctx context.Context, id domain.ItemID, by domain.Actor) (domain.Item, error) {
	it, err := e.store.Item(ctx, id)
	if err != nil {
		return domain.Item{}, fmt.Errorf("retry %s: %w", id, err)
	}

	if it.State != domain.StateBlocked {
		return domain.Item{}, fmt.Errorf("retry %s: item is %s: %w", id, it.State, ErrNotBlocked)
	}

	l, err := e.lineIn(ctx, it.FactoryID)
	if err != nil {
		return domain.Item{}, fmt.Errorf("retry %s: %w", id, err)
	}

	st, ok := l.Station(it.Station)
	if !ok {
		return domain.Item{}, fmt.Errorf("retry %s: station %q is not on the line", id, it.Station)
	}

	now := e.clock.Now()

	got, err := e.store.Transition(ctx, ports.Transition{
		ItemID: it.ID, From: it.Position(), To: domain.Position{Station: st.ID, State: st.Arrival()}, At: now,
		Events: []domain.Event{decision(it, domain.EventRetried, now, by, nil)},
	})
	if errors.Is(err, ports.ErrConflict) {
		return domain.Item{}, fmt.Errorf("retry %s: %w", id, errors.Join(ErrNotBlocked, err))
	}

	if err != nil {
		return domain.Item{}, fmt.Errorf("retry %s: %w", id, err)
	}

	return got, nil
}
