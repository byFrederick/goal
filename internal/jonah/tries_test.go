package jonah_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/jonah"
)

// modelKeyRef is the model key of the tests' versions on secrets.
const modelKeyRef = "{{claude-key}}"

const (
	tryAgent   = "writer"
	tryVersion = domain.DefinitionVersionID("try-version-1")
	stSpec     = "spec"
	tryTimeout = 10 * time.Minute
	stTryDone  = "done"
	tryRunID   = domain.RunID("try-run")
)

var tryHead = strings.Repeat("c", 40)

// tryDiff is the diff the graded tries' runs leave.
const tryDiff = "diff --git a/notes.go b/notes.go\n"

// contractNote is the output contract the try tests build a station with.
const contractNote = "note"

// tryDefs is TryDefinitions over each organization's one line: ProposedLine answers the line of
// the version and the stations reached[version] names, every agent station by default.
type tryDefs struct {
	mu      sync.Mutex
	lines   map[domain.OrgID]domain.Line
	reached map[string][]string // by the proposal's first path
	stale   bool                // no proposal's edits apply
	// validate answers ValidateProposed; nil passes every output. validated counts its calls.
	validate  func(contract string, edits []domain.ProposalEdit, output json.RawMessage) error
	validated int
}

func (d *tryDefs) ValidateProposed(_ context.Context, v domain.DefinitionVersionID, edits []domain.ProposalEdit, contract string,
	output json.RawMessage,
) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.validated++

	if v != tryVersion {
		return ports.ErrNotFound
	}

	if d.validate == nil {
		return nil
	}

	return d.validate(contract, edits, output)
}

// ContentKey is a key of v and the first edit's path, as two contents that match share one.
func (d *tryDefs) ContentKey(_ context.Context, v domain.DefinitionVersionID, edits []domain.ProposalEdit) (string, error) {
	if len(edits) == 0 {
		return "key-" + string(v), nil
	}

	return "key-" + string(v) + "-" + edits[0].Path, nil
}

// build changes org's build station with change.
func (d *tryDefs) build(org domain.OrgID, change func(*domain.Station)) {
	d.mu.Lock()
	defer d.mu.Unlock()

	l := d.lines[org]
	l.Stations = slices.Clone(l.Stations)
	change(&l.Stations[1])
	d.lines[org] = l
}

func (d *tryDefs) CurrentLines(ctx context.Context) ([]domain.Line, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	org, _ := domain.OrgFrom(ctx)

	return []domain.Line{d.lines[org]}, nil
}

func (d *tryDefs) ProposedLine(ctx context.Context, v domain.DefinitionVersionID, edits []domain.ProposalEdit) (domain.Line, []string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	org, _ := domain.OrgFrom(ctx)

	l := d.lines[org]
	if v != l.VersionID {
		return domain.Line{}, nil, ports.ErrNotFound
	}

	if d.stale {
		return domain.Line{}, nil, fmt.Errorf("%s: %w", edits[0].Path, domain.ErrEditDoesNotApply)
	}

	if r, ok := d.reached[edits[0].Path]; ok {
		return l, r, nil
	}

	return l, []string{stBuild}, nil
}

// tryWorld is a try loop over orgs, each with one factory whose line is spec (human), build (the
// writer agent) and done, on connections with a repository.
type tryWorld struct {
	t      *testing.T
	store  *porttest.Store
	tn     *porttest.Tenancy
	clock  *porttest.Clock
	defs   *tryDefs
	runner *taskRunner
	loop   *jonah.Tries
	orgs   []domain.OrgID
	// factories are each org's factory.
	factories map[domain.OrgID]domain.FactoryID
}

func newTryWorld(t *testing.T, orgs int) *tryWorld {
	t.Helper()

	clock := porttest.NewClock(rt0)
	store := porttest.NewStore()
	w := &tryWorld{
		t: t, store: store, tn: porttest.NewTenancy(clock.Now).WithStore(store), clock: clock,
		defs:      &tryDefs{lines: map[domain.OrgID]domain.Line{}, reached: map[string][]string{}},
		factories: map[domain.OrgID]domain.FactoryID{},
		runner:    &taskRunner{},
	}
	w.runner.answer = func(ctx context.Context, req domain.RunRequest) (domain.RunResult, error) {
		if req.Bundle == domain.TaskKindGrade {
			return w.recorded(ctx, req, rightGrade(req.GradeOf)), nil
		}

		return domain.RunResult{Status: domain.RunSucceeded, Output: json.RawMessage(`{}`), CostUSD: 0.25, Diff: "diff --git a/x b/x\n", RunID: tryRunID}, nil
	}

	for i := range orgs {
		o, err := w.tn.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: fmt.Sprintf("acme-%d", i), Name: "Acme tries"})
		if err != nil {
			t.Fatal(err)
		}

		f := domain.FactoryID(fmt.Sprintf("00000000-0000-4000-8000-0000000000%02d", i+1))
		store.AddFactory(o.ID, f)
		w.orgs, w.factories[o.ID] = append(w.orgs, o.ID), f
		w.defs.lines[o.ID] = domain.Line{
			OrgID: o.ID, FactoryID: f, VersionID: tryVersion, Number: 1, Jonah: domain.DefaultJonahPolicy(),
			Runtime:      domain.Runtime{ModelKey: modelKeyRef, Model: reviewModel, Repository: domain.Repository{URL: "https://example.com/acme.git"}},
			Environments: []domain.Environment{{Name: domain.DefaultEnvironment}},
			Agents:       []domain.AgentSpec{{Name: tryAgent}},
			Stations: []domain.Station{
				{ID: stSpec, Kind: domain.KindHuman}, {ID: stBuild, Kind: domain.KindAgent, Agent: tryAgent}, {ID: stTryDone, Kind: domain.KindDone},
			},
		}
	}

	w.loop = jonah.NewTries(jonah.TryDeps{
		Orgs: w.tn, Jonah: store, Tries: store, Kept: store, Definitions: w.defs, Waiting: store, Events: store, Runs: store,
		Runner: w.runner, Clock: clock,
	}, jonah.TryConfig{Timeout: tryTimeout, Margin: time.Minute})

	return w
}

func (w *tryWorld) ctx(org domain.OrgID) context.Context { return domain.WithOrg(w.t.Context(), org) }

// recorded is res, the result of the grade run req, recorded in the store as RunTask records a
// grade: a run of kind try without an item, so its cost is Jonah's.
func (w *tryWorld) recorded(ctx context.Context, req domain.RunRequest, res domain.RunResult) domain.RunResult {
	w.t.Helper()

	id, err := w.store.StartRun(ctx, ports.NewRun{FactoryID: req.FactoryID, Station: req.Station, Agent: req.Agent, StartedAt: w.clock.Now(), Kind: domain.RunKindTry})
	if err != nil {
		w.t.Fatal(err)
	}

	if err := w.store.FinishRun(ctx, id, res, w.clock.Now()); err != nil {
		w.t.Fatal(err)
	}

	res.RunID = id

	return res
}

// gradeCost is what each of the fake grader's runs costs.
const gradeCost = 0.01

// grade is a grade run's result that answers verdict with a reason naming the result of.
func grade(of domain.GradeOf, verdict domain.PastTryVerdict) domain.RunResult {
	out, _ := json.Marshal(domain.GradeAnswer{Reason: "the grader on the " + string(of) + " result", Verdict: verdict})

	return domain.RunResult{Status: domain.RunSucceeded, Output: out, CostUSD: gradeCost}
}

// rightGrade is the answer of a grader that reads each result right: the original not fixed,
// the final and the try fixed.
func rightGrade(of domain.GradeOf) domain.RunResult {
	if of == domain.GradeOriginal {
		return grade(of, domain.PastTryNotFixed)
	}

	return grade(of, domain.PastTryFixed)
}

// gradesOf are the results reqs' grade runs grade, in order.
func gradesOf(reqs []domain.RunRequest) []domain.GradeOf {
	var out []domain.GradeOf

	for _, r := range reqs {
		if r.Bundle == domain.TaskKindGrade {
			out = append(out, r.GradeOf)
		}
	}

	return out
}

// originalOutput is the output of a past item's build run.
const originalOutput = `{"draft":true}`

// pastItem is a done item of org's factory whose build run, started at, recorded a commit (first),
// output originalOutput and cost $100 of the line's spend.
func (w *tryWorld) pastItem(org domain.OrgID, at time.Time, first bool) (domain.Item, domain.RunID) {
	w.t.Helper()

	return w.pastItemCost(org, at, first, 100)
}

// pastItemCost is pastItem with a run that cost usd.
func (w *tryWorld) pastItemCost(org domain.OrgID, at time.Time, first bool, usd float64) (domain.Item, domain.RunID) {
	w.t.Helper()

	ctx := w.ctx(org)

	it, err := w.store.CreateItem(ctx, domain.NewItem{FactoryID: w.factories[org], Title: "past", Station: stTryDone, State: domain.StateDone})
	if err != nil {
		w.t.Fatal(err)
	}

	id, err := w.store.StartRun(ctx, ports.NewRun{FactoryID: w.factories[org], ItemID: it.ID, Station: stBuild, Agent: tryAgent, StartedAt: at})
	if err != nil {
		w.t.Fatal(err)
	}

	in := domain.RunInput{Brief: "the brief"}
	if first {
		in.Repositories = []domain.RunRepository{{BranchHead: tryHead}}
	}

	if err := w.store.RecordRunInput(ctx, id, in); err != nil {
		w.t.Fatal(err)
	}

	if err := w.store.FinishRun(ctx, id, domain.RunResult{Status: domain.RunSucceeded, Output: json.RawMessage(originalOutput), CostUSD: usd},
		at.Add(time.Minute)); err != nil {
		w.t.Fatal(err)
	}

	return it, id
}

// propose stores a proposal of org's factory of path that cites items.
func (w *tryWorld) propose(org domain.OrgID, path string, items ...domain.Item) domain.Proposal {
	w.t.Helper()

	ctx := w.ctx(org)

	_, err := w.store.QueueJonahReview(ctx, domain.NewJonahReview{
		FactoryID: w.factories[org], VersionID: tryVersion, Trigger: domain.JonahManual, WindowStart: rt0.Add(-time.Hour), WindowEnd: rt0,
		By: domain.LocalUser, At: rt0,
	})
	if err != nil {
		w.t.Fatal(err)
	}

	rv, err := w.store.ClaimJonahReview(ctx, rt0, time.Hour)
	if err != nil {
		w.t.Fatal(err)
	}

	n := domain.NewProposal{Title: "Change " + path, Edits: []domain.ProposalEdit{{Path: path, Replacements: []domain.Replacement{{Old: "old\n", New: "new\n"}}}}}
	for _, it := range items {
		n.Evidence = append(n.Evidence, domain.ProposalEvidence{ItemID: it.ID, Station: stBuild, Quote: "no tests"})
	}

	_, ps, err := w.store.FinishJonahReview(ctx, rv, domain.JonahReviewResult{State: domain.JonahDone, At: rt0, Proposals: []domain.NewProposal{n}})
	if err != nil {
		w.t.Fatal(err)
	}

	return ps[0]
}

func (w *tryWorld) tries(org domain.OrgID, p domain.Proposal) []domain.PastTry {
	w.t.Helper()

	got, err := w.store.ProposalPastTries(w.ctx(org), p.ID)
	if err != nil {
		w.t.Fatal(err)
	}

	return got
}

func (w *tryWorld) sweep() {
	w.t.Helper()

	if err := w.loop.Sweep(w.t.Context()); err != nil {
		w.t.Fatalf("Sweep: %v", err)
	}
}

func (w *tryWorld) work() int {
	w.t.Helper()

	n, err := w.loop.Work(w.t.Context())
	if err != nil {
		w.t.Fatalf("Work: %v", err)
	}

	return n
}

// A proposal that reaches a triable station gets one try per cited item at that station, of the
// item's run there that was a claim's first, on the agent's model; a second sweep queues no more.
func TestTries_SweepQueuesReachedTriableStations(t *testing.T) {
	t.Parallel()

	w := newTryWorld(t, 1)
	org := w.orgs[0]
	a, aRun := w.pastItem(org, rt0.Add(-3*time.Hour), true)
	w.pastItem(org, rt0.Add(-2*time.Hour), false) // a retry of another item: no commit, never tried
	b, bRun := w.pastItem(org, rt0.Add(-time.Hour), true)
	p := w.propose(org, writerPath, a, b)

	w.sweep()
	w.sweep()

	got := w.tries(org, p)
	if len(got) != 2 {
		t.Fatalf("tries = %+v; want one per cited item", got)
	}

	if got[0].ItemID != b.ID || got[0].OriginalRunID != bRun || got[1].ItemID != a.ID || got[1].OriginalRunID != aRun {
		t.Errorf("tries = %+v; want b's then a's, newest first, each of its first run", got)
	}

	if got[0].Station != stBuild || got[0].Model != reviewModel || got[0].State != domain.PastTryQueued {
		t.Errorf("try = %+v; want a queued try at %s on %s", got[0], stBuild, reviewModel)
	}
}

// A proposal that reaches no station (memory-only or flow-only), or only one whose agent needs
// connections or an environment's files or MCP servers, gets no tries.
func TestTries_SweepSkipsUntriableAndMemoryOnly(t *testing.T) {
	t.Parallel()

	w := newTryWorld(t, 1)
	org := w.orgs[0]
	it, _ := w.pastItem(org, rt0.Add(-time.Hour), true)
	w.defs.reached["factory.yaml"] = nil // a flow-only change: memory-only is the same, as it reaches no station

	flow := w.propose(org, "factory.yaml", it)
	w.sweep()

	if got := w.tries(org, flow); len(got) != 0 {
		t.Errorf("tries of a proposal that reaches no station = %+v; want none", got)
	}

	l := w.defs.lines[org]
	l.Agents = []domain.AgentSpec{{Name: tryAgent, Connections: []string{"issues"}}}
	w.defs.lines[org] = l

	untriable := w.propose(org, writerPath, it)
	w.sweep()

	if got := w.tries(org, untriable); len(got) != 0 {
		t.Errorf("tries at a station whose agent needs a connection = %+v; want none", got)
	}

	l.Agents = []domain.AgentSpec{{Name: tryAgent}}
	l.Runtime.ModelConnection, l.Runtime.ModelKey = "", modelKeyRef
	l.Environments = []domain.Environment{{
		Name: domain.DefaultEnvironment, Env: map[string]string{"NPM_TOKEN": "{{npm}}"},
		MCP: map[string]domain.MCPServer{"linear": {URL: "https://mcp.example.com"}},
	}}
	w.defs.lines[org] = l

	w.sweep()

	if got := w.tries(org, untriable); len(got) != 0 {
		t.Errorf("tries at a station whose environment has MCP servers = %+v; want none", got)
	}
}

// A proposal whose edits no longer apply to its version is not tried: the sweep queues none, and
// a try already queued ends with verdict error, never fail.
func TestTries_EditsThatDoNotApplyAreNotTried(t *testing.T) {
	t.Parallel()

	w := newTryWorld(t, 1)
	org := w.orgs[0]
	it, _ := w.pastItem(org, rt0.Add(-time.Hour), true)
	w.defs.stale = true

	p := w.propose(org, writerPath, it)
	w.sweep()

	if got := w.tries(org, p); len(got) != 0 {
		t.Errorf("tries of a proposal whose edits do not apply = %+v; want none", got)
	}

	w.defs.stale = false
	w.sweep()

	if got := w.tries(org, p); len(got) != 1 {
		t.Fatalf("%d tries queued once the edits apply; want 1", len(got))
	}

	w.defs.stale = true

	got := w.onlyTry(org)
	if got.Verdict != domain.PastTryError || !strings.Contains(got.Error, "does not apply") || len(w.runner.requests()) != 0 {
		t.Errorf("try = %+v after %d runs; want verdict error naming the edits, and no run", got, len(w.runner.requests()))
	}
}

// A proposal citing more than five items gets tries on the five whose runs are newest.
func TestTries_SweepCapsAtFiveNewestFirst(t *testing.T) {
	t.Parallel()

	w := newTryWorld(t, 1)
	org := w.orgs[0]

	var items []domain.Item

	for i := range 7 {
		it, _ := w.pastItem(org, rt0.Add(-time.Duration(7-i)*time.Hour), true)
		items = append(items, it)
	}

	p := w.propose(org, writerPath, items...)
	w.sweep()

	got := w.tries(org, p)
	if len(got) != domain.MaxPastTryItems {
		t.Fatalf("%d tries; want %d", len(got), domain.MaxPastTryItems)
	}

	for i, tr := range got {
		if want := items[6-i].ID; tr.ItemID != want {
			t.Errorf("try %d of item %s; want %s, newest first", i, tr.ItemID, want)
		}
	}
}

// queued queues a proposal of org's factory with n tries.
func (w *tryWorld) queued(org domain.OrgID, n int) {
	w.t.Helper()

	var items []domain.Item

	for i := range n {
		it, _ := w.pastItem(org, rt0.Add(-time.Duration(i+1)*time.Hour), true)
		items = append(items, it)
	}

	p := w.propose(org, writerPath, items...)
	w.sweep()

	if got := w.tries(org, p); len(got) != n {
		w.t.Fatalf("%d tries queued; want %d", len(got), n)
	}
}

// Each pass of Work claims one try of each organization in turn, so no organization's tries wait
// for another's to run out.
func TestTries_ClaimsFairlyAcrossOrgs(t *testing.T) {
	t.Parallel()

	w := newTryWorld(t, 2)
	w.queued(w.orgs[0], 3)
	w.queued(w.orgs[1], 2)

	if n := w.work(); n != 5 {
		t.Errorf("Work finished %d tries; want 5", n)
	}

	runs := slices.DeleteFunc(w.runner.requests(), func(r domain.RunRequest) bool { return r.Bundle == domain.TaskKindGrade })

	var order []domain.FactoryID
	for _, r := range runs {
		order = append(order, r.FactoryID)
	}

	a, b := w.factories[w.orgs[0]], w.factories[w.orgs[1]]
	if want := []domain.FactoryID{a, b, a, b, a}; !slices.Equal(order, want) {
		t.Errorf("tries ran for %v; want %v, the organizations in turn", order, want)
	}

	for _, r := range runs {
		if r.Kind != domain.RunKindTry || r.Item != "" || r.Lease != nil || r.Commit != tryHead || r.Bundle != domain.TaskKindTry ||
			r.TryOf == "" || len(r.Connections) != 0 || r.Timeout != tryTimeout {
			t.Errorf("request %+v; want a try of the recorded commit with no item, lease or connection", r)
		}
	}
}

// No try of a factory is claimed while PerFactory of its tries run under a live lease, on this
// goald or another.
func TestTries_CapPerFactory(t *testing.T) {
	t.Parallel()

	w := newTryWorld(t, 1)
	org := w.orgs[0]
	w.queued(org, 3)

	for range jonah.DefaultTriesPerFactory { // another goald runs two
		if _, err := w.store.ClaimPastTry(w.ctx(org), w.factories[org], rt0, time.Hour, 3); err != nil {
			t.Fatal(err)
		}
	}

	if n := w.work(); n != 0 || len(w.runner.requests()) != 0 {
		t.Errorf("Work ran %d tries; want none at the cap", len(w.runner.requests()))
	}
}

// A factory with an item waiting at an agent station gets no try claimed: tries give way to
// production work.
func TestTries_GivesWayToProductionWork(t *testing.T) {
	t.Parallel()

	w := newTryWorld(t, 1)
	org := w.orgs[0]
	w.queued(org, 1)

	if _, err := w.store.CreateItem(w.ctx(org), domain.NewItem{FactoryID: w.factories[org], Title: "new work", Station: stBuild, State: domain.StateQueued}); err != nil {
		t.Fatal(err)
	}

	if n := w.work(); n != 0 || len(w.runner.requests()) != 0 {
		t.Errorf("Work ran %d tries; want none while an item waits at %s", len(w.runner.requests()), stBuild)
	}
}

// A try whose goald died is claimed again once its lease ends, and finished then.
func TestTries_LostLeaseIsClaimedAgain(t *testing.T) {
	t.Parallel()

	w := newTryWorld(t, 1)
	org := w.orgs[0]
	w.queued(org, 1)

	dead, err := w.store.ClaimPastTry(w.ctx(org), w.factories[org], rt0, tryTimeout, 2)
	if err != nil {
		t.Fatal(err)
	}

	w.clock.Advance(tryTimeout + time.Second)

	if n := w.work(); n != 1 {
		t.Fatalf("Work finished %d tries; want the one whose lease ended", n)
	}

	got, err := w.store.PastTry(w.ctx(org), dead.ID)
	if err != nil || got.State != domain.PastTryDone || got.Attempts != 2 {
		t.Errorf("try = %+v, %v; want done on its second attempt", got, err)
	}

	if _, err := w.store.FinishPastTry(w.ctx(org), dead, domain.PastTryResult{At: rt0}); !errors.Is(err, ports.ErrLeaseLost) {
		t.Errorf("the dead claim's finish = %v; want ErrLeaseLost", err)
	}
}

// A factory whose Jonah share is used gets no try started: its tries stay queued.
func TestTries_OverBudgetStaysQueued(t *testing.T) {
	t.Parallel()

	w := newTryWorld(t, 1)
	org := w.orgs[0]
	w.queued(org, 1) // the line spent $100: Jonah's 5% is $5

	id, err := w.store.StartRun(w.ctx(org), ports.NewRun{FactoryID: w.factories[org], Agent: "jonah", StartedAt: rt0, Kind: domain.RunKindJonah})
	if err != nil {
		t.Fatal(err)
	}

	if err := w.store.FinishRun(w.ctx(org), id, domain.RunResult{Status: domain.RunSucceeded, CostUSD: 5}, rt0); err != nil {
		t.Fatal(err)
	}

	if n := w.work(); n != 0 || len(w.runner.requests()) != 0 {
		t.Errorf("Work ran %d tries; want none over the budget", len(w.runner.requests()))
	}

	if _, err := w.store.ClaimPastTry(w.ctx(org), w.factories[org], rt0, time.Minute, 2); err != nil {
		t.Errorf("the try is not queued: %v", err)
	}
}

// onlyTry runs the world's one queued try and returns how it was stored.
func (w *tryWorld) onlyTry(org domain.OrgID) domain.PastTry {
	w.t.Helper()

	if n := w.work(); n != 1 {
		w.t.Fatalf("Work finished %d tries; want 1", n)
	}

	ps, err := w.store.Proposals(w.ctx(org), ports.ProposalQuery{Factory: w.factories[org], Limit: 10})
	if err != nil {
		w.t.Fatal(err)
	}

	for _, p := range ps {
		if got := w.tries(org, p); len(got) == 1 {
			return got[0]
		}
	}

	w.t.Fatal("no try")

	return domain.PastTry{}
}

// A run that could not be carried out (a lost box, a failed prepare or setup) makes the try done
// with verdict error and the reason, never fail; its cost and run are kept.
func TestTries_InfraFailureIsError(t *testing.T) {
	t.Parallel()

	w := newTryWorld(t, 1)
	org := w.orgs[0]
	w.queued(org, 1)
	w.runner.answer = func(context.Context, domain.RunRequest) (domain.RunResult, error) {
		return domain.RunResult{RunID: tryRunID, CostUSD: 0.01}, fmt.Errorf("agent: %w", ports.ErrSandboxLost)
	}

	got := w.onlyTry(org)
	if got.State != domain.PastTryDone || got.Verdict != domain.PastTryError || !strings.Contains(got.Error, "sandbox") ||
		got.TryRunID != tryRunID || got.CostUSD != 0.01 {
		t.Errorf("try = %+v; want done with verdict error, the reason, its run and cost", got)
	}
}

// A recorded commit that is gone fails the try's prepare: verdict error, with prepare's reason.
func TestTries_MissingCommitIsError(t *testing.T) {
	t.Parallel()

	w := newTryWorld(t, 1)
	org := w.orgs[0]
	w.queued(org, 1)
	w.runner.answer = func(context.Context, domain.RunRequest) (domain.RunResult, error) {
		return domain.RunResult{RunID: tryRunID}, errors.New("prepare the sandbox: goal-agent prepare exited 1: commit " + tryHead + " is gone")
	}

	got := w.onlyTry(org)
	if got.Verdict != domain.PastTryError || !strings.Contains(got.Error, "is gone") {
		t.Errorf("try = %+v; want verdict error naming the missing commit", got)
	}
}

// A run that ends is stored done with the grader's verdict, its diff, its cost with the grade's and
// its run. The past item's cost stays as it was.
func TestTries_FinishedRunStoresDiffAndCost(t *testing.T) {
	t.Parallel()

	w := newTryWorld(t, 1)
	org := w.orgs[0]
	it, _ := w.pastItem(org, rt0.Add(-time.Hour), true)
	w.propose(org, writerPath, it)
	w.sweep()

	got := w.onlyTry(org)
	if got.State != domain.PastTryDone || got.Verdict != domain.PastTryFixed || got.Diff != "diff --git a/x b/x\n" || got.CostUSD != 0.25+gradeCost ||
		got.TryRunID != tryRunID {
		t.Errorf("try = %+v; want done and fixed, the run's diff, its cost with the grade's and its run", got)
	}

	if after, err := w.store.Item(w.ctx(org), it.ID); err != nil || after.CostUSD != 100 {
		t.Errorf("past item cost = %v, %v; want 100, unchanged", after.CostUSD, err)
	}
}

// A try whose run succeeded but failed one of its station's checks, which the proposed line names
// and its box ran, is not fixed, with the check's command, exit code and output tail as its
// reason, and keeps its diff and cost; the grader never sees it.
func TestTries_FailingCheckIsNotFixed(t *testing.T) {
	t.Parallel()

	w := newTryWorld(t, 1)
	org := w.orgs[0]
	checks := []string{"make lint", "make test"}

	w.defs.build(org, func(s *domain.Station) { s.Checks = checks })
	w.queued(org, 1)
	w.runner.answer = func(context.Context, domain.RunRequest) (domain.RunResult, error) {
		return domain.RunResult{
			Status: domain.RunSucceeded, Output: json.RawMessage(`{}`), CostUSD: 0.25, Diff: tryDiff, RunID: tryRunID,
			FailedCheck: &domain.FailedCheck{Command: checkMakeTest, ExitCode: 2, Output: "FAIL TestNotes"},
		}, nil
	}

	got := w.onlyTry(org)
	if got.Verdict != domain.PastTryNotFixed || !strings.HasPrefix(got.Reason, "check `make test` exited 2") ||
		!strings.Contains(got.Reason, "FAIL TestNotes") || got.Diff != tryDiff || got.CostUSD != 0.25 || got.Error != "" {
		t.Errorf("try = %+v; want not fixed, the failing check first in its reason, its diff and cost", got)
	}

	if reqs := w.runner.requests(); len(reqs) != 1 || !slices.Equal(reqs[0].Checks, checks) {
		t.Errorf("requests = %+v; want one naming the station's checks %q", reqs, checks)
	}
}

// A try's output is checked against the station's contract as the proposal changes it: one that
// fails it is not fixed, the contract and its error first in the reason; a contract the proposal
// no longer has makes the try an error, not a failing one.
func TestTries_ContractOfTheProposalIsChecked(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name    string
		err     error
		verdict domain.PastTryVerdict
		want    string
	}{
		{
			name: "a violation", err: fmt.Errorf("%w: note: missing title", ports.ErrContractViolation), verdict: domain.PastTryNotFixed,
			want: "the output does not satisfy contract note",
		},
		{name: "a contract that is gone", err: errors.New(`unknown contract "note"`), verdict: domain.PastTryError, want: "contract note"},
	} {
		w := newTryWorld(t, 1)
		org := w.orgs[0]
		w.defs.build(org, func(s *domain.Station) { s.Output = contractNote })
		w.queued(org, 1)

		var seen []domain.ProposalEdit

		w.defs.validate = func(contract string, edits []domain.ProposalEdit, output json.RawMessage) error {
			if contract != contractNote || string(output) != `{}` {
				return fmt.Errorf("validated %s with %s; want note with the run's output", contract, output)
			}

			seen = edits

			return tt.err
		}

		got := w.onlyTry(org)

		text := got.Reason
		if tt.verdict == domain.PastTryError {
			text = got.Error
		}

		if got.Verdict != tt.verdict || !strings.HasPrefix(text, tt.want) {
			t.Errorf("%s: try = %+v; want verdict %s starting %q", tt.name, got, tt.verdict, tt.want)
		}

		if len(seen) != 1 || seen[0].Path != writerPath {
			t.Errorf("%s: validated with edits %+v; want the proposal's", tt.name, seen)
		}
	}
}

// gradedItem is a past item of org whose build run started at started and was sent back by a
// person for reason, then built again and accepted: its original and final results.
func (w *tryWorld) gradedItem(org domain.OrgID, started time.Time, reason string) domain.Item {
	w.t.Helper()

	ctx := w.ctx(org)
	it, _ := w.pastItem(org, started, true)

	err := w.store.AppendEvents(ctx, domain.Event{
		FactoryID: w.factories[org], ItemID: it.ID, At: started.Add(10 * time.Minute), Kind: domain.EventRejected, Station: stBuild, Actor: "grace",
		Data: map[string]any{"reason": reason},
	})
	if err != nil {
		w.t.Fatal(err)
	}

	id, err := w.store.StartRun(ctx, ports.NewRun{FactoryID: w.factories[org], ItemID: it.ID, Station: stBuild, Agent: tryAgent, StartedAt: started.Add(20 * time.Minute)})
	if err != nil {
		w.t.Fatal(err)
	}

	res := domain.RunResult{Status: domain.RunSucceeded, Output: json.RawMessage(`{"tests":true}`), CostUSD: 1}
	if err := w.store.FinishRun(ctx, id, res, started.Add(30*time.Minute)); err != nil {
		w.t.Fatal(err)
	}

	return it
}

// A try that passes code grading at a station that is not a gate is graded by the grader: first the
// grader is checked on the item's original and final results, each check stored with its answer,
// cost and run, then one grade run grades the try, whose answer and reason the try takes and whose
// cost it adds. Each grade run is a try-kind run on a grade bundle with the current line's model
// and the model's access alone.
func TestTries_GraderGradesTry(t *testing.T) {
	t.Parallel()

	w := newTryWorld(t, 1)
	org := w.orgs[0]
	it := w.gradedItem(org, rt0.Add(-2*time.Hour), sendBack)
	p := w.propose(org, writerPath, it)
	w.sweep()

	got := w.onlyTry(org)
	if got.Verdict != domain.PastTryFixed || got.Reason != "the grader on the try result" || got.CostUSD != 0.25+gradeCost || got.TryRunID != tryRunID {
		t.Errorf("try = %+v; want fixed with the grader's reason, its cost with the grade's", got)
	}

	wantGradeRequests(t, w.runner.requests(), org, w.factories[org], got.ID)
	wantHits(t, w, org, p, it)
}

// wantHits fails t unless p has the grader's original then final check of it, both hits, each
// with its cost and run.
func wantHits(t *testing.T, w *tryWorld, org domain.OrgID, p domain.Proposal, it domain.Item) {
	t.Helper()

	checks, err := w.store.ProposalGraderChecks(w.ctx(org), p.ID)
	if err != nil || len(checks) != 2 || checks[0].Result != domain.GraderCheckOriginal || checks[1].Result != domain.GraderCheckFinal ||
		!checks[0].Hit() || !checks[1].Hit() || checks[0].CostUSD != gradeCost || checks[1].RunID == "" || checks[0].ItemID != it.ID {
		t.Errorf("checks = %+v, %v; want the original then the final check, both hits, with their cost and run", checks, err)
	}
}

// wantGradeRequests fails t unless reqs are the try's run then grades of the original, the final
// and the try: each a try-kind run on a grade bundle with the current line's model and the
// model's access alone.
func wantGradeRequests(t *testing.T, reqs []domain.RunRequest, org domain.OrgID, f domain.FactoryID, try domain.PastTryID) {
	t.Helper()

	if g := gradesOf(reqs); !slices.Equal(g, []domain.GradeOf{domain.GradeOriginal, domain.GradeFinal, domain.GradeTry}) || len(reqs) != 4 {
		t.Fatalf("grades %v of %d runs; want the original, the final, then the try, after the try's run", g, len(reqs))
	}

	for _, r := range reqs[1:] {
		want := domain.RunRequest{
			OrgID: org, FactoryID: f, Version: tryVersion, Kind: domain.RunKindTry, Station: stBuild, Agent: tryAgent,
			Model: reviewModel, Bundle: domain.TaskKindGrade, TryOf: try, GradeOf: r.GradeOf, ModelKey: modelKeyRef,
			Size: domain.SandboxSmall, Timeout: tryTimeout,
		}
		if !reflect.DeepEqual(r, want) {
			t.Errorf("grade request =\n%+v\nwant\n%+v", r, want)
		}

		if err := r.Check(); err != nil {
			t.Errorf("grade request Check: %v", err)
		}
	}
}

// When the grader misreads the item's original or final result, or the item has no final result,
// the try is unknown, its reason naming the misread result, and the try gets no grade run. A
// grade with no valid answer counts as unknown.
func TestTries_GraderMissMakesUnknown(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name    string
		answer  func(of domain.GradeOf) domain.RunResult
		noFinal bool
		grades  []domain.GradeOf
		reason  string
	}{
		{
			name:   "the original read as fixed",
			answer: func(of domain.GradeOf) domain.RunResult { return grade(of, domain.PastTryFixed) },
			grades: []domain.GradeOf{domain.GradeOriginal, domain.GradeFinal}, reason: "misread the item's original result",
		},
		{
			name:   "the final read as not fixed",
			answer: func(of domain.GradeOf) domain.RunResult { return grade(of, domain.PastTryNotFixed) },
			grades: []domain.GradeOf{domain.GradeOriginal, domain.GradeFinal}, reason: "misread the item's final result",
		},
		{
			name: "no valid answer on the final",
			answer: func(of domain.GradeOf) domain.RunResult {
				if of == domain.GradeFinal {
					return domain.RunResult{Status: domain.RunSucceeded, Output: json.RawMessage(`{"verdict":"maybe"}`), CostUSD: gradeCost}
				}

				return rightGrade(of)
			},
			grades: []domain.GradeOf{domain.GradeOriginal, domain.GradeFinal}, reason: "no valid answer",
		},
		{
			name: "no final result", answer: rightGrade, noFinal: true,
			grades: []domain.GradeOf{domain.GradeOriginal}, reason: "no final accepted result",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := newTryWorld(t, 1)
			org := w.orgs[0]

			var it domain.Item
			if tt.noFinal {
				it = w.unacceptedItem(org)
			} else {
				it = w.gradedItem(org, rt0.Add(-2*time.Hour), sendBack)
			}

			w.propose(org, writerPath, it)
			w.sweep()

			w.runner.answer = func(ctx context.Context, req domain.RunRequest) (domain.RunResult, error) {
				if req.Bundle == domain.TaskKindGrade {
					return w.recorded(ctx, req, tt.answer(req.GradeOf)), nil
				}

				return domain.RunResult{Status: domain.RunSucceeded, Output: json.RawMessage(`{}`), CostUSD: 0.25, RunID: tryRunID}, nil
			}

			got := w.onlyTry(org)
			if got.Verdict != domain.PastTryUnknown || !strings.Contains(got.Reason, tt.reason) || got.CostUSD != 0.25 {
				t.Errorf("try = %+v; want unknown with a reason naming %q, the try's cost alone", got, tt.reason)
			}

			if g := gradesOf(w.runner.requests()); !slices.Equal(g, tt.grades) {
				t.Errorf("grades = %v; want %v and none of the try", g, tt.grades)
			}
		})
	}
}

// unacceptedItem is a past item of org whose one build run, a claim's first, failed: it has no
// final accepted result.
func (w *tryWorld) unacceptedItem(org domain.OrgID) domain.Item {
	w.t.Helper()

	ctx := w.ctx(org)

	it, err := w.store.CreateItem(ctx, domain.NewItem{FactoryID: w.factories[org], Title: "past", Station: stBuild})
	if err != nil {
		w.t.Fatal(err)
	}

	at := rt0.Add(-time.Hour)

	id, err := w.store.StartRun(ctx, ports.NewRun{FactoryID: w.factories[org], ItemID: it.ID, Station: stBuild, Agent: tryAgent, StartedAt: at})
	if err != nil {
		w.t.Fatal(err)
	}

	if err := w.store.RecordRunInput(ctx, id, domain.RunInput{Brief: "the brief", Repositories: []domain.RunRepository{{BranchHead: tryHead}}}); err != nil {
		w.t.Fatal(err)
	}

	if err := w.store.FinishRun(ctx, id, domain.RunResult{Status: domain.RunFailed, CostUSD: 100}, at.Add(time.Minute)); err != nil {
		w.t.Fatal(err)
	}

	return it
}

// The grader is checked once per proposal and item: a second try of the item (Try again) reads
// the stored checks and runs one grade, of itself.
func TestTries_GraderChecksOncePerItem(t *testing.T) {
	t.Parallel()

	w := newTryWorld(t, 1)
	org := w.orgs[0]
	it := w.gradedItem(org, rt0.Add(-2*time.Hour), sendBack)
	p := w.propose(org, writerPath, it)
	w.sweep()

	if n := w.work(); n != 1 {
		t.Fatalf("Work finished %d tries; want 1", n)
	}

	if _, err := w.store.AddPastTries(w.ctx(org), w.factories[org], p.ID, []domain.PastTryAdd{{ItemID: it.ID, Tries: 1}}, rt0); err != nil {
		t.Fatal(err)
	}

	if n := w.work(); n != 1 {
		t.Fatalf("Work finished %d tries; want the second", n)
	}

	want := []domain.GradeOf{domain.GradeOriginal, domain.GradeFinal, domain.GradeTry, domain.GradeTry}
	if g := gradesOf(w.runner.requests()); !slices.Equal(g, want) {
		t.Errorf("grades = %v; want %v", g, want)
	}

	if checks, err := w.store.ProposalGraderChecks(w.ctx(org), p.ID); err != nil || len(checks) != 2 {
		t.Errorf("checks = %+v, %v; want the item's two", checks, err)
	}

	for _, tr := range w.tries(org, p) {
		if tr.Verdict != domain.PastTryFixed {
			t.Errorf("try %d = %+v; want fixed", tr.Attempt, tr)
		}
	}
}

// A try whose agent run failed at a station that is not a gate is not fixed, as it passes no code
// grading: its output is not validated and the grader never sees it.
func TestTries_FailedRunIsNotFixed(t *testing.T) {
	t.Parallel()

	w := newTryWorld(t, 1)
	org := w.orgs[0]
	w.defs.build(org, func(s *domain.Station) { s.Output, s.Checks = contractNote, []string{"make test"} })
	w.queued(org, 1)
	w.runner.answer = func(context.Context, domain.RunRequest) (domain.RunResult, error) {
		return domain.RunResult{Status: domain.RunFailed, Error: "the agent gave up", Diff: tryDiff, RunID: tryRunID, CostUSD: 0.25}, nil
	}

	got := w.onlyTry(org)
	if got.Verdict != domain.PastTryNotFixed || !strings.Contains(got.Reason, "the agent gave up") || got.Diff != tryDiff || got.CostUSD != 0.25 {
		t.Errorf("try = %+v; want not fixed with the run's error as its reason, its diff and cost", got)
	}

	if w.defs.validated != 0 || len(gradesOf(w.runner.requests())) != 0 {
		t.Errorf("%d validations, grades %v; want none", w.defs.validated, gradesOf(w.runner.requests()))
	}
}

// A try whose run used up Jonah's share waits under its lease with its run recorded, and its
// proposal's tries show budget_used; a later claim with budget left grades it from the recorded
// run without running it again.
func TestTries_GraderWaitsForBudget(t *testing.T) {
	t.Parallel()

	w := newTryWorld(t, 1)
	org := w.orgs[0]
	f := w.factories[org]
	it := w.gradedItem(org, rt0.Add(-2*time.Hour), sendBack) // the line spent $101: Jonah's 5% is $5.05
	p := w.propose(org, writerPath, it)
	w.sweep()

	w.runner.answer = func(ctx context.Context, req domain.RunRequest) (domain.RunResult, error) {
		if req.Bundle == domain.TaskKindGrade {
			return w.recorded(ctx, req, rightGrade(req.GradeOf)), nil
		}

		w.jonahSpent(ctx, f, 6)

		return domain.RunResult{Status: domain.RunSucceeded, Output: json.RawMessage(`{}`), CostUSD: 0.25, Diff: tryDiff, RunID: tryRunID}, nil
	}

	if n := w.work(); n != 0 {
		t.Fatalf("Work finished %d tries; want none past the budget", n)
	}

	waiting := w.tries(org, p)[0]
	if waiting.State != domain.PastTryRunning || waiting.TryRunID != tryRunID || waiting.Diff != tryDiff || len(gradesOf(w.runner.requests())) != 0 {
		t.Fatalf("try = %+v, grades %v; want running with its run recorded and no grade", waiting, gradesOf(w.runner.requests()))
	}

	if st := w.triedState(org, p); st != jonah.TriedBudgetUsed {
		t.Errorf("tried state = %s; want budget_used", st)
	}

	w.pastItem(org, rt0, false) // $100 more of the line's spend: $10.05 of share
	w.clock.Advance(tryTimeout + time.Minute + time.Second)

	if n := w.work(); n != 1 {
		t.Fatalf("Work finished %d tries; want the waiting one", n)
	}

	got := w.tries(org, p)[0]
	if got.Verdict != domain.PastTryFixed || got.CostUSD != 0.25+gradeCost || got.Attempts != 2 {
		t.Errorf("try = %+v; want fixed on its second attempt, its cost with the grade's", got)
	}

	if reqs := w.runner.requests(); len(reqs)-len(gradesOf(reqs)) != 1 {
		t.Errorf("%d try runs; want the one, never run again", len(reqs)-len(gradesOf(reqs)))
	}
}

// triedState is where p's tries are, as its page shows them, with Jonah's budget now.
func (w *tryWorld) triedState(org domain.OrgID, p domain.Proposal) jonah.TriedState {
	w.t.Helper()

	spend, err := w.store.JonahSpendSince(w.ctx(org), p.FactoryID, w.clock.Now().Add(-jonah.BudgetWindow))
	if err != nil {
		w.t.Fatal(err)
	}

	tried, _ := jonah.TriedOf(w.tries(org, p), domain.DefaultJonahPolicy().Budget(spend))

	return tried.State
}

// jonahSpent records a Jonah run of factory f that cost usd.
func (w *tryWorld) jonahSpent(ctx context.Context, f domain.FactoryID, usd float64) {
	w.t.Helper()

	id, err := w.store.StartRun(ctx, ports.NewRun{FactoryID: f, Agent: "jonah", StartedAt: w.clock.Now(), Kind: domain.RunKindJonah})
	if err != nil {
		w.t.Fatal(err)
	}

	if err := w.store.FinishRun(ctx, id, domain.RunResult{Status: domain.RunSucceeded, CostUSD: usd}, w.clock.Now()); err != nil {
		w.t.Fatal(err)
	}
}

// keep keeps it as a test of org's factory at station.
func (w *tryWorld) keep(org domain.OrgID, it domain.Item, station string) domain.KeptTest {
	w.t.Helper()

	k, err := w.store.KeepTest(w.ctx(org), domain.NewKeptTest{FactoryID: w.factories[org], ItemID: it.ID, Station: station, KeptBy: domain.LocalUser, KeptAt: rt0})
	if err != nil {
		w.t.Fatal(err)
	}

	return k
}

// A proposal that reaches a triable station gets a try of each kept test there, next to its cited
// items', with the content key of what it runs on; a kept test at a station it does not reach gets
// none, and a proposal that cites no item still tries the kept tests.
func TestTries_SweepQueuesKeptTestsOfReachedStations(t *testing.T) {
	t.Parallel()

	w := newTryWorld(t, 1)
	org := w.orgs[0]
	cited, _ := w.pastItem(org, rt0.Add(-3*time.Hour), true)
	kept, keptRun := w.pastItem(org, rt0.Add(-2*time.Hour), true)
	k := w.keep(org, kept, stBuild)
	w.keep(org, cited, stSpec) // a human station the proposal does not reach

	p := w.propose(org, writerPath, cited)
	w.sweep()
	w.sweep()

	got := w.tries(org, p)
	if len(got) != 2 || got[0].KeptTestID != "" || got[0].ItemID != cited.ID {
		t.Fatalf("tries = %+v; want the cited item's, then the kept test's", got)
	}

	if kt := got[1]; kt.KeptTestID != k.ID || kt.ItemID != kept.ID || kt.OriginalRunID != keptRun || kt.Station != stBuild ||
		kt.ContentKey != "key-"+string(tryVersion)+"-"+writerPath || kt.ProposalID != p.ID {
		t.Errorf("kept-test try = %+v; want test %s of its item's run at %s with the proposal's content key", kt, k.ID, stBuild)
	}

	alone := w.propose(org, writerPath)
	w.sweep()

	if got := w.tries(org, alone); len(got) != 1 || got[0].KeptTestID != k.ID {
		t.Errorf("tries of a proposal citing no item = %+v; want the kept test's alone", got)
	}
}

// A kept test passes (fixed) only when its try passes code grading and, at a gate, approves as
// the item did there; it fails (not fixed, with why) otherwise, a failed run included, and an
// infrastructure failure is error, never failed. A publish's try runs on its own version and edits.
func TestTries_KeptTestPassesOnlyWithChecksAndGateVerdict(t *testing.T) {
	t.Parallel()

	ok := func(output string) domain.RunResult {
		return domain.RunResult{Status: domain.RunSucceeded, Output: json.RawMessage(output), RunID: tryRunID}
	}

	for _, tt := range []struct {
		name    string
		gate    bool
		res     domain.RunResult
		err     error
		verdict domain.PastTryVerdict
		why     string
	}{
		{name: "passing checks", res: ok(`{}`), verdict: domain.PastTryFixed, why: "it passed"},
		{name: "a failing check", res: domain.RunResult{Status: domain.RunSucceeded, Output: json.RawMessage(`{}`), FailedCheck: &domain.FailedCheck{
			Command: checksMakeTest[0], ExitCode: 1,
		}}, verdict: domain.PastTryNotFixed, why: "check `make test` exited 1"},
		{name: "a failed run", res: domain.RunResult{Status: domain.RunFailed, Error: "max turns"}, verdict: domain.PastTryNotFixed, why: "the run failed: max turns"},
		{name: "a lost box", err: fmt.Errorf("agent: %w", ports.ErrSandboxLost), verdict: domain.PastTryError, why: "sandbox"},
		{name: "a gate that approves", gate: true, res: ok(`{"decision":"approve"}`), verdict: domain.PastTryFixed, why: "approved"},
		{name: "a gate that rejects", gate: true, res: ok(`{"decision":"reject","reason":"no tests"}`), verdict: domain.PastTryNotFixed, why: "the gate rejected it: no tests"},
		{name: "a gate with no verdict", gate: true, res: ok(`{}`), verdict: domain.PastTryNotFixed, why: "not a gate verdict"},
	} {
		w := newTryWorld(t, 1)
		org := w.orgs[0]
		w.defs.build(org, func(s *domain.Station) {
			s.Checks = checksMakeTest
			if tt.gate {
				s.Gate = &domain.Gate{OnReject: stSpec, MaxRework: 1}
			}
		})

		it, run := w.pastItem(org, rt0.Add(-time.Hour), true)
		k := w.keep(org, it, stBuild)
		edits := []domain.ProposalEdit{{Path: writerPath, Content: "# writer, test first\n", BaseSHA256: strings.Repeat("a", 64)}}

		_, err := w.store.QueuePublishTries(w.ctx(org), w.factories[org], tryVersion, edits, []domain.NewPastTry{{
			KeptTestID: k.ID, ContentKey: "key", ItemID: it.ID, Station: stBuild, OriginalRunID: run, Model: reviewModel,
		}}, rt0)
		if err != nil {
			t.Fatal(err)
		}

		w.runner.answer = func(context.Context, domain.RunRequest) (domain.RunResult, error) { return tt.res, tt.err }

		if n := w.work(); n != 1 {
			t.Fatalf("%s: Work finished %d tries; want 1", tt.name, n)
		}

		got, err := w.store.KeptTestTries(w.ctx(org), w.factories[org], "key")
		if err != nil || len(got) != 1 {
			t.Fatalf("%s: KeptTestTries = %+v, %v", tt.name, got, err)
		}

		text := got[0].Reason
		if tt.verdict == domain.PastTryError {
			text = got[0].Error
		}

		if got[0].Verdict != tt.verdict || !strings.Contains(text, tt.why) {
			t.Errorf("%s: try = %+v; want verdict %s saying %q", tt.name, got[0], tt.verdict, tt.why)
		}
	}
}

// checksMakeTest are the grading tests' station checks.
var checksMakeTest = []string{checkMakeTest}

// checkMakeTest is a station check the grading tests name.
const checkMakeTest = "make test"

// A try at a gate station, where the proposal changes the gate's own agent, rules or contract, is
// graded by what people did with the item after its original run, never by a model nor by the
// gate's own reasons: its decision against a person's send-back or a merge no person undid.
func TestTries_GateChangeGradedByPeople(t *testing.T) {
	t.Parallel()

	const gateReason = "the gate agent's own words"

	person := func(kv ...string) map[string]any {
		d := map[string]any{"actor": "user:ada"}
		for i := 0; i+1 < len(kv); i += 2 {
			d[kv[i]] = kv[i+1]
		}

		return d
	}

	for _, tt := range []struct {
		name    string
		events  func(f domain.FactoryID, it domain.ItemID, at time.Time) []domain.Event
		output  string
		verdict domain.PastTryVerdict
		reason  string
	}{
		{
			name: "merged",
			events: func(f domain.FactoryID, it domain.ItemID, at time.Time) []domain.Event {
				return []domain.Event{{FactoryID: f, ItemID: it, At: at.Add(time.Hour), Kind: domain.EventMerged, Station: stTryDone, Actor: "ada", Data: person()}}
			},
			output: `{"decision":"approve"}`, verdict: domain.PastTryFixed, reason: "merged",
		},
		{
			name: "sent back by a person",
			events: func(f domain.FactoryID, it domain.ItemID, at time.Time) []domain.Event {
				return []domain.Event{{
					FactoryID: f, ItemID: it, At: at.Add(time.Hour), Kind: domain.EventMoved, Station: stTryDone, Actor: "ada",
					Data: person("from", stTryDone, "to", stSpec, "reason", "it broke the build"),
				}}
			},
			output: `{"decision":"approve"}`, verdict: domain.PastTryNotFixed, reason: "it broke the build",
		},
		{
			name:   "not a gate verdict",
			events: func(domain.FactoryID, domain.ItemID, time.Time) []domain.Event { return nil },
			output: `{}`, verdict: domain.PastTryNotFixed, reason: "not a gate verdict",
		},
		{
			name:   "still open",
			events: func(domain.FactoryID, domain.ItemID, time.Time) []domain.Event { return nil },
			output: `{"decision":"approve"}`, verdict: domain.PastTryUnknown, reason: "no person",
		},
		{
			name: "gate's own rejection ignored",
			events: func(f domain.FactoryID, it domain.ItemID, at time.Time) []domain.Event {
				return []domain.Event{
					{
						FactoryID: f, ItemID: it, At: at.Add(time.Minute), Kind: domain.EventGateRejected, Station: stBuild, Actor: tryAgent,
						Data: map[string]any{"reason": gateReason},
					},
					{
						FactoryID: f, ItemID: it, At: at.Add(time.Minute), Kind: domain.EventMoved, Station: stBuild, Actor: domain.ActorEngine,
						Data: map[string]any{"from": stBuild, "to": stSpec},
					},
				}
			},
			output: `{"decision":"reject","reason":"no tests"}`, verdict: domain.PastTryUnknown, reason: "no person",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := newTryWorld(t, 1)
			org := w.orgs[0]
			w.defs.build(org, func(s *domain.Station) { s.Gate = &domain.Gate{OnReject: stSpec} })

			started := rt0.Add(-time.Hour)
			it, _ := w.pastItem(org, started, true)

			if err := w.store.AppendEvents(w.ctx(org), tt.events(w.factories[org], it.ID, started)...); err != nil {
				t.Fatal(err)
			}

			w.propose(org, writerPath, it)
			w.sweep()

			w.runner.answer = func(context.Context, domain.RunRequest) (domain.RunResult, error) {
				return domain.RunResult{Status: domain.RunSucceeded, Output: json.RawMessage(tt.output), Diff: tryDiff, RunID: tryRunID, CostUSD: 0.25}, nil
			}

			got := w.onlyTry(org)
			if got.State != domain.PastTryDone || got.Verdict != tt.verdict || !strings.Contains(got.Reason, tt.reason) || got.CostUSD != 0.25 {
				t.Errorf("try = %+v; want verdict %s with a reason naming %q", got, tt.verdict, tt.reason)
			}

			if strings.Contains(got.Reason, gateReason) {
				t.Errorf("reason %q quotes the gate agent's own reason", got.Reason)
			}
		})
	}
}
