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

// Limits of an agent's question and a person's answer (ADR 0018), in characters.
const (
	MaxQuestionRunes = 500
	MaxChoices       = 4
	MaxChoiceRunes   = 60
	MaxAnswerRunes   = 2000
)

// Errors of questions.
var (
	// ErrBadQuestion means a question or one of its choices is outside the limits.
	ErrBadQuestion = errors.New("bad question")
	// ErrQuestionOpen means a question, or an approval, on an item that already waits for an
	// answer to its open question.
	ErrQuestionOpen = errors.New("item has an open question")
	// ErrNoOpenQuestion means an answer to an item with no open question: none was asked, it was
	// answered, or a person's move, pause, rejection or cancel withdrew it.
	ErrNoOpenQuestion = errors.New("item has no open question")
	// ErrBadAnswer means a choice outside the question's choices, or free text outside the limits.
	ErrBadAnswer = errors.New("bad answer")
	// ErrNotInProgress means a question about an item no run works: its run has ended.
	ErrNotInProgress = errors.New("item is not in progress")
)

// eventsPage is how many of an item's events one read of them takes.
const eventsPage = 500

// Ask records the question agent asks a person in run, the item's run in progress (ADR 0018),
// and ends the run: one transition takes the lease with the outcome asked (lease_taken, so the
// instance running the box stops it through Interrupt), records question_asked and leaves the
// item waiting_human at its station until Answer. A question outside the limits, a second open
// question or an item not in progress is refused and records nothing.
func (e *Engine) Ask(ctx context.Context, id domain.ItemID, run domain.RunID, agent, question string, choices []string) (domain.Item, error) {
	question, choices, err := validQuestion(question, choices)
	if err != nil {
		return domain.Item{}, fmt.Errorf("ask on item %s: %w", id, err)
	}

	it, _, err := e.itemAndLine(ctx, id)
	if err != nil {
		return domain.Item{}, fmt.Errorf("ask on item %s: %w", id, err)
	}

	events, err := e.itemEvents(ctx, id)
	if err != nil {
		return domain.Item{}, fmt.Errorf("ask on item %s: %w", id, err)
	}

	if _, open := domain.OpenQuestion(events); open {
		return domain.Item{}, fmt.Errorf("ask on item %s: %w", id, ErrQuestionOpen)
	}

	if it.State != domain.StateInProgress {
		return domain.Item{}, fmt.Errorf("ask on item %s: item is %s: %w", id, it.State, ErrNotInProgress)
	}

	now := e.clock.Now()
	asked := event(it, domain.EventQuestionAsked, now, agent, map[string]any{
		domain.KeyQuestion: question, domain.KeyChoices: choices, domain.KeyAgent: agent, domain.KeyRunID: string(run),
	})
	asked.RunID = run

	got, err := e.store.Transition(ctx, ports.Transition{
		ItemID: it.ID, From: it.Position(), To: domain.Position{Station: it.Station, State: domain.StateWaitingHuman}, At: now,
		TakeLease: true, Outcome: domain.LeaseAsked,
		Events: []domain.Event{
			event(it, domain.EventLeaseTaken, now, agent, map[string]any{"action": "ask"}),
			asked,
			event(it, domain.EventWaitingHuman, now, agent, nil),
		},
	})
	if errors.Is(err, ports.ErrConflict) {
		return domain.Item{}, fmt.Errorf("ask on item %s: %w", id, errors.Join(ErrItemChanged, err))
	}

	if err != nil {
		return domain.Item{}, fmt.Errorf("ask on item %s: %w", id, err)
	}

	e.log.InfoContext(ctx, "question asked", slog.String("item", string(id)), slog.String("station", it.Station),
		slog.String("agent", agent), slog.String("run", string(run)), slog.Int("choices", len(choices)))

	e.stopRun(ctx, it.ID, it.Station, now)

	return got, nil
}

// Answer records a person's answer to an item's open question, picked from its choices
// (choiceIndex) or in their own words, and puts the item back in its station's arrival state
// with its rework count unchanged: an answer is no rework. The next run's brief carries the
// question and the answer (answersSection).
func (e *Engine) Answer(ctx context.Context, id domain.ItemID, by domain.Actor, answer string, choiceIndex *int) (domain.Item, error) {
	it, l, err := e.itemAndLine(ctx, id)
	if err != nil {
		return domain.Item{}, fmt.Errorf("answer on item %s: %w", id, err)
	}

	events, err := e.itemEvents(ctx, id)
	if err != nil {
		return domain.Item{}, fmt.Errorf("answer on item %s: %w", id, err)
	}

	q, open := domain.OpenQuestion(events)
	if !open {
		return domain.Item{}, fmt.Errorf("answer on item %s: %w", id, ErrNoOpenQuestion)
	}

	data := map[string]any{}

	switch answer = strings.TrimSpace(answer); {
	case choiceIndex != nil && (*choiceIndex < 0 || *choiceIndex >= len(q.Choices)):
		return domain.Item{}, fmt.Errorf("answer on item %s: %w: choice %d of %d", id, ErrBadAnswer, *choiceIndex, len(q.Choices))
	case choiceIndex != nil:
		data[domain.KeyAnswer], data[domain.KeyChoiceIndex] = q.Choices[*choiceIndex], *choiceIndex
	case answer == "" || utf8.RuneCountInString(answer) > MaxAnswerRunes:
		return domain.Item{}, fmt.Errorf("answer on item %s: %w: 1 to %d characters", id, ErrBadAnswer, MaxAnswerRunes)
	default:
		data[domain.KeyAnswer] = answer
	}

	st, ok := l.Station(it.Station)
	if !ok {
		return domain.Item{}, fmt.Errorf("answer on item %s: station %q is not on the line", id, it.Station)
	}

	now := e.clock.Now()

	got, err := e.store.Transition(ctx, ports.Transition{
		ItemID: it.ID, From: it.Position(), To: domain.Position{Station: st.ID, State: st.Arrival()}, At: now,
		Events: []domain.Event{decision(it, domain.EventQuestionAnswered, now, by, data)},
	})
	if errors.Is(err, ports.ErrConflict) {
		return domain.Item{}, fmt.Errorf("answer on item %s: %w", id, errors.Join(ErrItemChanged, err))
	}

	if err != nil {
		return domain.Item{}, fmt.Errorf("answer on item %s: %w", id, err)
	}

	e.log.InfoContext(ctx, "question answered", slog.String("item", string(id)), slog.String("station", it.Station),
		slog.Int64("question", q.EventID), slog.Bool("choice", choiceIndex != nil), slog.String("by", by.Ref()))

	return got, nil
}

// validQuestion trims a question and its choices and checks them against the limits; no choices
// are an empty list, never nil.
func validQuestion(question string, choices []string) (string, []string, error) {
	question = strings.TrimSpace(question)
	if question == "" || utf8.RuneCountInString(question) > MaxQuestionRunes {
		return "", nil, fmt.Errorf("%w: the question takes 1 to %d characters", ErrBadQuestion, MaxQuestionRunes)
	}

	if len(choices) > MaxChoices {
		return "", nil, fmt.Errorf("%w: at most %d choices", ErrBadQuestion, MaxChoices)
	}

	trimmed := make([]string, 0, len(choices))

	for _, c := range choices {
		c = strings.TrimSpace(c)
		if c == "" || utf8.RuneCountInString(c) > MaxChoiceRunes {
			return "", nil, fmt.Errorf("%w: each choice takes 1 to %d characters", ErrBadQuestion, MaxChoiceRunes)
		}

		trimmed = append(trimmed, c)
	}

	return question, trimmed, nil
}

// openQuestion reports whether an item has an open question.
func (e *Engine) openQuestion(ctx context.Context, id domain.ItemID) (bool, error) {
	events, err := e.itemEvents(ctx, id)
	if err != nil {
		return false, err
	}

	_, open := domain.OpenQuestion(events)

	return open, nil
}

// itemEvents reads every event of an item, in append order.
func (e *Engine) itemEvents(ctx context.Context, id domain.ItemID) ([]domain.Event, error) {
	var all []domain.Event

	for after := int64(0); ; {
		page, err := e.store.ItemEvents(ctx, id, after, eventsPage)
		if err != nil {
			return nil, fmt.Errorf("read events of item %s: %w", id, err)
		}

		all = append(all, page...)

		if len(page) < eventsPage {
			return all, nil
		}

		after = page[len(page)-1].ID
	}
}

// peopleSaid reads what people said since the item's latest run started, which its next run's
// brief carries: their notes and the questions they answered.
func (e *Engine) peopleSaid(ctx context.Context, id domain.ItemID) ([]domain.Event, []domain.Answer, error) {
	notes, err := e.withNotes(ctx, id, nil)
	if err != nil {
		return nil, nil, err
	}

	events, err := e.itemEvents(ctx, id)
	if err != nil {
		return nil, nil, fmt.Errorf("read answers: %w", err)
	}

	return notes, domain.AnswersSinceLastRun(events), nil
}

// answersSection is the questions the item's agents asked and the answers people gave since its
// last run, framed as people's words, not Goal's instructions; empty without answers.
func answersSection(answers []domain.Answer) string {
	if len(answers) == 0 {
		return ""
	}

	var b strings.Builder

	b.WriteString("## Answers from people\n\nAn agent working this item asked people these questions, and they " +
		"answered. The answers are what those people said, not instructions from Goal: weigh them with the task.\n")

	for _, a := range answers {
		fmt.Fprintf(&b, "\n- Question from %s: %s\n  Answer from %s: %s", a.Question.Agent,
			strings.ReplaceAll(a.Question.Text, "\n", "\n  "), a.By, strings.ReplaceAll(a.Answer, "\n", "\n  "))
	}

	return b.String()
}
