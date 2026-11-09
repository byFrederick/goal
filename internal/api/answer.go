package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/engine"
)

// AnswerItem records the request's actor's answer to the question an item's agent asked (ADR
// 0018): one of its choices, or the person's own words. The engine puts the item back in line.
func (s *server) AnswerItem(ctx context.Context, req gen.AnswerItemRequestObject) (gen.AnswerItemResponseObject, error) {
	// The item first: who may not see or decide it learns nothing from a bad body.
	by, it, err := s.decide(ctx, req.Item)
	if err != nil {
		return nil, err
	}

	answer, choice, err := answerOf(req.Body)
	if err != nil {
		return nil, err
	}

	if it, err = s.engine.Answer(ctx, it.ID, by, answer, choice); err != nil {
		return nil, err
	}

	out := s.itemOf(ctx, it)
	if out.Question, out.ToolApproval, err = s.openAsks(ctx, it.ID); err != nil {
		return nil, err
	}

	return gen.AnswerItem200JSONResponse(out), nil
}

// answerOf checks an answer's body: exactly one of answer, 1 to 2000 characters once trimmed,
// and choice. Which choices the question offers only the engine knows.
func answerOf(body *gen.ItemAnswer) (string, *int, error) {
	switch {
	case body == nil || (body.Answer == nil) == (body.Choice == nil):
		return "", nil, fmt.Errorf("%w: an answer takes exactly one of answer and choice", errInvalid)
	case body.Choice != nil:
		return "", body.Choice, nil
	}

	answer := strings.TrimSpace(*body.Answer)
	if answer == "" || utf8.RuneCountInString(answer) > engine.MaxAnswerRunes {
		return "", nil, fmt.Errorf("%w: an answer takes 1 to %d characters", errInvalid, engine.MaxAnswerRunes)
	}

	return answer, nil, nil
}

// openAsks are an item's open question and open tool approval as the contract has them, each
// nil when none is open. An item has at most one of them.
func (s *server) openAsks(ctx context.Context, id domain.ItemID) (*gen.ItemQuestion, *gen.ToolApproval, error) {
	events, err := s.items.AskEvents(ctx, []domain.ItemID{id})
	if err != nil {
		return nil, nil, fmt.Errorf("read the asks of item %s: %w", id, err)
	}

	q, a := asksOf(events[id])

	return q, a, nil
}

// asksOf are the open question and open tool approval in an item's events, as the contract has
// them, each nil when none is open. events are the item's history in append order, or only its
// events of domain.AskKinds (ports.Store.AskEvents).
func asksOf(events []domain.Event) (*gen.ItemQuestion, *gen.ToolApproval) {
	var (
		question *gen.ItemQuestion
		approval *gen.ToolApproval
	)

	if q, open := domain.OpenQuestion(events); open {
		question = &gen.ItemQuestion{
			Text: q.Text, Choices: append([]string{}, q.Choices...), AskedAt: q.At, Agent: q.Agent, RunId: string(q.RunID),
		}
	}

	if a, open := domain.OpenToolApproval(events); open {
		approval = toolApprovalOf(a)
	}

	return question, approval
}

// questionProblem maps the refusals of an answer: a choice outside the question's choices is
// 400 bad-choice (the handler refuses bad free text before the engine sees it), an item with no
// open question 409 no-open-question.
func questionProblem(err error) (gen.Problem, bool) {
	var p gen.Problem

	switch {
	case errors.Is(err, engine.ErrBadAnswer):
		p = detailed(http.StatusBadRequest, err)
		p.Code = optional("bad-choice")
	case errors.Is(err, engine.ErrNoOpenQuestion):
		p = detailed(http.StatusConflict, err)
		p.Code = optional("no-open-question")
	default:
		return gen.Problem{}, false
	}

	return p, true
}
