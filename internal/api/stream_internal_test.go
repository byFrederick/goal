package api

import (
	"context"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

// acme and labs are the organizations of the feed tests.
const (
	acme domain.OrgID = "019a1c00-0000-7000-8000-0000000000aa"
	labs domain.OrgID = "019a1c00-0000-7000-8000-0000000000bb"
)

// A client only ever holds its own organization's notices, so another organization's traffic
// neither reaches it nor fills its buffer; every client gets each resync.
func TestFeed_HandsEachClientItsOrgsNotices(t *testing.T) {
	t.Parallel()

	notices := porttest.NewItemNotices()
	definitions := porttest.NewDefinitionNotices()
	feed := NewFeed(notices, definitions, nil)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- feed.Run(ctx) }()

	if err := notices.AwaitListeners(ctx, 1); err != nil {
		t.Fatal(err)
	}

	// Each listener starts with a resync; the clients subscribe after both.
	if err := definitions.AwaitListeners(ctx, 1); err != nil {
		t.Fatal(err)
	}

	ofLabs, _ := feed.subscribe(labs)

	for i := range clientBuffer + 1 {
		notices.Publish(ports.ItemNotice{OrgID: acme, EventID: int64(i + 1)})
	}

	notices.Publish(ports.ItemNotice{OrgID: labs, EventID: 900})
	notices.Publish(ports.ItemNotice{Resync: true})

	if n := <-ofLabs; n.EventID != 900 {
		t.Errorf("labs' client got %+v first, want labs' own notice", n)
	}

	if n := <-ofLabs; !n.Resync {
		t.Errorf("labs' client got %+v, want the resync", n)
	}

	cancel()

	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// One slow stream client must never hold up the listener or the other clients.
func TestFeed_EndsTheStreamOfAClientThatFallsBehind(t *testing.T) {
	t.Parallel()

	notices := porttest.NewItemNotices()
	definitions := porttest.NewDefinitionNotices()
	feed := NewFeed(notices, definitions, nil)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- feed.Run(ctx) }()

	if err := notices.AwaitListeners(ctx, 1); err != nil {
		t.Fatal(err)
	}

	// Each listener starts with a resync; the clients subscribe after both.
	if err := definitions.AwaitListeners(ctx, 1); err != nil {
		t.Fatal(err)
	}

	slow, _ := feed.subscribe(acme)
	fast, _ := feed.subscribe(acme)

	for i := range clientBuffer + 1 {
		notices.Publish(ports.ItemNotice{OrgID: acme, EventID: int64(i + 1)})

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
