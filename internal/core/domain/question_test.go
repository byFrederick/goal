package domain

import (
	"reflect"
	"testing"
	"time"
)

var qt0 = time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)

// asked is a question_asked event as the engine writes it; JSON-decoded choices are []any.
func asked(id int64, text string, choices ...any) Event {
	return Event{ID: id, At: qt0.Add(time.Duration(id) * time.Minute), Kind: EventQuestionAsked, Station: "build",
		Actor: "frontend-engineer", RunID: "run-1",
		Data: map[string]any{KeyQuestion: text, KeyChoices: choices, KeyAgent: "frontend-engineer", KeyRunID: "run-1"}}
}

func answered(id int64, answer string, choice any) Event {
	data := map[string]any{KeyAnswer: answer}
	if choice != nil {
		data[KeyChoiceIndex] = choice
	}

	return Event{ID: id, At: qt0.Add(time.Duration(id) * time.Minute), Kind: EventQuestionAnswered, Actor: "Ada", Data: data}
}

func ev(id int64, kind EventKind) Event { return Event{ID: id, Kind: kind} }

func TestOpenQuestion(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		events []Event
		want   string // the open question's text; empty for none
	}{
		{name: "no events"},
		{name: "asked", events: []Event{ev(1, EventRunStarted), asked(2, "Org or factory?")}, want: "Org or factory?"},
		{name: "answered", events: []Event{asked(1, "Org or factory?"), answered(2, "Org", nil)}},
		{name: "asked again after an answer", events: []Event{asked(1, "One?"), answered(2, "yes", nil), ev(3, EventRunStarted),
			asked(4, "Two?")}, want: "Two?"},
		{name: "withdrawn by a move", events: []Event{asked(1, "One?"), ev(2, EventMoved)}},
		{name: "withdrawn by a pause", events: []Event{asked(1, "One?"), ev(2, EventPaused)}},
		{name: "withdrawn by a rejection", events: []Event{asked(1, "One?"), ev(2, EventRejected)}},
		{name: "withdrawn by a cancel", events: []Event{asked(1, "One?"), ev(2, EventCancelled)}},
		{name: "a note does not withdraw it", events: []Event{asked(1, "One?"), ev(2, EventNote)}, want: "One?"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			q, ok := OpenQuestion(tt.events)
			if ok != (tt.want != "") || q.Text != tt.want {
				t.Errorf("OpenQuestion = %+v, %v; want %q", q, ok, tt.want)
			}
		})
	}
}

func TestOpenQuestion_ReadsTheAskedEvent(t *testing.T) {
	t.Parallel()

	q, ok := OpenQuestion([]Event{asked(3, "Org or factory spend?", "Organization", "Factory")})
	want := Question{EventID: 3, Text: "Org or factory spend?", Choices: []string{"Organization", "Factory"},
		Agent: "frontend-engineer", RunID: "run-1", Station: "build", At: qt0.Add(3 * time.Minute)}

	if !ok || !reflect.DeepEqual(q, want) {
		t.Errorf("OpenQuestion = %+v, %v; want %+v", q, ok, want)
	}
}

func TestAnswersSinceLastRun(t *testing.T) {
	t.Parallel()

	two := 1

	events := []Event{
		ev(1, EventRunStarted), asked(2, "Old?"), answered(3, "old", nil),
		ev(4, EventRunStarted), asked(5, "Org or factory?", "Org", "Factory"), answered(6, "Factory", float64(1)),
		asked(7, "Withdrawn?"), ev(8, EventMoved),
		asked(9, "Free?"), answered(10, "Write it short.", nil),
	}

	got := AnswersSinceLastRun(events)
	if len(got) != 2 {
		t.Fatalf("AnswersSinceLastRun = %+v; want the two answers since the latest run started", got)
	}

	if got[0].Question.Text != "Org or factory?" || got[0].Answer != "Factory" || got[0].ChoiceIndex == nil ||
		*got[0].ChoiceIndex != two || got[0].By != "Ada" {
		t.Errorf("first answer %+v; want Factory, choice 1, by Ada", got[0])
	}

	if got[1].Question.Text != "Free?" || got[1].Answer != "Write it short." || got[1].ChoiceIndex != nil {
		t.Errorf("second answer %+v; want the free text and no choice", got[1])
	}

	if none := AnswersSinceLastRun(append(events, ev(11, EventRunStarted))); len(none) != 0 {
		t.Errorf("after a later run started: %+v; want none", none)
	}
}
