package api_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
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
		{Key: "a", Title: "Contract", Lane: laneAPI, Paths: []string{"contracts/api/openapi.yaml"}},
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
		{Index: 1, Wave: 0, Key: "a", State: "done", Needs: []string{}, WaitingOn: []string{}, Paths: []string{"contracts/api/openapi.yaml"}, Done: true},
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

	states := func() []gen.ItemState {
		var out []gen.ItemState
		for _, p := range h.parts(t, ref).Parts {
			out = append(out, p.State)
		}

		return out
	}

	done, blocked, queued := gen.ItemStateDone, gen.ItemStateBlocked, gen.ItemStateQueued
	steps := []struct {
		action, body string
		status       int
		parts        []gen.ItemState
	}{
		{actPause, `{"reason":"replanning"}`, http.StatusOK, []gen.ItemState{done, blocked, blocked}},
		{actPause, ``, http.StatusConflict, []gen.ItemState{done, blocked, blocked}},
		{actRetry, ``, http.StatusOK, []gen.ItemState{done, queued, queued}},
		{actRetry, ``, http.StatusConflict, []gen.ItemState{done, queued, queued}},
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

// A part's item read names its assembly with its place among the parts in plan order, from one
// read of the parts; a mutation's answer names its key without reading them, and neither a plain
// item nor an assembly is part of one.
func TestAPI_GetItemPartOfAssembly(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	asm := h.assemble(t, []domain.NewPart{
		{Key: "a", Title: "Contract", Lane: laneAPI},
		{Key: "b", Title: "Store", Lane: laneAPI, Needs: []string{"a"}},
		{Key: "c", Title: "Page", Lane: laneAPI, Needs: []string{"b"}},
	})
	plain := h.seed(t, stBuild, domain.StateQueued, h.clock.Now())

	parts, err := h.store.Parts(inOrg(t), asm.ID)
	if err != nil {
		t.Fatal(err)
	}

	h.seam.steps()

	var got gen.Item
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/"+string(parts.Items[1].ID), "", &got); code != http.StatusOK {
		t.Fatalf("GET part = %d; want 200", code)
	}

	want := &gen.PartOf{Id: string(asm.ID), Ref: "line#" + strconv.FormatInt(asm.Number, 10), Title: asm.Title, Part: 2, Parts: 3}
	if !reflect.DeepEqual(got.Assembly, want) || deref(got.PartKey) != "b" {
		t.Errorf("GET part: assembly %+v, key %v; want %+v, b", got.Assembly, got.PartKey, want)
	}

	if n := partsReads(h.seam.steps()); n != 1 {
		t.Errorf("GET part read the parts %d times; want once", n)
	}

	var paused gen.Item
	if code := h.call(t, http.MethodPost, "/v1/orgs/acme/items/"+string(parts.Items[2].ID)+"/pause", "", &paused); code != http.StatusOK {
		t.Fatalf("pause part = %d; want 200", code)
	}

	if deref(paused.PartKey) != "c" || paused.Assembly != nil {
		t.Errorf("pause answer: key %v, assembly %+v; want c and none", paused.PartKey, paused.Assembly)
	}

	if n := partsReads(h.seam.steps()); n != 0 {
		t.Errorf("pause read the parts %d times; want none", n)
	}

	for _, id := range []domain.ItemID{asm.ID, plain.ID} {
		var it gen.Item
		if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/"+string(id), "", &it); code != http.StatusOK || it.Assembly != nil {
			t.Errorf("GET %s = %d, assembly %+v; want 200 and none", id, code, it.Assembly)
		}
	}
}

func partsReads(steps []step) int {
	n := 0

	for _, s := range steps {
		if s.read == "Parts" {
			n++
		}
	}

	return n
}

// addPart posts body as a part of ref and returns the answer's status, parts and problem.
func (h *harness) addPart(t *testing.T, ref, body string) (int, gen.AssemblyParts, gen.Problem) {
	t.Helper()

	var raw json.RawMessage

	code := h.call(t, http.MethodPost, "/v1/orgs/acme/items/"+url.PathEscape(ref)+"/parts", body, &raw)

	var (
		parts gen.AssemblyParts
		p     gen.Problem
	)

	out := any(&p)
	if code == http.StatusCreated {
		out = &parts
	}

	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("add part to %s: %d %s: %v", ref, code, raw, err)
	}

	return code, parts, p
}

// TestAPI_AddItemPart: a part added to an open assembly comes back last among its parts, keyed by
// its title and waiting on what it needs, in the body listItemParts answers; a malformed part is
// 400 part-invalid, a plain item 409 not-an-assembly and a done assembly 409 item-closed.
func TestAPI_AddItemPart(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ref := h.partsInFlight(t)

	code, got, p := h.addPart(t, ref, `{"title":"  Docs page ","lane":"api","brief":"Document it","needs":["b"]}`)
	if code != http.StatusCreated {
		t.Fatalf("add part = %d %+v; want 201", code, p)
	}

	if !reflect.DeepEqual(got, h.parts(t, ref)) {
		t.Errorf("add part answered %+v; want the assembly's parts as listItemParts answers them", got)
	}

	b := got.Parts[1].Ref
	want := assemblyPart{Index: 4, Wave: 2, Key: "docs-page", State: "queued", Needs: []string{b}, WaitingOn: []string{b}, Paths: []string{}}

	if got.Total != 4 || len(got.Parts) != 4 || !reflect.DeepEqual(assemblyPartOf(got.Parts[3]), want) || got.Parts[3].Station != stBuild {
		t.Fatalf("parts = %d, new part %+v at %s; want 4 and %+v at %s", got.Total, got.Parts[len(got.Parts)-1], got.Parts[len(got.Parts)-1].Station, want, stBuild)
	}

	plain := h.seed(t, stBuild, domain.StateQueued, h.clock.Now())
	closed := newHarness(t)
	done := closed.assemble(t, []domain.NewPart{{Key: "a", Title: "Only", Lane: laneAPI}})
	closed.move(t, done, domain.Position{Station: stDone, State: domain.StateDone}, func(tr *ports.Transition) { tr.DoneAt = h.clock.Now() })

	refusals := []struct {
		h               *harness
		name, ref, body string
		status          int
		code            string
	}{
		{h, "an unknown need", ref, `{"title":"Late","brief":"x","needs":["zzz"]}`, http.StatusBadRequest, "part-invalid"},
		{h, "an empty title", ref, `{"title":"  ","brief":"x"}`, http.StatusBadRequest, "part-invalid"},
		{h, "a title too long", ref, `{"title":"` + strings.Repeat("t", 201) + `","brief":"x"}`, http.StatusBadRequest, "part-invalid"},
		{h, "a plain item", "line#" + strconv.FormatInt(plain.Number, 10), `{"title":"Late","brief":"x"}`, http.StatusConflict, "not-an-assembly"},
		{closed, "a done assembly", "line#" + strconv.FormatInt(done.Number, 10), `{"title":"Late","brief":"x"}`, http.StatusConflict, codeItemClosed},
	}

	for _, r := range refusals {
		if code, _, p := r.h.addPart(t, r.ref, r.body); code != r.status || deref(p.Code) != r.code {
			t.Errorf("add part with %s = %d %q; want %d %q", r.name, code, deref(p.Code), r.status, r.code)
		}
	}

	if after := h.parts(t, ref); after.Total != 4 {
		t.Errorf("parts after the refusals = %d; want 4: a refusal adds nothing", after.Total)
	}
}

// A factory viewer may read an assembly's parts but not add one: item:create is not theirs.
func TestAPI_AddItemPartNeedsItemCreate(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)

	var it gen.Item
	if res := h.as(t, ownerToken, http.MethodPost, acmeItems, `{"title":"Plain","type":"feature"}`); res.status != http.StatusCreated || json.Unmarshal(res.body, &it) != nil {
		t.Fatalf("the owner submitting = %d %s; want 201", res.status, res.body)
	}

	path := "/v1/orgs/acme/items/" + url.PathEscape(it.Ref) + "/parts"

	if res := h.as(t, viewerToken, http.MethodGet, path, ""); res.status != http.StatusOK {
		t.Errorf("the viewer reading parts = %d %s; want 200", res.status, res.body)
	}

	if res := h.as(t, viewerToken, http.MethodPost, path, `{"title":"Late","brief":"x"}`); res.status != http.StatusForbidden {
		t.Errorf("the viewer adding a part = %d %s; want 403", res.status, res.body)
	}
}
