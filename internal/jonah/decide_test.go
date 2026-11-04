package jonah_test

import (
	"context"
	"errors"
	"maps"
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
	decideOrg = domain.OrgID("00000000-0000-4000-9000-000000000001")
	agentPath = "agents/writer.md"
	rulePath  = "rules/evidence.md"
)

// errGate stands in for the eval gate's refusal, which keeps the draft open.
var errGate = errors.New("evals pending")

// definition is an Authoring over one factory's files in memory: the current version's and an
// open draft's, published as numbered versions.
type definition struct {
	mu       sync.Mutex
	current  map[string]string
	number   int
	draft    map[string]string
	revision int64
	// published are the files of each version Publish published, by number.
	published map[int]map[string]string
	// gate refuses every publish, keeping the draft, when set.
	gate      bool
	discarded int
}

var _ jonah.Authoring = (*definition)(nil)

func (d *definition) Head(context.Context, domain.FactoryID) (domain.DefinitionVersionID, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	return versionID(d.number), d.draft != nil, nil
}

func versionID(n int) domain.DefinitionVersionID {
	return domain.DefinitionVersionID("v" + strconv.Itoa(n))
}

func (d *definition) CurrentFile(_ context.Context, _ domain.FactoryID, path string) ([]byte, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	c, ok := d.current[path]

	return []byte(c), ok, nil
}

func (d *definition) VersionFile(_ context.Context, _ domain.FactoryID, version domain.DefinitionVersionID, path string) ([]byte, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	files := d.published[0]
	if n, err := strconv.Atoi(strings.TrimPrefix(string(version), "v")); err == nil && n == d.number {
		files = d.current
	} else if err == nil && d.published[n] != nil {
		files = d.published[n]
	}

	c, ok := files[path]

	return []byte(c), ok, nil
}

func (d *definition) DraftFile(_ context.Context, _ domain.FactoryID, path string) ([]byte, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	c, ok := d.draft[path]

	return []byte(c), ok, nil
}

func (d *definition) OpenDraft(context.Context, domain.FactoryID) (int64, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.draft != nil {
		return d.revision, false, nil
	}

	d.draft = maps.Clone(d.current)
	if d.draft == nil {
		d.draft = map[string]string{}
	}

	d.revision++

	return d.revision, true, nil
}

func (d *definition) WriteFile(_ context.Context, _ domain.FactoryID, path string, content []byte) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.draft[path] = string(content)
	d.revision++

	return d.revision, nil
}

func (d *definition) Publish(_ context.Context, _ domain.FactoryID, _ int64, expected domain.DefinitionVersionID, _ string) (domain.DefinitionVersion, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.gate {
		return domain.DefinitionVersion{}, true, errGate
	}

	if expected != versionID(d.number) {
		return domain.DefinitionVersion{}, false, errors.New("base moved")
	}

	if d.published == nil {
		d.published = map[int]map[string]string{}
	}

	d.published[d.number] = d.current
	d.current, d.draft = d.draft, nil
	d.number++

	return domain.DefinitionVersion{ID: versionID(d.number), Number: d.number}, false, nil
}

func (d *definition) DiscardDraft(context.Context, domain.FactoryID, int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.draft = nil
	d.discarded++

	return nil
}

// audits records the entries appended to it.
type audits struct {
	mu      sync.Mutex
	entries []domain.AuditEntry
}

func (a *audits) Append(_ context.Context, e domain.AuditEntry) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.entries = append(a.entries, e)

	return nil
}

func (a *audits) AuditLog(context.Context, ports.AuditQuery) ([]domain.AuditEntry, error) {
	return nil, nil
}

func (a *audits) actions() []string {
	a.mu.Lock()
	defer a.mu.Unlock()

	out := make([]string, 0, len(a.entries))
	for _, e := range a.entries {
		out = append(out, e.Action)
	}

	return out
}

// decideFixture is a Decider over a store holding one proposal (number 1) that edits the writer's
// prompt and adds a rule, made against v1.
type decideFixture struct {
	dc    *jonah.Decider
	store *porttest.Store
	def   *definition
	audit *audits
	clock *porttest.Clock
}

const writerV1 = "# writer\n"

func newDecideFixture(t *testing.T) (*decideFixture, context.Context) {
	t.Helper()

	ctx := domain.WithActor(domain.WithOrg(t.Context(), decideOrg), domain.LocalUser)
	f := &decideFixture{
		store: porttest.NewStore(), def: &definition{current: map[string]string{agentPath: writerV1}, number: 1},
		audit: &audits{}, clock: porttest.NewClock(t0),
	}
	f.store.AddFactory(decideOrg, factory)
	f.dc = jonah.NewDecider(jonah.DecideDeps{Proposals: f.store, Authoring: f.def, Audit: f.audit, Clock: f.clock})

	if _, err := f.store.QueueJonahReview(ctx, domain.NewJonahReview{
		FactoryID: factory, VersionID: versionID(1), Trigger: domain.JonahManual, WindowStart: t0.Add(-time.Hour), WindowEnd: t0,
		By: domain.LocalUser, At: t0,
	}); err != nil {
		t.Fatal(err)
	}

	claimed, err := f.store.ClaimJonahReview(ctx, t0, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := f.store.FinishJonahReview(ctx, claimed, domain.JonahReviewResult{
		State: domain.JonahDone, At: t0,
		Proposals: []domain.NewProposal{{
			Title: "Ask for a screenshot",
			Files: []domain.ProposalFile{
				{Path: agentPath, BaseSHA256: string(domain.HashBlob([]byte(writerV1))), Content: "# writer\nAttach a screenshot.\n"},
				{Path: rulePath, Content: "Show evidence.\n"},
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	f.run(ctx, t, domain.RunKindStation, 200) // the line's spend: Jonah's budget is $10

	return f, ctx
}

// run records a finished run of the factory of kind that cost usd, started an hour before t0: a
// station run is on an item still in progress, a jonah run on none.
func (f *decideFixture) run(ctx context.Context, t *testing.T, kind domain.RunKind, usd float64) {
	t.Helper()

	at := t0.Add(-time.Hour)
	n := ports.NewRun{FactoryID: factory, Agent: "builder", StartedAt: at, Kind: kind}

	if kind == domain.RunKindStation {
		it, err := f.store.CreateItem(ctx, domain.NewItem{
			FactoryID: factory, Title: "work in progress", Station: stationBuild, State: domain.StateInProgress,
			Class: domain.ClassStandard, CreatedAt: at,
		})
		if err != nil {
			t.Fatal(err)
		}

		n.ItemID, n.Station = it.ID, stationBuild
	}

	id, err := f.store.StartRun(ctx, n)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.store.FinishRun(ctx, id, domain.RunResult{Status: domain.RunSucceeded, CostUSD: usd}, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
}

func (f *decideFixture) state(ctx context.Context, t *testing.T) domain.Proposal {
	t.Helper()

	p, err := f.store.Proposal(ctx, factory, 1)
	if err != nil {
		t.Fatal(err)
	}

	return p
}

func TestDecide_Approve(t *testing.T) {
	t.Parallel()

	f, ctx := newDecideFixture(t)

	p, v, err := f.dc.Approve(ctx, factory, 1)
	if err != nil {
		t.Fatal(err)
	}

	if v.Number != 2 || p.State != domain.ProposalPublished || p.PublishedVersionID != v.ID {
		t.Errorf("approve = %+v, version %+v; want published as v2", p, v)
	}

	if f.def.current[agentPath] != "# writer\nAttach a screenshot.\n" || f.def.current[rulePath] != "Show evidence.\n" || f.def.draft != nil {
		t.Errorf("v2 = %v, draft %v; want both files and no draft", f.def.current, f.def.draft)
	}

	if got := f.audit.actions(); len(got) != 1 || got[0] != domain.AuditProposalPublished {
		t.Errorf("audit = %v, want one %s", got, domain.AuditProposalPublished)
	}

	if _, _, err := f.dc.Approve(ctx, factory, 1); !errors.Is(err, ports.ErrProposalDecided) {
		t.Errorf("second approve = %v, want ErrProposalDecided", err)
	}
}

func TestDecide_ApproveRefusals(t *testing.T) {
	t.Parallel()

	t.Run("draft open", func(t *testing.T) {
		t.Parallel()

		f, ctx := newDecideFixture(t)
		f.def.draft, f.def.revision = map[string]string{agentPath: "mine"}, 7

		if _, _, err := f.dc.Approve(ctx, factory, 1); !errors.Is(err, domain.ErrProposalDraftOpen) {
			t.Fatalf("approve = %v, want ErrProposalDraftOpen", err)
		}

		if f.def.draft[agentPath] != "mine" || f.def.revision != 7 || f.def.discarded != 0 {
			t.Errorf("draft = %v at %d, want it untouched", f.def.draft, f.def.revision)
		}
	})

	for name, current := range map[string]map[string]string{
		"changed base":      {agentPath: "# writer, changed\n"},
		"new file now here": {agentPath: writerV1, rulePath: "someone added it\n"},
		"base file removed": {},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f, ctx := newDecideFixture(t)
			f.def.current = current

			if _, _, err := f.dc.Approve(ctx, factory, 1); !errors.Is(err, domain.ErrProposalStale) {
				t.Fatalf("approve = %v, want ErrProposalStale", err)
			}

			if f.def.draft != nil || f.state(ctx, t).State != domain.ProposalProposed {
				t.Errorf("after a stale approve draft = %v, proposal %s; want no draft and still proposed", f.def.draft, f.state(ctx, t).State)
			}
		})
	}

	t.Run("gate keeps the draft", func(t *testing.T) {
		t.Parallel()

		f, ctx := newDecideFixture(t)
		f.def.gate = true

		if _, _, err := f.dc.Approve(ctx, factory, 1); !errors.Is(err, errGate) {
			t.Fatalf("approve = %v, want the gate's refusal", err)
		}

		if f.def.draft == nil || f.def.discarded != 0 || f.state(ctx, t).State != domain.ProposalProposed {
			t.Errorf("after the gate draft = %v, discarded %d; want it open and the proposal proposed", f.def.draft, f.def.discarded)
		}
	})
}

func TestDecide_Edit(t *testing.T) {
	t.Parallel()

	f, ctx := newDecideFixture(t)

	p, rev, err := f.dc.Edit(ctx, factory, 1)
	if err != nil {
		t.Fatal(err)
	}

	if p.State != domain.ProposalApplied || rev != f.def.revision || f.def.draft[rulePath] != "Show evidence.\n" || f.def.number != 1 {
		t.Errorf("edit = %+v at %d, draft %v, v%d; want applied in the draft, nothing published", p, rev, f.def.draft, f.def.number)
	}

	if got := f.audit.actions(); len(got) != 1 || got[0] != domain.AuditProposalApplied {
		t.Errorf("audit = %v, want one %s", got, domain.AuditProposalApplied)
	}

	if _, err := f.dc.Reject(ctx, factory, 1, "no"); !errors.Is(err, ports.ErrProposalDecided) {
		t.Errorf("reject after edit = %v, want ErrProposalDecided", err)
	}
}

func TestDecide_EditConflict(t *testing.T) {
	t.Parallel()

	f, ctx := newDecideFixture(t)
	f.def.draft, f.def.revision = map[string]string{agentPath: "# writer, edited in the draft\n"}, 3

	if _, _, err := f.dc.Edit(ctx, factory, 1); !errors.Is(err, domain.ErrProposalDraftConflict) {
		t.Fatalf("edit = %v, want ErrProposalDraftConflict", err)
	}

	if f.def.draft[agentPath] != "# writer, edited in the draft\n" || f.def.revision != 3 || f.def.discarded != 0 {
		t.Errorf("draft = %v at %d, want the person's draft untouched", f.def.draft, f.def.revision)
	}

	// A draft Edit opened itself, from a current version that moved, is discarded again.
	f.def.draft, f.def.current = nil, map[string]string{agentPath: "# moved\n"}

	if _, _, err := f.dc.Edit(ctx, factory, 1); !errors.Is(err, domain.ErrProposalDraftConflict) {
		t.Fatalf("edit on a moved version = %v, want ErrProposalDraftConflict", err)
	}

	if f.def.draft != nil || f.def.discarded != 1 {
		t.Errorf("draft = %v, discarded %d; want the opened draft discarded", f.def.draft, f.def.discarded)
	}
}

func TestDecide_Reject(t *testing.T) {
	t.Parallel()

	f, ctx := newDecideFixture(t)

	if _, err := f.dc.Reject(ctx, factory, 1, "  \n"); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("blank reject = %v, want ErrInvalid", err)
	}

	p, err := f.dc.Reject(ctx, factory, 1, " Screenshots are attached already. ")
	if err != nil {
		t.Fatal(err)
	}

	if p.State != domain.ProposalRejected || p.RejectReason != "Screenshots are attached already." {
		t.Errorf("reject = %+v, want rejected with the trimmed reason", p)
	}

	if got := f.audit.actions(); len(got) != 1 || got[0] != domain.AuditProposalRejected {
		t.Errorf("audit = %v, want one %s", got, domain.AuditProposalRejected)
	}

	if _, _, err := f.dc.Edit(ctx, factory, 1); !errors.Is(err, ports.ErrProposalDecided) {
		t.Errorf("edit after reject = %v, want ErrProposalDecided", err)
	}

	if _, err := f.dc.Reject(ctx, factory, 9, "no"); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("reject of an unknown proposal = %v, want ErrNotFound", err)
	}
}

func TestDecide_QueueReview(t *testing.T) {
	t.Parallel()

	f, ctx := newDecideFixture(t) // its review is done, its window ended at t0
	f.clock.Advance(2 * time.Hour)

	policy := domain.DefaultJonahPolicy()

	r, err := f.dc.QueueReview(ctx, factory, versionID(1), policy)
	if err != nil {
		t.Fatal(err)
	}

	if r.State != domain.JonahPending || r.Trigger != domain.JonahManual || !r.WindowStart.Equal(t0) || !r.WindowEnd.Equal(t0.Add(2*time.Hour)) {
		t.Errorf("review = %+v, want a pending manual review from t0 to now", r)
	}

	if got := f.audit.actions(); len(got) != 1 || got[0] != domain.AuditReviewQueued {
		t.Errorf("audit = %v, want one %s", got, domain.AuditReviewQueued)
	}

	if _, err := f.dc.QueueReview(ctx, factory, versionID(1), policy); !errors.Is(err, domain.ErrReviewInFlight) {
		t.Errorf("second review = %v, want ErrReviewInFlight", err)
	}

	none := policy
	none.Share = 0 // a share of nothing is already used up

	if _, err := f.dc.QueueReview(ctx, factory, versionID(1), none); !errors.Is(err, domain.ErrLearningBudgetExhausted) {
		t.Errorf("review with no share = %v, want ErrLearningBudgetExhausted", err)
	}

	f.run(ctx, t, domain.RunKindJonah, 10) // 5% of the line's $200

	if _, err := f.dc.QueueReview(ctx, factory, versionID(1), policy); !errors.Is(err, domain.ErrLearningBudgetExhausted) {
		t.Errorf("review over budget = %v, want ErrLearningBudgetExhausted", err)
	}
}

// publishFile publishes path as content on top of the current version, as a person's later
// publish does.
func (f *decideFixture) publishFile(ctx context.Context, t *testing.T, path, content string) {
	t.Helper()

	if _, _, err := f.def.OpenDraft(ctx, factory); err != nil {
		t.Fatal(err)
	}

	rev, err := f.def.WriteFile(ctx, factory, path, []byte(content))
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := f.def.Publish(ctx, factory, rev, versionID(f.def.number), "later"); err != nil {
		t.Fatal(err)
	}
}

// Undo this change reverses the published edit against the current version, after a later
// publish changed the same file elsewhere, and publishes nothing; the file the proposal added is
// left. A second undo waits for the first to be decided.
func TestDecider_UndoReversesAfterLaterPublish(t *testing.T) {
	t.Parallel()

	f, ctx := newDecideFixture(t)

	if _, _, err := f.dc.Approve(ctx, factory, 1); err != nil {
		t.Fatal(err)
	}

	f.publishFile(ctx, t, agentPath, "# writer\nAttach a screenshot.\nBe brief.\n")

	undo, err := f.dc.Undo(ctx, factory, 1)
	if err != nil {
		t.Fatal(err)
	}

	first := f.state(ctx, t)
	now := f.def.current[agentPath]

	want := []domain.ProposalFile{{Path: agentPath, BaseSHA256: string(domain.HashBlob([]byte(now))), Content: "# writer\nBe brief.\n"}}
	if undo.Number != 2 || undo.Undoes != first.ID || undo.State != domain.ProposalProposed || undo.VersionID != versionID(3) ||
		!reflect.DeepEqual(undo.Files, want) {
		t.Errorf("undo = %+v, want proposal 2 undoing proposal 1 against v3 with %+v", undo, want)
	}

	if f.def.number != 3 || f.def.draft != nil {
		t.Errorf("after the undo the factory is at v%d with draft %v; want v3 and no draft: nothing publishes", f.def.number, f.def.draft)
	}

	if got := f.audit.actions(); got[len(got)-1] != domain.AuditProposalUndo {
		t.Errorf("audit = %v, want %s last", got, domain.AuditProposalUndo)
	}

	if _, err := f.dc.Undo(ctx, factory, 1); !errors.Is(err, ports.ErrConflict) {
		t.Errorf("second undo = %v, want ErrConflict", err)
	}
}

func TestDecider_UndoRefusals(t *testing.T) {
	t.Parallel()

	t.Run("a proposal never published", func(t *testing.T) {
		t.Parallel()

		f, ctx := newDecideFixture(t)
		if _, err := f.dc.Undo(ctx, factory, 1); !errors.Is(err, domain.ErrProposalNotPublished) {
			t.Errorf("undo = %v, want ErrProposalNotPublished", err)
		}
	})

	t.Run("the changed text is gone", func(t *testing.T) {
		t.Parallel()

		f, ctx := newDecideFixture(t)
		if _, _, err := f.dc.Approve(ctx, factory, 1); err != nil {
			t.Fatal(err)
		}

		f.publishFile(ctx, t, agentPath, "# writer\nAttach two screenshots.\n")

		_, err := f.dc.Undo(ctx, factory, 1)
		if !errors.Is(err, domain.ErrUndoConflict) || !strings.Contains(err.Error(), agentPath) {
			t.Errorf("undo = %v, want ErrUndoConflict naming %s", err, agentPath)
		}
	})
}

// A failed review never moves the window: the next manual review starts where the last done one
// ended (ADR 0039 decision 3).
func TestDecide_QueueReviewAfterAFailedOne(t *testing.T) {
	t.Parallel()

	f, ctx := newDecideFixture(t) // its review is done, its window ended at t0
	f.clock.Advance(time.Hour)

	policy := domain.DefaultJonahPolicy()

	failedOne, err := f.dc.QueueReview(ctx, factory, versionID(1), policy)
	if err != nil {
		t.Fatal(err)
	}

	claimed, err := f.store.ClaimJonahReview(ctx, f.clock.Now(), time.Minute)
	if err != nil || claimed.ID != failedOne.ID {
		t.Fatalf("ClaimJonahReview = %+v, %v", claimed, err)
	}

	if _, _, err := f.store.FinishJonahReview(ctx, claimed, domain.JonahReviewResult{State: domain.JonahFailed, Error: "x", At: f.clock.Now()}); err != nil {
		t.Fatal(err)
	}

	f.clock.Advance(time.Hour)

	r, err := f.dc.QueueReview(ctx, factory, versionID(1), policy)
	if err != nil {
		t.Fatal(err)
	}

	if !r.WindowStart.Equal(t0) {
		t.Errorf("window starts %s, want %s, where the last done review ended", r.WindowStart, t0)
	}
}
