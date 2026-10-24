package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

// Feed fans the notices of two listeners, item and organization events and definition changes,
// out to this goald instance's stream clients, each client receiving only its organization's
// notices (and every resync). It holds only the clients connected to this instance, so any
// instance serves any client.
type Feed struct {
	notices     ports.ItemNotices
	definitions ports.DefinitionNotices
	taps        []func(ports.ItemNotice)
	log         *slog.Logger

	mu      sync.Mutex
	clients map[chan ports.ItemNotice]domain.OrgID
	closed  bool
}

// NewFeed returns a feed over item notices and definition notices; Run starts it. log may be
// nil. Each tap is told every item notice (resyncs included) before the clients, on the
// listener's goroutine, so it must not block: goald's engine is one, to stop the runs whose lease
// a person took (engine.Interrupt).
func NewFeed(notices ports.ItemNotices, definitions ports.DefinitionNotices, log *slog.Logger, taps ...func(ports.ItemNotice)) *Feed {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &Feed{notices: notices, definitions: definitions, taps: taps, log: log, clients: map[chan ports.ItemNotice]domain.OrgID{}}
}

// Run listens to both sources until ctx ends, or one of them fails, then ends every client's
// stream.
func (f *Feed) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errs := make(chan error, 2)

	go func() { errs <- f.notices.Listen(ctx, f.deliver) }()
	go func() { errs <- f.definitions.Listen(ctx, f.broadcastDefinition) }()

	// The first to return (ctx ended, or a listener failed) stops the other.
	err := <-errs

	cancel()

	err = errors.Join(err, <-errs)

	f.mu.Lock()
	defer f.mu.Unlock()

	f.closed = true
	for ch := range f.clients {
		delete(f.clients, ch)
		close(ch)
	}

	return err
}

// definitionEvent is the stream's name for definition changes.
const definitionEvent = "definition"

// broadcastDefinition hands a definition change to the clients of its organization as an
// organization event (event: definition); a resync of the definition listener is a resync of
// every client.
func (f *Feed) broadcastDefinition(n ports.DefinitionNotice) {
	if n.Resync {
		f.broadcast(ports.ItemNotice{Resync: true})
		return
	}

	// An organization's deletion is for goald's cache, not a definition event; its clients'
	// requests answer 404 from now on.
	if n.Kind == ports.NoticeOrg {
		return
	}

	data, err := json.Marshal(definitionEventOf(n))
	if err != nil {
		f.log.Error("encode definition event", slog.Any("error", err))
		return
	}

	f.broadcast(ports.ItemNotice{
		OrgID: n.OrgID, FactoryID: n.FactoryID,
		Org: &ports.OrgEvent{Name: definitionEvent, FactoryID: n.FactoryID, Data: data},
	})
}

func definitionEventOf(n ports.DefinitionNotice) gen.DefinitionEvent {
	e := gen.DefinitionEvent{
		OrgId: string(n.OrgID), FactoryId: string(n.FactoryID), Kind: gen.DefinitionEventKind(n.Kind),
		VersionId: optional(string(n.VersionID)), By: optional(n.By),
	}

	if n.Number > 0 {
		e.Number = &n.Number
	}

	if n.Revision > 0 {
		e.Revision = &n.Revision
	}

	return e
}

// deliver tells the taps an item notice, then broadcasts it.
func (f *Feed) deliver(n ports.ItemNotice) {
	for _, tap := range f.taps {
		tap(n)
	}

	f.broadcast(n)
}

// broadcast hands a notice to every client of its organization (a resync to every client)
// without waiting: a client whose buffer is full is dropped (its channel closed), so one slow
// client never holds up the others.
func (f *Feed) broadcast(n ports.ItemNotice) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for ch, org := range f.clients {
		if !n.Resync && n.OrgID != org {
			continue
		}

		select {
		case ch <- n:
		default:
			f.log.Warn("stream client fell behind; ending its stream")
			delete(f.clients, ch)
			close(ch)
		}
	}
}

// subscribe returns a channel of org's notices, closed when the client is dropped or the feed
// stops, and the function that unsubscribes it.
func (f *Feed) subscribe(org domain.OrgID) (<-chan ports.ItemNotice, func()) {
	f.mu.Lock()
	defer f.mu.Unlock()

	ch := make(chan ports.ItemNotice, clientBuffer)
	if f.closed {
		close(ch)

		return ch, func() {}
	}

	f.clients[ch] = org

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
		f, err := s.factory(ctx, ref)
		if err != nil {
			return nil, err
		}

		factory = domain.FactoryID(f.Metadata.ID)
	}

	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, fmt.Errorf("stream: %w", domain.ErrNoOrg)
	}

	notices, unsubscribe := s.feed.subscribe(org)

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
// still there. The feed hands it only its organization's notices.
func (st stream) send(write func(string, ...any) bool, n ports.ItemNotice) bool {
	if n.Resync {
		return write("event: resync\ndata: {}\n\n")
	}

	if st.factory != "" && n.FactoryID != st.factory {
		return true
	}

	if n.Org != nil {
		return st.sendOrgEvent(write, n.Org)
	}

	data, err := json.Marshal(gen.StreamEvent{
		OrgId: string(n.OrgID), ItemId: string(n.ItemID), Ref: st.s.ref(st.ctx, n.FactoryID, n.Number), FactoryId: string(n.FactoryID),
		Kind: string(n.Kind), Station: optional(n.Station), State: gen.ItemState(n.State), At: n.At,
	})
	if err != nil {
		st.s.log.ErrorContext(st.ctx, "encode stream event", slog.Any("error", err))
		return true
	}

	return write("event: item\nid: %d\ndata: %s\n\n", n.EventID, data)
}

// sendOrgEvent writes an organization event under its own name, with its data object on one
// line. A name or data that could break the event stream is never written.
func (st stream) sendOrgEvent(write func(string, ...any) bool, e *ports.OrgEvent) bool {
	var data bytes.Buffer
	if err := json.Compact(&data, e.Data); err != nil || !domain.IsEventName(e.Name) {
		st.s.log.WarnContext(st.ctx, "organization event not sent", slog.String("event", e.Name), slog.Any("error", err))
		return true
	}

	return write("event: %s\ndata: %s\n\n", e.Name, data.Bytes())
}
