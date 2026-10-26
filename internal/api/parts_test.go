package api_test

import (
	"net/http"
	"reflect"
	"strconv"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// keyHandler is the key of the assembly's second part.
const keyHandler = "handler"

// assemble makes a plain item at build an assembly of three parts, as the engine would, and
// moves the second part on to merge; it returns the assembly and its parts in plan order.
func (h *harness) assemble(t *testing.T) (domain.Item, domain.Parts) {
	t.Helper()

	it := h.seed(t, stBuild, domain.StateQueued, t0)

	asm, parts, err := h.store.Assemble(inOrg(t), ports.Assembly{
		Transition: ports.Transition{
			ItemID: it.ID, From: it.Position(), To: domain.Position{Station: stBuild, State: domain.StateAssembly}, At: h.clock.Now(),
		},
		Parts: []domain.NewPart{
			{Key: "store", Title: "The schema", Paths: []string{"migrations/**"}},
			{Key: keyHandler, Title: "The endpoint", Paths: []string{"internal/api/**", "api/openapi.yaml"}, Needs: []string{"store"}},
			{Key: "screen", Title: "The page", Needs: []string{keyHandler}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	parts.Items[1] = h.move(t, parts.Items[1], domain.Position{Station: stMerge, State: domain.StateWaitingHuman}, nil)

	return asm, parts
}

// An assembly's item read says it is an assembly and lists its parts in plan order, each where
// it stands, with how many are done of how many; a plain item's read has no parts and no
// assembly.
func TestAPI_GetItemAssemblyParts(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	asm, parts := h.assemble(t)
	plain := h.seed(t, stBuild, domain.StateQueued, t0)

	var got gen.Item
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/"+string(asm.ID), "", &got); code != http.StatusOK {
		t.Fatalf("get assembly = %d", code)
	}

	if got.State != gen.ItemStateAssembly || got.Assembly != nil || got.PartKey != nil {
		t.Errorf("assembly read: state %s, assembly %+v, part key %v; want assembly, none, none", got.State, got.Assembly, got.PartKey)
	}

	want := &gen.ItemParts{Done: parts.Done, Total: parts.Total}
	for i, p := range []struct {
		key, station string
		state        gen.ItemState
	}{{"store", stBuild, gen.ItemStateQueued}, {keyHandler, stMerge, gen.ItemStateWaitingHuman}, {"screen", stBuild, gen.ItemStateQueued}} {
		it := parts.Items[i]
		want.List = append(want.List, gen.ItemPart{
			Id: string(it.ID), Ref: "line#" + strconv.FormatInt(it.Number, 10), Number: it.Number, Title: it.Title,
			PartKey: p.key, Station: p.station, State: p.state,
		})
	}

	if !reflect.DeepEqual(got.Parts, want) {
		t.Errorf("assembly parts = %+v, want %+v", got.Parts, want)
	}

	var other gen.Item
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/"+string(plain.ID), "", &other); code != http.StatusOK {
		t.Fatalf("get plain item = %d", code)
	}

	if other.Parts != nil || other.Assembly != nil || other.PartKey != nil || other.PartPaths != nil {
		t.Errorf("plain item: parts %+v, assembly %+v, part key %v, paths %v; want none", other.Parts, other.Assembly, other.PartKey, other.PartPaths)
	}
}

// A part's item read names its key, its paths and its assembly with its place in plan order; a
// mutation's answer names the key and paths without reading the assembly's parts.
func TestAPI_GetItemPartOfAssembly(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	asm, parts := h.assemble(t)
	second := parts.Items[1]

	h.seam.steps()

	var got gen.Item
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/"+string(second.ID), "", &got); code != http.StatusOK {
		t.Fatalf("get part = %d", code)
	}

	wantOf := &gen.PartOf{Id: string(asm.ID), Ref: "line#" + strconv.FormatInt(asm.Number, 10), Title: asm.Title, Part: 2, Parts: 3}
	wantPaths := []string{"internal/api/**", "api/openapi.yaml"}

	if !reflect.DeepEqual(got.Assembly, wantOf) || deref(got.PartKey) != keyHandler || got.PartPaths == nil || !reflect.DeepEqual(*got.PartPaths, wantPaths) || got.Parts != nil {
		t.Errorf("part read: assembly %+v, key %v, paths %v, parts %+v; want %+v, %s, %v, none", got.Assembly, got.PartKey, got.PartPaths, got.Parts, wantOf, keyHandler, wantPaths)
	}

	if n := partsReads(h.seam.steps()); n != 1 {
		t.Errorf("get part read parts %d times, want once", n)
	}

	var paused gen.Item
	if code := h.call(t, http.MethodPost, "/v1/orgs/acme/items/"+string(parts.Items[2].ID)+"/pause", ``, &paused); code != http.StatusOK {
		t.Fatalf("pause part = %d", code)
	}

	if deref(paused.PartKey) != "screen" || paused.PartPaths != nil || paused.Assembly != nil {
		t.Errorf("pause answer: key %v, paths %v, assembly %+v; want screen, none, none", paused.PartKey, paused.PartPaths, paused.Assembly)
	}

	if n := partsReads(h.seam.steps()); n != 0 {
		t.Errorf("pause read parts %d times, want none", n)
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
