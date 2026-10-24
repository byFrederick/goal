package api_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// runModel is the model of the runs the tests record.
const runModel = "claude-sonnet-5-5"

// startRun records a run of the backend agent on it at the build station, on version.
func (h *harness) startRun(t *testing.T, it domain.Item, version domain.DefinitionVersionID, at time.Time) domain.RunID {
	t.Helper()

	id, err := h.store.StartRun(inOrg(t), ports.NewRun{
		FactoryID: lineID, DefinitionVersionID: version, ItemID: it.ID, Station: stBuild, Agent: agBackend,
		Model: runModel, StartedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}

	return id
}

// lineV1 is the line factory's published version 1.
func (h *harness) lineV1(t *testing.T) domain.DefinitionVersion {
	t.Helper()

	v, err := h.defs.VersionByNumber(inOrg(t), lineID, 1)
	if err != nil {
		t.Fatal(err)
	}

	return v
}

func TestAPI_RunsOfAnItem(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	v1 := h.lineV1(t)
	h.seed(t, stBuild, domain.StateQueued, t0)
	it := h.claim(t, 0)

	first := h.startRun(t, it, v1.ID, t0.Add(time.Minute))
	ended := t0.Add(5 * time.Minute)
	res := domain.RunResult{
		Status: domain.RunFailed, CostUSD: 1.25, Turns: 14, InputTokens: 900, OutputTokens: 300, Error: "output failed its checks",
	}

	if err := h.store.FinishRun(inOrg(t), first, res, ended); err != nil {
		t.Fatal(err)
	}

	second := h.startRun(t, it, "", t0.Add(6*time.Minute)) // a run from before versions, still running
	version, versionID, failure := v1.Number, string(v1.ID), res.Error
	wantFirst := gen.Run{
		Id: string(first), ItemId: string(it.ID), Station: stBuild, Agent: agBackend, Model: runModel,
		Status: gen.RunStatus(domain.RunFailed), Turns: 14, InputTokens: 900, OutputTokens: 300, CostUsd: 1.25,
		Error: &failure, StartedAt: t0.Add(time.Minute), EndedAt: &ended, DefinitionVersionId: &versionID,
		DefinitionVersion: &version,
	}
	wantSecond := gen.Run{
		Id: string(second), ItemId: string(it.ID), Station: stBuild, Agent: agBackend, Model: runModel,
		Status: gen.RunStatus(domain.RunRunning), StartedAt: t0.Add(6 * time.Minute),
	}

	var list gen.RunList
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/line%231/runs", "", &list); code != http.StatusOK {
		t.Fatalf("list runs = %d", code)
	}

	if got, want := asJSON(t, list), asJSON(t, gen.RunList{Runs: []gen.Run{wantFirst, wantSecond}}); got != want {
		t.Errorf("runs =\n  %s\nwant oldest first\n  %s", got, want)
	}

	h.seam.steps()

	var run gen.Run
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/runs/"+string(first), "", &run); code != http.StatusOK ||
		asJSON(t, run) != asJSON(t, wantFirst) {
		t.Errorf("get run = %d %s, want %s", code, asJSON(t, run), asJSON(t, wantFirst))
	}

	if steps := h.seam.steps(); !slices.ContainsFunc(steps, func(s step) bool {
		return s.access != nil && s.access.Operation == "getRun" && s.access.Factory == lineID
	}) {
		t.Errorf("get run: seam steps %+v, want the run's factory authorized", steps)
	}

	for _, path := range []string{
		"/v1/orgs/acme/runs/00000000-0000-7000-8000-ffffffffffff", // no such run
		"/v1/orgs/acme/runs/not-a-run",
		"/v1/orgs/example-labs/runs/" + string(first), // a run of another organization
		"/v1/orgs/example-labs/items/" + string(it.ID) + "/runs",
		"/v1/orgs/acme/items/line%2399/runs",
	} {
		if code := h.call(t, http.MethodGet, path, "", nil); code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, code)
		}
	}
}

func TestAPI_RunsOfAnItemAreCappedAt200(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	v1 := h.lineV1(t)
	it := h.seed(t, stBuild, domain.StateQueued, t0)

	for i := range 201 {
		h.startRun(t, it, v1.ID, t0.Add(time.Duration(i)*time.Second))
	}

	var list gen.RunList
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/line%231/runs", "", &list); code != http.StatusOK {
		t.Fatalf("list runs = %d", code)
	}

	if !list.Truncated || len(list.Runs) != 200 || !list.Runs[0].StartedAt.Equal(t0) {
		t.Errorf("runs: %d, truncated %v; want the oldest 200 and truncated", len(list.Runs), list.Truncated)
	}
}

// asJSON encodes v, so contract values compare by what a client reads.
func asJSON(t *testing.T, v any) string {
	t.Helper()

	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}
