package engine

import (
	"context"
	"errors"
	"fmt"
	"maps"
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
// what the agent acts on.
func (e *Engine) Reject(ctx context.Context, id domain.ItemID, by domain.Actor, reason string) (domain.Item, error) {
	if reason == "" {
		return domain.Item{}, fmt.Errorf("reject %s: a reason is required", id)
	}

	it, l, err := e.waitingHuman(ctx, id)
	if err != nil {
		return domain.Item{}, err
	}

	target, err := l.ReworkTarget(it.Station)
	if err != nil {
		return domain.Item{}, fmt.Errorf("reject %s: %w", id, err)
	}

	now := e.clock.Now()
	pos, events, doneAt := e.arrive(it, target, by.Name)

	return e.decide(ctx, "reject", ports.Transition{
		ItemID: it.ID, From: it.Position(), To: pos, Rework: true, At: now, DoneAt: doneAt,
		Events: append([]domain.Event{decision(it, domain.EventRejected, now, by, map[string]any{keyReason: reason})}, events...),
	})
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
