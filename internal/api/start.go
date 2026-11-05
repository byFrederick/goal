package api

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
)

// SetItemStart changes when an item that has not started may start (engine.SetStart).
func (s *server) SetItemStart(ctx context.Context, req gen.SetItemStartRequestObject) (gen.SetItemStartResponseObject, error) {
	by, it, err := s.decide(ctx, req.Item)
	if err != nil {
		return nil, err
	}

	if req.Body == nil || (deref(req.Body.At) == "" && deref(req.Body.In) == "" && len(deref(req.Body.After)) == 0) {
		return nil, fmt.Errorf("%w: a start has at, in or after", errInvalid)
	}

	start, err := s.startOf(ctx, *req.Body)
	if err != nil {
		return nil, err
	}

	it, err = s.engine.SetStart(ctx, it.ID, start, by)
	if err != nil {
		return nil, startProblem(err)
	}

	return gen.SetItemStart200JSONResponse(s.itemOf(ctx, it)), nil
}

// ClearItemStart starts an item now: it clears its start time (engine.StartNow).
func (s *server) ClearItemStart(ctx context.Context, req gen.ClearItemStartRequestObject) (gen.ClearItemStartResponseObject, error) {
	by, it, err := s.decide(ctx, req.Item)
	if err != nil {
		return nil, err
	}

	if it, err = s.engine.StartNow(ctx, it.ID, by); err != nil {
		return nil, startProblem(err)
	}

	return gen.ClearItemStart200JSONResponse(s.itemOf(ctx, it)), nil
}

// startOf is a request's start as domain.File and engine.SetStart take it: its after refs
// resolved to the items the caller may see (nil for the rest, which the domain refuses), and the
// caller's time zone for a time without one.
func (s *server) startOf(ctx context.Context, b gen.Start) (domain.StartRequest, error) {
	r := domain.StartRequest{At: deref(b.At), In: deref(b.In)}

	for _, ref := range deref(b.After) {
		a := domain.After{Ref: ref}

		it, err := s.item(ctx, ref)

		switch {
		case err == nil:
			a.Item = &it
		case !errors.Is(err, errItemNotFound):
			return domain.StartRequest{}, err
		}

		r.After = append(r.After, a)
	}

	if r.At != "" {
		r.Zone = s.zoneOf(ctx)
	}

	return r, nil
}

// zoneOf is the caller's time zone: a person's from their notification settings, and UTC for an
// API key, a person who saved none or one Go cannot load.
func (s *server) zoneOf(ctx context.Context) *time.Location {
	p := principalOf(ctx)
	if s.notifications == nil || p.Kind != domain.PrincipalUser || p.User == "" {
		return time.UTC
	}

	prefs, err := s.notifications.NotificationPreferences(ctx, p.User)
	if err != nil {
		s.log.WarnContext(ctx, "time zone unread; using UTC", "error", err)
		return time.UTC
	}

	zone, err := time.LoadLocation(prefs.TimeZone)
	if err != nil || prefs.TimeZone == "" {
		return time.UTC
	}

	return zone
}

// startProblem is a refused start: what cannot be is a bad request, and an after item that
// already needs this one an edge cycle.
func startProblem(err error) error {
	switch {
	case errors.Is(err, domain.ErrCycle):
		return fmt.Errorf("%w: %w", errEdgeCycle, err)
	case errors.Is(err, domain.ErrInvalid):
		return fmt.Errorf("%w: %s", errInvalid, err.Error())
	default:
		return err
	}
}

// itemStartOf is it's start for the API, nil when it may start now.
func itemStartOf(it domain.Item) *gen.ItemStart {
	if it.StartAt.IsZero() {
		return nil
	}

	return &gen.ItemStart{At: it.StartAt}
}
