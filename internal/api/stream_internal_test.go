package api

import (
	"context"
	"testing"

	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

// One slow stream client must never hold up the listener or the other clients.
func TestFeed_EndsTheStreamOfAClientThatFallsBehind(t *testing.T) {
	t.Parallel()

	notices := porttest.NewItemNotices()
	feed := NewFeed(notices, nil)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- feed.Run(ctx) }()

	if err := notices.AwaitListeners(ctx, 1); err != nil {
		t.Fatal(err)
	}

	slow, _ := feed.subscribe()
	fast, _ := feed.subscribe()

	for i := range clientBuffer + 1 {
		notices.Publish(ports.ItemNotice{EventID: int64(i + 1)})

		if n := <-fast; n.EventID != int64(i+1) {
			t.Fatalf("fast client got event %d, want %d", n.EventID, i+1)
		}
	}

	got := 0
	for range slow {
		got++
	}

	if got != clientBuffer {
		t.Errorf("slow client got %d notices before its stream ended, want its buffer (%d)", got, clientBuffer)
	}

	cancel()

	if err := <-done; err != nil {
		t.Fatal(err)
	}

	if _, open := <-fast; open {
		t.Error("a client's stream is still open after the feed stopped")
	}
}
