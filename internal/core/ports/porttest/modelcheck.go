package porttest

import (
	"context"
	"slices"
	"sync"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.ModelCheck = (*ModelCheck)(nil)

// ModelCheck is an in-memory model check: it records every question and returns the answer or
// the error it was scripted with.
type ModelCheck struct {
	mu     sync.Mutex
	calls  []ModelCheckCall
	answer domain.ModelAnswer
	err    error
}

// ModelCheckCall is one question asked of the fake, with the connection that would pay for it.
type ModelCheckCall struct {
	Connection domain.ResolvedConnection
	Question   domain.ModelQuestion
}

// NewModelCheck returns a model check that answers every question with answer.
func NewModelCheck(answer domain.ModelAnswer) *ModelCheck { return &ModelCheck{answer: answer} }

// Ask implements ports.ModelCheck.
func (m *ModelCheck) Ask(_ context.Context, conn domain.ResolvedConnection, q domain.ModelQuestion) (domain.ModelAnswer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls = append(m.calls, ModelCheckCall{Connection: conn, Question: q})
	if m.err != nil {
		return domain.ModelAnswer{}, m.err
	}

	return m.answer, nil
}

// Answer makes every later question get a (and no error).
func (m *ModelCheck) Answer(a domain.ModelAnswer) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.answer, m.err = a, nil
}

// Fail makes every later question fail with err (nil: answer again).
func (m *ModelCheck) Fail(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.err = err
}

// Calls are the questions asked so far, oldest first.
func (m *ModelCheck) Calls() []ModelCheckCall {
	m.mu.Lock()
	defer m.mu.Unlock()

	return slices.Clone(m.calls)
}
