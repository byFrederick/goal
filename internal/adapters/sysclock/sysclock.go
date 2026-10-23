// Package sysclock implements ports.Clock with the system clock.
package sysclock

import (
	"time"

	"github.com/byFrederick/goal/internal/ports"
)

var _ ports.Clock = Clock{}

// Clock is the system clock.
type Clock struct{}

// Now implements ports.Clock.
func (Clock) Now() time.Time { return time.Now() }

// NewTicker implements ports.Clock.
func (Clock) NewTicker(d time.Duration) ports.Ticker { return ticker{time.NewTicker(d)} }

type ticker struct{ t *time.Ticker }

func (t ticker) C() <-chan time.Time { return t.t.C }
func (t ticker) Stop()               { t.t.Stop() }
