// Package engine is Goal's flow engine: it pulls work items through each factory's line of
// stations, runs agents on them and applies gates and human decisions.
//
// The engine depends only on internal/domain and internal/ports. It holds no state that
// another goald instance would need: every decision reads and writes the Store, claims are
// slot leases that never exceed a station's capacity, every transition is compare-and-set,
// and work held by a dead instance returns to the queue when its lease expires. Several
// instances can run Tick and SweepLeases at once, and share one host: ReapOrphans (reap.go)
// removes only what runs the store says are over left there.
//
// Organizations. Every line belongs to one organization, and every store call the engine makes
// for a line is bound to it (domain.WithOrg), so row-level security keeps each organization's
// work apart even through a bug. Tick claims fairly across organizations (claim.go), sweeps run
// once per active organization, and Submit and the human decisions act only on factories of the
// organization in their context.
//
// Weekly budgets. Each tick reads the spend of every line with a weekly budget once
// (budget.go). A line whose budget is used up claims no agent work; under BudgetStop the runs
// this instance has in progress there are cancelled and their items requeued. Queue releases
// and human decisions carry on. The per-run cancel and the transition log are local to the
// instance; every instance enforces the budget on its own runs. The spend is the cost runs
// record when they finish (Store.SpendSince), and nothing is reserved at claim time, so a line
// can overshoot its cap by what its claimed items spend before the next tick sees it: at most
// the slots of its agent stations (across instances) × 2 (a run and its one retry) × the per-run
// cap (Runtime.PerItemUSD, or an agent's own). A tick at $59 of $60 that fills four free slots of
// $4 runs may end near $75, or $91 if every one retries.
//
// The andon cord (andon.go). A person pulls it to stop a factory's line and clears it to restart
// the line. While a pull is open the line releases nothing from its queue stations and claims
// nothing; its runs in progress finish and move on, submissions still enter the first station
// and every human decision still acts. Nothing pulls or clears the cord automatically in 2.2.
//
// Three things stop work on a line, and they combine without precedence, none lifting another:
// a pause (Line.Paused) and an open andon pull each stop releases and claims; a used-up weekly
// budget stops claims only (queue stations still release), and under BudgetStop it cancels the
// line's runs in progress whether or not the line is also paused or stopped. A line releases when
// it is neither paused nor stopped, and claims when it is also within its budget: resuming a
// paused line leaves an open pull in force, clearing the pull of a paused line leaves it paused,
// and clearing the pull of a line over budget releases work but claims none.
//
// Phase 1 scope. The engine pulls into every agent station with free capacity, releases
// items waiting at queue stations straight to the next station, prepares a fresh workspace
// (a clone of the factory's repository on the item's branch) for every run, validates outputs
// against station contracts, runs the station's command checks against the workspace, applies
// gates and human approvals, routes by lane and sweeps expired leases. Humans also send
// blocked items back to their queue (Retry) and, at a human station whose action is merge,
// merge the item's pull request through the CodeHost port before approving it (Merge).
// These arrive in later phases and are deliberately absent here: drum-buffer-rope
// release (the rope that limits release from queue stations), constraint detection,
// station buffers, the automatic andon pull on a gate's rework rate, the expedite WIP limit,
// and Knowledge and ModelAuth ports. Singleton jobs that need them will run under a PostgreSQL advisory lock.
package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// ErrNotWaitingHuman means a human decision was made on an item that is not waiting for one.
var ErrNotWaitingHuman = errors.New("item is not waiting for a human")

// Orgs lists the organizations whose leases the engine sweeps; ports.Tenancy satisfies it.
type Orgs interface {
	ActiveOrgs(ctx context.Context) ([]domain.OrgID, error)
}

// Deps are the ports the engine works through.
type Deps struct {
	Store      ports.Store
	Orgs       Orgs
	Runner     ports.Runner
	Workspaces ports.Workspaces
	Checker    ports.Checker
	Contracts  ports.Contracts
	Secrets    ports.Secrets
	CodeHost   ports.CodeHost
	Clock      ports.Clock
	Logger     *slog.Logger // nil discards logs
	// RunEnded, when set, is told each run whose agent task has ended (goald stops the run's
	// MCP sidecars). It is called once per run, with the run's result already recorded.
	RunEnded func(ctx context.Context, run domain.RunID)
	// Reapers hold what runs leave on this instance's host (containers, run folders,
	// workspaces); ReapOrphans removes what runs that are over left there. None: nothing to reap.
	Reapers []ports.Reaper
}

func (d Deps) complete() bool {
	return d.Store != nil && d.Orgs != nil && d.Runner != nil && d.Workspaces != nil && d.Checker != nil && d.Contracts != nil &&
		d.Secrets != nil && d.CodeHost != nil && d.Clock != nil
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
	// ReapEvery is how often Run reaps what runs that are over left on the host, the first time
	// when it starts (default LeaseTTL/2).
	ReapEvery time.Duration
	// TaskTimeout stops an agent task that runs longer (default 2h).
	TaskTimeout time.Duration
	// CheckTimeout stops one command check that runs longer (default 30m).
	CheckTimeout time.Duration
	// SweepLimit caps leases requeued per sweep and items released per queue station per tick (default 100).
	SweepLimit int
	// GatewayURL is the MCP gateway agents use; when set, GatewayKeyRef names the secret
	// that signs per-task gateway tokens.
	GatewayURL    string
	GatewayKeyRef string
	// TaskEnv maps environment variables for every task to secret references, resolved
	// through Secrets when the task starts (for example model credentials).
	TaskEnv map[string]string
	// MaxRuns caps the agent runs this instance has in flight at once; 0 means no cap. With a
	// cap, a busy organization cannot fill the instance before a quiet one's first item starts.
	MaxRuns int
}

// keyReason is the event data key that explains a block, rejection or escalation.
const keyReason = "reason"

// keyCategory is the event data key of a gate rejection's category (domain.Verdict.Category).
const keyCategory = "category"

// keyFrom and keyTo are the data keys of a move: the station the item left and the one it reached.
const (
	keyFrom = "from"
	keyTo   = "to"
)

const (
	defaultTickEvery    = 5 * time.Second
	defaultLeaseTTL     = 2 * time.Minute
	defaultTaskTimeout  = 2 * time.Hour
	defaultCheckTimeout = 30 * time.Minute
	defaultSweepLimit   = 100
	cleanupTimeout      = 10 * time.Second
)

// Engine runs factory lines. It is safe for concurrent use.
type Engine struct {
	store      ports.Store
	orgs       Orgs
	runner     ports.Runner
	workspaces ports.Workspaces
	checker    ports.Checker
	contracts  ports.Contracts
	secrets    ports.Secrets
	codehost   ports.CodeHost
	clock      ports.Clock
	log        *slog.Logger
	runEnded   func(context.Context, domain.RunID)
	reapers    []ports.Reaper
	cfg        Config

	mu    sync.RWMutex
	lines map[domain.FactoryID]domain.Line

	// runs tracks this instance's in-flight agent runs so shutdown can wait for them, and
	// running counts them for MaxRuns.
	runs    sync.WaitGroup
	running atomic.Int64
	// ticks rotates the order in which organizations and factories claim, tick after tick.
	// Like running, it is a local scheduling hint, not state.
	ticks atomic.Uint64

	// active holds this instance's runs by item, so the engine can cancel them (BudgetStop).
	activeMu sync.Mutex
	active   map[domain.ItemID]*activeRun
	// exhausted holds the factories whose weekly budget was used up at the last tick, so each
	// transition is logged once. It is a local hint, not state: an instance that starts reads
	// the spend afresh and logs what it finds.
	budgetMu  sync.Mutex
	exhausted map[domain.FactoryID]bool
}

// New returns an engine for the given lines.
func New(d Deps, cfg Config) (*Engine, error) {
	if !d.complete() {
		return nil, errors.New("engine: every port in Deps is required")
	}

	cfg = withDefaults(cfg)
	if cfg.HeartbeatEvery >= cfg.LeaseTTL {
		return nil, fmt.Errorf("engine: heartbeat every %s must be shorter than the lease TTL %s", cfg.HeartbeatEvery, cfg.LeaseTTL)
	}

	if cfg.GatewayURL != "" && cfg.GatewayKeyRef == "" {
		return nil, errors.New("engine: a gateway URL needs a gateway key reference")
	}

	if cfg.MaxRuns < 0 {
		return nil, fmt.Errorf("engine: max runs %d is negative", cfg.MaxRuns)
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
		store: d.Store, orgs: d.Orgs, runner: d.Runner, workspaces: d.Workspaces, checker: d.Checker, contracts: d.Contracts,
		secrets: d.Secrets, codehost: d.CodeHost, clock: d.Clock, log: log, runEnded: d.RunEnded, reapers: d.Reapers, cfg: cfg, lines: lines,
		active: map[domain.ItemID]*activeRun{}, exhausted: map[domain.FactoryID]bool{},
	}, nil
}

// SetLines replaces the lines the engine runs, for example after a definition version was
// published. Runs already started keep the line (and version) they started with; the next move
// of their item follows the new line, matched by station id. A factory that is no longer loaded
// (its organization was deleted) stops at once: the runs this instance has there are cancelled
// and their items released to their queue, so no lease outlives it; its other items stay where
// they are.
func (e *Engine) SetLines(ls []domain.Line) error {
	lines, err := index(ls)
	if err != nil {
		return err
	}

	e.mu.Lock()
	e.lines = lines
	e.mu.Unlock()

	e.activeMu.Lock()
	defer e.activeMu.Unlock()

	for _, r := range e.active {
		if _, loaded := lines[r.factory]; !loaded {
			r.cancel(errUnloaded)
		}
	}

	return nil
}

// index keys lines by factory and rejects lines the engine cannot run.
func index(ls []domain.Line) (map[domain.FactoryID]domain.Line, error) {
	lines := make(map[domain.FactoryID]domain.Line, len(ls))

	for _, l := range ls {
		if _, dup := lines[l.FactoryID]; dup {
			return nil, fmt.Errorf("engine: factory %s has two lines", l.FactoryID)
		}

		if l.OrgID == "" {
			return nil, fmt.Errorf("engine: factory %s belongs to no organization", l.FactoryID)
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

	if cfg.ReapEvery <= 0 {
		cfg.ReapEvery = cfg.LeaseTTL / 2
	}

	if cfg.TaskTimeout <= 0 {
		cfg.TaskTimeout = defaultTaskTimeout
	}

	if cfg.CheckTimeout <= 0 {
		cfg.CheckTimeout = defaultCheckTimeout
	}

	if cfg.SweepLimit <= 0 {
		cfg.SweepLimit = defaultSweepLimit
	}

	return cfg
}

// Run sweeps expired leases and pulls work every TickEvery until ctx ends, and reaps what runs
// that are over left on the host when it starts and every ReapEvery, right after a sweep, so
// the leftovers of the leases it swept go at once. On return, every run this instance started
// has finished or released its item.
func (e *Engine) Run(ctx context.Context) error {
	t := e.clock.NewTicker(e.cfg.TickEvery)
	defer t.Stop()
	defer e.runs.Wait()

	var reapedAt time.Time

	for {
		if _, err := e.SweepLeases(ctx); err != nil && ctx.Err() == nil {
			e.log.ErrorContext(ctx, "sweep leases", slog.Any("error", err))
		}

		if now := e.clock.Now(); reapedAt.IsZero() || now.Sub(reapedAt) >= e.cfg.ReapEvery {
			reapedAt = now

			n, err := e.ReapOrphans(ctx)
			if n > 0 {
				e.log.InfoContext(ctx, "reaped orphaned runs", slog.Int("runs", n))
			}

			if err != nil && ctx.Err() == nil {
				e.log.ErrorContext(ctx, "reap orphaned runs", slog.Any("error", err))
			}
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

// Submit creates an item at the first station of its factory's line. The factory must belong
// to the organization in ctx; any other is ports.ErrNotFound.
func (e *Engine) Submit(ctx context.Context, n domain.NewItem) (domain.Item, error) {
	l, err := e.lineIn(ctx, n.FactoryID)
	if err != nil {
		return domain.Item{}, err
	}

	// The type is one of the line's work types, or "" when the factory declares none.
	if n.Title == "" {
		return domain.Item{}, errors.New("submit: an item needs a title")
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

// Tick does one round of pulling: items waiting at queue stations move to the next station,
// line by line, the weekly budgets are enforced (budgets), then agent stations of the lines
// within budget claim work into their free slots and start runs, fairly across organizations
// (claim). A paused line, and a line stopped by an open andon pull (read once per tick; an
// unreadable one counts as open), neither releases nor claims, whatever its budget, while its
// runs in progress finish; its budget is still enforced, so the stop policy still cancels its
// runs. Each line's store calls are bound to its organization, whatever ctx carries. Runs
// continue in the background under ctx; Wait waits for them.
func (e *Engine) Tick(ctx context.Context) error {
	var errs []error

	e.mu.RLock()
	lines := maps.Clone(e.lines)
	e.mu.RUnlock()

	running := maps.Clone(lines)
	maps.DeleteFunc(running, func(_ domain.FactoryID, l domain.Line) bool { return l.Paused })

	stopped, err := e.stopped(ctx, running)
	errs = append(errs, err)

	maps.DeleteFunc(running, func(id domain.FactoryID, _ domain.Line) bool { return stopped[id] })

	for _, id := range slices.Sorted(maps.Keys(running)) {
		l := running[id]
		lctx := domain.WithOrg(ctx, l.OrgID)

		for _, st := range l.Stations {
			if st.Kind == domain.KindQueue {
				errs = append(errs, e.releaseQueue(lctx, l, st))
			}
		}
	}

	// Every line's budget is enforced, a paused or stopped one's too: the stop policy cancels runs.
	claimable, err := e.budgets(ctx, lines)
	maps.DeleteFunc(claimable, func(id domain.FactoryID, _ domain.Line) bool { _, ok := running[id]; return !ok })
	errs = append(errs, err, e.claim(ctx, claimable))

	return errors.Join(errs...)
}

// SweepLeases returns items whose lease expired (their holder died or stalled) to the queue
// at the same station, in every active organization, including leases on factories that are no
// longer loaded, and reports how many it requeued. It also finishes, as cancelled, the runs that
// lost their instance after a person took their lease (Move, Pause, Reject, send back): a run
// whose lease ended more than a LeaseTTL ago and that is still running has no instance left to
// finish it, since the live one notices at its next heartbeat, within the TTL.
func (e *Engine) SweepLeases(ctx context.Context) (int, error) {
	orgs, err := e.orgs.ActiveOrgs(ctx)
	if err != nil {
		return 0, fmt.Errorf("sweep leases: list organizations: %w", err)
	}

	var (
		swept int
		errs  []error
	)

	for _, org := range orgs {
		leases, err := e.store.RequeueExpiredLeases(domain.WithOrg(ctx, org), e.clock.Now(), e.cfg.SweepLimit)
		if err != nil {
			errs = append(errs, fmt.Errorf("sweep leases of organization %s: %w", org, err))
			continue
		}

		for _, l := range leases {
			e.log.WarnContext(ctx, "lease expired; item requeued", slog.String("org_id", string(org)),
				slog.String("factory", string(l.FactoryID)), slog.String("item", string(l.ItemID)), slog.String("station", l.Station))
		}

		swept += len(leases)

		now := e.clock.Now()

		runs, err := e.store.FinishLostRuns(domain.WithOrg(ctx, org), now.Add(-e.cfg.LeaseTTL), now, e.cfg.SweepLimit)
		if err != nil {
			errs = append(errs, fmt.Errorf("sweep lost runs of organization %s: %w", org, err))
		}

		for _, r := range runs {
			e.log.WarnContext(ctx, "run lost with its instance; cancelled", slog.String("org_id", string(org)),
				slog.String("factory", string(r.FactoryID)), slog.String("item", string(r.ItemID)), slog.String("run", string(r.ID)))
		}
	}

	return swept, errors.Join(errs...)
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

// arrive describes an item arriving at station to: its position there, the events that say
// so and, at the done station, when it finished.
func (e *Engine) arrive(it domain.Item, to domain.Station, actor string) (domain.Position, []domain.Event, time.Time) {
	now := e.clock.Now()
	pos := domain.Position{Station: to.ID, State: to.Arrival()}
	events := []domain.Event{event(it, domain.EventMoved, now, actor, map[string]any{keyFrom: it.Station, keyTo: to.ID})}

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

// lineIn returns the line of a factory of the organization in ctx. A factory of another
// organization is ports.ErrNotFound, so an API bug cannot act on another organization's work.
func (e *Engine) lineIn(ctx context.Context, id domain.FactoryID) (domain.Line, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.Line{}, fmt.Errorf("factory %s: %w", id, domain.ErrNoOrg)
	}

	e.mu.RLock()
	l, ok := e.lines[id]
	e.mu.RUnlock()

	switch {
	case !ok:
		return domain.Line{}, fmt.Errorf("factory %s is not loaded", id)
	case l.OrgID != org:
		return domain.Line{}, fmt.Errorf("factory %s: %w", id, ports.ErrNotFound)
	}

	return l, nil
}

// lineNow is the line an item at station of l moves on now: the factory's current line when it
// still has that station, and l otherwise (the factory was unloaded meanwhile).
func (e *Engine) lineNow(l domain.Line, station string) domain.Line {
	e.mu.RLock()
	cur, ok := e.lines[l.FactoryID]
	e.mu.RUnlock()

	if _, has := cur.Station(station); ok && has && cur.OrgID == l.OrgID {
		return cur
	}

	return l
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
