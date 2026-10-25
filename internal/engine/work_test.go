package engine_test

import (
	"context"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

func TestRun_SuccessMovesItemToTheNextStation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		next  []domain.Station
		want  domain.Position
		event domain.EventKind
	}{
		{
			name:  "agent station queues",
			next:  []domain.Station{agentStation("second", agWorker, 1, 1)},
			want:  domain.Position{Station: "second", State: domain.StateQueued},
			event: domain.EventMoved,
		},
		{
			name:  "human station waits for a human",
			next:  []domain.Station{{ID: stApprove, Kind: domain.KindHuman}},
			want:  domain.Position{Station: stApprove, State: domain.StateWaitingHuman},
			event: domain.EventWaitingHuman,
		},
		{
			name:  "done station finishes",
			want:  domain.Position{Station: stDone, State: domain.StateDone},
			event: domain.EventDone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			work := agentStation(stWork, agWorker, 1, 1)
			work.Output = contractResult
			h := newHarness(t, lineOf(append([]domain.Station{work}, tt.next...)...), porttest.Succeed(`{"answer":42}`))
			h.contracts.Require(v1, contractResult, "answer")
			it := h.submit(t, domain.NewItem{})

			h.step(t)

			got := h.item(t, it.ID)
			if got.Position() != tt.want {
				t.Fatalf("item at %+v, want %+v (events %v)", got.Position(), tt.want, h.kinds(it.ID))
			}

			h.expectOutcomes(t, it.ID, domain.LeaseAdvanced)

			if string(got.Outputs[stWork]) != `{"answer":42}` {
				t.Errorf("outputs = %s, want the work station's output stored", got.Outputs)
			}

			if _, held := h.store.Lease(it.ID); held {
				t.Error("lease still held after the run finished")
			}

			if kinds := h.kinds(it.ID); kinds[len(kinds)-1] != tt.event {
				t.Errorf("events %v, want the last to be %s", kinds, tt.event)
			}

			if tt.want.State == domain.StateDone && got.DoneAt.IsZero() {
				t.Error("done item has no done_at")
			}

			wantKinds := []domain.EventKind{
				domain.EventCreated, domain.EventMoved, domain.EventClaimed, domain.EventRunStarted, domain.EventSandboxStarted,
				domain.EventPrepared, domain.EventRunFinished,
			}
			if !slices.Equal(h.kinds(it.ID)[:len(wantKinds)], wantKinds) {
				t.Errorf("events %v, want them to start with %v", h.kinds(it.ID), wantKinds)
			}
		})
	}
}

func TestContract_FailureRetriesOnceThenBlocks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		results []domain.RunResult
		want    domain.ItemState
		failed  int
	}{
		{name: "valid first time", results: []domain.RunResult{ok(`{"answer":1}`)}, want: domain.StateDone},
		{name: "valid on retry", results: []domain.RunResult{ok(`{}`), ok(`{"answer":1}`)}, want: domain.StateDone, failed: 1},
		{name: "invalid twice blocks", results: []domain.RunResult{ok(`{}`), ok(`[]`)}, want: domain.StateBlocked, failed: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			work := agentStation(stWork, agWorker, 1, 1)
			work.Output = contractResult
			h := newHarness(t, lineOf(work), porttest.Sequence(tt.results...))
			h.contracts.Require(v1, contractResult, "answer")
			it := h.submit(t, domain.NewItem{Title: "count"})

			h.step(t)

			if got := h.item(t, it.ID); got.State != tt.want {
				t.Fatalf("item is %s, want %s (events %v)", got.State, tt.want, h.kinds(it.ID))
			}

			if got := len(h.eventsOf(it.ID, domain.EventContractFailed)); got != tt.failed {
				t.Errorf("%d contract_failed events, want %d", got, tt.failed)
			}

			runs := h.worker.Runs()
			if want := min(len(tt.results), 2); len(runs) != want {
				t.Fatalf("%d agent runs, want %d", len(runs), want)
			}

			if len(runs) == 2 && !strings.Contains(runs[1].Env["GOAL_FEEDBACK"], `missing property "answer"`) {
				t.Errorf("retry feedback %q does not carry the contract error", runs[1].Env["GOAL_FEEDBACK"])
			}

			if tt.want == domain.StateBlocked {
				if _, held := h.store.Lease(it.ID); held || len(h.eventsOf(it.ID, domain.EventBlocked)) != 1 {
					t.Errorf("blocked item: lease held %v, events %v; want released and one blocked", held, h.kinds(it.ID))
				}
			}
		})
	}
}

// reviewLine is build → review (gate back to build), driven by agents that answer per agent.
func reviewLine(maxRework int) domain.Line {
	review := agentStation("review", agReviewer, 1, 1)
	review.Gate = &domain.Gate{OnReject: "build", MaxRework: maxRework}

	return lineOf(agentStation("build", agWorker, 1, 1), review)
}

func byAgent(outputs map[string]string) porttest.AgentFunc {
	return func(_ context.Context, run porttest.ExecCall, _ io.Writer) domain.RunResult {
		return ok(outputs[run.Sandbox.Agent])
	}
}

func TestGate_ApproveAdvancesRejectSendsBack(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		verdict string
		want    domain.Position
		rework  int
		event   domain.EventKind
		// review is how the review's lease ended (the build's advanced).
		review domain.LeaseOutcome
	}{
		{
			name: "approve", verdict: `{"decision":"approve"}`,
			want: domain.Position{Station: stDone, State: domain.StateDone}, event: domain.EventGateApproved,
			review: domain.LeaseAdvanced,
		},
		{
			name: "reject", verdict: `{"decision":"reject","reason":"no test for the sweep","category":"Missing tests"}`,
			want: domain.Position{Station: "build", State: domain.StateQueued}, rework: 1, event: domain.EventGateRejected,
			review: domain.LeaseRejected,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, reviewLine(2), byAgent(map[string]string{agWorker: `{}`, agReviewer: tt.verdict}))
			it := h.submit(t, domain.NewItem{})

			h.step(t) // inbox → build, built → review
			h.step(t) // reviewed

			got := h.item(t, it.ID)
			if got.Position() != tt.want || got.ReworkCount != tt.rework {
				t.Fatalf("item at %+v rework %d, want %+v rework %d (events %v)",
					got.Position(), got.ReworkCount, tt.want, tt.rework, h.kinds(it.ID))
			}

			if len(h.eventsOf(it.ID, tt.event)) != 1 {
				t.Errorf("events %v, want one %s", h.kinds(it.ID), tt.event)
			}

			h.expectOutcomes(t, it.ID, domain.LeaseAdvanced, tt.review)

			if tt.rework > 0 {
				data := h.eventsOf(it.ID, domain.EventGateRejected)[0].Data
				if data["reason"] != "no test for the sweep" || data["category"] != "Missing tests" {
					t.Errorf("gate_rejected data = %v, want the reviewer's reason and category", data)
				}
			}
		})
	}
}

func TestGate_RejectBeyondMaxReworkGoesToHuman(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		maxRework int
	}{
		{name: "no rework allowed", maxRework: 0},
		{name: "two reworks allowed", maxRework: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, reviewLine(tt.maxRework),
				byAgent(map[string]string{agWorker: `{}`, agReviewer: `{"decision":"reject","reason":"still wrong"}`}))
			it := h.submit(t, domain.NewItem{})

			for range 2 * (tt.maxRework + 1) { // each round: build, then review
				h.step(t)
			}

			got := h.item(t, it.ID)
			want := domain.Position{Station: "review", State: domain.StateWaitingHuman}

			if got.Position() != want || got.ReworkCount != tt.maxRework {
				t.Fatalf("item at %+v rework %d, want %+v rework %d (events %v)",
					got.Position(), got.ReworkCount, want, tt.maxRework, h.kinds(it.ID))
			}

			if n := len(h.eventsOf(it.ID, domain.EventGateRejected)); n != tt.maxRework+1 {
				t.Errorf("%d gate_rejected events, want %d", n, tt.maxRework+1)
			}

			if len(h.eventsOf(it.ID, domain.EventWaitingHuman)) != 1 {
				t.Errorf("events %v, want one waiting_human", h.kinds(it.ID))
			}

			// Each round: the build advances, the review rejects; the last rejection escalates.
			var rounds []domain.LeaseOutcome
			for range tt.maxRework {
				rounds = append(rounds, domain.LeaseAdvanced, domain.LeaseRejected)
			}

			h.expectOutcomes(t, it.ID, append(rounds, domain.LeaseAdvanced, domain.LeaseEscalated)...)
		})
	}
}

func TestRoute_PicksAgentByLane(t *testing.T) {
	t.Parallel()

	build := domain.Station{
		ID: "build", Kind: domain.KindAgent, Agents: 2, WIP: 2,
		Route: map[string]string{"backend": agBackend, "frontend": agFrontend},
	}

	tests := []struct {
		name      string
		lane      string // lane given at intake
		specLane  string // lane the spec station's output declares
		wantAgent string
	}{
		{name: "lane from intake", lane: "frontend", wantAgent: agFrontend},
		{name: "lane from an upstream output", specLane: "backend", wantAgent: agBackend},
		{name: "output lane wins", lane: "frontend", specLane: "backend", wantAgent: agBackend},
		{name: "no route blocks", lane: "infra"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			spec := `{}`
			if tt.specLane != "" {
				spec = `{"lane":"` + tt.specLane + `"}`
			}

			h := newHarness(t, lineOf(agentStation("spec", "architect", 1, 1), build),
				byAgent(map[string]string{"architect": spec, agBackend: `{}`, agFrontend: `{}`}))
			it := h.submit(t, domain.NewItem{Lane: tt.lane})

			h.step(t) // spec
			h.step(t) // build

			started := h.worker.Runs()
			got := h.item(t, it.ID)

			if tt.wantAgent == "" {
				blocked := h.eventsOf(it.ID, domain.EventBlocked)
				if got.State != domain.StateBlocked || len(started) != 1 || len(blocked) != 1 {
					t.Fatalf("item %s, %d tasks, events %v; want blocked before any build task", got.State, len(started), h.kinds(it.ID))
				}

				if reason, _ := blocked[0].Data["reason"].(string); !strings.Contains(reason, `"infra"`) {
					t.Errorf("blocked reason %q does not name the lane", reason)
				}

				return
			}

			if len(started) != 2 || started[1].Sandbox.Agent != tt.wantAgent || got.State != domain.StateDone {
				t.Fatalf("runs %d, build agent %q, item %s; want %s and done", len(started), started[len(started)-1].Sandbox.Agent, got.State, tt.wantAgent)
			}
		})
	}
}

// policyLine gives the worker agent a full definition: its own model, skills, tools and connections.
func policyLine() domain.Line {
	line := lineOf(agentStation(stWork, agWorker, 1, 1))
	line.Agents[1] = domain.AgentSpec{
		Name: agWorker, PromptPath: "./agents/worker.md", Model: "claude-opus-5-5", Connections: []string{"github"},
		Skills:     []domain.SkillRef{{Name: "conventions", Source: "./skills/conventions"}},
		ToolsAllow: []string{"Read"}, ToolsDeny: []string{"Bash(git push*)"},
	}

	return line
}

// The run's task token names exactly its run: the gateway serves its bundle (the agent's prompt,
// skills, tools, model and budget) and tools from it.
func TestRun_GatewayTokenIsScopedToTheTask(t *testing.T) {
	t.Parallel()

	h := newHarness(t, policyLine(), porttest.Succeed(`{}`))
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	claims := h.claims(t, h.worker.Runs()[0].Env["GOAL_GATEWAY_TOKEN"])

	want := domain.TaskClaims{
		Audience: domain.TaskAudience, OrgID: testOrg, FactoryID: factoryID, VersionID: v1, ItemID: it.ID, Station: stWork, Agent: agWorker, RunID: h.runs(t, testOrg, it.ID)[0].ID,
		Connections: []string{"github"}, ExpiresAt: claims.ExpiresAt,
	}
	if !reflect.DeepEqual(claims, want) {
		t.Errorf("token claims = %+v, want %+v", claims, want)
	}
}

func TestRun_EndedRunsAreReported(t *testing.T) {
	t.Parallel()

	h := newHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)),
		porttest.Sequence(domain.RunResult{Status: domain.RunFailed, Error: "container exited 1"}))
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	if got, want := h.endedRuns(), []domain.RunID{h.runs(t, testOrg, it.ID)[0].ID}; !slices.Equal(got, want) {
		t.Fatalf("ended runs = %v, want %v: the gateway would keep the run's sidecars", got, want)
	}
}

func TestRun_FailedTaskBlocksItem(t *testing.T) {
	t.Parallel()

	h := newHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)),
		porttest.Sequence(domain.RunResult{Status: domain.RunFailed, Error: "container exited 1", CostUSD: 0.5}))
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	got := h.item(t, it.ID)
	if got.State != domain.StateBlocked || got.CostUSD != 0.5 {
		t.Fatalf("item %s cost %v, want blocked with the run's cost", got.State, got.CostUSD)
	}

	if reason, _ := h.eventsOf(it.ID, domain.EventBlocked)[0].Data["reason"].(string); !strings.Contains(reason, "container exited 1") {
		t.Errorf("blocked reason %q does not carry the run error", reason)
	}

	h.expectOutcomes(t, it.ID, domain.LeaseBlocked)
}

func TestRun_HeartbeatKeepsLeaseAlive(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	h := newHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)), porttest.Block(release, `{}`))
	it := h.submit(t, domain.NewItem{})

	if err := h.e.Tick(inOrg(t)); err != nil {
		t.Fatal(err)
	}

	if err := h.clock.WaitTickers(bounded(t), 1); err != nil {
		t.Fatal(err)
	}

	for range 6 { // two minutes: twice the lease TTL
		h.clock.Advance(heartbeat)

		now := h.clock.Now()
		if err := h.store.Await(bounded(t), func() bool {
			l, held := h.store.Lease(it.ID)
			return held && l.HeartbeatAt.Equal(now)
		}); err != nil {
			t.Fatal(err)
		}
	}

	if n, err := h.e.SweepLeases(inOrg(t)); err != nil || n != 0 {
		t.Fatalf("sweep requeued %d (%v) a heartbeated lease", n, err)
	}

	close(release)
	h.wait(t, h.e)

	if got := h.item(t, it.ID); got.State != domain.StateDone {
		t.Errorf("item %s, want done", got.State)
	}
}

func TestRun_LostLeaseStopsTheTask(t *testing.T) {
	t.Parallel()

	h := newHarness(t, lineOf(agentStation(stWork, agWorker, 1, 1)), porttest.Block(nil, `{}`))
	it := h.submit(t, domain.NewItem{})

	if err := h.e.Tick(inOrg(t)); err != nil {
		t.Fatal(err)
	}

	if err := h.clock.WaitTickers(bounded(t), 1); err != nil {
		t.Fatal(err)
	}

	// Another instance saw the lease expire (clock skew, a long pause) and requeued the item.
	if _, err := h.store.RequeueExpiredLeases(inOrg(t), h.clock.Now().Add(ttl), 10); err != nil {
		t.Fatal(err)
	}

	h.clock.Advance(heartbeat)
	h.wait(t, h.e) // returns only because the lost heartbeat cancelled the blocking task

	got := h.item(t, it.ID)
	if got.Position() != (domain.Position{Station: stWork, State: domain.StateQueued}) {
		t.Fatalf("item at %+v, want left queued at work for the next claim", got.Position())
	}

	if n := len(h.eventsOf(it.ID, domain.EventBlocked)) + len(h.eventsOf(it.ID, domain.EventReleased)); n != 0 {
		t.Errorf("events %v: the instance that lost the lease must not move the item", h.kinds(it.ID))
	}
}

func TestRun_ShutdownReleasesItemsToTheQueue(t *testing.T) {
	t.Parallel()

	h := newHarness(t, lineOf(agentStation(stWork, agWorker, 2, 2)), porttest.Block(nil, `{}`))
	a := h.submit(t, domain.NewItem{})
	b := h.submit(t, domain.NewItem{})

	ctx, cancel := context.WithCancel(inOrg(t))
	if err := h.e.Tick(ctx); err != nil {
		t.Fatal(err)
	}

	if err := h.worker.WaitRuns(bounded(t), 2); err != nil {
		t.Fatal(err)
	}

	cancel()
	h.wait(t, h.e)

	for _, id := range []domain.ItemID{a.ID, b.ID} {
		got := h.item(t, id)
		_, held := h.store.Lease(id)

		if got.State != domain.StateQueued || held || len(h.eventsOf(id, domain.EventReleased)) != 1 {
			t.Errorf("item %s, lease held %v, events %v; want queued, released", got.State, held, h.kinds(id))
		}

		h.expectOutcomes(t, id, domain.LeaseReleased)
	}
}

// v2 is the version published while a run on v1 is in progress.
const v2 domain.DefinitionVersionID = "019a1c00-0000-7000-8000-0000000000a2"

// A run keeps the version it started on: it records it, its gateway token names it (the
// gateway serves that version's bundle), and its output is checked against that version's
// contract even after v2 is published.
// The item's next move follows v2, matched by station id.
func TestRun_KeepsItsVersionAndTheNextMoveUsesTheCurrentLine(t *testing.T) {
	t.Parallel()

	work := agentStation(stWork, agWorker, 1, 1)
	work.Output = contractResult
	release := make(chan struct{})
	h := newHarness(t, lineOf(work), porttest.Block(release, `{"answer":42}`))
	h.contracts.Require(v1, contractResult, "answer")
	h.contracts.Require(v2, contractResult, "verdict") // v1's output would break v2's contract
	it := h.submit(t, domain.NewItem{})

	if err := h.e.Tick(inOrg(t)); err != nil {
		t.Fatal(err)
	}

	if err := h.worker.WaitRuns(bounded(t), 1); err != nil {
		t.Fatal(err)
	}

	next := lineOf(work, domain.Station{ID: stApprove, Kind: domain.KindHuman})
	next.VersionID, next.Number = v2, 2

	if err := h.e.SetLines([]domain.Line{next}); err != nil {
		t.Fatal(err)
	}

	close(release)
	h.wait(t, h.e)

	if got := h.item(t, it.ID).Position(); got != (domain.Position{Station: stApprove, State: domain.StateWaitingHuman}) {
		t.Fatalf("item at %+v, want approve/waiting_human: v1's output passes v1's contract and moves on v2's line (events %v)",
			got, h.kinds(it.ID))
	}

	if runs := h.runs(t, testOrg, it.ID); len(runs) != 1 || runs[0].DefinitionVersionID != v1 {
		t.Errorf("runs = %+v, want one recorded on %s", runs, v1)
	}

	if claims := h.claims(t, h.worker.Runs()[0].Env["GOAL_GATEWAY_TOKEN"]); claims.VersionID != v1 {
		t.Errorf("token version = %q, want %q", claims.VersionID, v1)
	}
}
