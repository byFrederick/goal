package porttest

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.Minter = (*Minter)(nil)

// Minter is an in-memory aws-sts minter: it mints the values the platform's aws and bedrock kinds
// render, each naming the run (MintedValue), and records every mint.
type Minter struct {
	now func() time.Time

	mu    sync.Mutex
	mints []MintCall
	err   error
}

// MintCall is one mint asked of the fake.
type MintCall struct {
	Values map[string]string
	Run    domain.RunID
	TTL    time.Duration
}

// NewMinter returns a minter whose credentials expire ttl after now's time.
func NewMinter(now func() time.Time) *Minter { return &Minter{now: now} }

// MintedValue is the value the fake mints under name for run.
func MintedValue(name string, run domain.RunID) string { return "minted-" + name + "-" + string(run) }

// Mint implements ports.Minter.
func (m *Minter) Mint(_ context.Context, values map[string]string, run domain.RunID, ttl time.Duration) (map[string]string, time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.mints = append(m.mints, MintCall{Values: values, Run: run, TTL: ttl})
	if m.err != nil {
		return nil, time.Time{}, m.err
	}

	expires := m.now().Add(ttl)
	minted := map[string]string{}

	for _, name := range []string{"access_key_id", "secret_access_key", "session_token"} {
		minted[name] = MintedValue(name, run)
	}

	return minted, expires, nil
}

// Fail makes every later mint fail with err (nil: succeed again).
func (m *Minter) Fail(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.err = err
}

// Mints returns the mints asked for, in order.
func (m *Minter) Mints() []MintCall {
	m.mu.Lock()
	defer m.mu.Unlock()

	return slices.Clone(m.mints)
}
