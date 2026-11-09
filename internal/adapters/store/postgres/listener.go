package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// Reconnect backoff after the listening connection is lost.
const (
	firstRetry = 100 * time.Millisecond
	maxRetry   = 10 * time.Second
)

// listenChannels are every NOTIFY channel goald follows. The listening connection LISTENs on all
// of them whoever subscribes, so a subscriber that joins later needs no LISTEN of its own.
var listenChannels = []string{noticeChannel, orgEventChannel, definitionChannel, accessChannel}

// listenHub is a store's one listening connection, outside its pool, so long-lived listeners
// never hold a pooled connection and all of them together hold one connection. It is open while
// at least one listener listens, LISTENs on every channel in listenChannels, hands each
// notification to the listeners of its channel in turn, and reconnects with backoff when the
// connection is lost.
type listenHub struct {
	cfg *pgx.ConnConfig

	mu   sync.Mutex
	log  *slog.Logger
	subs []*subscription
	// run is the connection loop, nil while nobody listens.
	run *hubRun
}

// hubRun is one start of the connection loop, cancelled when the last listener leaves.
type hubRun struct {
	cancel context.CancelFunc
	done   chan struct{}
	// listening is true once this run's connection LISTENs, until it is lost; open while it is
	// connected.
	listening, open bool
}

func newListenHub(cfg *pgx.ConnConfig) *listenHub { return &listenHub{cfg: cfg} }

// opened reports whether the listening connection is open: 0 or 1.
func (h *listenHub) opened() int {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.run != nil && h.run.open {
		return 1
	}

	return 0
}

// subscription is one listener's place on the hub. notify handles a notification of one of its
// channels and returns an error only when the hub's context ended while it read the notice back.
type subscription struct {
	channels []string
	resync   func()
	notify   func(ctx context.Context, channel, payload string) error

	// mu orders deliveries with leaving: once gone, nothing is delivered.
	mu   sync.Mutex
	gone bool
}

// send runs deliver unless the listener has left.
func (s *subscription) send(deliver func()) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.gone {
		deliver()
	}
}

// subscribe adds s. When the connection already listens, s gets its resync notice now, before
// any notification; otherwise the connection loop delivers it once it listens (and starts, when
// nobody listened).
func (h *listenHub) subscribe(s *subscription, log *slog.Logger) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.log == nil {
		h.log = log
	}

	h.subs = append(h.subs, s)

	switch {
	case h.run == nil:
		ctx, cancel := context.WithCancel(context.Background())
		h.run = &hubRun{cancel: cancel, done: make(chan struct{})}

		go h.loop(ctx, h.run)
	case h.run.listening:
		s.send(s.resync)
	}
}

// unsubscribe removes s; nothing is delivered to it once it returns. When s was the last
// listener, it stops the connection loop and waits for its connection to close.
func (h *listenHub) unsubscribe(s *subscription) {
	h.mu.Lock()

	h.subs = slices.DeleteFunc(h.subs, func(o *subscription) bool { return o == s })

	var stopped *hubRun
	if len(h.subs) == 0 && h.run != nil {
		stopped, h.run = h.run, nil
		stopped.cancel()
	}

	h.mu.Unlock()

	s.mu.Lock()
	s.gone = true
	s.mu.Unlock()

	if stopped != nil {
		<-stopped.done
	}
}

// subscribers returns the listeners of r, none once r is no longer the hub's run.
func (h *listenHub) subscribers(r *hubRun) []*subscription {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.run != r {
		return nil
	}

	return slices.Clone(h.subs)
}

// set updates r's state under the hub's lock and returns the listeners to resync when r starts
// listening.
func (h *listenHub) set(r *hubRun, open, listening bool) []*subscription {
	h.mu.Lock()
	defer h.mu.Unlock()

	r.open, r.listening = open, listening

	if !listening || h.run != r {
		return nil
	}

	return slices.Clone(h.subs)
}

func (h *listenHub) logger() *slog.Logger {
	h.mu.Lock()
	defer h.mu.Unlock()

	return discardIfNil(h.log)
}

// loop listens until ctx ends, reconnecting with backoff.
func (h *listenHub) loop(ctx context.Context, r *hubRun) {
	defer close(r.done)

	retry := firstRetry

	for ctx.Err() == nil {
		// A connection that got as far as listening starts the backoff over.
		listened, err := h.serve(ctx, r)
		if listened {
			retry = firstRetry
		}

		if ctx.Err() == nil {
			h.logger().WarnContext(ctx, "notices: listening connection lost; reconnecting",
				slog.Duration("in", retry), slog.Any("error", err))
			sleep(ctx, retry)
		}

		retry = min(2*retry, maxRetry)
	}
}

// sleep waits d or until ctx ends.
func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func (h *listenHub) serve(ctx context.Context, r *hubRun) (bool, error) {
	conn, err := pgx.ConnectConfig(ctx, h.cfg)
	if err != nil {
		return false, fmt.Errorf("connect: %w", err)
	}

	h.set(r, true, false)

	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), closeTimeout)
		defer cancel()

		_ = conn.Close(closeCtx)

		h.set(r, false, false)
	}()

	for _, channel := range listenChannels {
		if _, err := conn.Exec(ctx, "LISTEN "+channel); err != nil {
			return false, fmt.Errorf("listen %s: %w", channel, err)
		}
	}

	for _, s := range h.set(r, true, true) {
		s.send(s.resync)
	}

	for {
		msg, err := conn.WaitForNotification(ctx)
		if err != nil {
			return true, err
		}

		for _, s := range h.subscribers(r) {
			if !slices.Contains(s.channels, msg.Channel) {
				continue
			}

			if err := s.notify(ctx, msg.Channel, msg.Payload); err != nil {
				return true, err
			}
		}
	}
}

// listener is one kind of notice on the store's listening connection (listenHub): it decodes
// each payload of its channels into a notice N and reads it back. Notices, DefinitionNotices
// and AccessNotices are listeners.
type listener[N any] struct {
	hub      *listenHub
	log      *slog.Logger
	channels []string
	// name prefixes log messages ("item notices").
	name string
	// resync is the notice delivered each time listening starts.
	resync N
	// decode reads the payload of a notification on channel.
	decode func(channel, payload string) (N, error)
	// readBack replaces a decoded notice with what the database says, read through the row-level
	// security of the organization it names: any role that may connect can NOTIFY, so a payload
	// is a claim and never data. It reports false for a notice to drop (nothing to read back).
	readBack func(ctx context.Context, n N) (N, bool, error)
}

// Listen delivers notices until ctx ends, then returns nil. Every time it starts listening,
// the first time included, it delivers the resync notice before any other, so a caller knows
// from when on it sees every notification.
func (l *listener[N]) Listen(ctx context.Context, deliver func(N)) error {
	s := &subscription{channels: l.channels, resync: func() { deliver(l.resync) }}
	s.notify = func(ctx context.Context, channel, payload string) error {
		notice, err := l.decode(channel, payload)
		if err != nil {
			l.log.WarnContext(ctx, l.name+": unreadable notification", slog.Any("error", err))

			return nil
		}

		notice, ok, err := l.readBack(ctx, notice)

		switch {
		case err != nil && ctx.Err() != nil:
			return err
		case err != nil:
			// The notice is lost, so this listener re-reads everything instead.
			l.log.WarnContext(ctx, l.name+": notification not read back; resyncing", slog.String("channel", channel), slog.Any("error", err))
			s.send(s.resync)
		case !ok:
			l.log.DebugContext(ctx, l.name+": notification names nothing; dropped", slog.String("channel", channel))
		default:
			s.send(func() { deliver(notice) })
		}

		return nil
	}

	l.hub.subscribe(s, l.log)
	<-ctx.Done()
	l.hub.unsubscribe(s)

	return nil
}

func discardIfNil(log *slog.Logger) *slog.Logger {
	if log == nil {
		return slog.New(slog.DiscardHandler)
	}

	return log
}
