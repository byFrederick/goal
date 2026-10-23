package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

var (
	errTooManySidecars = errors.New("this goald runs as many MCP sidecars as it may; try again when a run ends")
	errSidecarsClosed  = errors.New("the gateway is shutting down")
)

// sidecarKey names one sidecar: a run's stdio connection.
type sidecarKey struct {
	run  domain.RunID
	conn string
}

// sidecarPool holds the MCP sessions of the sidecars this instance runs. It is the gateway's
// only state: a sidecar starts on the first request that needs it, serves the rest of its
// run's requests, and stops when the run ends (EndRun), when no request has used it for the
// idle time, or when its server exits.
type sidecarPool struct {
	port         ports.Sidecars
	idle         time.Duration
	max          int
	startTimeout time.Duration
	maxLine      int
	log          *slog.Logger

	mu      sync.Mutex
	running map[sidecarKey]*sidecarEntry
	closed  bool
}

// sidecarEntry is one sidecar, starting or running. sc, cs and err are set before ready
// closes; users and idle are guarded by the pool's mutex.
type sidecarEntry struct {
	key     sidecarKey
	ready   chan struct{}
	sc      ports.Sidecar
	cs      *mcp.ClientSession
	err     error
	users   int
	idle    *time.Timer
	stopped sync.Once
}

func newSidecarPool(port ports.Sidecars, cfg Config, log *slog.Logger) *sidecarPool {
	return &sidecarPool{
		port: port, idle: cfg.SidecarIdle, max: cfg.MaxSidecars, startTimeout: cfg.SidecarStartTimeout,
		maxLine: int(cfg.MaxResponseBytes), log: log, running: map[sidecarKey]*sidecarEntry{},
	}
}

// session returns the MCP session of spec's sidecar, starting the sidecar when it is not
// running, and a release func the caller calls when its request is done. ctx bounds the wait
// only: a start outlives the request that began it, so a slow first pull still serves the
// requests after it.
func (p *sidecarPool) session(ctx context.Context, spec ports.SidecarSpec) (*mcp.ClientSession, func(), error) {
	e, err := p.acquire(spec)
	if err != nil {
		return nil, nil, err
	}

	select {
	case <-e.ready:
	case <-ctx.Done():
		p.release(e)

		return nil, nil, fmt.Errorf("start sidecar: %w", ctx.Err())
	}

	if e.err != nil {
		p.release(e)

		return nil, nil, e.err
	}

	return e.cs, func() { p.release(e) }, nil
}

func (p *sidecarPool) acquire(spec ports.SidecarSpec) (*sidecarEntry, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil, errSidecarsClosed
	}

	key := sidecarKey{run: spec.RunID, conn: spec.Connection}
	if e, ok := p.running[key]; ok {
		e.users++

		if e.idle != nil {
			e.idle.Stop()
			e.idle = nil
		}

		return e, nil
	}

	if len(p.running) >= p.max {
		return nil, errTooManySidecars
	}

	e := &sidecarEntry{key: key, ready: make(chan struct{}), users: 1}
	p.running[key] = e

	go p.start(e, spec)

	return e, nil
}

// start starts e's sidecar and opens its MCP session. A sidecar dropped while it started (its
// run ended) is stopped at once; one whose server exits later is dropped then.
func (p *sidecarPool) start(e *sidecarEntry, spec ports.SidecarSpec) {
	ctx, cancel := context.WithTimeout(context.Background(), p.startTimeout)
	defer cancel()

	e.sc, e.cs, e.err = p.open(ctx, spec)
	close(e.ready)

	if e.err != nil {
		p.drop(e)

		return
	}

	if !p.holds(e) {
		p.stop(e)

		return
	}

	go func() {
		_ = e.cs.Wait()

		if p.drop(e) {
			p.log.Warn("MCP sidecar session ended", "run", e.key.run, "connection", e.key.conn)
			p.stop(e)
		}
	}()
}

func (p *sidecarPool) open(ctx context.Context, spec ports.SidecarSpec) (ports.Sidecar, *mcp.ClientSession, error) {
	sc, err := p.port.Start(ctx, spec)
	if err != nil {
		return nil, nil, fmt.Errorf("start sidecar: %w", err)
	}

	cs, err := mcp.NewClient(implementation, nil).Connect(ctx, &mcp.IOTransport{Reader: sc, Writer: sc, MaxLineLength: p.maxLine}, nil)
	if err != nil {
		_ = sc.Close()

		return nil, nil, fmt.Errorf("initialize sidecar: %w", err)
	}

	return sc, cs, nil
}

// release ends one request's use of e; the last one out arms its idle timer.
func (p *sidecarPool) release(e *sidecarEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()

	e.users--
	if e.users > 0 || p.running[e.key] != e {
		return
	}

	e.idle = time.AfterFunc(p.idle, func() {
		p.mu.Lock()
		idle := e.users == 0 && p.running[e.key] == e

		if idle {
			delete(p.running, e.key)
		}
		p.mu.Unlock()

		if idle {
			p.log.Info("MCP sidecar idle; stopping it", "run", e.key.run, "connection", e.key.conn)
			p.stopStarted(e)
		}
	})
}

// holds reports whether e is still the pool's sidecar for its key.
func (p *sidecarPool) holds(e *sidecarEntry) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.running[e.key] == e
}

// drop removes e from the pool and reports whether it was there.
func (p *sidecarPool) drop(e *sidecarEntry) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running[e.key] != e {
		return false
	}

	delete(p.running, e.key)

	if e.idle != nil {
		e.idle.Stop()
	}

	return true
}

// stop closes a started sidecar's session and removes its container, once.
func (p *sidecarPool) stop(e *sidecarEntry) {
	e.stopped.Do(func() {
		if e.cs != nil {
			_ = e.cs.Close()
		}

		if e.sc != nil {
			if err := e.sc.Close(); err != nil {
				p.log.Warn("stop MCP sidecar", "run", e.key.run, "connection", e.key.conn, "error", err)
			}
		}
	})
}

// stopStarted stops a sidecar the pool no longer holds. One still starting is left to start,
// which stops it as soon as it sees the pool no longer holds it.
func (p *sidecarPool) stopStarted(e *sidecarEntry) {
	select {
	case <-e.ready:
		p.stop(e)
	default:
	}
}

// end stops the sidecars of run, or of every run when all is set (shutdown, after which nothing
// starts), and waits until they are gone.
func (p *sidecarPool) end(run domain.RunID, all bool) {
	p.mu.Lock()

	if all {
		p.closed = true
	}

	var ended []*sidecarEntry

	for key, e := range p.running {
		if all || key.run == run {
			delete(p.running, key)

			if e.idle != nil {
				e.idle.Stop()
			}

			ended = append(ended, e)
		}
	}
	p.mu.Unlock()

	var wg sync.WaitGroup

	for _, e := range ended {
		wg.Go(func() { p.stopStarted(e) })
	}

	wg.Wait()
}
