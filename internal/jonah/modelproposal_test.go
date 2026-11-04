package jonah_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/jonah"
)

const (
	modelAgent = "writer"
	modelOld   = "claude-sonnet-5-5"
	modelNew   = "claude-opus-5-5"
	modelVer   = domain.DefinitionVersionID("model-version-1")
	stReview   = "review"
)

// modelFactory is a factory.yaml whose writer names no model (the runtime's), whose reviewer
// names one, and whose skills list has an entry named like the writer.
const modelFactory = `apiVersion: goal/v1
kind: Factory
metadata:
  id: 3a1f6c2e-7b4d-4e85-9c0a-5d6e7f8a9b11
  name: acme-models
spec:
  runtime:
    model: claude-sonnet-5-5
  skills:
    - name: writer
      source: ./skills/writer
  agents:
    - name: writer
      prompt: ./agents/writer.md
    - name: reviewer
      model: claude-sonnet-5-5
      prompt: ./agents/reviewer.md
  board:
    stations:
      - { id: build, kind: agent, agent: writer }
      - { id: review, kind: agent, agent: reviewer }
      - { id: done, kind: done }
`

// modelFiles are a definition of factory.yaml raw with its agents' prompts and its skill.
func modelFiles(raw string) map[string][]byte {
	return map[string][]byte{
		"factory.yaml": []byte(raw), "agents/writer.md": []byte("# writer\n"), "agents/reviewer.md": []byte("# reviewer\n"),
		"skills/writer/SKILL.md": []byte("---\nname: writer\ndescription: Writes.\n---\nWrite.\n"),
	}
}

// modelWorld is ModelProposals over a porttest store with one factory whose line is build (the
// writer), review (the reviewer) and done, and whose policy allows the old and the new model.
type modelWorld struct {
	t     *testing.T
	store *porttest.Store
	defs  *reviewDefs
	audit *audits
	clock *porttest.Clock
	mp    *jonah.ModelProposals
	items int
}

func newModelWorld(t *testing.T) *modelWorld {
	t.Helper()

	clock := porttest.NewClock(rt0)
	store := porttest.NewStore()
	store.AddFactory(decideOrg, factory)

	policy := domain.DefaultJonahPolicy()
	policy.Share = 10

	w := &modelWorld{
		t: t, store: store, audit: &audits{}, clock: clock,
		defs: &reviewDefs{
			line: domain.Line{
				OrgID: decideOrg, FactoryID: factory, VersionID: modelVer, Number: 1, Jonah: policy,
				Runtime: domain.Runtime{ModelConnection: modelConn, Model: modelOld},
				Agents:  []domain.AgentSpec{{Name: modelAgent}, {Name: agentReviewer, Model: modelOld}},
				Stations: []domain.Station{
					{ID: stBuild, Kind: domain.KindAgent, Agent: modelAgent},
					{ID: stReview, Kind: domain.KindAgent, Agent: agentReviewer},
					{ID: stTryDone, Kind: domain.KindDone},
				},
			},
			files: modelFiles(modelFactory),
		},
	}

	w.mp = jonah.NewModelProposals(jonah.ModelDeps{
		Store: store, Definitions: w.defs, Check: config.ProposalCheck{}, Policies: porttest.NewOrgPolicies([]string{modelOld, modelNew}, nil, clock.Now),
		Audit: w.audit, Clock: clock,
	})

	return w
}

// doneItem is a done item the writer built at build, in a first run of a claim that cost usd,
// started at, sent back at review sentBack times.
func (w *modelWorld) doneItem(at time.Time, usd float64, sentBack int) domain.Item {
	w.t.Helper()

	w.items++

	it, err := w.store.CreateItem(w.ctx(), domain.NewItem{FactoryID: factory, Title: fmt.Sprintf("item %d", w.items), Station: stBuild})
	if err != nil {
		w.t.Fatal(err)
	}

	if it, err = w.store.Transition(w.ctx(), ports.Transition{
		ItemID: it.ID, From: it.Position(), To: domain.Position{Station: stTryDone, State: domain.StateDone}, At: at.Add(time.Hour),
		DoneAt: at.Add(time.Hour),
	}); err != nil {
		w.t.Fatal(err)
	}

	id, err := w.store.StartRun(w.ctx(), ports.NewRun{FactoryID: factory, ItemID: it.ID, Station: stBuild, Agent: modelAgent, StartedAt: at})
	if err != nil {
		w.t.Fatal(err)
	}

	if err := w.store.RecordRunInput(w.ctx(), id, domain.RunInput{Brief: "the brief", BranchHead: tryHead}); err != nil {
		w.t.Fatal(err)
	}

	if err := w.store.FinishRun(w.ctx(), id, domain.RunResult{Status: domain.RunSucceeded, CostUSD: usd}, at.Add(time.Minute)); err != nil {
		w.t.Fatal(err)
	}

	for range sentBack {
		if err := w.store.AppendEvents(w.ctx(), domain.Event{
			FactoryID: factory, ItemID: it.ID, At: at.Add(2 * time.Minute), Kind: domain.EventGateRejected, Station: stReview, Actor: agentReviewer,
		}); err != nil {
			w.t.Fatal(err)
		}
	}

	return it
}

func (w *modelWorld) ctx() context.Context {
	return domain.WithActor(domain.WithOrg(w.t.Context(), decideOrg), domain.LocalUser)
}

func (w *modelWorld) propose(confirm bool) (jonah.ModelEstimate, domain.Proposal, error) {
	return w.mp.Propose(w.ctx(), factory, jonah.ModelRequest{Version: modelVer, Agent: modelAgent, Model: modelNew, Confirm: confirm})
}

func TestModelProposals_EstimateStoresNothing(t *testing.T) {
	t.Parallel()

	w := newModelWorld(t)

	var items []domain.Item
	for i := range 6 {
		items = append(items, w.doneItem(rt0.Add(-time.Duration(6-i)*time.Hour), float64(i+1), i%2))
	}

	est, p, err := w.propose(false)
	if err != nil || p.ID != "" {
		t.Fatalf("Propose estimate = %+v, %v; want an estimate and no proposal", p, err)
	}

	expectEstimate(t, est, items)

	if ps, _ := w.store.Proposals(w.ctx(), ports.ProposalQuery{Factory: factory, Limit: 10}); len(ps) != 0 || len(w.audit.actions()) != 0 {
		t.Errorf("an estimate stored %d proposals and %v audit entries, want none", len(ps), w.audit.actions())
	}
}

func TestModelProposals_ConfirmQueuesThreeTriesPerItem(t *testing.T) {
	t.Parallel()

	w := newModelWorld(t)
	a := w.doneItem(rt0.Add(-2*time.Hour), 1, 2)
	b := w.doneItem(rt0.Add(-time.Hour), 0.5, 0)

	est, p, err := w.propose(true)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}

	if est.Tries != 6 {
		t.Errorf("estimate %+v, want 6 tries", est)
	}

	expectModelEdit(t, p)

	if len(p.Evidence) != 2 || p.Evidence[0].ItemID != b.ID || p.Evidence[1].Quote != fmt.Sprintf("#%d item 1: sent back 2 times at review, $1.00", a.Number) {
		t.Errorf("evidence = %+v, want b then a with how each went on the line", p.Evidence)
	}

	w.expectTries(p, a.ID, b.ID)

	if got := w.audit.actions(); len(got) != 1 || got[0] != domain.AuditProposalModel {
		t.Errorf("audit = %v, want one %s", got, domain.AuditProposalModel)
	}

	// The try sweep leaves a proposal with tries alone.
	w.sweep()
	w.expectTries(p, a.ID, b.ID)

	// From there it is an ordinary proposal.
	dc := jonah.NewDecider(jonah.DecideDeps{Proposals: w.store, Audit: w.audit, Clock: w.clock})
	if r, err := dc.Reject(w.ctx(), factory, p.Number, "too dear"); err != nil || r.State != domain.ProposalRejected {
		t.Errorf("Reject = %+v, %v; want rejected", r, err)
	}
}

// expectEstimate fails t unless est is of the writer's five newest of items, newest first: they
// cost 2 to 6, so 15 tries at $60, and the line spent 21 over the week and Jonah nothing, so a
// share of 10 leaves $210.
func expectEstimate(t *testing.T, est jonah.ModelEstimate, items []domain.Item) {
	t.Helper()

	if est.Agent != modelAgent || est.CurrentModel != modelOld || est.Model != modelNew || len(est.Items) != domain.MaxPastTryItems ||
		est.Items[0].ItemID != items[5].ID || est.Items[4].ItemID != items[1].ID {
		t.Fatalf("estimate = %+v, want the writer's five newest items, newest first, from %s to %s", est, modelOld, modelNew)
	}

	if est.Tries != 15 || est.EstimateUSD != 60 || est.BudgetLeftUSD != 210 {
		t.Errorf("estimate tries %d, $%v, $%v left; want 15 tries, $60 and $210 left", est.Tries, est.EstimateUSD, est.BudgetLeftUSD)
	}

	if got := est.Items[0].LineResult(); got != "sent back once at review, $6.00" {
		t.Errorf("line result of the newest = %q", got)
	}
}

// sweep runs a try sweep over w's line with Jonah on.
func (w *modelWorld) sweep() {
	w.t.Helper()

	line := w.defs.line
	line.Jonah.Enabled = true

	loop := jonah.NewTries(jonah.TryDeps{
		Orgs: oneOrg(decideOrg), Jonah: w.store, Tries: w.store, Definitions: &tryDefs{lines: map[domain.OrgID]domain.Line{decideOrg: line}},
		Waiting: w.store, Clock: w.clock,
	}, jonah.TryConfig{})
	if err := loop.Sweep(w.ctx()); err != nil {
		w.t.Fatal(err)
	}
}

// oneOrg is jonah.Orgs with one live organization.
type oneOrg domain.OrgID

func (o oneOrg) ActiveOrgs(context.Context) ([]domain.OrgID, error) {
	return []domain.OrgID{domain.OrgID(o)}, nil
}

// expectModelEdit fails t unless p is proposal 1 of the writer and its one edit, a replacement in factory.yaml, gives the writer
// the new model and leaves the skill named like it.
func expectModelEdit(t *testing.T, p domain.Proposal) {
	t.Helper()

	if p.Number != 1 || p.Agent != modelAgent || p.ReviewID != "" || p.VersionID != modelVer || p.State != domain.ProposalProposed {
		t.Fatalf("proposal %+v, want proposal 1 of the writer on %s", p, modelVer)
	}

	if len(p.Edits) != 1 || p.Edits[0].Path != "factory.yaml" || len(p.Edits[0].Replacements) != 1 {
		t.Fatalf("edits = %+v, want one replacement in factory.yaml", p.Edits)
	}

	after, err := p.Edits[0].Apply([]byte(modelFactory), true)
	if err != nil || !strings.Contains(after, "    - name: writer\n      model: "+modelNew+"\n      prompt: ./agents/writer.md\n") ||
		!strings.Contains(after, "    - name: writer\n      source: ./skills/writer\n") {
		t.Fatalf("factory.yaml after = %q, %v; want the writer's entry with the new model and the skill unchanged", after, err)
	}
}

// expectTries fails the test unless p has three queued tries of each of items, at build with
// the new model.
func (w *modelWorld) expectTries(p domain.Proposal, items ...domain.ItemID) {
	w.t.Helper()

	tries, err := w.store.ProposalPastTries(w.ctx(), p.ID)
	if err != nil || len(tries) != domain.ModelTriesPerItem*len(items) {
		w.t.Fatalf("tries = %+v, %v; want three per item", tries, err)
	}

	per := map[domain.ItemID]int{}

	for _, tr := range tries {
		if tr.Model != modelNew || tr.Station != stBuild || tr.State != domain.PastTryQueued {
			w.t.Errorf("try %+v, want a queued try of %s at %s", tr, modelNew, stBuild)
		}

		per[tr.ItemID]++
	}

	for _, it := range items {
		if per[it] != domain.ModelTriesPerItem {
			w.t.Errorf("tries per item = %v, want three of %s", per, it)
		}
	}
}

func TestModelProposals_SupersedesOverlapping(t *testing.T) {
	t.Parallel()

	w := newModelWorld(t)
	w.doneItem(rt0.Add(-time.Hour), 1, 0)

	_, first, err := w.propose(true)
	if err != nil {
		t.Fatal(err)
	}

	_, second, err := w.mp.Propose(w.ctx(), factory, jonah.ModelRequest{Version: modelVer, Agent: modelAgent, Model: modelNew, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}

	if got, _ := w.store.Proposal(w.ctx(), factory, first.Number); got.State != domain.ProposalSuperseded || second.Number != 2 {
		t.Errorf("first is %s after a second model proposal %d, want superseded", got.State, second.Number)
	}
}

func TestModelProposals_EditsTheAgentsEntry(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, factory, want string
	}{
		{
			"model line", strings.Replace(modelFactory, "    - name: writer\n      prompt", "    - name: writer\n      model: claude-sonnet-5-5\n      prompt", 1),
			"    - name: writer\n      model: " + modelNew + "\n      prompt",
		},
		{
			"model before name", strings.Replace(modelFactory, "    - name: writer\n      prompt: ./agents/writer.md\n",
				"    - model: claude-sonnet-5-5\n      name: writer\n      prompt: ./agents/writer.md\n", 1),
			"    - model: " + modelNew + "\n      name: writer\n",
		},
		{
			"flow style", strings.Replace(modelFactory, "    - name: writer\n      prompt: ./agents/writer.md\n",
				"    - { name: writer, prompt: ./agents/writer.md }\n", 1),
			"    - { name: writer, prompt: ./agents/writer.md, model: " + modelNew + " }\n",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			w := newModelWorld(t)
			w.defs.files = modelFiles(c.factory)
			w.doneItem(rt0.Add(-time.Hour), 1, 0)

			_, p, err := w.propose(true)
			if err != nil {
				t.Fatalf("Propose: %v", err)
			}

			after, err := p.Edits[0].Apply([]byte(c.factory), true)
			if err != nil || !strings.Contains(after, c.want) {
				t.Errorf("factory.yaml after = %q, %v; want %q", after, err, c.want)
			}
		})
	}
}

func TestModelProposals_Refusals(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		setup  func(w *modelWorld)
		r      jonah.ModelRequest
		want   error
		priced bool
	}{
		{"model outside the policy", nil, jonah.ModelRequest{Agent: modelAgent, Model: "claude-haiku-5-5"}, domain.ErrModelNotAllowed, false},
		{"the agent's model", nil, jonah.ModelRequest{Agent: modelAgent, Model: modelOld}, domain.ErrModelNotAllowed, false},
		{"unknown agent", nil, jonah.ModelRequest{Agent: "nobody", Model: modelNew}, domain.ErrAgentNotFound, false},
		{
			"agent with connections", func(w *modelWorld) { w.defs.line.Agents[0].Connections = []string{"github"} },
			jonah.ModelRequest{Agent: modelAgent, Model: modelNew},
			domain.ErrAgentNotTriable, false,
		},
		{"no finished items", nil, jonah.ModelRequest{Agent: agentReviewer, Model: modelNew}, domain.ErrNoFinishedItems, false},
		{
			"over budget", func(w *modelWorld) { w.defs.line.Jonah.Share = 0.05 },
			jonah.ModelRequest{Agent: modelAgent, Model: modelNew, Confirm: true},
			domain.ErrLearningBudgetExhausted, true,
		},
		{
			"over budget, estimate only", func(w *modelWorld) { w.defs.line.Jonah.Share = 0.05 },
			jonah.ModelRequest{Agent: modelAgent, Model: modelNew},
			domain.ErrLearningBudgetExhausted, true,
		},
		{"version gone", nil, jonah.ModelRequest{Version: "gone", Agent: modelAgent, Model: modelNew}, ports.ErrNotFound, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			w := newModelWorld(t)
			w.doneItem(rt0.Add(-time.Hour), 4, 0)

			if c.setup != nil {
				c.setup(w)
			}

			if c.r.Version == "" {
				c.r.Version = modelVer
			}

			est, p, err := w.mp.Propose(w.ctx(), factory, c.r)
			if !errors.Is(err, c.want) || p.ID != "" {
				t.Fatalf("Propose = %+v, %v; want %v and no proposal", p, err, c.want)
			}

			if c.priced != (est.EstimateUSD == 12) {
				t.Errorf("estimate with the refusal = %+v, want it priced: %v", est, c.priced)
			}

			if ps, _ := w.store.Proposals(w.ctx(), ports.ProposalQuery{Factory: factory, Limit: 10}); len(ps) != 0 {
				t.Errorf("a refusal stored %d proposals", len(ps))
			}
		})
	}
}

// A model proposal also tries each kept test at the agent's stations, three times on the new
// model with the content key of the version it changes, and its estimate counts those tries; a
// kept test at another agent's station gets none.
func TestModelProposals_ConfirmQueuesThreeTriesPerKeptTest(t *testing.T) {
	t.Parallel()

	w := newModelWorld(t)
	a := w.doneItem(rt0.Add(-time.Hour), 1, 0)

	k, err := w.store.KeepTest(w.ctx(), domain.NewKeptTest{FactoryID: factory, ItemID: a.ID, Station: stBuild, KeptBy: domain.LocalUser, KeptAt: rt0})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := w.store.KeepTest(w.ctx(), domain.NewKeptTest{FactoryID: factory, ItemID: a.ID, Station: stReview, KeptBy: domain.LocalUser, KeptAt: rt0}); err != nil {
		t.Fatal(err)
	}

	est, p, err := w.propose(true)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}

	if est.Tries != 2*domain.ModelTriesPerItem || est.EstimateUSD != 2*domain.ModelTriesPerItem {
		t.Errorf("estimate %d tries at $%v, want 6 at $6: three of the item and three of its kept test", est.Tries, est.EstimateUSD)
	}

	w.expectKeptTries(p, k.ID)
}

// expectKeptTries fails the test unless p's kept-test tries are three of test, at build on the new
// model, with the content key of the files p's edits make.
func (w *modelWorld) expectKeptTries(p domain.Proposal, test domain.KeptTestID) {
	w.t.Helper()

	edited, err := domain.ApplyEdits(w.defs.files, p.Edits)
	if err != nil {
		w.t.Fatal(err)
	}

	tries, err := w.store.ProposalPastTries(w.ctx(), p.ID)
	if err != nil {
		w.t.Fatal(err)
	}

	tries = slices.DeleteFunc(tries, func(tr domain.PastTry) bool { return tr.KeptTestID == "" })
	if len(tries) != domain.ModelTriesPerItem {
		w.t.Fatalf("kept-test tries = %+v, want three", tries)
	}

	for i, tr := range tries {
		if tr.KeptTestID != test || tr.Attempt != i+1 || tr.Model != modelNew || tr.Station != stBuild || tr.ContentKey != domain.ContentKey(edited) {
			w.t.Errorf("kept-test try %+v, want attempt %d of test %s at %s on %s with the proposed content's key", tr, i+1, test, stBuild, modelNew)
		}
	}
}
