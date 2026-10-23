// Package engine is Goal's flow engine: it pulls work items through each factory's line of
// stations, runs agents on them and applies gates and human decisions.
//
// The engine depends only on internal/domain and internal/ports. It holds no state that
// another goald instance would need: every decision reads and writes the Store, claims are
// slot leases that never exceed a station's capacity, every transition is compare-and-set,
// and work held by a dead instance returns to the queue when its lease expires. Several
// instances can run Tick and SweepLeases at once.
//
// Phase 1 scope. The engine pulls into every agent station with free capacity, releases
// items waiting at queue stations straight to the next station, validates outputs against
// station contracts, applies gates and human approvals, routes by lane and sweeps expired
// leases. These arrive in later phases and are deliberately absent here: drum-buffer-rope
// release (the rope that limits release from queue stations), constraint detection,
// station buffers, andon pulls, the expedite WIP limit, budget enforcement in the engine
// (the worker enforces per-task limits), deterministic command checks, and Knowledge and
// ModelAuth ports. Singleton jobs that need them will run under a PostgreSQL advisory lock.
package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// ErrNotWaitingHuman means a human decision was made on an item that is not waiting for one.
var ErrNotWaitingHuman = errors.New("item is not waiting for a human")

// Deps are the ports the engine works through.
type Deps struct {
	Store     ports.Store
	Runner    ports.Runner
	Contracts ports.Contracts
	Secrets   ports.Secrets
	Clock     ports.Clock
	Logger    *slog.Logger // nil discards logs
}

// Config is the engine's behaviour; zero durations take the defaults below.
type Config struct {
	// Lines are the factories this engine runs, one line per factory.
	Lines []domain.Line
	// TickEvery is how often Run pulls work and sweeps leases (default 5s).
	TickEvery time.Duration
	// LeaseTTL is how long a claim survives without a heartbeat (default 2m).
	LeaseTTL time.Duration
	// HeartbeatEvery must be shorter than LeaseTTL (default LeaseTTL/3).
	HeartbeatEvery time.Duration
	// TaskTimeout stops an agent task that runs longer (default 2h).
	TaskTimeout time.Duration
	// SweepLimit caps leases requeued per sweep and items released per queue station per tick (default 100).
	SweepLimit int
	// GatewayURL is the MCP gateway agents use; when set, GatewayKeyRef names the secret
	// that signs per-task gateway tokens.
	GatewayURL    string
	GatewayKeyRef string
	// TaskEnv maps environment variables for every task to secret references, resolved
	// through Secrets when the task starts (for example model credentials).
	TaskEnv map[string]string
}

// keyReason is the event data key that explains a block, rejection or escalation.
const keyReason = "reason"

const (
	defaultTickEvery   = 5 * time.Second
	defaultLeaseTTL    = 2 * time.Minute
	defaultTaskTimeout = 2 * time.Hour
	defaultSweepLimit  = 100
	cleanupTimeout     = 10 * time.Second
)

// Engine runs factory lines. It is safe for concurrent use.
type Engine struct {
	store     ports.Store
	runner    ports.Runner
	contracts ports.Contracts
	secrets   ports.Secrets
	clock     ports.Clock
	log       *slog.Logger
	cfg       Config
	lines     map[domain.FactoryID]domain.Line

	// runs tracks this instance's in-flight agent runs so shutdown can wait for them.
	runs sync.WaitGroup
}

// New returns an engine for the given lines.
func New(d Deps, cfg Config) (*Engine, error) {
	if d.Store == nil || d.Runner == nil || d.Contracts == nil || d.Secrets == nil || d.Clock == nil {
		return nil, errors.New("engine: every port in Deps is required")
	}

	cfg = withDefaults(cfg)
	if cfg.HeartbeatEvery >= cfg.LeaseTTL {
		return nil, fmt.Errorf("engine: heartbeat every %s must be shorter than the lease TTL %s", cfg.HeartbeatEvery, cfg.LeaseTTL)
	}

	if cfg.GatewayURL != "" && cfg.GatewayKeyRef == "" {
		return nil, errors.New("engine: a gateway URL needs a gateway key reference")
	}

	lines, err := index(cfg.Lines)
	if err != nil {
		return nil, err
	}

	log := d.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &Engine{
		store: d.Store, runner: d.Runner, contracts: d.Contracts, secrets: d.Secrets, clock: d.Clock,
		log: log, cfg: cfg, lines: lines,
	}, nil
}

// index keys lines by factory and rejects lines the engine cannot run.
func index(ls []domain.Line) (map[domain.FactoryID]domain.Line, error) {
	lines := make(map[domain.FactoryID]domain.Line, len(ls))

	for _, l := range ls {
		if _, dup := lines[l.FactoryID]; dup {
			return nil, fmt.Errorf("engine: factory %s has two lines", l.FactoryID)
		}

		if len(l.Stations) == 0 || l.Stations[len(l.Stations)-1].Kind != domain.KindDone {
			return nil, fmt.Errorf("engine: factory %s: the line must end with a done station", l.FactoryID)
		}

		lines[l.FactoryID] = l
	}

	return lines, nil
}

func withDefaults(cfg Config) Config {
	if cfg.TickEvery <= 0 {
		cfg.TickEvery = defaultTickEvery
	}

	if cfg.LeaseTTL <= 0 {
		cfg.LeaseTTL = defaultLeaseTTL
	}

	if cfg.HeartbeatEvery <= 0 {
		cfg.HeartbeatEvery = cfg.LeaseTTL / 3
	}

	if cfg.TaskTimeout <= 0 {
		cfg.TaskTimeout = defaultTaskTimeout
	}

	if cfg.SweepLimit <= 0 {
		cfg.SweepLimit = defaultSweepLimit
	}

	return cfg
}

// Run sweeps expired leases and pulls work every TickEvery until ctx ends. On return, every
// run this instance started has finished or released its item.
func (e *Engine) Run(ctx context.Context) error {
	t := e.clock.NewTicker(e.cfg.TickEvery)
	defer t.Stop()
	defer e.runs.Wait()

	for {
		if _, err := e.SweepLeases(ctx); err != nil && ctx.Err() == nil {
			e.log.ErrorContext(ctx, "sweep leases", slog.Any("error", err))
		}

		if err := e.Tick(ctx); err != nil && ctx.Err() == nil {
			e.log.ErrorContext(ctx, "tick", slog.Any("error", err))
		}

		select {
		case <-ctx.Done():
			return nil
		case <-t.C():
		}
	}
}

// Wait blocks until every agent run started by this instance's ticks has finished.
func (e *Engine) Wait() { e.runs.Wait() }

// Submit creates an item at the first station of its factory's line.
func (e *Engine) Submit(ctx context.Context, n domain.NewItem) (domain.Item, error) {
	l, err := e.line(n.FactoryID)
	if err != nil {
		return domain.Item{}, err
	}

	if n.Title == "" || n.Type == "" {
		return domain.Item{}, errors.New("submit: an item needs a title and a type")
	}

	first, err := l.First()
	if err != nil {
		return domain.Item{}, err
	}

	n.Station, n.State, n.Class, n.CreatedAt = first.ID, first.Arrival(), l.ClassFor(n.Type), e.clock.Now()

	it, err := e.store.CreateItem(ctx, n)
	if err != nil {
		return domain.Item{}, fmt.Errorf("submit: %w", err)
	}

	return it, nil
}

// Tick does one round of pulling for every line: items waiting at queue stations move to the
// next station, and every agent station claims work into its free slots and starts runs.
// Runs continue in the background under ctx; Wait waits for them.
func (e *Engine) Tick(ctx context.Context) error {
	var errs []error

	for _, id := range slices.Sorted(maps.Keys(e.lines)) {
		l := e.lines[id]
		for _, st := range l.Stations {
			switch st.Kind {
			case domain.KindQueue:
				errs = append(errs, e.releaseQueue(ctx, l, st))
			case domain.KindAgent:
				errs = append(errs, e.pull(ctx, l, st))
			case domain.KindHuman, domain.KindDone:
			}
		}
	}

	return errors.Join(errs...)
}

// SweepLeases returns items whose lease expired (their holder died or stalled) to the queue
// at the same station and reports how many it requeued.
func (e *Engine) SweepLeases(ctx context.Context) (int, error) {
	leases, err := e.store.RequeueExpiredLeases(ctx, e.clock.Now(), e.cfg.SweepLimit)
	if err != nil {
		return 0, fmt.Errorf("sweep leases: %w", err)
	}

	for _, l := range leases {
		e.log.WarnContext(ctx, "lease expired; item requeued",
			slog.String("factory", string(l.FactoryID)), slog.String("item", string(l.ItemID)), slog.String("station", l.Station))
	}

	return len(leases), nil
}

// releaseQueue moves items waiting at a queue station to the next station.
func (e *Engine) releaseQueue(ctx context.Context, l domain.Line, st domain.Station) error {
	items, err := e.store.Waiting(ctx, l.FactoryID, st.ID, e.cfg.SweepLimit)
	if err != nil {
		return fmt.Errorf("station %s: list waiting: %w", st.ID, err)
	}

	next, err := l.Next(st.ID)
	if err != nil {
		return err
	}

	for _, it := range items {
		to, events, doneAt := e.arrive(it, next, domain.ActorEngine)

		_, err := e.store.Transition(ctx, ports.Transition{
			ItemID: it.ID, From: it.Position(), To: to, At: e.clock.Now(), DoneAt: doneAt, Events: events,
		})
		if err != nil && !errors.Is(err, ports.ErrConflict) {
			return fmt.Errorf("station %s: release item %s: %w", st.ID, it.ID, err)
		}
	}

	return nil
}

// pull fills the station's free slots with claims and starts a run for each.
func (e *Engine) pull(ctx context.Context, l domain.Line, st domain.Station) error {
	leases, err := e.store.Leases(ctx, l.FactoryID, st.ID)
	if err != nil {
		return fmt.Errorf("station %s: list leases: %w", st.ID, err)
	}

	taken := make(map[int]bool, len(leases))
	for _, ls := range leases {
		taken[ls.Slot] = true
	}

	for slot := range st.Capacity() {
		if taken[slot] {
			continue
		}

		now := e.clock.Now()

		it, lease, err := e.store.ClaimNext(ctx, ports.Claim{
			FactoryID: l.FactoryID, Station: st, Slot: slot, Now: now, ExpiresAt: now.Add(e.cfg.LeaseTTL),
		})

		switch {
		case errors.Is(err, ports.ErrNoWork):
			return nil
		case errors.Is(err, ports.ErrSlotTaken):
			continue
		case err != nil:
			return fmt.Errorf("station %s: claim slot %d: %w", st.ID, slot, err)
		}

		e.runs.Go(func() { e.work(ctx, l, st, it, lease) })
	}

	return nil
}

// arrive describes an item arriving at station to: its position there, the events that say
// so and, at the done station, when it finished.
func (e *Engine) arrive(it domain.Item, to domain.Station, actor string) (domain.Position, []domain.Event, time.Time) {
	now := e.clock.Now()
	pos := domain.Position{Station: to.ID, State: to.Arrival()}
	events := []domain.Event{event(it, domain.EventMoved, now, actor, map[string]any{"from": it.Station, "to": to.ID})}

	var doneAt time.Time

	switch pos.State {
	case domain.StateWaitingHuman:
		events = append(events, event(it, domain.EventWaitingHuman, now, actor, nil))
	case domain.StateDone:
		doneAt = now
		events = append(events, event(it, domain.EventDone, now, actor, nil))
	case domain.StateQueued, domain.StateReady, domain.StateInProgress, domain.StateBlocked, domain.StateCancelled:
	}

	for i := 1; i < len(events); i++ {
		events[i].Station = to.ID
	}

	return pos, events, doneAt
}

func (e *Engine) line(id domain.FactoryID) (domain.Line, error) {
	l, ok := e.lines[id]
	if !ok {
		return domain.Line{}, fmt.Errorf("factory %s is not loaded", id)
	}

	return l, nil
}

// event builds an item event at the item's current station.
func event(it domain.Item, kind domain.EventKind, at time.Time, actor string, data map[string]any) domain.Event {
	return domain.Event{
		FactoryID: it.FactoryID, ItemID: it.ID, At: at, Kind: kind, Station: it.Station, Actor: actor, Data: data,
	}
}

// cleanup returns a context for store writes that must happen even as ctx is cancelled,
// such as releasing a lease during shutdown.
func cleanup(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
}
