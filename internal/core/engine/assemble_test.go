package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

const (
	agArchitect = "architect"
	testFactory = "acme-api"
	keyStore    = "store"
	// plan is a station output that splits its item in three: api and web both need store.
	plan = `{"lane":"data","plan":[
		{"key":"store","title":"Store","lane":"backend","brief":"Add the table.","paths":["db/migrations/"]},
		{"key":"api","title":"API","lane":"backend","brief":"Expose it.","paths":["contracts/api/"],"needs":["store"]},
		{"key":"web","title":"Web","lane":"frontend","brief":"Show it.","paths":["web/"],"needs":["store"]}
	]}`
	approvedPlan = `{"decision":"approve","plan":[{"key":"store","title":"Store","brief":"Add the table."}]}`
)

// planLine is spec then build, with materialize as given.
func planLine(materialize bool) domain.Line {
	l := lineOf(agentStation(stSpec, agArchitect, 1, 2), agentStation(stBuild, agWorker, 1, 1))
	l.Flow.Assemblies.Materialize = materialize

	return l
}

// planGateLine is spec then a review gate that merges on approval, with materialize on.
func planGateLine() domain.Line {
	review := agentStation(stReview, agReviewer, 1, 1)
	review.Gate = &domain.Gate{OnReject: stSpec, MaxRework: 2}
	review.Action = domain.ActionMerge

	l := lineOf(agentStation(stSpec, agArchitect, 1, 1), review)
	l.Flow.Assemblies.Materialize = true

	return l
}

func (h *harness) parts(t *testing.T, id domain.ItemID) domain.Parts {
	t.Helper()

	parts, err := h.store.Parts(inOrg(t), id)
	if err != nil {
		t.Fatal(err)
	}

	return parts
}

// needsOf returns the keys of the parts a part needs, through planned edges.
func (h *harness) needsOf(t *testing.T, part domain.Item, keys map[domain.ItemID]string) []string {
	t.Helper()

	edges, err := h.store.Edges(inOrg(t), part.ID)
	if err != nil {
		t.Fatal(err)
	}

	var needs []string

	for _, e := range edges.Needs {
		if e.Kind == domain.EdgePlanned {
			needs = append(needs, keys[e.Needs])
		}
	}

	slices.Sort(needs)

	return needs
}

// A station that finishes with a plan makes its item an assembly at that station: each part is
// queued there with its brief and one planned edge per need, and the item does not move on.
func TestAssemble_PlanBecomesAssemblyWithParts(t *testing.T) {
	t.Parallel()

	h := newHarness(t, planLine(true), porttest.Succeed(plan))
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	got := h.item(t, it.ID)
	if got.Position() != (domain.Position{Station: stSpec, State: domain.StateAssembly}) {
		t.Fatalf("item at %+v, want an assembly at %s (events %v)", got.Position(), stSpec, h.kinds(it.ID))
	}

	if got.Lane != "data" || len(got.Outputs[stSpec]) == 0 {
		t.Errorf("assembly lane %q outputs %s, want the spec's lane and output kept", got.Lane, got.Outputs)
	}

	h.expectOutcomes(t, it.ID, domain.LeaseAdvanced)

	if len(h.eventsOf(it.ID, domain.EventAssembled)) != 1 {
		t.Errorf("events %v, want one assembled", h.kinds(it.ID))
	}

	parts := h.parts(t, it.ID)
	if parts.Total != 3 {
		t.Fatalf("parts = %d, want 3", parts.Total)
	}

	keys := map[domain.ItemID]string{}
	for _, p := range parts.Items {
		keys[p.ID] = p.PartKey
	}

	briefs := map[string]string{keyStore: "Add the table.", "api": "Expose it.", "web": "Show it."}
	needs := map[string][]string{keyStore: nil, "api": {keyStore}, "web": {keyStore}}

	for _, p := range parts.Items {
		if p.Position() != (domain.Position{Station: stSpec, State: domain.StateQueued}) || p.ParentID != it.ID {
			t.Errorf("part %s at %+v parent %s, want queued at %s under %s", p.PartKey, p.Position(), p.ParentID, stSpec, it.ID)
		}

		if p.Body != briefs[p.PartKey] {
			t.Errorf("part %s body %q, want its brief %q", p.PartKey, p.Body, briefs[p.PartKey])
		}

		if got := h.needsOf(t, p, keys); !slices.Equal(got, needs[p.PartKey]) {
			t.Errorf("part %s needs %v, want %v", p.PartKey, got, needs[p.PartKey])
		}
	}
}

// At a gate only an approval assembles, and it merges nothing: the parts make the pull requests.
// A rejection ignores the plan and sends the item back.
func TestAssemble_AtAGateOnlyAnApprovalAssembles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		verdict string
		want    domain.Position
		parts   int
	}{
		{
			name: "approve", verdict: approvedPlan,
			want: domain.Position{Station: stReview, State: domain.StateAssembly}, parts: 1,
		},
		{
			name:    "reject",
			verdict: `{"decision":"reject","reason":"too big","plan":[{"key":"a","title":"A","needs":["a"]}]}`,
			want:    domain.Position{Station: stSpec, State: domain.StateQueued},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, planGateLine(), byAgent(map[string]string{agArchitect: `{}`, agReviewer: tt.verdict}))
			it := h.submit(t, domain.NewItem{})

			h.step(t) // specified
			h.step(t) // reviewed

			if got := h.item(t, it.ID); got.Position() != tt.want {
				t.Fatalf("item at %+v, want %+v (events %v)", got.Position(), tt.want, h.kinds(it.ID))
			}

			if got := h.parts(t, it.ID).Total; got != tt.parts {
				t.Errorf("parts = %d, want %d", got, tt.parts)
			}

			if n := len(h.codehost.Merges()); n != 0 {
				t.Errorf("%d pull requests merged, want none", n)
			}
		})
	}
}

// With materialize off a plan is ignored and the item moves on as without one.
func TestAssemble_MaterializeOffMovesOn(t *testing.T) {
	t.Parallel()

	h := newHarness(t, planLine(false), porttest.Succeed(plan))
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	if got := h.item(t, it.ID); got.Position() != (domain.Position{Station: stBuild, State: domain.StateQueued}) {
		t.Fatalf("item at %+v, want queued at %s (events %v)", got.Position(), stBuild, h.kinds(it.ID))
	}

	if got := h.parts(t, it.ID).Total; got != 0 {
		t.Errorf("parts = %d, want none", got)
	}
}

// Assemblies do not nest: a part whose output has a plan is blocked and gets no parts.
func TestAssemble_PartPlanIsBlocked(t *testing.T) {
	t.Parallel()

	h := newHarness(t, planLine(true), porttest.Succeed(plan))
	it := h.submit(t, domain.NewItem{})

	h.step(t) // the item becomes an assembly, its parts queued at spec
	h.step(t) // store, the part that needs none, runs spec and plans again

	for _, p := range h.parts(t, it.ID).Items {
		want := domain.StateQueued // api and web wait for store
		if p.PartKey == keyStore {
			want = domain.StateBlocked
		}

		if p.State != want {
			t.Errorf("part %s %s, want %s", p.PartKey, p.State, want)
		}

		if got := h.parts(t, p.ID).Total; got != 0 {
			t.Errorf("part %s has %d parts, want none", p.PartKey, got)
		}

		if p.State != domain.StateBlocked {
			continue
		}

		ev := h.eventsOf(p.ID, domain.EventBlocked)
		if len(ev) != 1 || ev[0].Data["reason"] != "a part cannot plan an assembly" {
			t.Errorf("part %s blocked events %+v, want one saying a part cannot plan an assembly", p.PartKey, ev)
		}
	}
}

// A part's agent hears which assembly it serves: the assembly's ref and title, the part's key,
// the other parts with their state, what it needs and that a part returns a spec, never a plan.
// An item that is not a part hears none of it.
func TestAssemble_PartRunInputNamesItsAssembly(t *testing.T) {
	t.Parallel()

	h := newHarness(t, planLine(true), porttest.Succeed(plan))
	it := h.submit(t, domain.NewItem{Title: "Ship the report"})

	h.step(t) // the item plans its assembly
	h.step(t) // store, the part that needs none, runs spec

	runs := h.worker.Runs()
	if len(runs) != 2 {
		t.Fatalf("%d runs, want the item's and store's", len(runs))
	}

	if f := h.feedback(0); strings.Contains(f, "## Assembly") {
		t.Errorf("the planning item's feedback has an assembly section:\n%s", f)
	}

	ref := func(n int64) string { return domain.ItemRef{Factory: testFactory, Number: n}.String() }
	want := []string{
		"## Assembly", "part `store` of assembly " + ref(it.Number) + ", \"Ship the report\"",
		"needs no other part", "A part returns a spec, never a plan",
	}

	for _, p := range h.parts(t, it.ID).Items {
		if p.PartKey == keyStore {
			if runs[1].Sandbox.ItemID != p.ID {
				t.Fatalf("second run is of %s, want store's", runs[1].Sandbox.ItemID)
			}

			continue
		}

		want = append(want, fmt.Sprintf("- `%s` %s %q: %s", p.PartKey, ref(p.Number), p.Title, domain.StateQueued))
	}

	f := h.feedback(1)
	for _, w := range want {
		if !strings.Contains(f, w) {
			t.Errorf("store's feedback lacks %q:\n%s", w, f)
		}
	}
}

// A part that needs others hears their keys once they are done and it runs.
func TestAssemble_PartRunInputNamesItsNeeds(t *testing.T) {
	t.Parallel()

	ok := func(out string) domain.RunResult {
		return domain.RunResult{Status: domain.RunSucceeded, Output: json.RawMessage(out)}
	}
	h := newHarness(t, planLine(true), porttest.Sequence(ok(plan), ok(`{"lane":"backend"}`)))
	it := h.submit(t, domain.NewItem{})

	h.step(t) // the item plans its assembly

	var api domain.Item

	for _, p := range h.parts(t, it.ID).Items {
		if p.PartKey == "api" {
			api = p
		}
	}

	for range 6 { // store runs spec and build; then api runs spec
		h.step(t)
	}

	i := slices.IndexFunc(h.worker.Runs(), func(c porttest.ExecCall) bool { return c.Sandbox.ItemID == api.ID })
	if i < 0 {
		t.Fatalf("api never ran (events %v)", h.kinds(api.ID))
	}

	if f := h.feedback(i); !strings.Contains(f, "This part needs `store`.") {
		t.Errorf("api's feedback lacks its need:\n%s", f)
	}
}

// partsDown is a store whose Parts fails once the assembly has parts.
type partsDown struct{ ports.Store }

var errPartsDown = errors.New("parts unavailable")

func (s partsDown) Parts(ctx context.Context, id domain.ItemID) (domain.Parts, error) {
	parts, err := s.Store.Parts(ctx, id)
	if err == nil && parts.Total > 0 {
		return domain.Parts{}, errPartsDown
	}

	return parts, err
}

// A part whose assembly cannot be read is blocked with the reason; it never runs without it.
func TestAssemble_PartWithoutItsAssemblyIsBlocked(t *testing.T) {
	t.Parallel()

	h := newHarness(t, planLine(true), porttest.Succeed(plan))
	h.e = h.engine(t, partsDown{Store: h.store})
	it := h.submit(t, domain.NewItem{})

	h.step(t) // the item plans its assembly
	h.step(t) // store is claimed, but its assembly cannot be read

	if n := len(h.worker.Runs()); n != 1 {
		t.Errorf("%d runs, want only the item's", n)
	}

	parts, err := h.store.Parts(inOrg(t), it.ID)
	if err != nil {
		t.Fatal(err)
	}

	for _, p := range parts.Items {
		if p.PartKey != keyStore {
			continue
		}

		ev := h.eventsOf(p.ID, domain.EventBlocked)
		if len(ev) != 1 || !strings.Contains(fmt.Sprint(ev[0].Data["reason"]), errPartsDown.Error()) {
			t.Errorf("store %s, blocked events %+v; want blocked saying %q", h.item(t, p.ID).State, ev, errPartsDown)
		}
	}
}

// A plan the platform cannot assemble is a failed output: the agent tries again, then the item
// is blocked.
func TestAssemble_BadPlanRetriesThenBlocks(t *testing.T) {
	t.Parallel()

	cycle := `{"plan":[{"key":"a","title":"A","needs":["b"]},{"key":"b","title":"B","needs":["a"]}]}`
	h := newHarness(t, planLine(true), porttest.Succeed(cycle))
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	if got := h.item(t, it.ID); got.Position() != (domain.Position{Station: stSpec, State: domain.StateBlocked}) {
		t.Fatalf("item at %+v, want blocked at %s (events %v)", got.Position(), stSpec, h.kinds(it.ID))
	}

	if n := len(h.eventsOf(it.ID, domain.EventContractFailed)); n != 2 {
		t.Errorf("%d contract_failed events, want 2 (events %v)", n, h.kinds(it.ID))
	}
}

// lostAssembly is a store whose Assemble commits but whose acknowledgement never arrives.
type lostAssembly struct{ ports.Store }

var errLostAck = errors.New("connection reset after commit")

func (s lostAssembly) Assemble(ctx context.Context, a ports.Assembly) (domain.Item, domain.Parts, error) {
	if _, _, err := s.Store.Assemble(ctx, a); err != nil {
		return domain.Item{}, domain.Parts{}, err
	}

	return domain.Item{}, domain.Parts{}, errLostAck
}

// An Assemble whose acknowledgement is lost still leaves one assembly with one set of parts, and
// asking again returns the same parts.
func TestAssemble_LostAcknowledgementCreatesNoDuplicates(t *testing.T) {
	t.Parallel()

	h := newHarness(t, planLine(true), porttest.Succeed(plan))
	h.e = h.engine(t, lostAssembly{Store: h.store})
	it := h.submit(t, domain.NewItem{})

	h.step(t)

	if got := h.item(t, it.ID); got.State != domain.StateAssembly {
		t.Fatalf("item %s/%s, want an assembly (events %v)", got.Station, got.State, h.kinds(it.ID))
	}

	first := h.parts(t, it.ID)
	if first.Total != 3 {
		t.Fatalf("parts = %d, want 3", first.Total)
	}

	parts, err := domain.PlanFrom([]byte(plan))
	if err != nil {
		t.Fatal(err)
	}

	_, again, err := h.store.Assemble(inOrg(t), ports.Assembly{
		Transition: ports.Transition{
			ItemID: it.ID, From: domain.Position{Station: stSpec, State: domain.StateInProgress},
			To: domain.Position{Station: stSpec, State: domain.StateAssembly}, At: h.clock.Now(),
		},
		Parts: parts,
	})
	if err != nil {
		t.Fatal(err)
	}

	ids := func(p domain.Parts) []domain.ItemID {
		var out []domain.ItemID
		for _, it := range p.Items {
			out = append(out, it.ID)
		}

		return out
	}

	if !slices.Equal(ids(again), ids(first)) || h.parts(t, it.ID).Total != 3 {
		t.Errorf("retry returned %v, want the same parts %v and no more", ids(again), ids(first))
	}
}
