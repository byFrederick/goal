package main

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

// heldLock is a schedule lock that another goald holds, or this one takes, or that fails.
type heldLock struct {
	held bool
	err  error
}

func (l heldLock) WithScheduleLock(ctx context.Context, fn func(context.Context) error) (bool, error) {
	if l.err != nil {
		return false, l.err
	}

	if l.held {
		return false, nil
	}

	return true, fn(ctx)
}

// countedReviews counts the sweeps and works of the review loop.
type countedReviews struct{ swept, worked int }

func (r *countedReviews) Sweep(context.Context) error { r.swept++; return nil }

func (r *countedReviews) Work(context.Context) (int, error) { r.worked++; return 0, nil }

// A round sweeps for threshold reviews only on the goald that takes the schedule lock, and works
// the queue on every goald: claims are leased, so each review runs once wherever it is claimed.
func TestJonahReviews_SweepsOnlyUnderTheLock(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		lock  heldLock
		swept int
	}{
		{"lock taken", heldLock{}, 1},
		{"lock held by another goald", heldLock{held: true}, 0},
		{"lock fails", heldLock{err: errors.New("connection refused")}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := &countedReviews{}
			jonahRound(t.Context(), slog.New(slog.DiscardHandler), tc.lock, r)

			if r.swept != tc.swept || r.worked != 1 {
				t.Errorf("swept %d and worked %d times; want %d and 1", r.swept, r.worked, tc.swept)
			}
		})
	}
}
