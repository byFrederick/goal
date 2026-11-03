package engine_test

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
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

	conns, ok := h.connections.(*porttest.Connections)
	if !ok {
		t.Fatalf("the harness's connections are a %T, not the fake", h.connections)
	}

	kb := &knowledgeBase{kb: porttest.NewKnowledge(conns, h.tenancy), at: time.Now().UTC().Add(time.Hour)}
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

	c, err := k.kb.ClaimSource(ctx, k.at, k.at.Add(time.Minute), "")
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
