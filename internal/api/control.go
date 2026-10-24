package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/engine"
)

// MoveItem puts an item at another station of its line (engine.Move): the request's actor moves
// it, from any open state; in progress, its run is stopped.
func (s *server) MoveItem(ctx context.Context, req gen.MoveItemRequestObject) (gen.MoveItemResponseObject, error) {
	by, it, err := s.decide(ctx, req.Item)
	if err != nil {
		return nil, err
	}

	if it, err = s.engine.Move(ctx, it.ID, strings.TrimSpace(req.Body.To), by, deref(req.Body.Reason)); err != nil {
		return nil, err
	}

	return gen.MoveItem200JSONResponse(s.itemOf(ctx, it)), nil
}

// PauseItem stops an open item where it is until a retry (engine.Pause).
func (s *server) PauseItem(ctx context.Context, req gen.PauseItemRequestObject) (gen.PauseItemResponseObject, error) {
	var reason string
	if req.Body != nil {
		reason = deref(req.Body.Reason)
	}

	by, it, err := s.decide(ctx, req.Item)
	if err != nil {
		return nil, err
	}

	if it, err = s.engine.Pause(ctx, it.ID, by, reason); err != nil {
		return nil, err
	}

	return gen.PauseItem200JSONResponse(s.itemOf(ctx, it)), nil
}

// controlProblem maps the refusals of a person's control of an item to their problem details:
// a request naming no station of the line, or lacking a reason, is 400; a state or station that
// does not allow it 409, each with its code.
func controlProblem(err error) (gen.Problem, bool) {
	refusals := []struct {
		err    error
		status int
		code   string
	}{
		{engine.ErrUnknownStation, http.StatusBadRequest, "unknown-station"},
		{engine.ErrReasonRequired, http.StatusBadRequest, "reason-required"},
		{engine.ErrSameStation, http.StatusConflict, "same-station"},
		{engine.ErrDoneStation, http.StatusConflict, "done-station"},
		{engine.ErrAlreadyBlocked, http.StatusConflict, "already-blocked"},
		{engine.ErrCannotSendBack, http.StatusConflict, "cannot-send-back"},
		{engine.ErrItemChanged, http.StatusConflict, "item-changed"},
	}

	for _, r := range refusals {
		if errors.Is(err, r.err) {
			p := detailed(r.status, err)
			p.Code = optional(r.code)

			return p, true
		}
	}

	return gen.Problem{}, false
}
