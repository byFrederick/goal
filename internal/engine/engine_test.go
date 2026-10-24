package engine_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/engine"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

const factoryID domain.FactoryID = "31efd04e-b231-4939-9f13-8986bf7e462a"

// contractResult is the contract the tests' agent stations declare.
const contractResult = "result"

// v1 is the definition version the tests' lines come from.
const v1 domain.DefinitionVersionID = "019a1c00-0000-7000-8000-0000000000a1"

// testOrg is the organization the tests' factories and items belong to (the first one the
// harness creates): every store call carries one.
const testOrg domain.OrgID = "00000000-0000-7000-8000-000000000001"

// inOrg is the test's context bound to testOrg.
func inOrg(t *testing.T) context.Context {
	t.Helper()

	return domain.WithOrg(t.Context(), testOrg)
}

var t0 = time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)

// Names the test lines use.
const (
	stDone       = "done"
	stInbox      = "inbox"
	stApprove    = "approve"
	agReviewer   = "reviewer"
	agBackend    = "backend-engineer"
	agFrontend   = "frontend-engineer"
	agWorker     = "worker"
	stWork       = "work"
	typeIncident = "incident"
	typeFeature  = "feature"
	anItem       = "an item"
)

const (
	ttl       = time.Minute
	heartbeat = 20 * time.Second
	tickEvery = 5 * time.Second
)

type harness struct {
	e          *engine.Engine
	store      *porttest.Store
	tenancy    *porttest.Tenancy
	runner     *porttest.Runner
	workspaces *porttest.Workspaces
	checker    *porttest.Checker
	clock      *porttest.Clock
	contracts  *porttest.Contracts
	codehost   *porttest.CodeHost
	line       domain.Line
	logs       *logBuffer // what every engine of the harness logs

	mu    sync.Mutex
	ended []domain.RunID // runs reported to RunEnded
	// ticking, while step's tick runs, is closed when it returns; every task waits for it
	// before it plays, so no task ends within the tick that started it (see step).
	ticking chan struct{}
}

func (h *harness) runEnded(_ context.Context, run domain.RunID) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.ended = append(h.ended, run)
}

func (h *harness) endedRuns() []domain.RunID {
	h.mu.Lock()
	defer h.mu.Unlock()

	return slices.Clone(h.ended)
}

// runs returns an item's runs in org, oldest first.
func (h *harness) runs(t *testing.T, org domain.OrgID, id domain.ItemID) []domain.Run {
	t.Helper()

	runs, err := h.store.Runs(domain.WithOrg(t.Context(), org), id, 100)
	if err != nil {
		t.Fatal(err)
	}

	return runs
}

func newHarness(t *testing.T, line domain.Line, run porttest.RunFunc) *harness {
	t.Helper()

	clock := porttest.NewClock(t0)
	store := porttest.NewStore()
	h := &harness{
		store:      store,
		tenancy:    porttest.NewTenancy(clock.Now).WithStore(store),
		workspaces: porttest.NewWorkspaces(),
		checker:    porttest.NewChecker(),
		clock:      clock,
		contracts:  porttest.NewContracts(),
		codehost:   porttest.NewCodeHost(),
		line:       line,
		logs:       &logBuffer{},
	}

	h.runner = porttest.NewRunner(func(ctx context.Context, spec ports.TaskSpec, events chan<- ports.RunEvent) domain.RunResult {
		h.mu.Lock()
		ticking := h.ticking
		h.mu.Unlock()

		if ticking != nil {
			select {
			case <-ticking:
			case <-ctx.Done():
			}
		}

		return run(ctx, spec, events)
	})

	if org := h.newOrg(t, "acme"); org != testOrg {
		t.Fatalf("the first organization is %s, want %s", org, testOrg)
	}

	h.e = h.engine(t, h.store)

	return h
}

// newOrg creates an organization and returns its id.
func (h *harness) newOrg(t *testing.T, slug string) domain.OrgID {
	t.Helper()

	o, err := h.tenancy.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: slug, Name: slug})
	if err != nil {
		t.Fatal(err)
	}

	return o.ID
}

// engine builds another goald instance on the same store, as a second process would.
func (h *harness) engine(t *testing.T, store ports.Store) *engine.Engine {
	t.Helper()

	return h.engineWith(t, store, 0, h.line)
}

// engineWith builds an instance running lines with at most maxRuns runs at once (0: no cap).
func (h *harness) engineWith(t *testing.T, store ports.Store, maxRuns int, lines ...domain.Line) *engine.Engine {
	t.Helper()

	e, err := engine.New(engine.Deps{
		Store: store, Orgs: h.tenancy, Runner: h.runner, Workspaces: h.workspaces, Checker: h.checker, Contracts: h.contracts, Clock: h.clock,
		Secrets: porttest.Secrets{"GOAL_GATEWAY_KEY": "k3y", "MODEL_ACCESS": "tok"}, CodeHost: h.codehost, RunEnded: h.runEnded,
		Logger: slog.New(slog.NewJSONHandler(h.logs, nil)),
	}, engine.Config{
		Lines:          lines,
		MaxRuns:        maxRuns,
		TickEvery:      tickEvery,
		LeaseTTL:       ttl,
		HeartbeatEvery: heartbeat,
		GatewayURL:     "http://gateway.example.com/mcp",
		GatewayKeyRef:  "env:GOAL_GATEWAY_KEY",
		TaskEnv:        map[string]string{"MODEL_ACCESS": "env:MODEL_ACCESS"},
	})
	if err != nil {
		t.Fatal(err)
	}

	return e
}

func (h *harness) submit(t *testing.T, n domain.NewItem) domain.Item {
	t.Helper()

	n.FactoryID = factoryID
	if n.Title == "" {
		n.Title = anItem
	}

	if n.Type == "" {
		n.Type = typeFeature
	}

	it, err := h.e.Submit(inOrg(t), n)
	if err != nil {
		t.Fatal(err)
	}

	return it
}

// step runs one tick and waits for every run it started. The tasks play only once the tick has
// returned, so a step moves an item through one agent station whatever the scheduler does: a
// run that ended within the tick would let the same tick claim its item again at the next
// agent station.
func (h *harness) step(t *testing.T) {
	t.Helper()

	ticking := make(chan struct{})

	h.mu.Lock()
	h.ticking = ticking
	h.mu.Unlock()

	err := h.e.Tick(inOrg(t))

	h.mu.Lock()
	h.ticking = nil
	h.mu.Unlock()
	close(ticking)

	if err != nil {
		t.Fatal(err)
	}

	h.wait(t, h.e)
}

// deadline bounds how long a test waits for the engine's goroutines, so a regression fails
// the test instead of hanging the package.
const deadline = 10 * time.Second

// wait waits for e's in-flight runs, failing the test if they never finish.
func (h *harness) wait(t *testing.T, e *engine.Engine) {
	t.Helper()

	done := make(chan struct{})

	go func() {
		e.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(deadline):
		t.Fatal("engine runs did not finish")
	}
}

// bounded is the test's context with a deadline, for waits on fakes.
func bounded(t *testing.T) context.Context {
	t.Helper()

	c, cancel := context.WithTimeout(inOrg(t), deadline)
	t.Cleanup(cancel)

	return c
}

// claimAsDeadInstance moves an item to station and claims it there the way another goald
// instance would, then never heartbeats: the instance died.
func (h *harness) claimAsDeadInstance(t *testing.T, it domain.Item, station string) {
	t.Helper()

	h.claimDead(t, h.line, it, station)
}

// claimDead is claimAsDeadInstance for an item of any line.
func (h *harness) claimDead(t *testing.T, l domain.Line, it domain.Item, station string) {
	t.Helper()

	ctx := domain.WithOrg(t.Context(), l.OrgID)

	if _, err := h.store.Transition(ctx, ports.Transition{
		ItemID: it.ID, From: it.Position(), To: domain.Position{Station: station, State: domain.StateQueued}, At: t0,
	}); err != nil {
		t.Fatal(err)
	}

	st, _ := l.Station(station)
	if _, _, err := h.store.ClaimNext(ctx, ports.Claim{
		FactoryID: l.FactoryID, Station: st, Slot: 0, Now: h.clock.Now(), ExpiresAt: h.clock.Now().Add(ttl),
	}); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) item(t *testing.T, id domain.ItemID) domain.Item {
	t.Helper()

	it, err := h.store.Item(inOrg(t), id)
	if err != nil {
		t.Fatal(err)
	}

	return it
}

// outcomes are how an item's leases ended, in order.
func (h *harness) outcomes(id domain.ItemID) []domain.LeaseOutcome {
	var out []domain.LeaseOutcome
	for _, l := range h.store.LeaseSpans(testOrg, id) {
		out = append(out, l.Outcome)
	}

	return out
}

// expectOutcomes checks how an item's leases ended.
func (h *harness) expectOutcomes(t *testing.T, id domain.ItemID, want ...domain.LeaseOutcome) {
	t.Helper()

	if got := h.outcomes(id); !slices.Equal(got, want) {
		t.Errorf("lease outcomes = %v, want %v (events %v)", got, want, h.kinds(id))
	}
}

func (h *harness) kinds(id domain.ItemID) []domain.EventKind {
	var out []domain.EventKind
	for _, e := range h.store.Events(id) {
		out = append(out, e.Kind)
	}

	return out
}

func (h *harness) eventsOf(id domain.ItemID, kind domain.EventKind) []domain.Event {
	var out []domain.Event

	for _, e := range h.store.Events(id) {
		if e.Kind == kind {
			out = append(out, e)
		}
	}

	return out
}

// lineOf builds a line inbox → stations... → done with the agents the tests use.
func lineOf(stations ...domain.Station) domain.Line {
	all := append([]domain.Station{{ID: stInbox, Kind: domain.KindQueue}}, stations...)
	all = append(all, domain.Station{ID: stDone, Kind: domain.KindDone})

	agents := []domain.AgentSpec{}
	for _, name := range []string{"architect", agWorker, agReviewer, agBackend, agFrontend} {
		agents = append(agents, domain.AgentSpec{Name: name, PromptPath: "./agents/" + name + ".md"})
	}

	return domain.Line{
		OrgID:         testOrg,
		FactoryID:     factoryID,
		VersionID:     v1,
		Number:        1,
		Runtime:       domain.Runtime{Image: "ghcr.io/acme/agent:1", Model: "claude-sonnet-5-5", MaxTurnsPerTask: 40, PerItemUSD: 3},
		Stations:      all,
		Agents:        agents,
		ExpediteTypes: []string{typeIncident},
	}
}

func agentStation(id, agent string, agents, wip int) domain.Station {
	return domain.Station{ID: id, Kind: domain.KindAgent, Agent: agent, Agents: agents, WIP: wip}
}

func ok(output string) domain.RunResult {
	return domain.RunResult{Status: domain.RunSucceeded, Output: json.RawMessage(output)}
}

func TestSubmit_EntersFirstStationWithClassOfService(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		workType string
		want     domain.ClassOfService
	}{
		{name: "expedite type", workType: typeIncident, want: domain.ClassExpedite},
		{name: "other type", workType: "feature", want: domain.ClassStandard},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)), porttest.Succeed(`{}`))
			it := h.submit(t, domain.NewItem{Type: tt.workType})

			if it.Station != stInbox || it.State != domain.StateQueued || it.Class != tt.want || it.Number != 1 {
				t.Errorf("item = %s/%s class %s number %d; want inbox/queued class %s number 1",
					it.Station, it.State, it.Class, it.Number, tt.want)
			}
		})
	}
}

func TestTick_ClaimsExpediteBeforePriority(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		items []domain.NewItem // submitted in this order, one second apart
		want  []string         // titles in the order the single slot works them
	}{
		{
			name: "expedite beats priority and age",
			items: []domain.NewItem{
				{Title: "old", Priority: 0},
				{Title: "urgent", Priority: 5},
				{Title: typeIncident, Type: typeIncident, Priority: 0},
			},
			want: []string{typeIncident, "urgent", "old"},
		},
		{
			name:  "priority beats age",
			items: []domain.NewItem{{Title: "low"}, {Title: "high", Priority: 2}},
			want:  []string{"high", "low"},
		},
		{
			name:  "age breaks ties",
			items: []domain.NewItem{{Title: "first"}, {Title: "second"}},
			want:  []string{"first", "second"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)), porttest.Succeed(`{}`))
			for _, n := range tt.items {
				h.submit(t, n)
				h.clock.Advance(time.Second)
			}

			for range tt.items {
				h.step(t)
			}

			var got []string
			for _, spec := range h.runner.Started() {
				got = append(got, spec.Brief)
			}

			if len(got) != len(tt.want) {
				t.Fatalf("worked %q, want %q", got, tt.want)
			}

			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("worked %q, want %q", got, tt.want)
				}
			}
		})
	}
}

func TestTick_NeverExceedsWIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		agents    int
		wip       int
		instances int
		want      int
	}{
		{name: "wip below agents", agents: 3, wip: 2, instances: 1, want: 2},
		{name: "agents below wip", agents: 1, wip: 4, instances: 1, want: 1},
		{name: "unset wip", agents: 3, instances: 1, want: 3},
		{name: "three instances share one store", agents: 3, wip: 3, instances: 3, want: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			release := make(chan struct{})
			h := newHarness(t, lineOf(agentStation(stWork, agWorker, tt.agents, tt.wip)), porttest.Block(release, `{}`))

			for range 6 {
				h.submit(t, domain.NewItem{})
			}

			engines := []*engine.Engine{h.e}
			for range tt.instances - 1 {
				engines = append(engines, h.engine(t, h.store))
			}

			for range 2 {
				for _, e := range engines {
					if err := e.Tick(inOrg(t)); err != nil {
						t.Fatal(err)
					}
				}
			}

			leases, err := h.store.Leases(inOrg(t), factoryID, stWork)
			if err != nil {
				t.Fatal(err)
			}

			if len(leases) != tt.want {
				t.Errorf("%d leases at work, want %d", len(leases), tt.want)
			}

			if err := h.runner.WaitStarted(bounded(t), tt.want); err != nil {
				t.Fatal(err)
			}

			close(release)

			for _, e := range engines {
				h.wait(t, e)
			}

			if got := len(h.runner.Started()); got != tt.want {
				t.Errorf("%d tasks started, want %d", got, tt.want)
			}
		})
	}
}

func TestSweep_ExpiredLeaseRequeues(t *testing.T) {
	t.Parallel()

	h := newHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)), porttest.Succeed(`{}`))
	crashed := h.submit(t, domain.NewItem{})

	h.claimAsDeadInstance(t, crashed, stWork)

	h.step(t)

	if len(h.runner.Started()) != 0 {
		t.Fatal("a tick started work while the dead instance's lease filled the only slot")
	}

	if n, err := h.e.SweepLeases(inOrg(t)); err != nil || n != 0 {
		t.Fatalf("sweep before expiry requeued %d (%v), want 0", n, err)
	}

	h.clock.Advance(ttl)

	n, err := h.e.SweepLeases(inOrg(t))
	if err != nil || n != 1 {
		t.Fatalf("sweep after expiry requeued %d (%v), want 1", n, err)
	}

	if got := h.item(t, crashed.ID); got.Station != stWork || got.State != domain.StateQueued {
		t.Fatalf("item is %s/%s, want work/queued", got.Station, got.State)
	}

	if len(h.eventsOf(crashed.ID, domain.EventLeaseExpired)) != 1 {
		t.Errorf("events %v, want one lease_expired", h.kinds(crashed.ID))
	}

	h.expectOutcomes(t, crashed.ID, domain.LeaseExpired)

	if n, err := h.e.SweepLeases(inOrg(t)); err != nil || n != 0 {
		t.Errorf("second sweep requeued %d (%v), want 0", n, err)
	}

	h.step(t)

	if got := h.item(t, crashed.ID); got.State != domain.StateDone {
		t.Errorf("item is %s/%s after the next tick, want done", got.Station, got.State)
	}
}

// A person pauses an item while the instance running it is dead: nobody hears the pause, so the
// sweep finishes the dead instance's run as cancelled once a lease TTL has passed since the pause,
// the time its instance would have had to notice at its next heartbeat.
func TestSweep_RunLostWithItsInstanceIsCancelledAfterTheGrace(t *testing.T) {
	t.Parallel()

	h := newHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)), porttest.Succeed(`{}`))
	crashed := h.submit(t, domain.NewItem{})

	h.claimAsDeadInstance(t, crashed, stWork)

	run, err := h.store.StartRun(inOrg(t), ports.NewRun{
		FactoryID: h.line.FactoryID, ItemID: crashed.ID, Station: stWork, Agent: agWorker, StartedAt: h.clock.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := h.e.Pause(inOrg(t), crashed.ID, human, "the vendor is away"); err != nil {
		t.Fatal(err)
	}

	runOf := func() domain.Run {
		t.Helper()

		r, err := h.store.Run(inOrg(t), run)
		if err != nil {
			t.Fatal(err)
		}

		return r
	}

	h.clock.Advance(ttl)

	if _, err := h.e.SweepLeases(inOrg(t)); err != nil || runOf().Result.Status != domain.RunRunning {
		t.Fatalf("sweep within the grace: run %+v (%v); want still running", runOf(), err)
	}

	h.clock.Advance(time.Second)

	if _, err := h.e.SweepLeases(inOrg(t)); err != nil {
		t.Fatal(err)
	}

	if r := runOf(); r.Result.Status != domain.RunCancelled || r.Result.Error != domain.RunLostError || !r.EndedAt.Equal(h.clock.Now()) {
		t.Errorf("run after the grace = %+v; want cancelled as lost, ended now", r)
	}

	if got := h.item(t, crashed.ID); got.Position() != (domain.Position{Station: stWork, State: domain.StateBlocked}) {
		t.Errorf("item at %+v; want still paused at work", got.Position())
	}
}

func TestRun_LoopTicksOnTheClock(t *testing.T) {
	t.Parallel()

	h := newHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)), porttest.Succeed(`{}`))

	ctx, cancel := context.WithCancel(inOrg(t))
	done := make(chan error)

	go func() { done <- h.e.Run(ctx) }()

	if err := h.clock.WaitTickers(bounded(t), 1); err != nil {
		t.Fatal(err)
	}

	it := h.submit(t, domain.NewItem{})
	h.clock.Advance(tickEvery)

	if err := h.store.Await(bounded(t), func() bool { return h.item(t, it.ID).State == domain.StateDone }); err != nil {
		t.Fatal(err)
	}

	cancel()

	if err := <-done; err != nil {
		t.Errorf("Run returned %v after cancel, want nil", err)
	}
}

func TestSetLines_LoadsAndUnloadsFactories(t *testing.T) {
	t.Parallel()

	h := newHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)), porttest.Succeed(`{}`))

	other := lineOf(agentStation(stWork, agWorker, 1, 1))
	other.FactoryID = "5c0d7a52-8b1e-4f43-9d3a-2e6b7c8f9a01"
	n := domain.NewItem{FactoryID: other.FactoryID, Title: anItem, Type: typeFeature}

	if _, err := h.e.Submit(inOrg(t), n); err == nil {
		t.Fatal("submit to a factory that is not loaded succeeded")
	}

	if err := h.e.SetLines([]domain.Line{h.line, other}); err != nil {
		t.Fatal(err)
	}

	it, err := h.e.Submit(inOrg(t), n)
	if err != nil {
		t.Fatalf("submit after loading the factory: %v", err)
	}

	h.step(t)

	if got := h.item(t, it.ID); got.State != domain.StateDone {
		t.Errorf("item of the new factory is %s, want done", got.State)
	}

	if err := h.e.SetLines([]domain.Line{other}); err != nil {
		t.Fatal(err)
	}

	if _, err := h.e.Submit(inOrg(t), domain.NewItem{FactoryID: factoryID, Title: "x", Type: typeFeature}); err == nil {
		t.Error("submit to an unloaded factory succeeded")
	}
}

// A paused line takes no work: nothing is released from its queue or claimed, and it picks up
// where it was once resumed.
func TestTick_PausedLineTakesNoWork(t *testing.T) {
	t.Parallel()

	line := lineOf(agentStation(stWork, agWorker, 1, 1))
	line.Paused = true
	h := newHarness(t, line, porttest.Succeed(`{}`))
	it := h.submit(t, domain.NewItem{})

	h.step(t)
	h.step(t)

	if got := h.item(t, it.ID); got.Station != stInbox || len(h.runner.Started()) != 0 {
		t.Fatalf("paused line: item at %s, %d tasks started; want inbox and none", got.Station, len(h.runner.Started()))
	}

	line.Paused = false
	if err := h.e.SetLines([]domain.Line{line}); err != nil {
		t.Fatal(err)
	}

	h.step(t)

	if got := h.item(t, it.ID); got.State != domain.StateDone {
		t.Errorf("item after resuming is %s/%s, want done", got.Station, got.State)
	}
}
