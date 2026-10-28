package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The question tool as an agent names it, its arguments and a question the tests ask.
const (
	questionTool = "question__ask"
	argQuestion  = "question"
	argChoices   = "choices"
	shipIt       = "Ship it?"
)

// fakeQuestions stands in for the engine behind the gateway: it keeps every question it is
// asked and answers with err.
type fakeQuestions struct {
	mu    sync.Mutex
	asked []Question
	err   error
}

func (f *fakeQuestions) Ask(_ context.Context, q Question) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.asked = append(f.asked, q)

	return f.err
}

func (f *fakeQuestions) questions() []Question {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.asked)
}

func (f *fakeQuestions) answer(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.err = err
}

// ADR 0018 decision 1: every run is served question__ask, whose schema bounds the question and
// its choices.
func TestGateway_QuestionToolListed(t *testing.T) {
	t.Parallel()

	list, err := newHarness(t, Config{}).connect(t, "").ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	i := slices.IndexFunc(list.Tools, func(tool *mcp.Tool) bool { return tool.Name == questionTool })
	if i < 0 {
		t.Fatalf("tools = %v; want %s", toolNames(list), questionTool)
	}

	tool := list.Tools[i]
	if !strings.Contains(tool.Description, "run ends") || !strings.Contains(tool.Description, "next run gets the answer") {
		t.Errorf("description = %q; want it to say the run ends and the next run gets the answer", tool.Description)
	}

	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatal(err)
	}

	var got askSchemaShape
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}

	want := askSchemaShape{AdditionalProperties: false, Required: [2]string{argQuestion}}
	want.Properties.Question = stringShape{Type: "string", MinLength: 1, MaxLength: 500}
	want.Properties.Choices = arrayShape{Type: "array", MinItems: 0, MaxItems: 4, Items: stringShape{Type: "string", MinLength: 1, MaxLength: 60}}

	if got != want {
		t.Errorf("input schema = %s; want question 1-500, choices 0-4 of 1-60, nothing else", raw)
	}
}

// askSchemaShape is what TestGateway_QuestionToolListed checks of question__ask's input schema.
type askSchemaShape struct {
	// AdditionalProperties is a bool when the schema sets it.
	AdditionalProperties any       `json:"additionalProperties"`
	Required             [2]string `json:"required"`
	Properties           struct {
		Question stringShape `json:"question"`
		Choices  arrayShape  `json:"choices"`
	} `json:"properties"`
}

type stringShape struct {
	Type      string `json:"type"`
	MinLength int    `json:"minLength"`
	MaxLength int    `json:"maxLength"`
}

type arrayShape struct {
	Type     string      `json:"type"`
	MinItems int         `json:"minItems"`
	MaxItems int         `json:"maxItems"`
	Items    stringShape `json:"items"`
}

// A good question reaches the engine with the task's item, run and agent, and the agent is told
// to stop.
func TestGateway_QuestionAskReachesEngine(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Config{})
	cs := h.connect(t, "")

	text, isErr := knowledgeCall(t, cs, questionTool, map[string]any{
		argQuestion: " Which colour should the button be? ", argChoices: []string{"Blue", "Green"},
	})
	if isErr || !strings.Contains(text, "sent") || !strings.Contains(text, "Stop now") {
		t.Errorf("result = %q (error %v); want that the question was sent and to stop now", text, isErr)
	}

	want := Question{Item: itemID, Run: h.claims().RunID, Agent: agentEngineer, Text: "Which colour should the button be?", Choices: []string{"Blue", "Green"}}
	if got := h.questions.questions(); len(got) != 1 || !questionEqual(got[0], want) {
		t.Errorf("asked = %+v; want %+v", got, want)
	}

	// Choices are optional.
	if _, isErr := knowledgeCall(t, cs, questionTool, map[string]any{argQuestion: shipIt}); isErr {
		t.Error("a question without choices is a tool error; want it sent")
	}
}

func questionEqual(a, b Question) bool {
	return a.Item == b.Item && a.Run == b.Run && a.Agent == b.Agent && a.Text == b.Text && slices.Equal(a.Choices, b.Choices)
}

// Input outside ADR 0018's limits is a tool error with the reason, and never reaches the engine.
func TestGateway_QuestionAskRefusesBadInput(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Config{})
	cs := h.connect(t, "")

	for name, args := range map[string]map[string]any{
		"not a string":     {argQuestion: 5},
		"unknown field":    {argQuestion: shipIt, "urgent": true},
		"empty question":   {argQuestion: "  "},
		"long question":    {argQuestion: strings.Repeat("é", 501)},
		"five choices":     {argQuestion: shipIt, argChoices: []string{"a", "b", "c", "d", "e"}},
		"long choice":      {argQuestion: shipIt, argChoices: []string{strings.Repeat("x", 61)}},
		"empty choice":     {argQuestion: shipIt, argChoices: []string{" "}},
		"choices a string": {argQuestion: shipIt, argChoices: "yes"},
	} {
		if text, isErr := knowledgeCall(t, cs, questionTool, args); !isErr || text == "" {
			t.Errorf("%s: result = %q (error %v); want a tool error with the reason", name, text, isErr)
		}
	}

	if got := h.questions.questions(); len(got) != 0 {
		t.Errorf("asked = %+v; want no question to reach the engine", got)
	}

	for _, e := range h.events() {
		if e.Data["outcome"] != outcomeToolError {
			t.Errorf("audit outcome = %v; want %s", e.Data["outcome"], outcomeToolError)
		}
	}
}

// The engine's refusal of a second open question is a tool error with its reason; any other
// failure is a tool error that says to try again and shows nothing of the failure.
func TestGateway_QuestionAskAlreadyOpen(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Config{})
	cs := h.connect(t, "")

	const open = "this item already has an open question; stop and wait for its answer"

	h.questions.answer(fmt.Errorf("ask: %w", &QuestionRefusedError{Reason: open}))

	if text, isErr := knowledgeCall(t, cs, questionTool, map[string]any{argQuestion: shipIt}); !isErr || text != open {
		t.Errorf("result = %q (error %v); want the tool error %q", text, isErr, open)
	}

	h.questions.answer(errors.New("store: connection reset by peer"))

	if text, isErr := knowledgeCall(t, cs, questionTool, map[string]any{argQuestion: shipIt}); !isErr || !strings.Contains(text, "try again") || strings.Contains(text, "peer") {
		t.Errorf("result = %q (error %v); want a generic try-again tool error", text, isErr)
	}

	var outcomes []any
	for _, e := range h.events() {
		outcomes = append(outcomes, e.Data["outcome"])
	}

	if want := []any{outcomeToolError, outcomeFailed}; !slices.Equal(outcomes, want) {
		t.Errorf("audit outcomes = %v; want %v", outcomes, want)
	}
}

// Every call is audited as question__ask without the question or its choices.
func TestGateway_QuestionAskAuditHasNoArguments(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Config{})
	cs := h.connect(t, "")

	if _, isErr := knowledgeCall(t, cs, questionTool, map[string]any{argQuestion: "Which secret colour?", argChoices: []string{"Mauve"}}); isErr {
		t.Fatal("the question was refused; want it sent")
	}

	events := h.events()
	if len(events) != 1 {
		t.Fatalf("audit events = %+v; want one", events)
	}

	e := events[0]
	if e.Kind != EventToolCall || e.RunID != h.claims().RunID || e.Actor != agentEngineer ||
		e.Data["connection"] != "question" || e.Data["tool"] != "ask" || e.Data["outcome"] != outcomeOK {
		t.Errorf("event = %+v; want an ok question/ask audit event of the run", e)
	}

	if s := fmt.Sprint(e.Data); strings.Contains(s, "secret colour") || strings.Contains(s, "Mauve") {
		t.Errorf("audit data = %s; want neither the question nor its choices", s)
	}
}
