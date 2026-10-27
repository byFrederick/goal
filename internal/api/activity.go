package api

import (
	"context"
	"fmt"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
)

// A factory's activity is its latest milestone events (domain.ActivityKinds), newest first.
const (
	defaultActivityLimit = 20
	maxActivityLimit     = 50
)

func (s *server) ListFactoryActivity(ctx context.Context, req gen.ListFactoryActivityRequestObject) (gen.ListFactoryActivityResponseObject, error) {
	limit := defaultActivityLimit
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}

	if limit < 1 || limit > maxActivityLimit {
		return nil, fmt.Errorf("%w: limit in [1, %d]", errInvalid, maxActivityLimit)
	}

	f, err := s.factory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	events, err := s.items.FactoryActivity(ctx, domain.FactoryID(f.Metadata.ID), limit)
	if err != nil {
		return nil, err
	}

	out := gen.ListFactoryActivity200JSONResponse{Events: make([]gen.ActivityEvent, 0, len(events))}
	for _, e := range events {
		ev := s.eventOf(e.Event)
		out.Events = append(out.Events, gen.ActivityEvent{
			Id: ev.Id, ItemId: ev.ItemId, RunId: ev.RunId, At: ev.At, Kind: ev.Kind, Station: ev.Station, Actor: ev.Actor, Data: ev.Data,
			Ref: domain.ItemRef{Factory: f.Metadata.Name, Number: e.Number}.String(), Number: e.Number, Title: e.Title,
		})
	}

	return out, nil
}
