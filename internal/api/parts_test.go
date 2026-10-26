package api_test

import (
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// assemble makes a new line item an assembly at build with plan's parts, as the engine would
// after its agent planned them, and returns the assembly.
func (h *harness) assemble(t *testing.T, plan []domain.NewPart) domain.Item {
	t.Helper()

	h.seed(t, stBuild, domain.StateQueued, h.clock.Now())

	it, l, err := h.store.ClaimNext(inOrg(t), ports.Claim{
		FactoryID: lineID, Station: build, Now: h.clock.Now(), ExpiresAt: h.clock.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}

	parent, _, err := h.store.Assemble(inOrg(t), ports.Assembly{
		Transition: ports.Transition{
			ItemID: it.ID, From: it.Position(), To: domain.Position{Station: stBuild, State: domain.StateAssembly},
			Lease: &l, Outcome: domain.LeaseAdvanced, At: h.clock.Now(),
		},
		Parts: plan,
	})
	if err != nil {
		t.Fatal(err)
	}

	return parent
}

func (h *harness) parts(t *testing.T, ref string) gen.AssemblyParts {
	t.Helper()

	var out gen.AssemblyParts
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/"+url.PathEscape(ref)+"/parts", "", &out); code != http.StatusOK {
		t.Fatalf("GET parts of %s = %d; want 200", ref, code)
	}

	return out
}

// assemblyPart is what TestAPI_ItemParts checks of one part.
type assemblyPart struct {
	Index, Wave      int
	Key, State       string
	Needs, WaitingOn []string
	Paths            []string
	Agent            string
	Done             bool
}

func assemblyPartOf(p gen.AssemblyPart) assemblyPart {
	return assemblyPart{
		Index: p.Index, Wave: p.Wave, Key: p.Key, State: string(p.State), Needs: p.Needs, WaitingOn: p.WaitingOn,
		Paths: p.Paths, Agent: deref(p.Agent), Done: p.DoneAt != nil,
	}
}

// partsInFlight is an assembly of three parts at build an hour after its first part, a, was
// done: b needs a and is being built, c needs a and b. It returns the assembly's ref.
func (h *harness) partsInFlight(t *testing.T) string {
	t.Helper()

	assembly := h.assemble(t, []domain.NewPart{
		{Key: "a", Title: "Contract", Lane: laneAPI, Paths: []string{"api/openapi.yaml"}},
		{Key: "b", Title: "Store", Lane: laneAPI, Paths: []string{"queries/a.sql"}, Needs: []string{"a"}},
		{Key: "c", Title: "Page", Lane: laneAPI, Needs: []string{"a", "b"}},
	})
	ref := "line#" + strconv.FormatInt(assembly.Number, 10)

	a, err := h.store.ItemByNumber(inOrg(t), lineID, assembly.Number+1)
	if err != nil {
		t.Fatal(err)
	}

	h.clock.Advance(time.Hour)
	h.move(t, a, domain.Position{Station: stDone, State: domain.StateDone}, func(tr *ports.Transition) { tr.DoneAt = h.clock.Now() })
	h.claim(t, 0) // b: its one need is done; c still waits on b
	h.clock.Advance(time.Hour)

	return ref
}

// TestAPI_ItemParts: an assembly's parts come in plan order with their needs, the needs still
// open, their waves and the agent working each; its own item shows state assembly and each part
// its key and paths. An item that is not an assembly has no parts; an unknown one is 404.
func TestAPI_ItemParts(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ref := h.partsInFlight(t)
	got := h.parts(t, ref)

	if got.Done != 1 || got.Total != 3 || got.LeadTimeSeconds != int64((2*time.Hour)/time.Second) || len(got.Parts) != 3 {
		t.Fatalf("parts = %d of %d, lead time %ds, %d parts; want 1 of 3, 7200s, 3 parts", got.Done, got.Total, got.LeadTimeSeconds, len(got.Parts))
	}

	a, b := got.Parts[0].Ref, got.Parts[1].Ref
	want := []assemblyPart{
		{Index: 1, Wave: 0, Key: "a", State: "done", Needs: []string{}, WaitingOn: []string{}, Paths: []string{"api/openapi.yaml"}, Done: true},
		{Index: 2, Wave: 1, Key: "b", State: "in_progress", Needs: []string{a}, WaitingOn: []string{}, Paths: []string{"queries/a.sql"}, Agent: agBackend},
		{Index: 3, Wave: 2, Key: "c", State: "queued", Needs: []string{a, b}, WaitingOn: []string{b}, Paths: []string{}},
	}

	for i, p := range got.Parts {
		if g := assemblyPartOf(p); !reflect.DeepEqual(g, want[i]) {
			t.Errorf("part %d = %+v; want %+v", i+1, g, want[i])
		}
	}

	h.partItems(t, ref, b)
}

// partItems checks the items of TestAPI_ItemParts' assembly ref and its part b, the parts of a
// part and of an unknown item.
func (h *harness) partItems(t *testing.T, ref, b string) {
	t.Helper()

	var it gen.Item
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/"+url.PathEscape(ref), "", &it); code != http.StatusOK || it.State != gen.ItemStateAssembly || it.PartKey != nil {
		t.Errorf("GET assembly = %d, state %q, part key %v; want 200, assembly and none", code, it.State, it.PartKey)
	}

	var part gen.Item
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/"+url.PathEscape(b), "", &part); code != http.StatusOK ||
		deref(part.PartKey) != "b" || part.PartPaths == nil || !reflect.DeepEqual(*part.PartPaths, []string{"queries/a.sql"}) {
		t.Errorf("GET part = %d, key %v, paths %v; want 200, b and [queries/a.sql]", code, part.PartKey, part.PartPaths)
	}

	if none := h.parts(t, b); !reflect.DeepEqual(none, gen.AssemblyParts{Parts: []gen.AssemblyPart{}}) {
		t.Errorf("parts of a part = %+v; want [] with 0 of 0", none)
	}

	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/line%23999/parts", "", nil); code != http.StatusNotFound {
		t.Errorf("GET parts of an unknown item = %d; want 404", code)
	}
}

// TestAPI_PauseAndRetryAnAssembly: pausing an assembly blocks its open parts and leaves it an
// assembly; retrying it sends them back to their queue, the done part untouched. With nothing
// left to pause or retry, both are conflicts.
func TestAPI_PauseAndRetryAnAssembly(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ref := h.partsInFlight(t)
	item := "/v1/orgs/acme/items/" + url.PathEscape(ref)

	states := func() []string {
		var out []string
		for _, p := range h.parts(t, ref).Parts {
			out = append(out, string(p.State))
		}

		return out
	}

	steps := []struct {
		action, body string
		status       int
		parts        []string
	}{
		{"pause", `{"reason":"replanning"}`, http.StatusOK, []string{"done", "blocked", "blocked"}},
		{"pause", ``, http.StatusConflict, []string{"done", "blocked", "blocked"}},
		{"retry", ``, http.StatusOK, []string{"done", "queued", "queued"}},
		{"retry", ``, http.StatusConflict, []string{"done", "queued", "queued"}},
	}

	for _, s := range steps {
		var it gen.Item

		code := h.call(t, http.MethodPost, item+"/"+s.action, s.body, &it)
		if code != s.status || (code == http.StatusOK && it.State != gen.ItemStateAssembly) {
			t.Fatalf("%s of the assembly = %d, state %q; want %d and assembly", s.action, code, it.State, s.status)
		}

		if got := states(); !reflect.DeepEqual(got, s.parts) {
			t.Errorf("parts after %s (%d) = %v; want %v", s.action, s.status, got, s.parts)
		}
	}
}
