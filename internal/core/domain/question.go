package domain

import (
	"slices"
	"time"
)

// Event data keys of questions (EventQuestionAsked, EventQuestionAnswered).
const (
	KeyQuestion    = "question"
	KeyChoices     = "choices"
	KeyAgent       = "agent"
	KeyRunID       = "run_id"
	KeyAnswer      = "answer"
	KeyChoiceIndex = "choice_index"
)

// Question is what an agent asked a person about an item (ADR 0018), read from its
// question_asked event.
type Question struct {
	// EventID is the question_asked event's id.
	EventID int64
	Text    string
	// Choices are the quick answers the agent offered; none for a free answer only.
	Choices []string
	// Agent is the asking agent's role name, never its slot.
	Agent   string
	RunID   RunID
	Station string
	At      time.Time
}

// Answer is a person's answer to a question, read from its question_answered event.
type Answer struct {
	Question Question
	// Answer is the picked choice's text, or the person's own words.
	Answer string
	// ChoiceIndex is the picked choice; nil for a free answer.
	ChoiceIndex *int
	// By is the person's name, as the event's actor.
	By string
	At time.Time
}

// withdraws are the person's decisions that withdraw an open question without answering it.
var withdraws = []EventKind{EventMoved, EventPaused, EventRejected, EventCancelled}

// OpenQuestion finds an item's open question in its events, in append order: the latest
// question_asked with no question_answered, moved, paused, rejected or cancelled after it.
func OpenQuestion(events []Event) (Question, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		switch e := events[i]; {
		case e.Kind == EventQuestionAsked:
			return QuestionOf(e), true
		case e.Kind == EventQuestionAnswered || slices.Contains(withdraws, e.Kind):
			return Question{}, false
		}
	}

	return Question{}, false
}

// AnswersSinceLastRun returns the questions answered after the item's latest run_started event,
// each with its answer, in the order they were answered: what the next run's brief carries.
func AnswersSinceLastRun(events []Event) []Answer {
	since := 0

	for i, e := range slices.Backward(events) {
		if e.Kind == EventRunStarted {
			since = i + 1
			break
		}
	}

	var (
		answers []Answer
		asked   *Event
	)

	for i, e := range events {
		switch {
		case e.Kind == EventQuestionAsked:
			asked = &events[i]
		case e.Kind == EventQuestionAnswered && asked != nil:
			if i >= since {
				answers = append(answers, answerOf(QuestionOf(*asked), e))
			}

			asked = nil
		case slices.Contains(withdraws, e.Kind):
			asked = nil
		}
	}

	return answers
}

// QuestionOf reads a question_asked event. Its data may come from JSON, so choices are []any and
// []string alike.
func QuestionOf(e Event) Question {
	q := Question{EventID: e.ID, Station: e.Station, At: e.At, RunID: e.RunID}
	q.Text, _ = e.Data[KeyQuestion].(string)
	q.Agent, _ = e.Data[KeyAgent].(string)

	if run, ok := e.Data[KeyRunID].(string); ok && run != "" {
		q.RunID = RunID(run)
	}

	switch cs := e.Data[KeyChoices].(type) {
	case []string:
		q.Choices = slices.Clone(cs)
	case []any:
		for _, c := range cs {
			if s, ok := c.(string); ok {
				q.Choices = append(q.Choices, s)
			}
		}
	}

	return q
}

func answerOf(q Question, e Event) Answer {
	a := Answer{Question: q, By: e.Actor, At: e.At}
	a.Answer, _ = e.Data[KeyAnswer].(string)

	switch n := e.Data[KeyChoiceIndex].(type) {
	case int:
		a.ChoiceIndex = &n
	case float64:
		i := int(n)
		a.ChoiceIndex = &i
	}

	return a
}
