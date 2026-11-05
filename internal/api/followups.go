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
