package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api"
	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/builder"
	"github.com/byFrederick/goal/internal/definitions"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/engine"
	"github.com/byFrederick/goal/internal/factorystore"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

const (
	acmeID    = "8d4f2b1a-3c5e-4f60-9a7b-1c2d3e4f5a6b"
	acmeItems = "/v1/orgs/acme/factories/acme/items"
)

// acme is inbox → approve (human) → done: enough to submit, read and decide on items.
const acme = `apiVersion: goal/v1
kind: Factory
metadata: { id: ` + acmeID + `, name: acme, title: Acme }
spec:
  runtime: { runner: docker, image: ghcr.io/acme/agent:1.0.0, auth: anthropic-api-key }
  knowledgeBase: { collections: [notes] }
  agents:
    - { name: writer, prompt: ./agents/writer.md }
  board:
    workTypes: [feature, fix]
    stations:
      - { id: inbox, kind: queue }
      - { id: approve, kind: human }
      - { id: done, kind: done }
`

// line is inbox → build (agents, routed by lane) → merge (human, action merge) → done, on a
// repository with a weekly budget: enough for boards, metrics, overviews, retries and merges. Tests seed its items straight
// into the store.
const line = `apiVersion: goal/v1
kind: Factory
metadata: { id: ` + lineID + `, name: line }
spec:
  runtime:
    runner: docker
    image: ghcr.io/acme/agent:1.0.0
    auth: anthropic-api-key
    repository: { url: "https://github.com/acme/app.git", token: env:REPO_TOKEN }
    budget: { weeklyUsd: 70 }
  knowledgeBase: { collections: [notes] }
  agents:
    - { name: backend, prompt: ./agents/backend.md }
    - { name: frontend, prompt: ./agents/frontend.md }
  board:
    stations:
      - { id: inbox, kind: queue }
      - { id: build, kind: agent, route: { api: backend, web: frontend }, agents: 2, wip: 2 }
      - { id: merge, kind: human, action: merge }
      - { id: done, kind: done }
`

const lineID = "0b6f1c9e-5d0a-4c39-9a51-6f1f3f0e2a10"

// testOrg is the organization the tests' factories and items belong to (the first the harness
// creates): every store call carries one.
const testOrg domain.OrgID = "00000000-0000-7000-8000-000000000001"

// otherOrg is the second organization the harness creates (example-labs), which has no
// factories: nothing of acme's may be reached through it.
const otherOrg domain.OrgID = "00000000-0000-7000-8000-000000000002"

// inOrg is the test's context bound to testOrg.
func inOrg(t *testing.T) context.Context {
	t.Helper()

	return domain.WithOrg(t.Context(), testOrg)
}

var t0 = time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)

type harness struct {
	srv      *httptest.Server
	engine   *engine.Engine
	store    *porttest.Store
	clock    *porttest.Clock
	codehost *porttest.CodeHost
	notices  *porttest.ItemNotices
	seam     *seam
	tenancy  *porttest.Tenancy
	// skills is the bucket the skills catalog is read from; skillSource what it is synced from.
	skills      *flakyObjects
	skillSource *porttest.SkillSource
	builder     *builder.Service
}

// seam is the harness's Authorizer: it records every question in order, together with the
// API's reads of the store (reads), and answers deny (nil: allow).
type seam struct {
	mu    sync.Mutex
	log   []step
	denyF func(api.AccessRequest) error
}

// step is one entry of the seam's log: an authorization question or a store read.
type step struct {
	access *api.AccessRequest
	read   string
}

func (s *seam) Authorize(_ context.Context, r api.AccessRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.log = append(s.log, step{access: &r})

	if s.denyF != nil {
		return s.denyF(r)
	}

	return nil
}

func (s *seam) record(read string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.log = append(s.log, step{read: read})
}

// deny makes the seam answer every question with fn's error.
func (s *seam) deny(fn func(api.AccessRequest) error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.denyF = fn
}

// steps returns the log and starts a new one.
func (s *seam) steps() []step {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := s.log
	s.log = nil

	return out
}

// reads is the store as the API reads it, logging each read in the seam's log.
type reads struct {
	*porttest.Store

	seam *seam
}

func (r reads) Item(ctx context.Context, id domain.ItemID) (domain.Item, error) {
	r.seam.record("Item")
	return r.Store.Item(ctx, id)
}

func (r reads) ItemByNumber(ctx context.Context, f domain.FactoryID, n int64) (domain.Item, error) {
	r.seam.record("ItemByNumber")
	return r.Store.ItemByNumber(ctx, f, n)
}

func (r reads) ListItems(ctx context.Context, q ports.ItemQuery) ([]domain.Item, error) {
	r.seam.record("ListItems")
	return r.Store.ListItems(ctx, q)
}

func (r reads) StationLoads(ctx context.Context, f domain.FactoryID) ([]domain.StationLoad, error) {
	r.seam.record("StationLoads")
	return r.Store.StationLoads(ctx, f)
}

func (r reads) Attention(ctx context.Context) ([]domain.Attention, error) {
	r.seam.record("Attention")
	return r.Store.Attention(ctx)
}

func (r reads) WaitingHuman(ctx context.Context, fs []domain.FactoryID, limit int) ([]domain.Item, error) {
	r.seam.record("WaitingHuman")
	return r.Store.WaitingHuman(ctx, fs, limit)
}

func (r reads) FlowMetrics(ctx context.Context, f domain.FactoryID, since, until time.Time) (domain.FlowMetrics, error) {
	r.seam.record("FlowMetrics")
	return r.Store.FlowMetrics(ctx, f, since, until)
}

// newTenancy holds acme (testOrg) and example-labs (otherOrg).
func newTenancy(t *testing.T) *porttest.Tenancy {
	t.Helper()

	tenancy := porttest.NewTenancy(time.Now)

	for _, o := range []struct {
		id         domain.OrgID
		slug, name string
	}{{testOrg, acmeSlug, "Acme"}, {otherOrg, labsSlug, "Example Labs"}} {
		if got, err := tenancy.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: o.slug, Name: o.name}); err != nil || got.ID != o.id {
			t.Fatalf("create organization %s = %v (%v), want %s", o.slug, got.ID, err, o.id)
		}
	}

	return tenancy
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	catalog := factorystore.New(porttest.NewObjectStore())
	tenancy := newTenancy(t)

	for _, files := range []map[string]string{
		{"factory.yaml": acme, "agents/writer.md": "# writer\n"},
		{"factory.yaml": line, "agents/backend.md": "# backend\n", "agents/frontend.md": "# frontend\n"},
	} {
		dir := t.TempDir()
		for name, body := range files {
			if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0o750); err != nil {
				t.Fatal(err)
			}

			if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}

		if _, _, err := catalog.Push(t.Context(), testOrg, dir); err != nil {
			t.Fatal(err)
		}
	}

	h := &harness{
		store: porttest.NewStore(), clock: porttest.NewClock(t0), codehost: porttest.NewCodeHost(),
		notices: porttest.NewItemNotices(),
	}
	tenancy.WithStore(h.store) // one database holds the organizations and their factories' rows

	e, err := engine.New(engine.Deps{
		Store: h.store, Orgs: tenancy, Runner: porttest.NewRunner(porttest.Succeed(`{}`)), Workspaces: porttest.NewWorkspaces(),
		Checker: porttest.NewChecker(), Contracts: porttest.NewContracts(), Secrets: porttest.Secrets{},
		CodeHost: h.codehost, Clock: h.clock,
	}, engine.Config{})
	if err != nil {
		t.Fatal(err)
	}

	h.engine = e

	// As goald does: the definitions cache registers every loaded factory in its organization and
	// hands its line to the engine.
	defs := definitions.New(catalog, tenancy, func(_ context.Context, fs []definitions.Loaded) error {
		lines := make([]domain.Line, 0, len(fs))
		for _, f := range fs {
			h.store.AddFactory(f.Org, domain.FactoryID(f.Factory.Metadata.ID))
			lines = append(lines, f.Line())
		}

		return e.SetLines(lines)
	}, nil)

	// As goald does at startup: load every organization's factories once.
	if _, err := defs.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}

	feed := api.NewFeed(h.notices, nil)
	ctx, stop := context.WithCancel(domain.WithOrg(context.Background(), testOrg))
	done := make(chan error, 1)

	go func() { done <- feed.Run(ctx) }()

	h.seam, h.tenancy = &seam{}, tenancy
	h.skills, h.skillSource = &flakyObjects{ObjectStore: porttest.NewObjectStore()}, porttest.NewSkillSource()
	h.builder = builder.New(h.skills, h.clock, builder.Config{
		Models: []builder.Model{{ID: "claude-opus-5-5", Label: "Claude Opus 5.5"}, {ID: "claude-haiku-5-5", Label: "Claude Haiku 5.5"}},
	})

	handler, err := api.New(api.Deps{
		Engine: e, Items: reads{Store: h.store, seam: h.seam}, Factories: defs, Orgs: tenancy, Audit: tenancy,
		OrgRetention: retention, Authorizer: h.seam, Feed: feed, Builder: h.builder, Clock: h.clock,
	})
	if err != nil {
		t.Fatal(err)
	}

	h.srv = httptest.NewServer(handler)

	t.Cleanup(func() {
		stop() // ends every stream before the server closes

		if err := <-done; err != nil {
			t.Error(err)
		}

		h.srv.Close()
	})

	return h
}

// call sends a JSON request and decodes the response into out (when not nil), returning the
// status code.
func (h *harness) call(t *testing.T, method, path, body string, out any) int {
	t.Helper()

	req, err := http.NewRequestWithContext(inOrg(t), method, h.srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}

	if res.StatusCode >= 400 && res.Header.Get("Content-Type") != "application/problem+json" {
		t.Errorf("%s %s: error content type %q, want problem details", method, path, res.Header.Get("Content-Type"))
	}

	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%s %s: decode %s: %v", method, path, raw, err)
		}
	}

	return res.StatusCode
}

func (h *harness) submit(t *testing.T) gen.Item {
	t.Helper()

	var it gen.Item
	if code := h.call(t, http.MethodPost, acmeItems, `{"title":"Add a version command","type":"feature"}`, &it); code != http.StatusCreated {
		t.Fatalf("submit = %d, want 201", code)
	}

	return it
}

func TestAPI_HealthAndFactories(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	var health gen.Health
	if code := h.call(t, http.MethodGet, "/v1/health", "", &health); code != http.StatusOK || health.Status != "ok" {
		t.Errorf("health = %d %+v", code, health)
	}

	var list gen.FactoryList
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories", "", &list); code != http.StatusOK {
		t.Fatalf("list factories = %d", code)
	}

	// The factory was pushed before goald looked: a submit by name finds it, and from then on
	// it is listed.
	h.submit(t)

	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/factories", "", &list); code != http.StatusOK || len(list.Factories) != 2 {
		t.Fatalf("list factories = %d %+v", code, list)
	}

	f := list.Factories[0]
	if got := fmt.Sprintf("%s %s %v %d", f.Id, f.Name, f.WorkTypes, len(f.Stations)); got != acmeID+" acme [feature fix] 3" {
		t.Errorf("factory = %s", got)
	}
}

func TestAPI_SubmitValidatesAgainstTheFactory(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	tests := []struct {
		name, path, body string
		want             int
	}{
		{name: "by name", path: acmeItems, body: submitFix, want: http.StatusCreated},
		{name: "by id", path: "/v1/orgs/acme/factories/" + acmeID + "/items", body: `{"title":"t","type":"fix","priority":2}`, want: http.StatusCreated},
		{name: "unknown factory", path: "/v1/orgs/acme/factories/nope/items", body: submitFix, want: http.StatusNotFound},
		{name: "unknown work type", path: acmeItems, body: `{"title":"t","type":"chore"}`, want: http.StatusBadRequest},
		{name: "blank title", path: acmeItems, body: `{"title":"  ","type":"fix"}`, want: http.StatusBadRequest},
		{name: "malformed body", path: acmeItems, body: `{"title":`, want: http.StatusBadRequest},
	}

	for _, tt := range tests {
		if code := h.call(t, http.MethodPost, tt.path, tt.body, nil); code != tt.want {
			t.Errorf("%s: status %d, want %d", tt.name, code, tt.want)
		}
	}
}

func TestAPI_GetItem(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	submitted := h.submit(t)

	if err := h.engine.Tick(inOrg(t)); err != nil { // inbox → approve
		t.Fatal(err)
	}

	var it gen.Item
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/"+submitted.Id, "", &it); code != http.StatusOK {
		t.Fatalf("get item = %d", code)
	}

	if it.Station != "approve" || it.State != gen.ItemState(domain.StateWaitingHuman) || it.Number != 1 {
		t.Errorf("item at %s/%s number %d, want approve/waiting_human number 1", it.Station, it.State, it.Number)
	}

	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/00000000-0000-7000-8000-ffffffffffff", "", nil); code != http.StatusNotFound {
		t.Errorf("unknown item = %d, want 404", code)
	}
}

func TestAPI_EventPages(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	it := h.submit(t)

	if err := h.engine.Tick(inOrg(t)); err != nil {
		t.Fatal(err)
	}

	var first, rest gen.EventPage
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/"+it.Id+"/events?limit=2", "", &first); code != http.StatusOK || len(first.Events) != 2 {
		t.Fatalf("first page = %d %+v, want 2 events", code, first)
	}

	path := fmt.Sprintf("/v1/orgs/acme/items/%s/events?after=%d", it.Id, first.NextAfter)
	if code := h.call(t, http.MethodGet, path, "", &rest); code != http.StatusOK || len(rest.Events) == 0 {
		t.Fatalf("next page = %d %+v, want the remaining events", code, rest)
	}

	if rest.Events[0].Id <= first.NextAfter || first.Events[0].Kind != string(domain.EventCreated) {
		t.Errorf("pages %+v then %+v; want created first and the second page after the first", first.Events, rest.Events)
	}

	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/"+it.Id+"/events?limit=5000", "", nil); code != http.StatusBadRequest {
		t.Errorf("limit 5000 = %d, want 400", code)
	}
}

func TestAPI_HumanDecisions(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	it := h.submit(t)

	path := "/v1/orgs/acme/items/" + it.Id

	if code := h.call(t, http.MethodPost, path+"/approve", ``, nil); code != http.StatusConflict {
		t.Errorf("approve while queued = %d, want 409", code)
	}

	if err := h.engine.Tick(inOrg(t)); err != nil {
		t.Fatal(err)
	}

	if code := h.call(t, http.MethodPost, path+"/reject", `{"reason":" "}`, nil); code != http.StatusBadRequest {
		t.Errorf("reject without a reason = %d, want 400", code)
	}

	var done gen.Item
	if code := h.call(t, http.MethodPost, path+"/approve", ``, &done); code != http.StatusOK || done.Station != "done" {
		t.Errorf("approve = %d at %s, want 200 at done", code, done.Station)
	}

	events := h.store.Events(domain.ItemID(it.Id))
	if last := events[len(events)-3]; last.Kind != domain.EventApproved || last.Actor != domain.LocalUser.Name || last.Data["actor"] != "user:local" {
		t.Errorf("events %+v, want an approval by the local user", events)
	}
}
