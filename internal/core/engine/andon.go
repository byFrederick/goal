package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Refusals of the andon cord.
var (
	// ErrBadAndon means a pull without a reason, or a reason or clearing note that is too long.
	ErrBadAndon = errors.New("bad andon")
	// ErrAndonPulled means the cord was pulled on a line that is already stopped.
	ErrAndonPulled = errors.New("the andon cord is already pulled")
	// ErrAndonNotPulled means the cord was cleared on a line that runs.
	ErrAndonNotPulled = errors.New("the andon cord is not pulled")
)

// PullAndon stops a factory's line on by's behalf: from the next tick on it releases and claims
// nothing until someone clears the cord. Runs in progress finish and move on; people still
// submit, approve, reject, merge and retry. The reason (1 to domain.MaxAndonReason characters,
// trimmed) is required. The factory must belong to the organization in ctx.
func (e *Engine) PullAndon(ctx context.Context, f domain.FactoryID, by domain.Actor, reason string) (domain.AndonPull, error) {
	reason = strings.TrimSpace(reason)
	if n := utf8.RuneCountInString(reason); n == 0 || n > domain.MaxAndonReason {
		return domain.AndonPull{}, fmt.Errorf("pull the andon of %s: %w: the reason is 1 to %d characters", f, ErrBadAndon, domain.MaxAndonReason)
	}

	l, err := e.lineIn(ctx, f)
	if err != nil {
		return domain.AndonPull{}, err
	}

	p, err := e.store.PullAndon(ctx, domain.AndonPull{
		FactoryID: f, Kind: domain.AndonManual, Reason: reason, PulledAt: e.clock.Now(),
	}, by)
	if errors.Is(err, ports.ErrConflict) {
		return domain.AndonPull{}, fmt.Errorf("pull the andon of %s: %w", f, errors.Join(ErrAndonPulled, err))
	}

	if err != nil {
		return domain.AndonPull{}, fmt.Errorf("pull the andon of %s: %w", f, err)
	}

	e.log.InfoContext(ctx, "andon pulled", slog.String("org_id", string(l.OrgID)), slog.String("factory", string(f)),
		slog.String("kind", string(p.Kind)), slog.String("reason", p.Reason), slog.String("by", p.PulledBy))

	return p, nil
}

// ClearAndon restarts a factory's stopped line on by's behalf, with an optional note (at most
// domain.MaxAndonNote characters). A line that is also paused, or over its weekly budget, stays
// stopped by that.
func (e *Engine) ClearAndon(ctx context.Context, f domain.FactoryID, by domain.Actor, note string) (domain.AndonPull, error) {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > domain.MaxAndonNote {
		return domain.AndonPull{}, fmt.Errorf("clear the andon of %s: %w: the note is at most %d characters", f, ErrBadAndon, domain.MaxAndonNote)
	}

	l, err := e.lineIn(ctx, f)
	if err != nil {
		return domain.AndonPull{}, err
	}

	p, err := e.store.ClearAndon(ctx, f, by, note, e.clock.Now())
	if errors.Is(err, ports.ErrNotFound) {
		return domain.AndonPull{}, fmt.Errorf("clear the andon of %s: %w", f, ErrAndonNotPulled)
	}

	if err != nil {
		return domain.AndonPull{}, fmt.Errorf("clear the andon of %s: %w", f, err)
	}

	e.log.InfoContext(ctx, "andon cleared", slog.String("org_id", string(l.OrgID)), slog.String("factory", string(f)),
		slog.Int64("pull", p.ID), slog.String("by", p.ClearedBy))

	return p, nil
}

// stopped reads the open andon pull of every line and returns the lines it stops. A line whose
// pull cannot be read counts as stopped for this tick: no work starts against a pull nobody saw.
func (e *Engine) stopped(ctx context.Context, lines map[domain.FactoryID]domain.Line) (map[domain.FactoryID]bool, error) {
	out := map[domain.FactoryID]bool{}

	var errs []error

	for id, l := range lines {
		p, err := e.store.OpenAndon(domain.WithOrg(ctx, l.OrgID), id)
		if err != nil {
			errs = append(errs, fmt.Errorf("factory %s: read the andon: %w", id, err))
		}

		if err != nil || p != nil {
			out[id] = true
		}
	}

	return out, errors.Join(errs...)
}
