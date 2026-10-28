package jonah_test

import (
	"context"
	"errors"
	"maps"
	"strconv"
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

	return f, ctx
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

	policy.WeeklyUSD = 0 // no spend at all is already the budget

	if _, err := f.dc.QueueReview(ctx, factory, versionID(1), policy); !errors.Is(err, domain.ErrLearningBudgetExhausted) {
		t.Errorf("review over budget = %v, want ErrLearningBudgetExhausted", err)
	}
}
