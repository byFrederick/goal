//go:build integration

package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/adapters/store/postgres"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/definitions"
)

// The query budget (assembly software#988): how many queries each read of the API makes over
// PostgreSQL, counted by the store's tracer and read from the request's access log line. A read
// whose count grows with the number of items is one query per item, which a pool of a few
// connections turns into seconds at production size.

const (
	budgetOrg     domain.OrgID     = "0b6f1c9e-0000-7000-8000-00000000b0d6"
	budgetSlug                     = "budget"
	budgetFactory domain.FactoryID = "0b6f1c9e-5d0a-4c39-9a51-6f1f3f0eb0d6"
)

// The budget factory's stations the seed puts items at.
const (
	budgetInbox  = "inbox"
	budgetReview = "review"
	budgetDone   = "done"
)

// budgetDefinition is inbox → build (agents) → review (human) → done.
const budgetDefinition = `apiVersion: goal/v1
kind: Factory
metadata: { id: ` + string(budgetFactory) + `, name: budget, title: Budget }
spec:
  runtime: { model: claude-fable-5-1 }
  agents:
    - { name: builder, prompt: ./agents/builder.md }
  board:
    workTypes: [feature, fix]
    stations:
      - { id: inbox, kind: queue }
      - { id: build, kind: agent, agent: builder, agents: 20, wip: 20 }
      - { id: review, kind: human }
      - { id: done, kind: done }
`

// budgetSeed is what seedProductionSize wrote: the organization and factory its items are in, and
// the API over them, with its access log.
type budgetSeed struct {
	db      mxDatabase
	objects *porttest.ObjectStore
	store   *postgres.Store
	clock   *porttest.Clock
	srv     *httptest.Server
	logs    *budgetLogs
	open    int
	parts   int
	done    int
	created int
}

// seedProductionSize gives a fresh database one organization with one factory holding openItems
// open items, as production has them at its size: one in twenty is an assembly, each with
// openItems/5 parts (10 assemblies of 40 parts at 200 open items); a quarter of the rest waits
// for a person at review, a tenth is blocked at build and the others are queued at inbox. One in
// twenty open items waits with an open question and as many with an open tool approval, each
// after more than a page of a run's events. It
// also has four done items per open one, finished over the last fourteen days, so this window and
// the one before have lead times. The items are written through the store, as the engine writes
// them. It serves the API over that database under GOAL_AUTH=none, with an open draft of the
// factory. Later parts of the assembly reuse it for /board and /inbox.
func seedProductionSize(t *testing.T, openItems int) budgetSeed {
	t.Helper()

	d := newMXDatabase(t)
	d.inOrg(t, budgetOrg, "SELECT * FROM goal_provision_org($1, $2, 'Budget', 'Bu', $3, $4, 'system:test', $5)", string(budgetOrg), budgetSlug,
		nil, nil, nil) // no owner, no request

	now := time.Now().UTC().Truncate(time.Second)
	b := budgetSeed{db: d, objects: porttest.NewObjectStore(), store: postgres.New(d.app), clock: porttest.NewClock(now), logs: &budgetLogs{}}
	b.publish(t)
	b.seedItems(t, openItems, now)
	b.serve(t)

	return b
}

// ctx is the test's context in the budget organization, acting as the local user.
func (b *budgetSeed) ctx(t *testing.T) context.Context {
	t.Helper()

	return domain.WithActor(domain.WithOrg(t.Context(), budgetOrg), domain.LocalUser)
}

// publish imports the factory, publishes it as v1 and opens a draft of it.
func (b *budgetSeed) publish(t *testing.T) {
	t.Helper()

	defs := postgres.NewDefinitions(b.db.app, b.objects, nil)
	authors := authoring.New(authoring.Deps{Store: defs, Loads: b.store, Templates: porttest.NewObjectStore(), Clock: b.clock})
	ctx := b.ctx(t)

	c, err := authors.CreateFactory(ctx, authoring.NewFactoryRequest{Start: authoring.Start{Kind: authoring.StartImport, KeepID: true, Files: map[string][]byte{
		"factory.yaml": []byte(budgetDefinition), "agents/builder.md": []byte("# builder\n"),
		"environments/default.yaml": []byte("apiVersion: goal/v1\nkind: Environment\nspec:\n  size: medium\n"),
	}}})
	if err != nil {
		t.Fatalf("create the factory: %v", err)
	}

	if _, err := defs.Publish(ctx, ports.Publish{
		Factory: c.Factory.ID, DraftRevision: c.Draft.Revision, Name: c.Factory.Name, Title: c.Factory.Title,
		Start: true, By: domain.LocalUser, At: b.clock.Now(),
	}); err != nil {
		t.Fatalf("publish the factory: %v", err)
	}

	if _, _, err := authors.OpenDraft(ctx, c.Factory.ID, authoring.OpenDraftRequest{}); err != nil {
		t.Fatalf("open a draft: %v", err)
	}
}

// seedItems writes the open, assembled and done items of seedProductionSize.
func (b *budgetSeed) seedItems(t *testing.T, openItems int, now time.Time) {
	t.Helper()

	ctx := b.ctx(t)
	build := domain.Station{ID: "build", Kind: domain.KindAgent, Agent: "builder", Agents: 20, WIP: 20}
	at := now.Add(-time.Hour)

	file := func(title, station string, state domain.ItemState, created time.Time) domain.Item {
		it, err := b.store.CreateItem(ctx, domain.NewItem{
			FactoryID: budgetFactory, FactoryName: "budget", Type: "feature", Title: title, Station: station, State: state,
			Class: domain.ClassStandard, CreatedAt: created, CreatedBy: domain.LocalUser,
		})
		if err != nil {
			t.Fatalf("file %s: %v", title, err)
		}

		b.created++

		return it
	}

	// Every planning item is claimed before any is assembled, so no claim takes a part.
	assemblies, partsEach := max(openItems/20, 1), max(openItems/5, 1)
	leased := make([]struct {
		it domain.Item
		l  domain.Lease
	}, 0, assemblies)

	for i := range assemblies {
		file(fmt.Sprintf("assembly %d", i), build.ID, domain.StateQueued, at.Add(-24*time.Hour))

		it, l, err := b.store.ClaimNext(ctx, ports.Claim{FactoryID: budgetFactory, Station: build, Slot: i, Now: at, ExpiresAt: at.Add(time.Hour)})
		if err != nil {
			t.Fatalf("claim assembly %d: %v", i, err)
		}

		leased = append(leased, struct {
			it domain.Item
			l  domain.Lease
		}{it, l})
	}

	for i, p := range leased {
		plan := make([]domain.NewPart, partsEach)
		for k := range plan {
			plan[k] = domain.NewPart{Key: fmt.Sprintf("p%d", k), Title: fmt.Sprintf("part %d of %d", k, i), Brief: "a slice", Paths: []string{fmt.Sprintf("internal/p%d/**", k)}}
			if k > 0 {
				plan[k].Needs = []string{fmt.Sprintf("p%d", k-1)}
			}
		}

		if _, _, err := b.store.Assemble(ctx, ports.Assembly{
			Transition: ports.Transition{
				ItemID: p.it.ID, From: p.it.Position(), To: domain.Position{Station: build.ID, State: domain.StateAssembly},
				Lease: &p.l, Outcome: domain.LeaseAdvanced, At: at,
			},
			Parts: plan, By: domain.LocalUser,
		}); err != nil {
			t.Fatalf("assemble %d: %v", i, err)
		}

		b.parts += partsEach
	}

	waiting := b.seedOpen(t, openItems-assemblies, build.ID, at, file)
	b.askers(t, waiting, max(openItems/20, 1), at)

	for i := range 4 * openItems {
		doneAt := now.Add(-time.Duration(i%(14*24)) * time.Hour).Add(-time.Minute)
		it := file(fmt.Sprintf("done %d", i), budgetReview, domain.StateWaitingHuman, doneAt.Add(-6*time.Hour))

		if _, err := b.store.Transition(ctx, ports.Transition{
			ItemID: it.ID, From: it.Position(), To: domain.Position{Station: budgetDone, State: domain.StateDone}, At: doneAt, DoneAt: doneAt,
		}); err != nil {
			t.Fatalf("finish %s: %v", it.Title, err)
		}

		b.done++
	}

	b.open = openItems
}

// seedOpen files n open items: a quarter waits for a person at review, a tenth is blocked at
// build and the others are queued at inbox. It returns the waiting ones.
func (b *budgetSeed) seedOpen(t *testing.T, n int, build string, at time.Time, file func(string, string, domain.ItemState, time.Time) domain.Item) []domain.Item {
	t.Helper()

	var waiting []domain.Item

	for i := range n {
		station, state := budgetInbox, domain.StateQueued

		switch {
		case i%4 == 0:
			station, state = budgetReview, domain.StateWaitingHuman
		case i%10 == 1:
			station, state = build, domain.StateBlocked
		}

		it := file(fmt.Sprintf("open %d", i), station, state, at.Add(-time.Duration(i)*time.Minute))
		if state == domain.StateWaitingHuman {
			waiting = append(waiting, it)
		}
	}

	return waiting
}

// askers gives n of the waiting items an open question and the next n an open tool approval,
// each after a run's long history.
func (b *budgetSeed) askers(t *testing.T, waiting []domain.Item, n int, at time.Time) {
	t.Helper()

	for i, it := range waiting[:min(2*n, len(waiting))] {
		if i < n {
			b.ask(t, it, domain.Event{Kind: domain.EventQuestionAsked, Data: map[string]any{
				domain.KeyQuestion: "which way?", domain.KeyChoices: []any{"left", "right"}, domain.KeyAgent: factoryBuilder,
			}}, at)

			continue
		}

		b.ask(t, it, domain.Event{Kind: domain.EventToolApprovalRequested, Data: map[string]any{
			domain.KeyConnection: "github", domain.KeyTool: "merge", domain.KeyAgent: factoryBuilder,
		}}, at)
	}
}

// askHistory is how many events a run leaves before its ask in the seed: more than one page of
// ItemEvents (maxEventLimit), as an item whose agents ran for a while has.
const askHistory = maxEventLimit + 200

// ask appends to it a run's long history of tool calls and results, then the ask, as the engine
// appends them.
func (b *budgetSeed) ask(t *testing.T, it domain.Item, ask domain.Event, at time.Time) {
	t.Helper()

	events := make([]domain.Event, 0, askHistory+1)
	for k := range askHistory {
		kind := domain.EventToolCall
		if k%2 == 1 {
			kind = domain.EventToolResult
		}

		events = append(events, domain.Event{FactoryID: budgetFactory, ItemID: it.ID, At: at, Kind: kind, Station: "build", Actor: "agent:builder", Data: map[string]any{"i": k}})
	}

	ask.FactoryID, ask.ItemID, ask.At, ask.Station, ask.Actor = budgetFactory, it.ID, at, budgetReview, "agent:builder"

	if err := b.store.AppendEvents(b.ctx(t), append(events, ask)...); err != nil {
		t.Fatalf("history of %s: %v", it.Title, err)
	}
}

// serve starts the API over the seeded database as goald wires it for these reads; the rest of
// its collaborators are stubs, which none of the reads budgetAnswer here reaches.
func (b *budgetSeed) serve(t *testing.T) {
	t.Helper()

	defStore := postgres.NewDefinitions(b.db.app, b.objects, nil)
	cache := definitions.New(defStore, b.store, func(context.Context, []domain.Line) error { return nil }, definitions.Config{}, nil)

	if _, err := cache.Refresh(t.Context()); err != nil {
		t.Fatalf("load the factories: %v", err)
	}

	authors := authoring.New(authoring.Deps{Store: defStore, Loads: b.store, Templates: porttest.NewObjectStore(), Clock: b.clock})

	h, err := New(Deps{
		Engine: struct{ Engine }{}, Items: b.store, Factories: cache, Definitions: authors, Versions: defStore, Orgs: b.store, Audit: b.store,
		OrgRetention: time.Hour, Authorizer: AllowAll{}, Auth: AuthConfig{Mode: AuthNone}, Access: postgres.NewAccess(b.store),
		Platform: postgres.NewPlatform(b.store), Feed: NewFeed(porttest.NewItemNotices(), porttest.NewDefinitionNotices(), nil),
		Builder: struct{ Builder }{}, StandingRules: postgres.NewStandingRules(b.store), Credentials: struct{ ports.Credentials }{},
		OrgSecrets: struct{ ports.OrgSecrets }{}, EnvironmentTests: postgres.NewEnvironmentTests(b.store), ToolLister: struct{ ports.ToolLister }{},
		RepositoryTests: postgres.NewRepositoryTests(b.store), Intake: struct{ ports.Intake }{}, DraftRequests: postgres.NewDraftRequests(b.store),
		Conversions: postgres.NewFactoryConversions(b.store), ConnectionTransport: http.DefaultTransport,
		Knowledge: postgres.NewKnowledge(b.store), KnowledgeUploads: struct{ KnowledgeUploads }{}, UploadMaxBytes: 1 << 20,
		KnowledgeObjects: struct{ KnowledgeObjects }{}, KnowledgeCheck: struct{ KnowledgeCheck }{},
		OrgPolicies: postgres.NewOrgPolicies(b.store, []string{"claude-fable-5-1"}), BoxSizes: []domain.BoxSize{{Size: domain.SandboxMedium}},
		NotificationPreferences: postgres.NewNotificationPreferences(b.store), NotificationSettings: postgres.NewNotificationSettings(b.store),
		SlackMessenger: struct{ ports.DirectMessenger }{}, Clock: b.clock, Logger: slog.New(slog.NewJSONHandler(b.logs, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}

	b.srv = httptest.NewServer(h)
	t.Cleanup(b.srv.Close)
}

// budgetAnswer is one read's answer: its status, how many queries its access log line counted and
// how long it took on the wall clock.
type budgetAnswer struct {
	status  int
	queries int
	wall    time.Duration
}

// get sends GET path and reads its access log line.
func (b *budgetSeed) get(t *testing.T, path string) budgetAnswer {
	t.Helper()

	start := time.Now()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, b.srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}

	res, err := b.srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}

	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	wall := time.Since(start)

	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d %s", path, res.StatusCode, body)
	}

	return budgetAnswer{status: res.StatusCode, queries: b.logs.queries(t, res.Header.Get(requestIDHeader)), wall: wall}
}

// budgetLogs is the API's log, written by the server and read by the test.
type budgetLogs struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *budgetLogs) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.buf.Write(p)
}

// queries is the queries field of the access log line of the request with id.
func (l *budgetLogs) queries(t *testing.T, id string) int {
	t.Helper()

	l.mu.Lock()
	text := l.buf.String()
	l.mu.Unlock()

	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)

	for sc.Scan() {
		var line struct {
			Msg       string `json:"msg"`
			RequestID string `json:"request_id"`
			Queries   *int   `json:"queries"`
		}

		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			t.Fatalf("log line %q: %v", sc.Text(), err)
		}

		if line.Msg == "api request" && line.RequestID == id && line.Queries != nil {
			return *line.Queries
		}
	}

	t.Fatalf("no access log line with queries for request %s", id)

	return 0
}

// budgetRead is one read the budget pins: its path in the budget organization and the queries it
// may make at any size. A query is one round trip, so each store call's transaction counts at
// least three (BEGIN with set_config, the query, COMMIT); the counts are with one factory. growsToday marks a read whose count still grows with the items: the test
// logs it instead of failing, until a later part of assembly software#988 fixes it.
type budgetRead struct {
	name, path string
	budget     int
	growsToday bool
}

func budgetReads() []budgetRead {
	f := "/v1/orgs/" + budgetSlug + "/factories/" + string(budgetFactory)

	return []budgetRead{
		{name: "attention", path: "/v1/orgs/" + budgetSlug + "/attention", budget: 6},
		{name: "overview 7d", path: "/v1/orgs/" + budgetSlug + "/overview?window=7d", budget: 55},
		{name: "metrics compare previous", path: f + "/metrics?compare=previous", budget: 31},
		{name: "draft", path: f + "/draft", budget: 61},
		{name: "draft model", path: f + "/draft/model", budget: 29},
		{name: "inbox", path: "/v1/orgs/" + budgetSlug + "/inbox", budget: 33},
	}
}

// TestAPI_QueryBudgetAtProductionSize pins how many queries each read makes, the same with 20
// open items as with 200 (10 of them assemblies of 40 parts) and 800 done, and logs each read's
// count and wall time at both sizes.
func TestAPI_QueryBudgetAtProductionSize(t *testing.T) {
	t.Parallel()

	sizes := []int{20, 200}
	got := map[int]map[string]budgetAnswer{}

	for _, open := range sizes {
		b := seedProductionSize(t, open)
		t.Logf("seeded %d open items (%d parts) and %d done: %d items filed", b.open, b.parts, b.done, b.created+b.parts)

		got[open] = map[string]budgetAnswer{}

		for _, r := range budgetReads() {
			b.get(t, r.path) // warm: the first read of a factory loads its version into the cache
			m := b.get(t, r.path)
			got[open][r.name] = m
			t.Logf("%d open items: %s: %d queries in %v", open, r.name, m.queries, m.wall)
		}
	}

	for _, r := range budgetReads() {
		small, large := got[sizes[0]][r.name], got[sizes[1]][r.name]

		switch {
		case r.growsToday:
			// TODO(software#988): this read makes more queries with more items; the part that fixes
			// it sets growsToday to false.
			t.Logf("%s grows with the items today: %d queries at %d open, %d at %d", r.name, small.queries, sizes[0], large.queries, sizes[1])
		case small.queries != large.queries:
			t.Errorf("%s: %d queries at %d open items, %d at %d; want the same at any size", r.name, small.queries, sizes[0], large.queries, sizes[1])
		case large.queries != r.budget:
			t.Errorf("%s: %d queries; want its budget of %d", r.name, large.queries, r.budget)
		}
	}
}
