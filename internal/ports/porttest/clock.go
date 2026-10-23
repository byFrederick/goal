package porttest

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/byFrederick/goal/internal/ports"
)

var _ ports.Clock = (*Clock)(nil)

// Clock is a manual clock: time moves only when a test calls Advance.
type Clock struct {
	mu      sync.Mutex
	now     time.Time
	tickers []*ticker
	changes changes
}

// NewClock returns a clock stopped at now.
func NewClock(now time.Time) *Clock { return &Clock{now: now} }

// Now returns the clock's current time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

// NewTicker returns a ticker that fires on Advance each time its period has elapsed.
func (c *Clock) NewTicker(d time.Duration) ports.Ticker {
	c.mu.Lock()
	t := &ticker{clock: c, c: make(chan time.Time, 1), every: d, next: c.now.Add(d)}
	c.tickers = append(c.tickers, t)
	c.mu.Unlock()

	c.changes.signal()

	return t
}

// Advance moves time forward and fires every ticker that came due, dropping ticks a slow
// receiver has not taken (as time.Ticker does).
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)

	for _, t := range c.tickers {
		for !t.next.After(c.now) {
			select {
			case t.c <- t.next:
			default:
			}

			t.next = t.next.Add(t.every)
		}
	}
	c.mu.Unlock()

	c.changes.signal()
}

// WaitTickers blocks until at least n tickers are running, so a test knows a goroutine is
// ready for the tick it is about to cause.
func (c *Clock) WaitTickers(ctx context.Context, n int) error {
	return c.changes.await(ctx, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()

		return len(c.tickers) >= n
	})
}

type ticker struct {
	clock *Clock
	c     chan time.Time
	every time.Duration
	next  time.Time
}

func (t *ticker) C() <-chan time.Time { return t.c }

func (t *ticker) Stop() {
	c := t.clock
	c.mu.Lock()
	c.tickers = slices.DeleteFunc(c.tickers, func(o *ticker) bool { return o == t })
	c.mu.Unlock()

	c.changes.signal()
}
