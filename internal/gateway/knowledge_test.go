package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/knowledge"
)

// knowledgeYAML is a factory whose agents may search the knowledge source handbook only;
// plainYAML is one that may search none.
const (
	knowledgeYAML = `apiVersion: goal/v1
kind: Factory
metadata: { id: ` + string(factoryID) + `, name: acme-knowledge }
spec:
  runtime: { model: claude-sonnet-5-5 }
  knowledge: { sources: [handbook] }
  agents:
    - { name: engineer, prompt: ./agents/engineer.md }
  board:
    stations:
      - { id: build, kind: agent, agent: engineer }
      - { id: done, kind: done }
`
	plainYAML = `apiVersion: goal/v1
kind: Factory
metadata: { id: ` + string(otherID) + `, name: acme-plain }
spec:
  runtime: { model: claude-sonnet-5-5 }
  agents:
    - { name: engineer, prompt: ./agents/engineer.md }
  board:
    stations:
      - { id: build, kind: agent, agent: engineer }
      - { id: done, kind: done }
`
)

// The knowledge tools and what the tests search for.
const (
	searchTool  = "knowledge__search"
	readTool    = "knowledge__read"
	verdictTool = "knowledge__verdict"
	kbQuery     = "deploy"
	kbStation   = "build"
	argQuery    = "query"
	argDoc      = "document"
	helped      = "helped"
)

var kbAdmin = domain.Actor{Kind: domain.ActorUser, ID: "00000000-0000-7000-8000-000000000001", Name: "Ada"}

// kbDocs are the documents of the harness's knowledge bases: organization → source → title →
// text. acme's handbook is the source the factory lists, its secrets one it does not;
// example-labs has a handbook of its own.
var kbDocs = map[string]map[string]map[string]string{
	"acme": {
		"handbook": {"deploy.md": "# Deploy\n\nDeploys go out on Tuesdays.", "big.md": "# Big\n\n" + strings.Repeat("words of the style guide ", 10_000)},
		"secrets":  {"keys.md": "# Keys\n\nThe deploy key rotates monthly."},
	},
	"example-labs": {"handbook": {"labs.md": "# Fridays\n\nWe deploy on Fridays."}},
}

// kbHarness is a gateway over kbDocs, indexed by keywords.
type kbHarness struct {
	url   string
	key   []byte
	clock *porttest.Clock
	store *porttest.Store
	orgs  map[string]domain.OrgID               // by slug
	docs  map[string]domain.KnowledgeDocumentID // by title
	kb    *porttest.Knowledge
	logMu sync.Mutex
	log   bytes.Buffer
	gw    *Gateway
}

func newKBHarness(t *testing.T) *kbHarness {
	t.Helper()

	return newKBHarnessWith(t, func(s *knowledge.Searcher) Knowledge { return s })
}

// newKBHarnessWith is newKBHarness with the gateway's Knowledge wrap of the searcher, logging
// to h.log.
func newKBHarnessWith(t *testing.T, wrap func(*knowledge.Searcher) Knowledge) *kbHarness {
	t.Helper()

	h := &kbHarness{
		key: []byte("gateway-test-key"), clock: porttest.NewClock(time.Now().UTC().Add(time.Second)), store: porttest.NewStore(),
		orgs: map[string]domain.OrgID{}, docs: map[string]domain.KnowledgeDocumentID{},
	}

	tenancy := porttest.NewTenancy(h.clock.Now)
	conns := porttest.NewConnections(porttest.NewMasterKeys().Key("k1"), tenancy)
	kb := porttest.NewKnowledge(conns, tenancy)
	h.kb = kb
	uploads := knowledge.NewUploads(kb, porttest.NewObjectStore(), 0)

	for slug, sources := range kbDocs {
		o, err := tenancy.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: slug, Name: slug})
		if err != nil {
			t.Fatal(err)
		}

		h.orgs[slug] = o.ID
		h.upload(t, kb, uploads, o.ID, sources)
	}

	in := knowledge.NewIngestor(knowledge.IngestorConfig{Knowledge: kb, Connections: conns, Uploads: uploads, Clock: h.clock})

	for n := -1; n != 0; {
		var err error
		if n, err = in.SyncDue(t.Context()); err != nil {
			t.Fatal(err)
		}
	}

	var err error

	h.gw, err = New(Deps{
		Definitions: definitions{version: loadYAML(t, knowledgeYAML), otherVersion: loadYAML(t, plainYAML)},
		Items:       h.store, Connections: conns, Events: h.store, Clock: h.clock,
		Questions: &fakeQuestions{}, ToolApprovals: &fakeToolApprovals{}, Knowledge: wrap(knowledge.NewSearcher(kb, nil, nil)), Logger: slog.New(slog.NewTextHandler(&lockedWriter{mu: &h.logMu, w: &h.log}, nil)),
	}, Config{Key: h.key})
	if err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(h.gw)
	t.Cleanup(ts.Close)
	h.url = ts.URL

	return h
}

// upload adds sources (source → title → text) as upload sources of org.
func (h *kbHarness) upload(t *testing.T, kb ports.Knowledge, uploads *knowledge.Uploads, org domain.OrgID, sources map[string]map[string]string) {
	t.Helper()

	ctx := domain.WithOrg(t.Context(), org)

	for source, docs := range sources {
		if _, err := kb.CreateSource(ctx, domain.NewKnowledgeSource{Name: source, Title: source, Kind: domain.SourceUpload}, kbAdmin); err != nil {
			t.Fatal(err)
		}

		for title, text := range docs {
			d, err := uploads.Upload(ctx, source, title, []byte(text), kbAdmin)
			if err != nil {
				t.Fatal(err)
			}

			h.docs[title] = d.ID
		}
	}
}

func loadYAML(t *testing.T, yaml string) *config.Factory {
	t.Helper()

	f, err := config.Load(fstest.MapFS{"factory.yaml": {Data: []byte(yaml)}, "agents/engineer.md": {Data: []byte("# engineer\n")}})
	if err != nil {
		t.Fatal(err)
	}

	return f
}

// token is the task token of a run of version v of factory f on item, in acme.
func (h *kbHarness) token(t *testing.T, f domain.FactoryID, v domain.DefinitionVersionID, item domain.ItemID) string {
	t.Helper()

	tok, err := domain.SignTaskToken(h.key, domain.TaskClaims{
		OrgID: h.orgs["acme"], FactoryID: f, VersionID: v, ItemID: item, Station: kbStation, Agent: agentEngineer, RunID: "run-kb",
		ExpiresAt: h.clock.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}

	return tok
}

// connect opens the MCP session of a run of version v of factory f in acme.
func (h *kbHarness) connect(t *testing.T, f domain.FactoryID, v domain.DefinitionVersionID) *mcp.ClientSession {
	t.Helper()

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "acme-agent", Version: "1"}, nil).Connect(t.Context(),
		&mcp.StreamableClientTransport{
			Endpoint: h.url, HTTPClient: &http.Client{Transport: bearer(h.token(t, f, v, itemID))}, DisableStandaloneSSE: true,
		}, nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = cs.Close() })

	return cs
}

// knowledgeCall calls tool with args and returns its text and whether it is a tool error.
func knowledgeCall(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) (string, bool) {
	t.Helper()

	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}

	var b strings.Builder

	for _, c := range res.Content {
		if text, ok := c.(*mcp.TextContent); ok {
			b.WriteString(text.Text)
		}
	}

	return b.String(), res.IsError
}

// searchHits are the documents (source/title) a knowledge__search answer has hits in.
func searchHits(t *testing.T, text string) []string {
	t.Helper()

	var out struct {
		Hits []struct {
			Document, Source, Title string
		} `json:"hits"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("search answer %q: %v", text, err)
	}

	got := []string{}
	for _, hit := range out.Hits {
		got = append(got, hit.Source+"/"+hit.Title)
	}

	slices.Sort(got)

	return slices.Compact(got)
}

// toolCalls are the knowledge calls the store recorded, as tool:outcome.
func (h *kbHarness) toolCalls() []string {
	var calls []string

	for _, e := range h.store.Events(itemID) {
		if e.Kind == EventToolCall {
			calls = append(calls, fmt.Sprint(e.Data["connection"], "__", e.Data["tool"], ":", e.Data["outcome"]))
		}
	}

	return calls
}

// Threat row 8 at the gateway: a run searches the sources its factory's version lists, in its own
// organization, and nothing else, whatever it asks; every call is recorded.
func TestGateway_KnowledgeSearchOnlyTheFactorysSources(t *testing.T) {
	t.Parallel()

	h := newKBHarness(t)
	cs := h.connect(t, factoryID, version)

	list, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	if got := toolNames(list); !slices.Equal(got, []string{readTool, searchTool, verdictTool, questionTool}) {
		t.Fatalf("tools = %v; want the knowledge tools and the question tool", got)
	}

	text, isErr := knowledgeCall(t, cs, searchTool, map[string]any{argQuery: kbQuery})
	if got := searchHits(t, text); isErr || !slices.Equal(got, []string{"handbook/deploy.md"}) {
		t.Errorf("search = %v (error %v); want acme's handbook only", got, isErr)
	}

	refusals := []map[string]any{
		{argQuery: kbQuery, "source": "secrets"}, // a source the factory does not list
		{argQuery: " "},
		{argQuery: kbQuery, "limit": 21},
	}

	for _, args := range refusals {
		if text, isErr := knowledgeCall(t, cs, searchTool, args); !isErr || strings.Contains(text, "rotates") {
			t.Errorf("search %v = %q (error %v); want a tool error", args, text, isErr)
		}
	}

	if want := []string{"knowledge__search:ok", "knowledge__search:tool_error", "knowledge__search:tool_error", "knowledge__search:tool_error"}; !slices.Equal(h.toolCalls(), want) {
		t.Errorf("recorded calls = %v; want %v", h.toolCalls(), want)
	}
}

// A run reads whole documents of its factory's sources only: one of another source, or of another
// organization, answers as one that does not exist; a large one is cut at 200 KiB.
func TestGateway_KnowledgeReadOnlyTheFactorysSources(t *testing.T) {
	t.Parallel()

	h := newKBHarness(t)
	cs := h.connect(t, factoryID, version)

	if text, isErr := knowledgeCall(t, cs, readTool, map[string]any{argDoc: string(h.docs["deploy.md"])}); isErr || !strings.Contains(text, "Deploys go out on Tuesdays.") {
		t.Errorf("read of a listed document = %q (error %v); want its text", text, isErr)
	}

	for _, title := range []string{"keys.md", "labs.md"} {
		if text, isErr := knowledgeCall(t, cs, readTool, map[string]any{argDoc: string(h.docs[title])}); !isErr || strings.Contains(text, kbQuery) {
			t.Errorf("read of %s = %q (error %v); want a tool error that reveals nothing", title, text, isErr)
		}
	}

	big, _ := knowledgeCall(t, cs, readTool, map[string]any{argDoc: string(h.docs["big.md"])})
	if len(big) > maxReadBytes+512 || !strings.Contains(big, "truncated") {
		t.Errorf("read of a large document = %d bytes; want at most %d of text and a truncation note", len(big), maxReadBytes)
	}
}

// A run whose factory lists no knowledge source has no knowledge tools, only the question tool.
func TestGateway_NoKnowledgeToolsWithoutSources(t *testing.T) {
	t.Parallel()

	h := newKBHarness(t)
	cs := h.connect(t, otherID, otherVersion)

	list, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	if got := toolNames(list); !slices.Equal(got, []string{questionTool}) {
		t.Errorf("tools = %v; want only %s", got, questionTool)
	}

	for _, tool := range []string{searchTool, verdictTool} {
		if _, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: map[string]any{argQuery: kbQuery}}); err == nil {
			t.Errorf("%s without sources succeeded; want a protocol error, as for an unknown tool", tool)
		}
	}
}

// The run bundle names the sources the knowledge tools search, which the worker's prompt
// mentions; a factory that lists none gets no mention.
func TestServeRun_NamesTheKnowledgeSources(t *testing.T) {
	t.Parallel()

	h := newKBHarness(t)
	ts := httptest.NewServer(http.HandlerFunc(h.gw.ServeRun))
	t.Cleanup(ts.Close)

	for _, tc := range []struct {
		name    string
		factory domain.FactoryID
		version domain.DefinitionVersionID
		want    []string
	}{
		{name: "listed", factory: factoryID, version: version, want: []string{"handbook"}},
		{name: "none", factory: otherID, version: otherVersion},
	} {
		it, err := h.store.CreateItem(domain.WithOrg(t.Context(), h.orgs["acme"]), domain.NewItem{
			FactoryID: tc.factory, Title: "Ship", Station: kbStation, State: domain.StateQueued, CreatedAt: h.clock.Now(),
		})
		if err != nil {
			t.Fatal(err)
		}

		status, _, body := request(t, http.MethodGet, ts.URL+"/run", h.token(t, tc.factory, tc.version, it.ID))

		var b runBundle
		if err := json.Unmarshal(body, &b); status != http.StatusOK || err != nil {
			t.Fatalf("%s: GET /run = %d %s (%v)", tc.name, status, body, err)
		}

		if !slices.Equal(b.Spec.KnowledgeSources, tc.want) {
			t.Errorf("%s: knowledge_sources = %v, want %v", tc.name, b.Spec.KnowledgeSources, tc.want)
		}
	}
}

// knowledge__verdict says what it may change: not read-only, idempotent, not destructive, and
// its schema bounds the verdict and the note.
func TestGateway_KnowledgeVerdictTool(t *testing.T) {
	t.Parallel()

	h := newKBHarness(t)
	cs := h.connect(t, factoryID, version)

	list, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	i := slices.IndexFunc(list.Tools, func(tool *mcp.Tool) bool { return tool.Name == verdictTool })
	if i < 0 {
		t.Fatalf("tools = %v; want %s", toolNames(list), verdictTool)
	}

	a := list.Tools[i].Annotations
	if a == nil || a.ReadOnlyHint || !a.IdempotentHint || a.DestructiveHint == nil || *a.DestructiveHint || a.OpenWorldHint == nil || *a.OpenWorldHint {
		t.Errorf("annotations = %+v; want not read-only, idempotent, not destructive, closed world", a)
	}

	schema, err := json.Marshal(list.Tools[i].InputSchema)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{`"additionalProperties":false`, `"enum":["helped","misled"]`, `"maxLength":500`} {
		if !strings.Contains(string(schema), want) {
			t.Errorf("schema %s; want %s", schema, want)
		}
	}
}

// A search records one search use per document among its hits, with their chunks, in the run's
// factory, item and run.
func TestGateway_KnowledgeSearchRecordsUses(t *testing.T) {
	t.Parallel()

	h := newKBHarness(t)
	cs := h.connect(t, factoryID, version)

	if _, isErr := knowledgeCall(t, cs, searchTool, map[string]any{argQuery: kbQuery}); isErr {
		t.Fatal("search failed")
	}

	uses := h.uses(t)
	if len(uses) != 1 {
		t.Fatalf("uses = %+v; want one, of deploy.md", uses)
	}

	u := uses[0]
	if u.Document != h.docs["deploy.md"] || u.Kind != domain.KnowledgeSearched || u.Factory != factoryID || u.Item != itemID || u.Run != "run-kb" ||
		u.Source == "" || len(u.Chunks) == 0 {
		t.Errorf("use = %+v; want a search of deploy.md with its chunks in run-kb", u)
	}
}

// A read records one read use with no chunks.
func TestGateway_KnowledgeReadRecordsAUse(t *testing.T) {
	t.Parallel()

	h := newKBHarness(t)
	cs := h.connect(t, factoryID, version)

	if _, isErr := knowledgeCall(t, cs, readTool, map[string]any{argDoc: string(h.docs["deploy.md"])}); isErr {
		t.Fatal("read failed")
	}

	// A refused read records nothing.
	knowledgeCall(t, cs, readTool, map[string]any{argDoc: string(h.docs["keys.md"])})

	uses := h.uses(t)
	if len(uses) != 1 || uses[0].Document != h.docs["deploy.md"] || uses[0].Kind != domain.KnowledgeRead || uses[0].Item != itemID || len(uses[0].Chunks) != 0 {
		t.Errorf("uses = %+v; want one read of deploy.md", uses)
	}
}

// failingUses is a Knowledge whose recording of uses fails.
type failingUses struct{ *knowledge.Searcher }

var errRecord = errors.New("the store is down")

func (failingUses) RecordSearch(context.Context, domain.KnowledgeUse, []domain.KnowledgeHit) error {
	return errRecord
}

func (failingUses) RecordUse(context.Context, domain.KnowledgeUse) error { return errRecord }

// Recording is best effort: when it fails the call still answers, and the log names the failure
// without the document's text.
func TestGateway_KnowledgeUseFailureKeepsTheHits(t *testing.T) {
	t.Parallel()

	h := newKBHarnessWith(t, func(s *knowledge.Searcher) Knowledge { return failingUses{s} })
	cs := h.connect(t, factoryID, version)

	text, isErr := knowledgeCall(t, cs, searchTool, map[string]any{argQuery: kbQuery})
	if got := searchHits(t, text); isErr || !slices.Equal(got, []string{"handbook/deploy.md"}) {
		t.Errorf("search = %v (error %v); want its hits", got, isErr)
	}

	if text, isErr := knowledgeCall(t, cs, readTool, map[string]any{argDoc: string(h.docs["deploy.md"])}); isErr || !strings.Contains(text, "Tuesdays") {
		t.Errorf("read = %q (error %v); want the document", text, isErr)
	}

	if want := []string{"knowledge__search:ok", "knowledge__read:ok"}; !slices.Equal(h.toolCalls(), want) {
		t.Errorf("recorded calls = %v; want %v", h.toolCalls(), want)
	}

	log := h.logged()
	if strings.Count(log, "gateway knowledge use failed") != 2 || strings.Contains(log, "Tuesdays") || strings.Contains(log, "deploy.md") {
		t.Errorf("log = %s; want two failures without the document's text or title", log)
	}
}

// A verdict is given only on a document the run used, of the run's sources, and is the agent's.
func TestGateway_KnowledgeVerdictOnlyOnUsedDocuments(t *testing.T) {
	t.Parallel()

	h := newKBHarness(t)
	cs := h.connect(t, factoryID, version)
	deploy := string(h.docs["deploy.md"])

	refusals := []struct {
		args map[string]any
		want string
	}{
		{map[string]any{argDoc: deploy, toolVerdict: helped}, "has not searched or read"},
		{map[string]any{argDoc: string(h.docs["keys.md"]), toolVerdict: helped}, "no document"},
		{map[string]any{argDoc: string(h.docs["labs.md"]), toolVerdict: helped}, "no document"},
		{map[string]any{argDoc: "no-such-document", toolVerdict: helped}, "no document"},
		{map[string]any{argDoc: deploy, toolVerdict: "loved"}, "helped or misled"},
		{map[string]any{argDoc: deploy, toolVerdict: "misled", "note": strings.Repeat("n", 501)}, "500"},
	}

	for _, r := range refusals {
		if text, isErr := knowledgeCall(t, cs, verdictTool, r.args); !isErr || !strings.Contains(text, r.want) {
			t.Errorf("verdict %v = %q (error %v); want a tool error naming %q", r.args["verdict"], text, isErr, r.want)
		}
	}

	knowledgeCall(t, cs, readTool, map[string]any{argDoc: deploy})

	text, isErr := knowledgeCall(t, cs, verdictTool, map[string]any{argDoc: deploy, toolVerdict: "misled", "note": "Deploys moved to Wednesdays."})
	if isErr {
		t.Fatalf("verdict on a read document = %q; want it stored", text)
	}

	checkVerdict(t, text, deploy)

	calls := h.toolCalls()
	if n := len(calls); n == 0 || calls[n-1] != "knowledge__verdict:ok" || !slices.Contains(calls, "knowledge__verdict:tool_error") {
		t.Errorf("recorded calls = %v; want the verdicts among them", calls)
	}
}

// uses are the run's knowledge uses in acme.
func (h *kbHarness) uses(t *testing.T) []domain.KnowledgeUse {
	t.Helper()

	uses, err := h.kb.RunUses(domain.WithOrg(t.Context(), h.orgs["acme"]), "run-kb")
	if err != nil {
		t.Fatal(err)
	}

	return uses
}

func (h *kbHarness) logged() string {
	h.logMu.Lock()
	defer h.logMu.Unlock()

	return h.log.String()
}

// lockedWriter is a log writer the gateway's handlers and the test share.
type lockedWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.w.Write(p)
}

// checkVerdict checks a knowledge__verdict answer is the engineer's misled on document in run-kb.
func checkVerdict(t *testing.T, text, document string) {
	t.Helper()

	var got struct {
		Document, Run, Item, Verdict, Note, By string
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("verdict answer %q: %v", text, err)
	}

	if got.Document != document || got.Run != "run-kb" || got.Item != string(itemID) || got.Verdict != "misled" || got.By != "agent:"+agentEngineer {
		t.Errorf("verdict = %+v; want the engineer's misled on %s in run-kb", got, document)
	}
}
