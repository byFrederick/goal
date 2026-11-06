package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// kbHandbook is the source every run of the tests reads.
const kbHandbook = "handbook"

// knowledgeBase is a knowledge base for the harness's organization whose sources hold snapshots
// written as a sync writes them (putSnapshot).
type knowledgeBase struct {
	kb *porttest.Knowledge
	// at is when the next snapshot is made: each one is newer than the one before.
	at time.Time
}

func (h *harness) knowledgeBase(t *testing.T) *knowledgeBase {
	t.Helper()

	kb := &knowledgeBase{kb: porttest.NewKnowledge(h.tenancy), at: time.Now().UTC().Add(time.Hour)}
	h.knowledge = kb.kb

	return kb
}

// source creates the upload source name, which has no snapshot yet.
func (k *knowledgeBase) source(t *testing.T, name string) {
	t.Helper()

	if _, err := k.kb.CreateSource(inOrg(t), domain.NewKnowledgeSource{Name: name, Title: name, Kind: domain.SourceUpload}, domain.LocalUser); err != nil {
		t.Fatalf("create source %s: %v", name, err)
	}
}

// putSnapshot syncs the source name, creating it first when there is none, into a snapshot id
// newer than every snapshot before. It reports a failure with t.Error, so an agent's run may call it.
func (k *knowledgeBase) putSnapshot(t *testing.T, name string, id domain.KnowledgeSnapshotID) {
	t.Helper()

	ctx := domain.WithOrg(context.Background(), testOrg)

	if _, err := k.kb.Source(ctx, name); err != nil {
		k.source(t, name)
	} else if err := k.kb.SyncNow(ctx, name); err != nil {
		t.Errorf("sync %s now: %v", name, err)
		return
	}

	k.at = k.at.Add(time.Minute)

	c, err := k.kb.ClaimSource(ctx, k.at, k.at.Add(time.Minute))
	if err != nil || c == nil || c.Source.Name != name {
		t.Errorf("claim %s = %+v, %v; want its sync", name, c, err)
		return
	}

	if _, err := k.kb.PutSnapshot(ctx, c.Lease, domain.KnowledgeSnapshot{ID: id, Source: c.Source.ID, CreatedAt: k.at}, nil); err != nil {
		t.Errorf("put snapshot %s of %s: %v", id, name, err)
	}

	r := domain.SyncResult{At: k.at}
	if c.Source.NextSyncAt != nil {
		r.Due = *c.Source.NextSyncAt
	}

	if err := k.kb.FinishSync(ctx, c.Lease, r); err != nil {
		t.Errorf("finish the sync of %s: %v", name, err)
	}
}

// pinsOf are the knowledge pins of the task token the i-th prepare got.
func (h *harness) pinsOf(t *testing.T, i int) []domain.KnowledgePin {
	t.Helper()

	return h.claims(t, h.worker.Prepares()[i].Env["GOAL_GATEWAY_TOKEN"]).Knowledge
}

// A claim pins, once, the newest snapshot of each source that applies to its run: a lost box
// prepared again gets the same pins although a newer snapshot landed meanwhile.
func TestRun_KnowledgePinsSurviveALostBox(t *testing.T) {
	t.Parallel()

	line := repoLine()
	line.Stations[1].Checks = []string{makeTest}
	line.Knowledge = []domain.KnowledgeEntry{{Name: kbHandbook}}

	var (
		once sync.Once
		kb   *knowledgeBase
	)

	h := newHarness(t, line, func(ctx context.Context, c porttest.ExecCall, stdout io.Writer) domain.RunResult {
		once.Do(func() { kb.putSnapshot(t, kbHandbook, "h2") })
		return porttest.Succeed(`{}`)(ctx, c, stdout)
	})
	kb = h.knowledgeBase(t)
	kb.putSnapshot(t, kbHandbook, "h1")
	h.e = h.engine(t, h.store)
	h.sandboxes.Lose("sh -c "+makeTest, 1, lostBox)
	it := h.submit(t, domain.NewItem{})

	h.stepThroughBackoff(t)
	h.expectRetriedOnce(t, it, true)

	if snaps, err := kb.kb.Snapshots(inOrg(t), kbHandbook); err != nil || snaps[0].ID != "h2" {
		t.Fatalf("snapshots %+v, %v; want h2 newest, synced during the first run", snaps, err)
	}

	want := []domain.KnowledgePin{{Source: kbHandbook, Snapshot: "h1"}}
	for i := range 2 {
		if got := h.pinsOf(t, i); !reflect.DeepEqual(got, want) {
			t.Errorf("prepare %d's pins = %+v, want %+v, decided once at the claim", i+1, got, want)
		}
	}
}

// The pins are the newest snapshot of each source whose appliesTo matches the run, in the
// definition's order; a source with no snapshot, or one the organization does not have, gets no
// pin, and an agent whose builtins.read is off gets none at all.
func TestRun_KnowledgePinsTheNewestSnapshotOfWhatApplies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		deny []string
		want []domain.KnowledgePin
	}{
		{name: "an agent that reads", want: []domain.KnowledgePin{{Source: "runbooks", Snapshot: "r2"}, {Source: kbHandbook, Snapshot: "h1"}}},
		{name: "an agent whose builtins.read is off", deny: []string{"Read", "Grep", "Glob"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			line := repoLine()
			line.Knowledge = []domain.KnowledgeEntry{
				{Name: "runbooks", AppliesTo: domain.RuleScope{Stations: []string{stWork}}},
				{Name: "design", AppliesTo: domain.RuleScope{Agents: []string{agFrontend}}},
				{Name: kbHandbook},
				{Name: "unsynced"},
				{Name: "deleted"},
			}

			for i := range line.Agents {
				if line.Agents[i].Name == agWorker {
					line.Agents[i].ToolsDeny = tt.deny
				}
			}

			h := newHarness(t, line, porttest.Succeed(`{}`))
			kb := h.knowledgeBase(t)
			kb.putSnapshot(t, "runbooks", "r1")
			kb.putSnapshot(t, "runbooks", "r2")
			kb.putSnapshot(t, "design", "d1")
			kb.putSnapshot(t, kbHandbook, "h1")
			kb.source(t, "unsynced")
			h.e = h.engine(t, h.store)
			it := h.submit(t, domain.NewItem{})

			h.step(t)

			if got := h.item(t, it.ID); got.State != domain.StateDone {
				t.Fatalf("item is %s, want done (events %v)", got.State, h.kinds(it.ID))
			}

			if got := h.pinsOf(t, 0); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("pins = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// A factory that lists a synced source moving over converts into a repository source (ADR 0038
// decision 16) still claims and runs: the next claim gets no pin for it, since it has no snapshot
// yet, and the claim after its first sync pins that snapshot.
func TestRun_KnowledgeOfAConvertedSource(t *testing.T) {
	t.Parallel()

	line := repoLine()
	line.Knowledge = []domain.KnowledgeEntry{{Name: kbHandbook}}

	h := newHarness(t, line, porttest.Succeed(`{}`))
	kb := h.knowledgeBase(t)

	if _, err := kb.kb.CreateSource(inOrg(t), domain.NewKnowledgeSource{
		Name: kbHandbook, Title: kbHandbook, Kind: domain.SourceSynced, Secret: "docs-token",
		Config: map[string]string{domain.SourceReaderKey: "gitdocs", "repository": "https://git.example.com/acme/docs"},
	}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	if _, err := kb.kb.ConvertSource(inOrg(t), kbHandbook, domain.SourceRepository, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	h.e = h.engine(t, h.store)

	for i, want := range [][]domain.KnowledgePin{nil, {{Source: kbHandbook, Snapshot: "h1"}}} {
		if i == 1 {
			kb.putSnapshot(t, kbHandbook, "h1")
		}

		it := h.submit(t, domain.NewItem{})
		h.step(t)

		if got := h.item(t, it.ID); got.State != domain.StateDone {
			t.Fatalf("item %d is %s, want done (events %v)", i+1, got.State, h.kinds(it.ID))
		}

		if got := h.pinsOf(t, i); !reflect.DeepEqual(got, want) {
			t.Errorf("run %d's pins = %+v, want %+v", i+1, got, want)
		}
	}
}

// A draft request's box pins the snapshots of the sources that apply to the agent it is about, so
// its bundle's Knowledge section is the one that agent's runs see.
func TestRunDefinition_KnowledgePinsOfTheRequestsAgent(t *testing.T) {
	t.Parallel()

	line := definitionLine()
	line.Knowledge = []domain.KnowledgeEntry{
		{Name: kbHandbook},
		{Name: "reviews", AppliesTo: domain.RuleScope{Agents: []string{agReviewer}}},
		{Name: "design", AppliesTo: domain.RuleScope{Agents: []string{agFrontend}}},
	}

	h := newHarness(t, line, func(context.Context, porttest.ExecCall, io.Writer) domain.RunResult {
		return domain.RunResult{Status: domain.RunSucceeded, Output: json.RawMessage(definitionOutput)}
	})
	kb := h.knowledgeBase(t)
	kb.putSnapshot(t, kbHandbook, "h1")
	kb.putSnapshot(t, "reviews", "r1")
	kb.putSnapshot(t, "design", "d1")
	h.e = h.engine(t, h.store)

	if _, err := h.e.RunDefinition(inOrg(t), ports.DefinitionTask{Request: draftRequest(), Revision: 5}); err != nil {
		t.Fatal(err)
	}

	want := []domain.KnowledgePin{{Source: kbHandbook, Snapshot: "h1"}, {Source: "reviews", Snapshot: "r1"}}
	if got := h.pinsOf(t, 0); !reflect.DeepEqual(got, want) {
		t.Errorf("pins = %+v, want %+v", got, want)
	}
}

// The source and the tool of the reads tests.
const (
	kbRunbooks = "runbooks"
	toolRead   = "Read"
)

// toolLines writes the worker's tool_use lines for each call, a tool and its path, to stdout.
func toolLines(t *testing.T, stdout io.Writer, calls ...[2]string) {
	t.Helper()

	for i, c := range calls {
		line, err := json.Marshal(map[string]any{"type": "tool_use", "at": t0.Format(time.RFC3339), "id": fmt.Sprintf("tu_%d", i), "tool": c[0], "input": "{}", "path": c[1]})
		if err != nil {
			t.Fatal(err)
		}

		if _, err := fmt.Fprintf(stdout, "%s\n", line); err != nil {
			t.Error(err)
		}
	}
}

// kbReadCalls are the calls of the reads tests' runs: two reads of one runbook and a search of
// the runbooks, then calls that count for nothing (another source, the checkout, a tool other
// than Read, Grep and Glob).
var kbReadCalls = [][2]string{
	{toolRead, "/knowledge/runbooks/db/disk.md"},
	{toolRead, "/knowledge/runbooks/db/disk.md"},
	{"Grep", "/knowledge/runbooks"},
	{toolRead, "/knowledge/design/a.md"},
	{toolRead, "/workspace/README.md"},
	{"Bash", "/knowledge/runbooks/db/disk.md"},
}

// A run's reads and searches of the sources it pinned are counted once it ends, into the day's
// rollup of its factory; a source it did not pin counts nothing.
func TestRun_CountsKnowledgeReads(t *testing.T) {
	t.Parallel()

	line := repoLine()
	line.Knowledge = []domain.KnowledgeEntry{{Name: kbRunbooks}}

	var once sync.Once

	h := newHarness(t, line, func(ctx context.Context, c porttest.ExecCall, stdout io.Writer) domain.RunResult {
		once.Do(func() { toolLines(t, stdout, kbReadCalls...) })
		return porttest.Succeed(`{}`)(ctx, c, stdout)
	})
	kb := h.knowledgeBase(t)
	kb.putSnapshot(t, kbRunbooks, "r1")
	kb.putSnapshot(t, "design", "d1")
	h.e = h.engine(t, h.store)
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	if got := h.item(t, it.ID); got.State != domain.StateDone {
		t.Fatalf("item is %s, want done (events %v)", got.State, h.kinds(it.ID))
	}

	src, err := kb.kb.Source(inOrg(t), kbRunbooks)
	if err != nil {
		t.Fatal(err)
	}

	got, err := kb.kb.FileReads(inOrg(t), kbRunbooks, t0.AddDate(0, 0, -1))
	if err != nil {
		t.Fatal(err)
	}

	day := domain.KnowledgeDay(h.clock.Now())
	want := []domain.KnowledgeFileReads{
		{Source: src.ID, Path: domain.KnowledgeSourceRoot, Factory: factoryID, Day: day, Searches: 1},
		{Source: src.ID, Path: "db/disk.md", Factory: factoryID, Day: day, Reads: 2},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("reads = %+v\nwant %+v", got, want)
	}

	if other, err := kb.kb.FileReads(inOrg(t), "design", t0.AddDate(0, 0, -1)); err != nil || len(other) != 0 {
		t.Errorf("reads of the unpinned source = %+v, %v; want none", other, err)
	}
}

// failingReads is a knowledge base whose AddReads always fails.
type failingReads struct{ *porttest.Knowledge }

func (failingReads) AddReads(context.Context, []domain.KnowledgeFileReads) error {
	return errors.New("the rollup is down")
}

// Counting is best effort: when the reads cannot be added the run ends as it would have, and the
// failure is logged.
func TestRun_KnowledgeReadsFailureLeavesTheRunAlone(t *testing.T) {
	t.Parallel()

	line := repoLine()
	line.Knowledge = []domain.KnowledgeEntry{{Name: kbRunbooks}}

	h := newHarness(t, line, func(ctx context.Context, c porttest.ExecCall, stdout io.Writer) domain.RunResult {
		toolLines(t, stdout, kbReadCalls...)
		return porttest.Succeed(`{}`)(ctx, c, stdout)
	})
	kb := h.knowledgeBase(t)
	kb.putSnapshot(t, kbRunbooks, "r1")
	h.knowledgeDeps = failingReads{kb.kb}
	h.e = h.engine(t, h.store)
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	if got := h.item(t, it.ID); got.State != domain.StateDone {
		t.Fatalf("item is %s, want done (events %v)", got.State, h.kinds(it.ID))
	}

	for _, r := range h.runs(t, testOrg, it.ID) {
		if r.Result.Status != domain.RunSucceeded {
			t.Errorf("run %s is %s (%s), want succeeded", r.ID, r.Result.Status, r.Result.Error)
		}
	}

	if logs := h.logs.String(); !strings.Contains(logs, "knowledge reads not counted") || !strings.Contains(logs, "the rollup is down") {
		t.Errorf("logs lack the failed count:\n%s", logs)
	}
}

// A run that read or searched its pinned sources records one knowledge_read event once it ends,
// by its agent, naming each source's files read and folders searched; touches of an unpinned
// source, the checkout or another tool are left out. The event is recorded even when the
// rollup's AddReads fails.
func TestRun_RecordsAKnowledgeReadEvent(t *testing.T) {
	t.Parallel()

	line := repoLine()
	line.Knowledge = []domain.KnowledgeEntry{{Name: kbRunbooks}}

	var once sync.Once

	h := newHarness(t, line, func(ctx context.Context, c porttest.ExecCall, stdout io.Writer) domain.RunResult {
		once.Do(func() {
			toolLines(t, stdout,
				[2]string{toolRead, "/knowledge/runbooks/a.md"},
				[2]string{"Grep", "/knowledge/runbooks/"},
				[2]string{toolRead, "/knowledge/design/a.md"},
				[2]string{toolRead, "/workspace/README.md"},
				[2]string{"Bash", "/knowledge/runbooks/b.md"},
			)
		})

		return porttest.Succeed(`{}`)(ctx, c, stdout)
	})
	kb := h.knowledgeBase(t)
	kb.putSnapshot(t, kbRunbooks, "r1")
	kb.putSnapshot(t, "design", "d1")
	h.knowledgeDeps = failingReads{kb.kb}
	h.e = h.engine(t, h.store)
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	if got := h.item(t, it.ID); got.State != domain.StateDone {
		t.Fatalf("item is %s, want done (events %v)", got.State, h.kinds(it.ID))
	}

	evs := h.eventsOf(it.ID, domain.EventKnowledgeRead)
	if len(evs) != 1 {
		t.Fatalf("%d knowledge_read events, want one (events %v)", len(evs), h.kinds(it.ID))
	}

	ev, runs := evs[0], h.runs(t, testOrg, it.ID)
	if len(runs) != 1 || ev.RunID != runs[0].ID || ev.Actor != runs[0].Agent {
		t.Errorf("event of run %q by %q; want the one run's, by its agent (runs %+v)", ev.RunID, ev.Actor, runs)
	}

	want := map[string]any{"sources": []any{
		map[string]any{"source": kbRunbooks, "reads": []any{"a.md"}, "searches": []any{domain.KnowledgeSourceRoot}},
	}}
	if !reflect.DeepEqual(ev.Data, want) {
		t.Errorf("data = %#v\nwant %#v", ev.Data, want)
	}

	if logs := h.logs.String(); !strings.Contains(logs, "knowledge reads not counted") {
		t.Errorf("logs lack the failed count:\n%s", logs)
	}
}

// A run that touched no pinned knowledge records no knowledge_read event, and a run without an
// item (Jonah) records none even when it read its sources.
func TestRun_NoKnowledgeTouchesRecordsNoReadEvent(t *testing.T) {
	t.Parallel()

	line := repoLine()
	line.Knowledge = []domain.KnowledgeEntry{{Name: kbRunbooks}}

	h := newHarness(t, line, func(ctx context.Context, c porttest.ExecCall, stdout io.Writer) domain.RunResult {
		toolLines(t, stdout, [2]string{toolRead, "/workspace/README.md"}, [2]string{"Bash", "/knowledge/runbooks/a.md"})
		return porttest.Succeed(`{}`)(ctx, c, stdout)
	})
	kb := h.knowledgeBase(t)
	kb.putSnapshot(t, kbRunbooks, "r1")
	h.e = h.engine(t, h.store)
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	if got := h.item(t, it.ID); got.State != domain.StateDone {
		t.Fatalf("item is %s, want done (events %v)", got.State, h.kinds(it.ID))
	}

	if evs := h.eventsOf(it.ID, domain.EventKnowledgeRead); len(evs) != 0 {
		t.Errorf("knowledge_read events = %+v, want none", evs)
	}

	jonah := newHarness(t, connLine(), func(_ context.Context, _ porttest.ExecCall, stdout io.Writer) domain.RunResult {
		toolLines(t, stdout, [2]string{toolRead, "/knowledge/runbooks/a.md"})
		return domain.RunResult{Status: domain.RunSucceeded, Output: json.RawMessage(`{"observations":[],"proposals":[]}`)}
	})
	jonah.store.AddFactory(testOrg, factoryID)

	req := jonahRequest()
	req.Knowledge = []domain.KnowledgePin{{Source: kbRunbooks, Snapshot: "r1"}}

	if _, err := jonah.e.RunTask(t.Context(), req); err != nil {
		t.Fatalf("RunTask: %v", err)
	}

	if events := jonah.store.Events(""); len(events) != 0 {
		t.Errorf("events of a Jonah run = %+v; want none", events)
	}
}
