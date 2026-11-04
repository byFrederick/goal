package jonah_test

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"log/slog"
	"reflect"
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

// The review runs the fake runner names.
const (
	firstRun  = domain.RunID("run-1")
	secondRun = domain.RunID("run-2")
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

	return newReviewWorldAt(t, rt0)
}

// newReviewWorldAt is a review world whose clock starts at now.
func newReviewWorldAt(t *testing.T, now time.Time) *reviewWorld {
	t.Helper()

	clock := porttest.NewClock(now)
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

// at moves the world's clock forward to now.
func (w *reviewWorld) at(now time.Time) {
	w.t.Helper()

	if now.Before(w.clock.Now()) {
		w.t.Fatalf("at %s: the clock is already at %s", now, w.clock.Now())
	}

	w.clock.Advance(now.Sub(w.clock.Now()))
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

// queue queues a review by a person, over the first window, as of the clock.
func (w *reviewWorld) queue(trigger domain.JonahTrigger) domain.JonahReview {
	w.t.Helper()

	r, err := w.store.QueueJonahReview(w.ctx(), domain.NewJonahReview{
		FactoryID: reviewFactory, VersionID: reviewVersion, Trigger: trigger, WindowStart: w.clock.Now().Add(-jonah.FirstWindow),
		WindowEnd: w.clock.Now(), By: domain.LocalUser, At: w.clock.Now(),
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

// finished moves a new item to done at at: evidence for a review whose window holds at.
func (w *reviewWorld) finished(at time.Time) {
	w.t.Helper()

	it, err := w.store.CreateItem(w.ctx(), domain.NewItem{
		FactoryID: reviewFactory, Title: "a finished item", Type: "task", Station: stBuild, State: domain.StateQueued,
		Class: domain.ClassStandard, CreatedAt: at.Add(-time.Hour),
	})
	if err != nil {
		w.t.Fatalf("CreateItem: %v", err)
	}

	if _, err := w.store.Transition(w.ctx(), ports.Transition{
		ItemID: it.ID, From: it.Position(), To: domain.Position{Station: "done", State: domain.StateDone}, At: at, DoneAt: at,
	}); err != nil {
		w.t.Fatalf("Transition: %v", err)
	}
}

// today is the review world's day, at UTC midnight; reviewFactory's slot is ReviewHour into it.
var today = time.Date(rt0.Year(), rt0.Month(), rt0.Day(), 0, 0, 0, 0, time.UTC)

func reviewSlot() time.Time {
	return today.Add(time.Duration(jonah.ReviewHour(reviewFactory)) * time.Hour)
}

func TestReviews_Sweep(t *testing.T) {
	t.Parallel()

	if h := jonah.ReviewHour(reviewFactory); h == 0 || !rt0.After(reviewSlot()) {
		t.Fatalf("reviewFactory's hour is %d: the cases need one after midnight and before rt0", h)
	}

	cases := []struct {
		name string
		// from is the world's now before setup, rt0 when zero.
		from  time.Time
		setup func(w *reviewWorld)
		// want is the trigger of the factory's latest review after the sweep, "" for none, and
		// start its window's start when it is the daily review.
		want  domain.JonahTrigger
		start func(w *reviewWorld) time.Time
	}{
		{name: "after the slot", setup: func(w *reviewWorld) { w.finished(rt0.Add(-time.Hour)) }, want: domain.JonahThreshold, start: firstWindow},
		{name: "before the slot", from: reviewSlot().Add(-3 * time.Hour), setup: func(w *reviewWorld) {
			w.finished(reviewSlot().Add(-2 * time.Hour))
			w.at(reviewSlot().Add(-time.Minute))
		}},
		{name: "no evidence", setup: func(w *reviewWorld) { w.sentBacks(5) }},
		{name: "evidence before the first window", setup: func(w *reviewWorld) { w.finished(rt0.Add(-jonah.FirstWindow - time.Hour)) }},
		{name: "budget spent", setup: func(w *reviewWorld) {
			w.finished(rt0.Add(-time.Hour))
			w.spend(6, rt0.Add(-6*24*time.Hour))
			w.spend(4, rt0.Add(-time.Hour))
		}},
		{name: "budget spent before the window", setup: func(w *reviewWorld) {
			w.finished(rt0.Add(-time.Hour))
			w.spend(10, rt0.Add(-jonah.BudgetWindow-time.Hour))
		}, want: domain.JonahThreshold, start: firstWindow},
		{name: "review in flight", setup: func(w *reviewWorld) {
			w.finished(rt0.Add(-time.Hour))
			w.queue(domain.JonahManual)
		}, want: domain.JonahManual},
		{name: "already reviewed today", from: reviewSlot().Add(time.Minute), setup: func(w *reviewWorld) {
			w.finished(reviewSlot().Add(-time.Hour))
			w.finishedReview(domain.JonahDone)
			w.at(rt0)
			w.finished(rt0.Add(-time.Minute))
		}, want: domain.JonahManual},
		{name: "evidence only before the last done review's window ended", from: rt0.Add(-24 * time.Hour), setup: func(w *reviewWorld) {
			w.finished(rt0.Add(-25 * time.Hour))
			w.finishedReview(domain.JonahDone)
			w.at(rt0)
		}, want: domain.JonahManual},
		{name: "evidence since the last done review", from: rt0.Add(-24 * time.Hour), setup: func(w *reviewWorld) {
			w.finishedReview(domain.JonahDone)
			w.at(rt0)
			w.finished(rt0.Add(-time.Hour))
		}, want: domain.JonahThreshold, start: func(*reviewWorld) time.Time { return rt0.Add(-24 * time.Hour) }},
		{name: "a failed review does not move the window", from: rt0.Add(-48 * time.Hour), setup: func(w *reviewWorld) {
			w.finishedReview(domain.JonahDone)
			w.at(rt0.Add(-24 * time.Hour))
			w.finished(rt0.Add(-30 * time.Hour))
			w.finishedReview(domain.JonahFailed)
			w.at(rt0)
		}, want: domain.JonahThreshold, start: func(*reviewWorld) time.Time { return rt0.Add(-48 * time.Hour) }},
		{name: "learning off", setup: func(w *reviewWorld) {
			w.finished(rt0.Add(-time.Hour))
			w.defs.line.Jonah.Enabled = false
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := newReviewWorldAt(t, cmp.Or(tc.from, rt0))
			tc.setup(w)

			if err := w.loop.Sweep(t.Context()); err != nil {
				t.Fatalf("Sweep: %v", err)
			}

			w.expectSwept(tc.want, tc.start)
		})
	}

	t.Run("once a day", func(t *testing.T) {
		t.Parallel()

		w := newReviewWorld(t)
		w.finished(rt0.Add(-time.Hour))

		for range 2 {
			if err := w.loop.Sweep(t.Context()); err != nil {
				t.Fatalf("Sweep: %v", err)
			}

			w.work()
			w.finished(w.clock.Now())
			w.clock.Advance(time.Hour)
		}

		if rs := w.reviews(); len(rs) != 1 {
			t.Fatalf("reviews after two sweeps on one day = %d, want 1", len(rs))
		}

		w.at(reviewSlot().Add(24*time.Hour + time.Minute))

		if err := w.loop.Sweep(t.Context()); err != nil {
			t.Fatalf("Sweep: %v", err)
		}

		if rs := w.reviews(); len(rs) != 2 || rs[0].State != domain.JonahPending || !rs[0].WindowStart.Equal(rs[1].WindowEnd) {
			t.Fatalf("reviews the next day = %+v, want a second one starting where the first ended", rs)
		}
	})
}

func TestReviewHour_SpreadsFactories(t *testing.T) {
	t.Parallel()

	hours := map[int]bool{}

	for _, f := range []domain.FactoryID{reviewFactory, "00000000-0000-4000-8000-0000000000f2", "00000000-0000-4000-8000-0000000000f3"} {
		h := jonah.ReviewHour(f)
		if h < 0 || h > 23 || h != jonah.ReviewHour(f) {
			t.Fatalf("ReviewHour(%s) = %d, want a stable hour of the day", f, h)
		}

		hours[h] = true
	}

	if len(hours) < 2 {
		t.Errorf("three factories share one hour %v, want them spread", hours)
	}
}

func firstWindow(w *reviewWorld) time.Time { return w.clock.Now().Add(-jonah.FirstWindow) }

// finishedReview queues and claims a manual review over the first window, as of the clock, and
// ends it in state.
func (w *reviewWorld) finishedReview(state domain.JonahReviewState) {
	w.t.Helper()

	r := w.queue(domain.JonahManual)

	claimed, err := w.store.ClaimJonahReview(w.ctx(), w.clock.Now(), time.Minute)
	if err != nil || claimed.ID != r.ID {
		w.t.Fatalf("ClaimJonahReview = %+v, %v", claimed, err)
	}

	if _, _, err := w.store.FinishJonahReview(w.ctx(), claimed, domain.JonahReviewResult{State: state, At: w.clock.Now()}); err != nil {
		w.t.Fatalf("FinishJonahReview: %v", err)
	}
}

// expectSwept checks that the factory's latest review is of trigger want, that none is pending
// when want is "", and that a daily review is pending on the line's version from start to now.
func (w *reviewWorld) expectSwept(want domain.JonahTrigger, start func(*reviewWorld) time.Time) {
	w.t.Helper()

	rs := w.reviews()
	pending := len(rs) > 0 && rs[0].State == domain.JonahPending

	switch {
	case want == "":
		if pending {
			w.t.Fatalf("reviews = %+v, want none pending", rs)
		}

		return
	case len(rs) == 0 || rs[0].Trigger != want:
		w.t.Fatalf("reviews = %+v, want the latest of trigger %q", rs, want)
	case want != domain.JonahThreshold:
		return
	}

	now := w.clock.Now()
	if r := rs[0]; r.VersionID != reviewVersion || !r.WindowStart.Equal(start(w)) || !r.WindowEnd.Equal(now) || !pending || r.CreatedBy == "" {
		w.t.Errorf("daily review = %+v, want a pending review of %s from %s to %s", r, reviewVersion, start(w), now)
	}
}

func TestReviews_Work(t *testing.T) {
	t.Parallel()

	t.Run("claims and runs one jonah run", testWorkRuns)
	t.Run("threshold review is not manual", testWorkThreshold)
	t.Run("lost lease", testWorkLeaseLost)

	failures := []struct {
		name  string
		setup func(w *reviewWorld)
		// error is in the failed review's error; runs is how many runs started, each attempt one.
		error string
		runs  int
	}{
		{name: "budget spent", setup: func(w *reviewWorld) { w.spend(10, rt0.Add(-time.Hour)) }, error: domain.ErrLearningBudgetExhausted.Error()},
		{name: "run failed", setup: func(w *reviewWorld) {
			w.runner.answer = func(context.Context, domain.RunRequest) (domain.RunResult, error) {
				return domain.RunResult{RunID: firstRun, Status: domain.RunFailed, Error: "out of turns"}, nil
			}
		}, error: "out of turns", runs: jonah.MaxReviewAttempts},
		{name: "run not carried out", setup: func(w *reviewWorld) {
			w.runner.answer = func(context.Context, domain.RunRequest) (domain.RunResult, error) {
				return domain.RunResult{RunID: firstRun}, ports.ErrSandboxLost
			}
		}, error: "the review run failed", runs: jonah.MaxReviewAttempts},
		{name: "output does not parse", setup: func(w *reviewWorld) {
			w.runner.answer = func(context.Context, domain.RunRequest) (domain.RunResult, error) {
				res := succeeded(`"nothing"`)
				res.RunID = firstRun

				return res, nil
			}
		}, error: "does not parse", runs: jonah.MaxReviewAttempts},
		{name: "version gone", setup: func(w *reviewWorld) { w.defs.line.VersionID = "version-4" }, error: "is gone"},
	}

	for _, tc := range failures {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

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
			if r.State != domain.JonahFailed || !strings.Contains(r.Error, tc.error) || r.Attempts != max(tc.runs, 1) {
				t.Errorf("review = %s %q after %d attempts, want failed with %q after %d", r.State, r.Error, r.Attempts, tc.error, max(tc.runs, 1))
			}

			var wantRun domain.RunID
			if tc.runs > 0 {
				wantRun = firstRun
			}

			if r.RunID != wantRun {
				t.Errorf("review's run = %q, want %q", r.RunID, wantRun)
			}

			if n := len(w.logged("jonah review retried")); n != max(tc.runs-1, 0) {
				t.Errorf("retries logged = %d, want %d", n, max(tc.runs-1, 0))
			}
		})
	}

	t.Run("a retry that succeeds", testWorkRetrySucceeds)
	t.Run("claimed past its last attempt", testWorkPastLastAttempt)
}

// A run that fails once puts the review back with its error; the next attempt's success ends it
// done, with its own run.
func testWorkRetrySucceeds(t *testing.T) {
	t.Parallel()

	w := newReviewWorld(t)
	w.queue(domain.JonahManual)

	var seen []domain.JonahReview

	w.runner.answer = func(context.Context, domain.RunRequest) (domain.RunResult, error) {
		seen = append(seen, w.reviews()[0])
		if len(seen) == 1 {
			return domain.RunResult{RunID: firstRun, Status: domain.RunFailed, Error: "provider overloaded"}, nil
		}

		res := succeeded(`{"observations": [], "proposals": []}`)
		res.RunID = secondRun

		return res, nil
	}

	if n := w.work(); n != 1 {
		t.Fatalf("Work finished %d, want 1", n)
	}

	if len(seen) != 2 || seen[1].Attempts != 2 || !strings.Contains(seen[1].Error, "provider overloaded") || seen[1].RunID != firstRun {
		t.Fatalf("second attempt saw %+v, want attempt 2 with the first one's error and run", seen)
	}

	if r := w.reviews()[0]; r.State != domain.JonahDone || r.Attempts != 2 || r.RunID != secondRun {
		t.Errorf("review = %+v, want done after two attempts with run-2", r)
	}
}

// A review whose runs died with their goald and that is claimed a fourth time fails without a run.
func testWorkPastLastAttempt(t *testing.T) {
	t.Parallel()

	w := newReviewWorld(t)
	w.queue(domain.JonahManual)

	for range jonah.MaxReviewAttempts {
		if _, err := w.store.ClaimJonahReview(w.ctx(), w.clock.Now(), time.Minute); err != nil {
			t.Fatalf("ClaimJonahReview: %v", err)
		}

		w.clock.Advance(time.Minute)
	}

	if n := w.work(); n != 1 || len(w.runner.requests()) != 0 {
		t.Fatalf("Work finished %d and ran %d, want 1 and none", n, len(w.runner.requests()))
	}

	if r := w.reviews()[0]; r.State != domain.JonahFailed || !strings.Contains(r.Error, "did not finish in 3 attempts") {
		t.Errorf("review = %s %q, want failed after its attempts", r.State, r.Error)
	}
}

func testWorkRuns(t *testing.T) {
	t.Parallel()

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

	// No item, station, lease, repository or connection but the model's.
	want := domain.RunRequest{
		OrgID: w.org, FactoryID: reviewFactory, Version: reviewVersion, Kind: domain.RunKindJonah, Agent: jonah.ReviewAgent,
		Model: reviewModel, Bundle: domain.TaskKindJonah, WindowStart: queued.WindowStart, WindowEnd: queued.WindowEnd, Manual: true,
		ModelConnection: modelConn, Size: domain.SandboxSmall, Timeout: reviewTimeout,
	}
	if !reflect.DeepEqual(reqs[0], want) {
		t.Errorf("run request =\n%+v\nwant\n%+v", reqs[0], want)
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
}

func testWorkThreshold(t *testing.T) {
	t.Parallel()

	w := newReviewWorld(t)
	w.queue(domain.JonahThreshold)
	w.work()

	if reqs := w.runner.requests(); len(reqs) != 1 || reqs[0].Manual {
		t.Errorf("runs = %+v, want one that is not manual", reqs)
	}
}

func testWorkLeaseLost(t *testing.T) {
	t.Parallel()

	w := newReviewWorld(t)
	w.queue(domain.JonahManual)

	w.runner.answer = func(context.Context, domain.RunRequest) (domain.RunResult, error) {
		// The run outlives its lease and another instance claims the review.
		w.clock.Advance(reviewTimeout + reviewMargin + time.Second)

		if _, err := w.store.ClaimJonahReview(w.ctx(), w.clock.Now(), time.Hour); err != nil {
			t.Errorf("second claim: %v", err)
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
}

const (
	testsMissing       = "Tests are missing"
	screenshotsMissing = "Screenshots are missing"
)

func TestReviews_Finish(t *testing.T) {
	t.Parallel()

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
			{Summary: testsMissing, Detail: "two items", Items: []string{"#1", "#2", "#99"}, Count: 2, Proposal: new(1)},
			{Summary: screenshotsMissing, Items: []string{"#2"}, Count: 1},
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

	if got := w.reviews()[0]; got.State != domain.JonahDone {
		t.Errorf("review = %+v, want done", got)
	}

	ps, err := w.store.Proposals(w.ctx(), ports.ProposalQuery{Factory: reviewFactory, Limit: 10})
	if err != nil || len(ps) != 2 {
		t.Fatalf("Proposals = %+v, %v; want 2", ps, err)
	}

	// Newest first: the second proposal is number 2.
	expectTestsProposal(t, ps[1], r, ev)

	if want := flow.LeadTimeP85.Hours(); ps[0].Estimate.Before != want || ps[0].Estimate.Metric != domain.MetricLeadTimeP85 {
		t.Errorf("proposal 2 estimate = %+v, want before %v hours", ps[0].Estimate, want)
	}

	w.expectFinishedObservations(ps[0].ID)
}

// expectTestsProposal checks proposal 1 of TestReviews_Finish: its review and version, the before
// StationFlow gives (2 sent back of 4 passed), its evidence from the sent-back events and its file.
func expectTestsProposal(t *testing.T, p domain.Proposal, r domain.JonahReview, ev []domain.SentBack) {
	t.Helper()

	if p.Number != 1 || p.Title != "Ask for tests" || p.ReviewID != r.ID || p.VersionID != reviewVersion || p.State != domain.ProposalProposed {
		t.Errorf("proposal 1 = %+v", p)
	}

	wantEstimate := domain.ProposalEstimate{Metric: domain.MetricSentBackRate, Station: stBuild, Before: 0.5, After: 0.1, CostPerItemUSD: 0.02}
	if p.Estimate != wantEstimate {
		t.Errorf("proposal 1 estimate = %+v, want %+v", p.Estimate, wantEstimate)
	}

	wantEvidence := []domain.ProposalEvidence{
		{ItemID: ev[0].ItemID, EventID: ev[0].EventID, Station: stBuild, Quote: ev[0].Reason},
		{ItemID: ev[1].ItemID, EventID: ev[1].EventID, Station: stBuild, Quote: "no screenshot"},
	}
	if !reflect.DeepEqual(p.Evidence, wantEvidence) {
		t.Errorf("proposal 1 evidence = %+v, want %+v", p.Evidence, wantEvidence)
	}

	wantFiles := []domain.ProposalFile{{Path: writerPath, BaseSHA256: string(domain.HashBlob([]byte(writerText))), Content: newWriterText}}
	if !reflect.DeepEqual(p.Files, wantFiles) {
		t.Errorf("proposal 1 files = %+v, want %+v", p.Files, wantFiles)
	}
}

// expectFinishedObservations checks TestReviews_Finish's observations: the one naming index 1
// became proposal 2 (shorter) with the two known items, the other is watching.
func (w *reviewWorld) expectFinishedObservations(shorter domain.ProposalID) {
	w.t.Helper()

	obs, err := w.store.Observations(w.ctx(), reviewFactory, 10)
	if err != nil || len(obs) != 2 {
		w.t.Fatalf("Observations = %+v, %v; want 2", obs, err)
	}

	byName := map[string]domain.Observation{}
	for _, o := range obs {
		byName[o.Summary] = o
	}

	if m := byName[testsMissing]; m.State != domain.ObservationProposed || m.ProposalID != shorter || len(m.ItemIDs) != 2 || m.Count != 2 {
		w.t.Errorf("observation of proposal index 1 = %+v, want proposed as %s with the two known items", m, shorter)
	}

	if s := byName[screenshotsMissing]; s.State != domain.ObservationWatching || s.ProposalID != "" {
		w.t.Errorf("observation without a proposal = %+v, want watching", s)
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

const newWriterText = "Write the change and its tests.\n"

func idOf(s domain.SentBack) string { return strconv.FormatInt(s.EventID, 10) }

// newProposal edits path, as the version has it, on the evidence of e, to move metric at build.
func newProposal(title, path string, e domain.SentBack, metric domain.ProposalMetric) proposal {
	return proposal{
		Title: title, Summary: "Say it in the prompt.",
		Files:    []domain.ProposalFile{{Path: path, BaseSHA256: string(domain.HashBlob([]byte(writerText))), Content: newWriterText}},
		Evidence: []evidence{{Event: idOf(e), Reason: e.Reason}},
		Estimate: estimate{Metric: metric, Station: stBuild, After: 0.1, CostDelta: 0.02},
	}
}
