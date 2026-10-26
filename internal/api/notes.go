package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/engine"
)

// NoteItem records the request's actor's note for the agents working an item. The next run's
// brief carries it, and a run in progress reads it from the gateway between its agent's turns.
func (s *server) NoteItem(ctx context.Context, req gen.NoteItemRequestObject) (gen.NoteItemResponseObject, error) {
	if req.Body == nil {
		return nil, fmt.Errorf("%w: a note needs its text", errInvalid)
	}

	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("note: the request has no actor")
	}

	it, err := s.item(ctx, req.Item)
	if err != nil {
		return nil, err
	}

	ev, err := s.engine.Note(ctx, it.ID, by, req.Body.Text)
	if errors.Is(err, engine.ErrBadNote) {
		return nil, fmt.Errorf("%w: %w", errInvalid, err)
	}

	if err != nil {
		return nil, err
	}

	return gen.NoteItem202JSONResponse{EventId: ev.ID, Delivery: gen.NoteReceiptDeliveryNextRun}, nil
}
