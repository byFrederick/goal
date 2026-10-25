package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/knowledge"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
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
	searchTool = "knowledge__search"
	readTool   = "knowledge__read"
	kbQuery    = "deploy"
	kbStation  = "build"
	argQuery   = "query"
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
	gw    *Gateway
}

func newKBHarness(t *testing.T) *kbHarness {
	t.Helper()

	h := &kbHarness{
		key: []byte("gateway-test-key"), clock: porttest.NewClock(time.Now().UTC().Add(time.Second)), store: porttest.NewStore(),
		orgs: map[string]domain.OrgID{}, docs: map[string]domain.KnowledgeDocumentID{},
	}

	tenancy := porttest.NewTenancy(h.clock.Now)
	conns := porttest.NewConnections(porttest.NewMasterKeys().Key("k1"), tenancy)
	kb := porttest.NewKnowledge(conns, tenancy)
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
		Knowledge: knowledge.NewSearcher(kb, nil, nil),
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
		OrgID: h.orgs["acme"], FactoryID: f, VersionID: v, ItemID: item, Station: kbStation, Agent: "engineer", RunID: "run-kb",
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

	if got := toolNames(list); !slices.Equal(got, []string{readTool, searchTool}) {
		t.Fatalf("tools = %v; want the knowledge tools", got)
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

	if text, isErr := knowledgeCall(t, cs, readTool, map[string]any{"document": string(h.docs["deploy.md"])}); isErr || !strings.Contains(text, "Deploys go out on Tuesdays.") {
		t.Errorf("read of a listed document = %q (error %v); want its text", text, isErr)
	}

	for _, title := range []string{"keys.md", "labs.md"} {
		if text, isErr := knowledgeCall(t, cs, readTool, map[string]any{"document": string(h.docs[title])}); !isErr || strings.Contains(text, kbQuery) {
			t.Errorf("read of %s = %q (error %v); want a tool error that reveals nothing", title, text, isErr)
		}
	}

	big, _ := knowledgeCall(t, cs, readTool, map[string]any{"document": string(h.docs["big.md"])})
	if len(big) > maxReadBytes+512 || !strings.Contains(big, "truncated") {
		t.Errorf("read of a large document = %d bytes; want at most %d of text and a truncation note", len(big), maxReadBytes)
	}
}

// A run whose factory lists no knowledge source has no knowledge tools.
func TestGateway_NoKnowledgeToolsWithoutSources(t *testing.T) {
	t.Parallel()

	h := newKBHarness(t)
	cs := h.connect(t, otherID, otherVersion)

	list, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(list.Tools) != 0 {
		t.Errorf("tools = %v; want none", toolNames(list))
	}

	if _, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: searchTool, Arguments: map[string]any{argQuery: kbQuery}}); err == nil {
		t.Error("search without sources succeeded; want a protocol error, as for an unknown tool")
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
