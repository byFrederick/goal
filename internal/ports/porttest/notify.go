// Package porttest provides in-memory, concurrency-safe fakes of every port for tests of the
// engine and, later, the HTTP handlers. They behave like the real adapters (claims honour
// slots and pull order, transitions are compare-and-set) without being elaborate.
package porttest

import (
	"context"
	"sync"
)

// changes lets tests wait for a condition on a fake without sleeping: every mutation closes
// the current channel and opens a new one.
type changes struct {
	mu sync.Mutex
	ch chan struct{}
}

func (c *changes) current() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ch == nil {
		c.ch = make(chan struct{})
	}

	return c.ch
}

func (c *changes) signal() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ch != nil {
		close(c.ch)
	}

	c.ch = make(chan struct{})
}

// await blocks until cond holds or ctx ends. cond must take the fake's own lock if it reads state.
func (c *changes) await(ctx context.Context, cond func() bool) error {
	for {
		ch := c.current()

		if cond() {
			return nil
		}

		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
