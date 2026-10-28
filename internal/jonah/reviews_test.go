package jonah_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/jonah"
)

const (
	reviewFactory = domain.FactoryID("00000000-0000-4000-8000-0000000000f1")
	reviewVersion = domain.DefinitionVersionID("version-3")
	stBuild       = "build"
	stShip        = "ship"
	modelConn     = "model-access"
	reviewModel   = "claude-sonnet-5-5"
	writerPath    = "agents/writer.md"
	writerText    = "Write the change.\n"
	reviewTimeout = 10 * time.Minute
	reviewMargin  = time.Minute
)

// rt0 is the review world's now.
var rt0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// reviewDefs is ReviewDefinitions over one factory's line and files.
type reviewDefs struct {
	mu    sync.Mutex
	line  domain.Line
	files map[string][]byte
}

func (d *reviewDefs) CurrentLines(context.Context) ([]domain.Line, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	return []domain.Line{d.line}, nil
}

func (d *reviewDefs) Line(_ context.Context, id domain.DefinitionVersionID) (domain.Line, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if id != d.line.VersionID {
		return domain.Line{}, ports.ErrNotFound
	}

	return d.line, nil
}

func (d *reviewDefs) VersionFiles(context.Context, domain.DefinitionVersionID) (map[string][]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.files, nil
}

// taskRunner records each request and answers it with answer.
type taskRunner struct {
	mu     sync.Mutex
	reqs   []domain.RunRequest
	answer func(ctx context.Context, req domain.RunRequest) (domain.RunResult, error)
}

func (r *taskRunner) RunTask(ctx context.Context, req domain.RunRequest) (domain.RunResult, error) {
	r.mu.Lock()
	r.reqs = append(r.reqs, req)
	r.mu.Unlock()

	return r.answer(ctx, req)
}

func (r *taskRunner) requests() []domain.RunRequest {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]domain.RunRequest(nil), r.reqs...)
}

type reviewWorld struct {
	t      *testing.T
	org    domain.OrgID
	store  *porttest.Store
	clock  *porttest.Clock
	defs   *reviewDefs
	runner *taskRunner
	log    *bytes.Buffer
	loop   *jonah.Reviews
}

func newReviewWorld(t *testing.T) *reviewWorld {
	t.Helper()

	clock := porttest.NewClock(rt0)
	store := porttest.NewStore()
	tn := porttest.NewTenancy(clock.Now).WithStore(store)

	o, err := tn.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: "acme", Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}

	store.AddFactory(o.ID, reviewFactory)

	policy := domain.DefaultJonahPolicy()
	policy.Model = reviewModel

	w := &reviewWorld{
		t: t, org: o.ID, store: store, clock: clock, log: &bytes.Buffer{},
		defs: &reviewDefs{
			line: domain.Line{
				OrgID: o.ID, FactoryID: reviewFactory, VersionID: reviewVersion, Number: 3,
				Runtime: domain.Runtime{ModelConnection: modelConn}, Jonah: policy,
			},
			files: map[string][]byte{writerPath: []byte(writerText), "factory.yaml": []byte("metadata: {}\n")},
		},
		runner: &taskRunner{answer: func(context.Context, domain.RunRequest) (domain.RunResult, error) {
			return succeeded(`{"observations": [], "proposals": []}`), nil
		}},
	}
	w.loop = jonah.NewReviews(jonah.ReviewDeps{
		Orgs: tn, Jonah: store, Definitions: w.defs, Runner: w.runner, Clock: clock,
		Log: slog.New(slog.NewJSONHandler(w.log, nil)),
	}, jonah.ReviewConfig{Timeout: reviewTimeout, Margin: reviewMargin})

	return w
}

func succeeded(output string) domain.RunResult {
	return domain.RunResult{Status: domain.RunSucceeded, Output: json.RawMessage(output), CostUSD: 0.5}
}

func (w *reviewWorld) ctx() context.Context { return domain.WithOrg(w.t.Context(), w.org) }

// sentBack gives a person's rejection, with reason, of a new item at station at.
func (w *reviewWorld) sentBack(station string, at time.Time, reason string) {
	w.t.Helper()

	it, err := w.store.CreateItem(w.ctx(), domain.NewItem{
		FactoryID: reviewFactory, Title: "an item", Type: "task", Station: station, State: domain.StateQueued,
		Class: domain.ClassStandard, CreatedAt: at.Add(-time.Hour),
	})
	if err != nil {
		w.t.Fatalf("CreateItem: %v", err)
	}

	ada := domain.Actor{Kind: domain.ActorUser, ID: "ada", Name: "Ada"}
	if err := w.store.AppendEvents(w.ctx(), domain.Event{
		FactoryID: reviewFactory, ItemID: it.ID, At: at, Kind: domain.EventRejected, Station: station, Actor: ada.Name,
		Data: map[string]any{"actor": ada.Ref(), "reason": reason},
	}); err != nil {
		w.t.Fatalf("AppendEvents: %v", err)
	}
}

// sentBacks gives n rejections at build within the last day.
func (w *reviewWorld) sentBacks(n int) {
	w.t.Helper()

	for i := range n {
		w.sentBack(stBuild, rt0.Add(-time.Duration(i+1)*time.Hour), "missed the test")
	}
}

// span moves a new item through station: it enters at entered and leaves at left.
func (w *reviewWorld) span(station string, entered, left time.Time) {
	w.t.Helper()

	it, err := w.store.CreateItem(w.ctx(), domain.NewItem{
		FactoryID: reviewFactory, Title: "a passing item", Type: "task", Station: station, State: domain.StateQueued,
		Class: domain.ClassStandard, CreatedAt: entered,
	})
	if err != nil {
		w.t.Fatalf("CreateItem: %v", err)
	}

	if _, err := w.store.Transition(w.ctx(), ports.Transition{
		ItemID: it.ID, From: domain.Position{Station: station, State: domain.StateQueued},
		To: domain.Position{Station: stShip, State: domain.StateQueued}, At: left,
	}); err != nil {
		w.t.Fatalf("Transition: %v", err)
	}
}

// spend records a finished review run of the factory that cost usd, started at.
func (w *reviewWorld) spend(usd float64, at time.Time) {
	w.t.Helper()

	id, err := w.store.StartRun(w.ctx(), ports.NewRun{FactoryID: reviewFactory, Agent: jonah.ReviewAgent, StartedAt: at, Kind: domain.RunKindJonah})
	if err != nil {
		w.t.Fatalf("StartRun: %v", err)
	}

	if err := w.store.FinishRun(w.ctx(), id, domain.RunResult{Status: domain.RunSucceeded, CostUSD: usd}, at.Add(time.Minute)); err != nil {
		w.t.Fatalf("FinishRun: %v", err)
	}
}

// queue queues a review by a person, over the first window.
func (w *reviewWorld) queue(trigger domain.JonahTrigger) domain.JonahReview {
	w.t.Helper()

	r, err := w.store.QueueJonahReview(w.ctx(), domain.NewJonahReview{
		FactoryID: reviewFactory, VersionID: reviewVersion, Trigger: trigger, WindowStart: rt0.Add(-jonah.FirstWindow), WindowEnd: rt0,
		By: domain.LocalUser, At: rt0,
	})
	if err != nil {
		w.t.Fatalf("QueueJonahReview: %v", err)
	}

	return r
}

func (w *reviewWorld) reviews() []domain.JonahReview {
	w.t.Helper()

	rs, err := w.store.JonahReviews(w.ctx(), reviewFactory, 10)
	if err != nil {
		w.t.Fatalf("JonahReviews: %v", err)
	}

	return rs
}

func (w *reviewWorld) work() int {
	w.t.Helper()

	n, err := w.loop.Work(w.t.Context())
	if err != nil {
		w.t.Fatalf("Work: %v", err)
	}

	return n
}

// events are the factory's sent-back events in the first window.
func (w *reviewWorld) events() []domain.SentBack {
	w.t.Helper()

	es, err := w.store.SentBackBetween(w.ctx(), reviewFactory, rt0.Add(-jonah.FirstWindow), rt0, 100)
	if err != nil {
		w.t.Fatalf("SentBackBetween: %v", err)
	}

	return es
}

// logged are the log records with message msg.
func (w *reviewWorld) logged(msg string) []map[string]any {
	var out []map[string]any

	for line := range strings.SplitSeq(strings.TrimSpace(w.log.String()), "\n") {
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) == nil && rec["msg"] == msg {
			out = append(out, rec)
		}
	}

	return out
}

func TestReviews_Sweep(t *testing.T) {
	cases := []struct {
		name  string
		setup func(w *reviewWorld)
		// want is the trigger of the factory's newest review, "" for none.
		want domain.JonahTrigger
		// count is how many reviews the factory has after the sweep.
		count int
	}{
		{name: "below the threshold", setup: func(w *reviewWorld) { w.sentBacks(4) }},
		{name: "at the threshold", setup: func(w *reviewWorld) { w.sentBacks(5) }, want: domain.JonahThreshold, count: 1},
		{name: "budget spent", setup: func(w *reviewWorld) {
			w.sentBacks(5)
			w.spend(6, rt0.Add(-6*24*time.Hour))
			w.spend(4, rt0.Add(-time.Hour))
		}},
		{name: "budget spent before the window", setup: func(w *reviewWorld) {
			w.sentBacks(5)
			w.spend(10, rt0.Add(-jonah.BudgetWindow-time.Hour))
		}, want: domain.JonahThreshold, count: 1},
		{name: "review in flight", setup: func(w *reviewWorld) {
			w.sentBacks(5)
			w.queue(domain.JonahManual)
		}, want: domain.JonahManual, count: 1},
		{name: "touches before the last review's window ended", setup: func(w *reviewWorld) {
			w.sentBacks(5)
			r := w.queue(domain.JonahManual)
			claimed, err := w.store.ClaimJonahReview(w.ctx(), rt0, time.Minute)
			if err != nil || claimed.ID != r.ID {
				t.Fatalf("ClaimJonahReview = %+v, %v", claimed, err)
			}
			if _, _, err := w.store.FinishJonahReview(w.ctx(), claimed, domain.JonahReviewResult{State: domain.JonahDone, At: rt0}); err != nil {
				t.Fatalf("FinishJonahReview: %v", err)
			}
			w.clock.Advance(time.Hour)
		}, want: domain.JonahManual, count: 1},
		{name: "learning off", setup: func(w *reviewWorld) {
			w.sentBacks(5)
			w.defs.line.Jonah.Enabled = false
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newReviewWorld(t)
			tc.setup(w)

			if err := w.loop.Sweep(t.Context()); err != nil {
				t.Fatalf("Sweep: %v", err)
			}

			rs := w.reviews()
			if len(rs) != tc.count {
				t.Fatalf("reviews = %+v, want %d", rs, tc.count)
			}

			if tc.count == 0 {
				return
			}

			if rs[0].Trigger != tc.want {
				t.Errorf("trigger = %s, want %s", rs[0].Trigger, tc.want)
			}

			if tc.want == domain.JonahThreshold {
				now := w.clock.Now()
				if rs[0].VersionID != reviewVersion || !rs[0].WindowStart.Equal(now.Add(-jonah.FirstWindow)) || !rs[0].WindowEnd.Equal(now) ||
					rs[0].State != domain.JonahPending || rs[0].CreatedBy == "" {
					t.Errorf("threshold review = %+v, want a pending review of %s over the first window", rs[0], reviewVersion)
				}
			}
		})
	}
}

func TestReviews_Work(t *testing.T) {
	t.Run("claims and runs one jonah run", func(t *testing.T) {
		w := newReviewWorld(t)
		queued := w.queue(domain.JonahManual)

		var lease time.Time

		w.runner.answer = func(context.Context, domain.RunRequest) (domain.RunResult, error) {
			lease = w.reviews()[0].LeaseUntil

			return succeeded(`{"observations": [], "proposals": []}`), nil
		}

		if n := w.work(); n != 1 {
			t.Fatalf("Work finished %d, want 1", n)
		}

		reqs := w.runner.requests()
		if len(reqs) != 1 {
			t.Fatalf("runs = %d, want 1", len(reqs))
		}

		want := domain.RunRequest{
			OrgID: w.org, FactoryID: reviewFactory, Version: reviewVersion, Kind: domain.RunKindJonah, Agent: jonah.ReviewAgent,
			Model: reviewModel, Bundle: domain.TaskKindJonah, WindowStart: queued.WindowStart, WindowEnd: queued.WindowEnd, Manual: true,
			ModelConnection: modelConn, Size: domain.SandboxSmall, Timeout: reviewTimeout,
		}
		if got := reqs[0]; !runRequestEqual(got, want) {
			t.Errorf("run request =\n%+v\nwant\n%+v", got, want)
		}

		if err := reqs[0].Check(); err != nil {
			t.Errorf("run request Check: %v", err)
		}

		if !lease.Equal(rt0.Add(reviewTimeout + reviewMargin)) {
			t.Errorf("lease until %v, want %v", lease, rt0.Add(reviewTimeout+reviewMargin))
		}

		if r := w.reviews()[0]; r.State != domain.JonahDone || r.Attempts != 1 {
			t.Errorf("review = %+v, want done after one attempt", r)
		}

		if n := w.work(); n != 0 || len(w.runner.requests()) != 1 {
			t.Errorf("a second Work finished %d and ran %d, want nothing more", n, len(w.runner.requests()))
		}
	})

	t.Run("threshold review is not manual", func(t *testing.T) {
		w := newReviewWorld(t)
		w.queue(domain.JonahThreshold)
		w.work()

		if reqs := w.runner.requests(); len(reqs) != 1 || reqs[0].Manual {
			t.Errorf("runs = %+v, want one that is not manual", reqs)
		}
	})

	failures := []struct {
		name  string
		setup func(w *reviewWorld)
		// error is in the failed review's error; runs is how many runs started.
		error string
		runs  int
	}{
		{name: "budget spent", setup: func(w *reviewWorld) { w.spend(10, rt0.Add(-time.Hour)) }, error: domain.ErrLearningBudgetExhausted.Error()},
		{name: "run failed", setup: func(w *reviewWorld) {
			w.runner.answer = func(context.Context, domain.RunRequest) (domain.RunResult, error) {
				return domain.RunResult{Status: domain.RunFailed, Error: "out of turns"}, nil
			}
		}, error: "out of turns", runs: 1},
		{name: "run not carried out", setup: func(w *reviewWorld) {
			w.runner.answer = func(context.Context, domain.RunRequest) (domain.RunResult, error) {
				return domain.RunResult{}, ports.ErrSandboxLost
			}
		}, error: "the review run failed", runs: 1},
		{name: "output does not parse", setup: func(w *reviewWorld) {
			w.runner.answer = func(context.Context, domain.RunRequest) (domain.RunResult, error) { return succeeded(`"nothing"`), nil }
		}, error: "does not parse", runs: 1},
		{name: "version gone", setup: func(w *reviewWorld) { w.defs.line.VersionID = "version-4" }, error: "is gone"},
	}

	for _, tc := range failures {
		t.Run(tc.name, func(t *testing.T) {
			w := newReviewWorld(t)
			w.queue(domain.JonahManual)
			tc.setup(w)

			if n := w.work(); n != 1 {
				t.Fatalf("Work finished %d, want 1", n)
			}

			if n := len(w.runner.requests()); n != tc.runs {
				t.Errorf("runs = %d, want %d", n, tc.runs)
			}

			r := w.reviews()[0]
			if r.State != domain.JonahFailed || !strings.Contains(r.Error, tc.error) {
				t.Errorf("review = %s %q, want failed with %q", r.State, r.Error, tc.error)
			}
		})
	}

	t.Run("lost lease", func(t *testing.T) {
		w := newReviewWorld(t)
		w.queue(domain.JonahManual)

		w.runner.answer = func(context.Context, domain.RunRequest) (domain.RunResult, error) {
			// The run outlives its lease and another instance claims the review.
			w.clock.Advance(reviewTimeout + reviewMargin + time.Second)

			if _, err := w.store.ClaimJonahReview(w.ctx(), w.clock.Now(), time.Hour); err != nil {
				t.Fatalf("second claim: %v", err)
			}

			return succeeded(`{"observations": [], "proposals": []}`), nil
		}

		n, err := w.loop.Work(t.Context())
		if err != nil || n != 0 {
			t.Fatalf("Work = %d, %v; want 0 and no error", n, err)
		}

		if len(w.logged("jonah review lease lost")) != 1 {
			t.Errorf("log = %s, want one jonah review lease lost", w.log)
		}

		if r := w.reviews()[0]; r.State != domain.JonahRunning || r.Attempts != 2 {
			t.Errorf("review = %+v, want running under the second claim", r)
		}
	})
}

// runRequestEqual compares the fields a review run sets.
func runRequestEqual(a, b domain.RunRequest) bool {
	return a.OrgID == b.OrgID && a.FactoryID == b.FactoryID && a.Version == b.Version && a.Kind == b.Kind && a.Agent == b.Agent &&
		a.Model == b.Model && a.Bundle == b.Bundle && a.WindowStart.Equal(b.WindowStart) && a.WindowEnd.Equal(b.WindowEnd) &&
		a.Manual == b.Manual && a.ModelConnection == b.ModelConnection && a.Size == b.Size && a.Timeout == b.Timeout &&
		a.Item == "" && a.Station == "" && a.Lease == nil && a.Repository == (domain.Repository{}) && len(a.Connections) == 0 &&
		a.Brief == "" && a.Branch == "" && a.Base == ""
}

func TestReviews_Finish(t *testing.T) {
	w := newReviewWorld(t)

	w.sentBack(stBuild, rt0.Add(-2*time.Hour), "missed the test")
	w.sentBack(stBuild, rt0.Add(-time.Hour), "no screenshot")

	for i := range 4 {
		entered := rt0.Add(-time.Duration(10+i) * time.Hour)
		w.span(stBuild, entered, entered.Add(time.Duration(i+1)*time.Hour))
	}

	r := w.queue(domain.JonahManual)
	ev := w.events()

	flow, err := w.store.StationFlow(w.ctx(), reviewFactory, stBuild, r.WindowStart, r.WindowEnd)
	if err != nil || flow.Passed != 4 || flow.SentBack != 2 || flow.LeadTimeP85 == 0 {
		t.Fatalf("StationFlow = %+v, %v; want 4 passed and 2 sent back", flow, err)
	}

	out := output{
		Observations: []observation{
			{Summary: "Tests are missing", Detail: "two items", Items: []string{"#1", "#2", "#99"}, Count: 2, Proposal: ptr(1)},
			{Summary: "Screenshots are missing", Detail: "", Items: []string{"#2"}, Count: 1},
		},
		Proposals: []proposal{
			newProposal("Ask for tests", writerPath, ev[0], domain.MetricSentBackRate),
			newProposal("Shorten the build", writerPath, ev[1], domain.MetricLeadTimeP85),
		},
	}
	out.Proposals[0].Evidence = append(out.Proposals[0].Evidence, evidence{Event: idOf(ev[1]), Reason: "no screenshot"})

	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}

	// The model's own before is ignored.
	answer := strings.ReplaceAll(string(raw), `"after":`, `"before": 99, "after":`)
	w.runner.answer = func(context.Context, domain.RunRequest) (domain.RunResult, error) { return succeeded(answer), nil }

	if n := w.work(); n != 1 {
		t.Fatalf("Work finished %d, want 1", n)
	}

	ps, err := w.store.Proposals(w.ctx(), ports.ProposalQuery{Factory: reviewFactory, Limit: 10})
	if err != nil || len(ps) != 2 {
		t.Fatalf("Proposals = %+v, %v; want 2", ps, err)
	}

	// Newest first: the second proposal is number 2.
	tests, shorter := ps[1], ps[0]

	if tests.Number != 1 || tests.Title != "Ask for tests" || tests.ReviewID != r.ID || tests.VersionID != reviewVersion ||
		tests.State != domain.ProposalProposed {
		t.Errorf("proposal 1 = %+v", tests)
	}

	wantEstimate := domain.ProposalEstimate{Metric: domain.MetricSentBackRate, Station: stBuild, Before: 0.5, After: 0.1, CostPerItemUSD: 0.02}
	if tests.Estimate != wantEstimate {
		t.Errorf("proposal 1 estimate = %+v, want %+v", tests.Estimate, wantEstimate)
	}

	if want := flow.LeadTimeP85.Hours(); shorter.Estimate.Before != want || shorter.Estimate.Metric != domain.MetricLeadTimeP85 {
		t.Errorf("proposal 2 estimate = %+v, want before %v hours", shorter.Estimate, want)
	}

	wantEvidence := []domain.ProposalEvidence{
		{ItemID: ev[0].ItemID, EventID: ev[0].EventID, Station: stBuild, Quote: ev[0].Reason},
		{ItemID: ev[1].ItemID, EventID: ev[1].EventID, Station: stBuild, Quote: "no screenshot"},
	}
	if len(tests.Evidence) != 2 || tests.Evidence[0] != wantEvidence[0] || tests.Evidence[1] != wantEvidence[1] {
		t.Errorf("proposal 1 evidence = %+v, want %+v", tests.Evidence, wantEvidence)
	}

	if len(tests.Files) != 1 || tests.Files[0].Path != writerPath || tests.Files[0].BaseSHA256 != string(domain.HashBlob([]byte(writerText))) {
		t.Errorf("proposal 1 files = %+v", tests.Files)
	}

	obs, err := w.store.Observations(w.ctx(), reviewFactory, 10)
	if err != nil || len(obs) != 2 {
		t.Fatalf("Observations = %+v, %v; want 2", obs, err)
	}

	byName := map[string]domain.Observation{}
	for _, o := range obs {
		byName[o.Summary] = o
	}

	missing := byName["Tests are missing"]
	if missing.State != domain.ObservationProposed || missing.ProposalID != shorter.ID || len(missing.ItemIDs) != 2 || missing.Count != 2 {
		t.Errorf("observation of proposal index 1 = %+v, want proposed as %s with the two known items", missing, shorter.ID)
	}

	if s := byName["Screenshots are missing"]; s.State != domain.ObservationWatching || s.ProposalID != "" {
		t.Errorf("observation without a proposal = %+v, want watching", s)
	}

	if r := w.reviews()[0]; r.State != domain.JonahDone {
		t.Errorf("review = %+v, want done", r)
	}
}

// output mirrors the worker's JonahReply.
type output struct {
	Observations []observation `json:"observations"`
	Proposals    []proposal    `json:"proposals"`
}

type observation struct {
	Summary  string   `json:"summary"`
	Detail   string   `json:"detail"`
	Items    []string `json:"items"`
	Count    int      `json:"count"`
	Proposal *int     `json:"proposal,omitempty"`
}

type proposal struct {
	Title    string                `json:"title"`
	Summary  string                `json:"summary"`
	Files    []domain.ProposalFile `json:"files"`
	Evidence []evidence            `json:"evidence"`
	Estimate estimate              `json:"estimate"`
}

type evidence struct {
	Event  string `json:"event"`
	Reason string `json:"reason"`
}

type estimate struct {
	Metric    domain.ProposalMetric `json:"metric"`
	Station   string                `json:"station"`
	After     float64               `json:"after"`
	CostDelta float64               `json:"cost_per_item_usd_delta"`
}

func ptr(n int) *int { return &n }

func idOf(s domain.SentBack) string { return strconv.FormatInt(s.EventID, 10) }

// newProposal edits path, as the version has it, on the evidence of e, to move metric at build.
func newProposal(title, path string, e domain.SentBack, metric domain.ProposalMetric) proposal {
	return proposal{
		Title: title, Summary: "Say it in the prompt.",
		Files:    []domain.ProposalFile{{Path: path, BaseSHA256: string(domain.HashBlob([]byte(writerText))), Content: "Write the change and its tests.\n"}},
		Evidence: []evidence{{Event: idOf(e), Reason: e.Reason}},
		Estimate: estimate{Metric: metric, Station: stBuild, After: 0.1, CostDelta: 0.02},
	}
}
