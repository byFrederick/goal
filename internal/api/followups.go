package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var (
	errFollowUpNotFound = errors.New("follow-up not found")
	errFollowUpState    = errors.New("the follow-up cannot do that in its state")
)

// DropFollowUp drops a waiting follow-up of an item (engine.DropFollowUp).
func (s *server) DropFollowUp(ctx context.Context, req gen.DropFollowUpRequestObject) (gen.DropFollowUpResponseObject, error) {
	by, it, err := s.decide(ctx, req.Item)
	if err != nil {
		return nil, err
	}

	f, err := s.engine.DropFollowUp(ctx, it.ID, domain.FollowUpID(req.FollowUp), by)
	if err != nil {
		return nil, followUpErr(err)
	}

	return gen.DropFollowUp200JSONResponse(followUpOf(f)), nil
}

// FileFollowUp files an item from a dropped follow-up or one in a closed triage item
// (engine.FileFollowUp), by the caller.
func (s *server) FileFollowUp(ctx context.Context, req gen.FileFollowUpRequestObject) (gen.FileFollowUpResponseObject, error) {
	by, it, err := s.decide(ctx, req.Item)
	if err != nil {
		return nil, err
	}

	b := req.Body

	var start *domain.StartRequest

	if b.Start != nil {
		r, err := s.startOf(ctx, *b.Start)
		if err != nil {
			return nil, err
		}

		start = &r
	}

	filed, err := s.engine.FileFollowUp(ctx, it.ID, domain.FollowUpID(req.FollowUp), domain.FileRequest{
		By: by, Type: deref(b.Type), Title: b.Title, Body: deref(b.Body), Lane: deref(b.Lane), Start: start,
	})
	if errors.Is(err, domain.ErrInvalid) { // what the line does not take is a bad request, as for submitItem
		return nil, fmt.Errorf("%w: %s", errInvalid, err.Error())
	}

	if err != nil {
		return nil, followUpErr(err)
	}

	return gen.FileFollowUp201JSONResponse(s.itemOf(ctx, filed)), nil
}

// followUpErr is a refused File or Drop: a follow-up that is not the item's is not found, and
// one in another state a conflict.
func followUpErr(err error) error {
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return fmt.Errorf("%w: %w", errFollowUpNotFound, err)
	case errors.Is(err, ports.ErrConflict):
		return fmt.Errorf("%w: %w", errFollowUpState, err)
	default:
		return err
	}
}

// followUpProblem answers followUpErr's errors.
func followUpProblem(err error) (gen.Problem, bool) {
	for _, a := range []struct {
		err    error
		status int
		code   string
	}{
		{errFollowUpNotFound, http.StatusNotFound, "follow-up-not-found"},
		{errFollowUpState, http.StatusConflict, "follow-up-state"},
	} {
		if errors.Is(err, a.err) {
			p := detailed(a.status, err)
			p.Code = optional(a.code)

			return p, true
		}
	}

	return gen.Problem{}, false
}

// followUpOf is f for the API.
func followUpOf(f domain.FollowUp) gen.FollowUp {
	out := gen.FollowUp{
		Id: string(f.ID), ItemId: string(f.ItemID), Station: f.Station, Agent: f.Agent, Text: f.Text,
		State: gen.FollowUpState(f.State), Reason: optional(f.Reason), FiledItemId: optional(string(f.FiledItem)),
		CreatedAt: f.CreatedAt,
	}

	if f.Target != "" {
		out.Target = new(gen.FollowUpTarget(f.Target))
	}

	if f.For != nil {
		out.For = new(f.For.String())
	}

	if p := f.Start; p != nil {
		out.Start = &gen.FollowUpStart{Title: p.Title, At: optional(p.At), In: optional(p.In), Factory: optional(p.Factory)}
		if len(p.After) > 0 {
			out.Start.After = new(append([]string{}, p.After...))
		}
	}

	return out
}

// followUpsAndNotes are item's follow-ups, each filed one with the ref of the item it was filed
// as or onto, and the notes left on it, as getItem answers them.
func (s *server) followUpsAndNotes(ctx context.Context, item domain.ItemID) (*[]gen.FollowUp, *[]gen.ItemNoteFrom, error) {
	fs, err := s.items.FollowUps(ctx, item)
	if err != nil {
		return nil, nil, err
	}

	follows := make([]gen.FollowUp, 0, len(fs))

	for _, f := range fs {
		out := followUpOf(f)
		if f.FiledNumber > 0 {
			out.FiledItem = new(s.ref(ctx, f.FiledFactory, f.FiledNumber))
		}

		follows = append(follows, out)
	}

	events, err := s.items.ItemNotes(ctx, item, maxItemNotes)
	if err != nil {
		return nil, nil, err
	}

	notes := make([]gen.ItemNoteFrom, 0, len(events))
	for _, e := range events {
		notes = append(notes, noteOf(e))
	}

	return &follows, &notes, nil
}

// cameFrom is where a follow-up's or a trigger's item came from, as getItem answers it (ADR 0040
// decision 2): the follow-ups filed as it, each source and landing named the way filed_item is,
// or its trigger with the kind the factory's loaded definition gives it and a check's key; nil
// for any other origin.
func (s *server) cameFrom(ctx context.Context, it domain.Item) (*gen.ItemCameFrom, error) {
	switch it.Origin {
	case domain.OriginFollowUp:
		fs, err := s.items.FiledFrom(ctx, it.ID)
		if err != nil {
			return nil, err
		}

		out := make([]gen.CameFromFollowUp, 0, len(fs))
		for _, f := range fs {
			out = append(out, gen.CameFromFollowUp{
				Id: string(f.FollowUp), FromItem: s.ref(ctx, f.SourceFactory, f.SourceNumber), FromItemId: string(f.Source),
				Agent: f.Agent, Station: f.Station, LandedItem: s.ref(ctx, f.LandedFactory, f.LandedNumber), LandedItemId: string(f.Landed),
			})
		}

		return &gen.ItemCameFrom{FollowUps: &out}, nil
	case domain.OriginTrigger:
		t := gen.CameFromTrigger{Name: it.Trigger, Kind: s.triggerKind(ctx, it.FactoryID, it.Trigger)}

		if t.Kind != nil && *t.Kind == gen.CameFromTriggerKindCheck {
			key, err := s.intake.ItemCheckKey(ctx, it.ID)
			if err != nil {
				return nil, err
			}

			t.Key = optional(key)
		}

		return &gen.ItemCameFrom{Trigger: &t}, nil
	case domain.OriginPerson, domain.OriginPlan:
	}

	return nil, nil
}

// triggerKind is the kind of factory's trigger name in its loaded definition: check for one with
// a check, schedule for another schedule, webhook otherwise; nil when the definition has no such
// trigger.
func (s *server) triggerKind(ctx context.Context, factory domain.FactoryID, name string) *gen.CameFromTriggerKind {
	f, ok := s.loaded(ctx)[factory]
	if !ok {
		return nil
	}

	for _, t := range f.Line().Triggers {
		switch {
		case t.Name != name:
		case t.Check != nil:
			return new(gen.CameFromTriggerKindCheck)
		case t.On == domain.OnSchedule:
			return new(gen.CameFromTriggerKindSchedule)
		default:
			return new(gen.CameFromTriggerKindWebhook)
		}
	}

	return nil
}

// maxItemNotes is the most notes getItem answers.
const maxItemNotes = 200

// noteOf is an item_note event as getItem answers it: from another item, or from a trigger's
// check.
func noteOf(e domain.Event) gen.ItemNoteFrom {
	str := func(key string) *string {
		v, _ := e.Data[key].(string)

		return optional(v)
	}

	return gen.ItemNoteFrom{
		Id: e.ID, Text: deref(str("text")), Agent: deref(str(domain.KeyNoteAgent)), At: e.At,
		FromItem: str(domain.KeyNoteFromItem), FromItemId: str(domain.KeyNoteFromItemID), FromFactoryId: str(domain.KeyNoteFromFactory),
		Station: str(domain.KeyNoteStation), Trigger: str(domain.KeyNoteTrigger),
	}
}
