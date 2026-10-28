package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/byFrederick/goal/internal/core/domain"
)

// Questions records an agent's question to the person responsible for its item and ends its run
// (ADR 0018); cmd/goald adapts the engine's Ask to it. A question the engine refuses (one outside
// the limits, a second open question, an item no run works) is a *QuestionRefusedError, whose
// reason the agent reads; any other error is a failure the agent may retry.
type Questions interface {
	Ask(ctx context.Context, q Question) error
}

// Question is one question__ask call: the task's item, run and agent, the question and its
// quick-answer choices, trimmed.
type Question struct {
	Item    domain.ItemID
	Run     domain.RunID
	Agent   string
	Text    string
	Choices []string
}

// QuestionRefusedError is a question Questions refuses, with the reason the agent reads.
type QuestionRefusedError struct{ Reason string }

func (e *QuestionRefusedError) Error() string { return e.Reason }

func refuseQuestion(format string, a ...any) error {
	return &QuestionRefusedError{Reason: fmt.Sprintf(format, a...)}
}

// Goal's question tool (ADR 0018), served to every run: "question__ask".
const (
	questionServer = "question"
	toolAsk        = "ask"
)

// Limits of a question (ADR 0018 decision 1), in characters; the engine checks them again.
const (
	maxQuestionRunes = 500
	maxChoices       = 4
	maxChoiceRunes   = 60
)

const askSchema = `{"type":"object","additionalProperties":false,"required":["question"],"properties":{
"question":{"type":"string","minLength":1,"maxLength":500,"description":"The question, in plain words"},
"choices":{"type":"array","minItems":0,"maxItems":4,"items":{"type":"string","minLength":1,"maxLength":60},
"description":"Up to 4 quick answers the person can pick from; they can always write their own"}}}`

// askedText is what a good question__ask call answers.
const askedText = "Your question was sent to the person responsible. Stop now and do no more work: " +
	"your run ends, and the next run gets the answer."

// askTool is question__ask as tools/list describes it.
func askTool() *mcp.Tool {
	return &mcp.Tool{
		Name: questionServer + sep + toolAsk,
		Description: "Ask the person responsible for this item a question, with up to 4 quick-answer choices. " +
			"Ask only when you cannot go on without their answer. Your run ends, and the next run gets the answer.",
		InputSchema: json.RawMessage(askSchema),
		Annotations: &mcp.ToolAnnotations{Title: "Ask a person a question", DestructiveHint: new(bool), OpenWorldHint: new(bool)},
	}
}

// callQuestion answers one question__ask call and records it, without the question or its
// choices. Input outside the limits and the engine's refusals are tool errors with the reason;
// any other failure is a tool error that says to try again.
func (s *session) callQuestion(ctx context.Context, args json.RawMessage) *mcp.CallToolResult {
	start := s.g.clock.Now()
	rec := call{connection: questionServer, tool: toolAsk, outcome: outcomeOK, requestBytes: len(args)}
	text := askedText

	err := s.ask(ctx, args)

	var refusal *QuestionRefusedError

	switch {
	case errors.As(err, &refusal):
		rec.outcome, rec.err, text = outcomeToolError, refusal.Reason, refusal.Reason
	case err != nil:
		rec.outcome, rec.err, text = outcomeFailed, err.Error(), "the question was not sent; try again"
		s.g.log.WarnContext(ctx, "gateway question failed", claimAttrs(s.claims, "error", err)...)
	}

	rec.responseBytes = len(text)
	s.record(ctx, start, rec)

	return &mcp.CallToolResult{IsError: err != nil, Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

// ask checks args against the limits and passes the question to Questions.
func (s *session) ask(ctx context.Context, args json.RawMessage) error {
	var in struct {
		Question string   `json:"question"`
		Choices  []string `json:"choices"`
	}

	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()

	if err := dec.Decode(&in); err != nil {
		return refuseQuestion("the arguments are {question, choices?}: a question of 1 to %d characters and up to %d choices of 1 to %d characters",
			maxQuestionRunes, maxChoices, maxChoiceRunes)
	}

	q := Question{Item: s.claims.ItemID, Run: s.claims.RunID, Agent: s.claims.Agent, Text: strings.TrimSpace(in.Question)}

	if q.Text == "" || utf8.RuneCountInString(q.Text) > maxQuestionRunes {
		return refuseQuestion("question takes 1 to %d characters", maxQuestionRunes)
	}

	if len(in.Choices) > maxChoices {
		return refuseQuestion("choices are at most %d", maxChoices)
	}

	for _, c := range in.Choices {
		c = strings.TrimSpace(c)
		if c == "" || utf8.RuneCountInString(c) > maxChoiceRunes {
			return refuseQuestion("each choice takes 1 to %d characters", maxChoiceRunes)
		}

		q.Choices = append(q.Choices, c)
	}

	return s.g.questions.Ask(ctx, q)
}
