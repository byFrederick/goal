package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// keyAssembly is the data key of a part's event or audit entry that names the assembly a
// person acted on.
const keyAssembly = "assembly"

// keyParts is the data key of an assembly's event that counts the parts a person's act moved.
const keyParts = "parts"

// pauseAssembly pauses the open parts of assembly a that are not blocked yet, each as Pause
// would one item, in one store transaction (ADR 0012 §9); a stays an assembly with one paused
// event counting them. ErrAlreadyBlocked when there is none.
func (e *Engine) pauseAssembly(ctx context.Context, a domain.Item, by domain.Actor, reason string) (domain.Item, error) {
	parts, err := e.store.Parts(ctx, a.ID)
	if err != nil {
		return domain.Item{}, fmt.Errorf("pause %s: %w", a.ID, err)
	}

	now := e.clock.Now()
	c := ports.Cascade{Assembly: a.ID}

	var taken []domain.Item

	for _, p := range parts.Items {
		if closed(p) || p.State == domain.StateBlocked {
			continue
		}

		t := ports.Transition{
			ItemID: p.ID, From: p.Position(), To: domain.Position{Station: p.Station, State: domain.StateBlocked}, At: now,
			Events: []domain.Event{decision(p, domain.EventPaused, now, by, withReason(map[string]any{keyAssembly: string(a.ID)}, reason))},
			Audit:  itemAudit(p, now, by, domain.AuditItemPaused, withReason(map[string]any{"station": p.Station, keyAssembly: string(a.ID)}, reason)),
		}
		if takeLease("pause", p, by, &t) {
			taken = append(taken, p)
		}

		c.Parts = append(c.Parts, t)
	}

	if len(c.Parts) == 0 {
		return domain.Item{}, fmt.Errorf("pause %s: every open part: %w", a.ID, ErrAlreadyBlocked)
	}

	c.Events = []domain.Event{decision(a, domain.EventPaused, now, by, withReason(map[string]any{keyParts: len(c.Parts)}, reason))}

	return e.cascade(ctx, "pause", a, by, now, c, taken)
}

// cancelAssembly cancels the open parts of assembly a, each as Cancel would one item, in one
// store transaction that closes a as cancelled with its last open part (ADR 0012 §9); a gets
// one cancelled event counting them. An assembly whose parts are all closed is cancelled
// directly.
func (e *Engine) cancelAssembly(ctx context.Context, a domain.Item, by domain.Actor, reason string) (domain.Item, error) {
	parts, err := e.store.Parts(ctx, a.ID)
	if err != nil {
		return domain.Item{}, fmt.Errorf("cancel %s: %w", a.ID, err)
	}

	now := e.clock.Now()
	c := ports.Cascade{Assembly: a.ID, Cancel: true}

	var taken []domain.Item

	for _, p := range parts.Items {
		if closed(p) {
			continue
		}

		t := cancelOf(p, now, by, reason, map[string]any{keyAssembly: string(a.ID)})
		if takeLease("cancel", p, by, &t) {
			taken = append(taken, p)
		}

		c.Parts = append(c.Parts, t)
	}

	if len(c.Parts) == 0 {
		return e.control(ctx, "cancel", a, by, cancelOf(a, now, by, reason, map[string]any{keyParts: 0}))
	}

	c.Events = []domain.Event{decision(a, domain.EventCancelled, now, by, withReason(map[string]any{keyParts: len(c.Parts)}, reason))}

	if _, err := e.cascade(ctx, "cancel", a, by, now, c, taken); err != nil {
		return domain.Item{}, err
	}

	got, err := e.store.Item(ctx, a.ID)
	if err != nil {
		return domain.Item{}, fmt.Errorf("cancel %s: %w", a.ID, err)
	}

	return got, nil
}

// retryAssembly resumes the blocked parts of assembly a, each as Retry would one item, in one
// store transaction; a stays an assembly with one retried event counting them. ErrNotBlocked
// when there is none.
func (e *Engine) retryAssembly(ctx context.Context, a domain.Item, l domain.Line, by domain.Actor) (domain.Item, error) {
	parts, err := e.store.Parts(ctx, a.ID)
	if err != nil {
		return domain.Item{}, fmt.Errorf("retry %s: %w", a.ID, err)
	}

	now := e.clock.Now()
	c := ports.Cascade{Assembly: a.ID}

	for _, p := range parts.Items {
		if p.State != domain.StateBlocked {
			continue
		}

		st, ok := l.Station(p.Station)
		if !ok {
			return domain.Item{}, fmt.Errorf("retry %s: part %s: station %q is not on the line", a.ID, p.ID, p.Station)
		}

		c.Parts = append(c.Parts, ports.Transition{
			ItemID: p.ID, From: p.Position(), To: domain.Position{Station: st.ID, State: st.Arrival()}, At: now,
			Events: []domain.Event{decision(p, domain.EventRetried, now, by, map[string]any{keyAssembly: string(a.ID)})},
		})
	}

	if len(c.Parts) == 0 {
		return domain.Item{}, fmt.Errorf("retry %s: no part is blocked: %w", a.ID, ErrNotBlocked)
	}

	c.Events = []domain.Event{decision(a, domain.EventRetried, now, by, map[string]any{keyParts: len(c.Parts)})}

	return e.cascade(ctx, "retry", a, by, now, c, nil)
}

// cascade applies c, a person's change of assembly a to its parts at now, and stops this
// instance's runs of the parts whose lease it took (the others learn through Interrupt or their
// heartbeat).
func (e *Engine) cascade(ctx context.Context, verb string, a domain.Item, by domain.Actor, now time.Time, c ports.Cascade, taken []domain.Item) (domain.Item, error) {
	_, err := e.store.CascadeParts(ctx, c)
	if errors.Is(err, ports.ErrConflict) || errors.Is(err, ports.ErrLeaseLost) {
		return domain.Item{}, fmt.Errorf("%s %s: %w", verb, a.ID, errors.Join(ErrItemChanged, err))
	}

	if err != nil {
		return domain.Item{}, fmt.Errorf("%s %s: %w", verb, a.ID, err)
	}

	e.log.InfoContext(ctx, "assembly parts moved by a person", slog.String("item", string(a.ID)), slog.String("action", verb),
		slog.Int("parts", len(c.Parts)), slog.String("by", by.Ref()))

	for _, p := range taken {
		e.stopRun(ctx, p.ID, p.Station, now)
	}

	return a, nil
}
