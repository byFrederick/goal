package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Stream pacing and buffering.
const (
	heartbeatEvery = 15 * time.Second
	// clientBuffer is how many notices a stream client may fall behind before goald ends its
	// stream; the client reconnects and re-reads.
	clientBuffer = 256
	// reconnectMillis is the reconnection delay the stream asks EventSource clients to use.
	reconnectMillis = 3000
)

// Feed fans the item notices of one listener out to this goald instance's stream clients.
// It holds only the clients connected to this instance, so any instance serves any client.
type Feed struct {
	notices ports.ItemNotices
	log     *slog.Logger

	mu      sync.Mutex
	clients map[chan ports.ItemNotice]struct{}
	closed  bool
}

// NewFeed returns a feed over notices; Run starts it. log may be nil.
func NewFeed(notices ports.ItemNotices, log *slog.Logger) *Feed {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &Feed{notices: notices, log: log, clients: map[chan ports.ItemNotice]struct{}{}}
}

// Run listens until ctx ends, then ends every client's stream.
func (f *Feed) Run(ctx context.Context) error {
	err := f.notices.Listen(ctx, f.broadcast)

	f.mu.Lock()
	defer f.mu.Unlock()

	f.closed = true
	for ch := range f.clients {
		delete(f.clients, ch)
		close(ch)
	}

	return err
}

// broadcast hands a notice to every client without waiting: a client whose buffer is full is
// dropped (its channel closed), so one slow client never holds up the others.
func (f *Feed) broadcast(n ports.ItemNotice) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for ch := range f.clients {
		select {
		case ch <- n:
		default:
			f.log.Warn("stream client fell behind; ending its stream")
			delete(f.clients, ch)
			close(ch)
		}
	}
}

// subscribe returns a channel of notices, closed when the client is dropped or the feed stops,
// and the function that unsubscribes it.
func (f *Feed) subscribe() (<-chan ports.ItemNotice, func()) {
	f.mu.Lock()
	defer f.mu.Unlock()

	ch := make(chan ports.ItemNotice, clientBuffer)
	if f.closed {
		close(ch)

		return ch, func() {}
	}

	f.clients[ch] = struct{}{}

	return ch, func() {
		f.mu.Lock()
		defer f.mu.Unlock()

		if _, ok := f.clients[ch]; ok {
			delete(f.clients, ch)
			close(ch)
		}
	}
}

func (s *server) StreamEvents(ctx context.Context, req gen.StreamEventsRequestObject) (gen.StreamEventsResponseObject, error) {
	var factory domain.FactoryID

	if ref := deref(req.Params.Factory); ref != "" {
		f, err := s.factories.Lookup(ctx, ref)
		if err != nil {
			return nil, err
		}

		factory = domain.FactoryID(f.Metadata.ID)
	}

	notices, unsubscribe := s.feed.subscribe()

	return stream{ctx: ctx, s: s, factory: factory, notices: notices, unsubscribe: unsubscribe}, nil
}

// stream writes one client's Server-Sent Events until the client leaves, it falls behind or
// goald stops.
type stream struct {
	ctx         context.Context //nolint:containedctx // the request's context, kept for the generated Visit call
	s           *server
	factory     domain.FactoryID
	notices     <-chan ports.ItemNotice
	unsubscribe func()
}

func (st stream) VisitStreamEventsResponse(w http.ResponseWriter) error {
	defer st.unsubscribe()

	rc := http.NewResponseController(w)

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	heartbeat := st.s.clock.NewTicker(heartbeatEvery)
	defer heartbeat.Stop()

	// Once the status is written, errors cannot become problem details: a failed write means
	// the client left, so the stream just ends.
	write := func(format string, a ...any) bool {
		if _, err := fmt.Fprintf(w, format, a...); err != nil {
			return false
		}

		return rc.Flush() == nil
	}

	if !write("retry: %d\n\n", reconnectMillis) {
		return nil
	}

	for {
		select {
		case <-st.ctx.Done():
			return nil
		case <-heartbeat.C():
			if !write(": heartbeat\n\n") {
				return nil
			}
		case n, ok := <-st.notices:
			if !ok || !st.send(write, n) {
				return nil
			}
		}
	}
}

// send writes one notice, unless it is another factory's, and reports whether the client is
// still there.
func (st stream) send(write func(string, ...any) bool, n ports.ItemNotice) bool {
	if n.Resync {
		return write("event: resync\ndata: {}\n\n")
	}

	if st.factory != "" && n.FactoryID != st.factory {
		return true
	}

	data, err := json.Marshal(gen.StreamEvent{
		ItemId: string(n.ItemID), Ref: st.s.ref(n.FactoryID, n.Number), FactoryId: string(n.FactoryID),
		Kind: string(n.Kind), Station: optional(n.Station), State: gen.ItemState(n.State), At: n.At,
	})
	if err != nil {
		st.s.log.ErrorContext(st.ctx, "encode stream event", slog.Any("error", err))
		return true
	}

	return write("event: item\nid: %d\ndata: %s\n\n", n.EventID, data)
}
