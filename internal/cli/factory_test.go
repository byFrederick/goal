package cli

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/builder"
	"github.com/byFrederick/goal/internal/connkind"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/engine"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/definitions"
	"github.com/byFrederick/goal/internal/knowledge"
)

// The factory commands run against goald's real API handlers and authoring service over
// in-memory ports, so they are tested against the contract they call.

const factoryID = "0b6f1c9e-5d0a-4c39-9a51-6f1f3f0e2a10"

// factoryYAML is a small line: inbox → approve (human) → done.
const factoryYAML = `apiVersion: goal/v1
kind: Factory
metadata: { id: ` + factoryID + `, name: acme, title: Acme }
spec:
  runtime: { modelConnection: claude }
  agents:
    - { name: writer, prompt: ./agents/writer.md }
  board:
    workTypes: [feature, fix]
    stations:
      - { id: inbox, kind: queue }
      - { id: approve, kind: human }
      - { id: done, kind: done }
`

// Paths of the working copy.
const (
	yamlPath   = "factory.yaml"
	promptPath = "agents/writer.md"
	binaryPath = "evals/fixture.bin"
)

// workingCopy is the factory's files; fixture.bin is not text, so it travels as base64.
func workingCopy() map[string]string {
	return map[string]string{yamlPath: factoryYAML, promptPath: "# writer\n", binaryPath: "\x00\x01\xff"}
}

// defGoald is goald's API over in-memory ports with one organization, acme.
type defGoald struct {
	srv     *httptest.Server
	authors *authoring.Service
	cache   *definitions.Cache
	orgID   domain.OrgID
	// keys holds the organization's API keys.
	keys *porttest.Identity
	// conns holds the organization's connections, with goald's platform kinds.
	conns *porttest.Connections
	// creds holds the organization's credentials, which its connections may refer to.
	creds *porttest.Credentials
	// kb is the organization's knowledge base; sync indexes what is due in it, as goald's loop.
	kb   *porttest.Knowledge
	sync func(t *testing.T)

	mu sync.Mutex
	// beforeTree runs once before the next PUT …/draft/tree is served.
	beforeTree func()
}

func newDefGoald(t *testing.T) *defGoald {
	t.Helper()

	tenancy := porttest.NewTenancy(time.Now)
	store := porttest.NewStore()
	tenancy.WithStore(store)

	o, err := tenancy.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: orgAcme, Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}

	clock := porttest.NewClock(time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC))
	defNotices := porttest.NewDefinitionNotices()
	defs := porttest.NewDefinitions(porttest.DefinitionsDeps{Objects: porttest.NewObjectStore(), Items: store, Notices: defNotices, Now: clock.Now})
	objects := porttest.NewObjectStore()
	authors := authoring.New(authoring.Deps{Store: defs, Loads: store, Templates: objects, Clock: clock})

	e, err := engine.New(engine.Deps{
		Store: store, Orgs: tenancy, Sandboxes: porttest.NewSandboxes(porttest.NewWorker(porttest.Succeed(`{}`)).Exec),
		Contracts: porttest.NewContracts(), Secrets: porttest.Secrets{}, Clock: clock,
		CodeHosts:   map[string]ports.CodeHost{},
		Connections: porttest.NewConnections(porttest.NewMasterKeys().Key("k1"), tenancy),
		Credentials: porttest.NewCredentials(porttest.NewMasterKeys().Key("k1"), tenancy, nil),
	}, engine.Config{Image: "ghcr.io/acme/goal-agent:1", GatewayURL: "http://gateway.example.com/mcp", GatewayKeyRef: "env:GOAL_GATEWAY_KEY"})
	if err != nil {
		t.Fatal(err)
	}

	cache := definitions.New(defs, tenancy, func(_ context.Context, lines []domain.Line) error { return e.SetLines(lines) },
		definitions.Config{}, nil)

	keys := porttest.NewIdentity(clock.Now)
	keys.AddOrg(o)

	conns := porttest.NewConnections(porttest.NewMasterKeys().Key("k1"), tenancy)

	kinds, err := connkind.Platform()
	if err != nil {
		t.Fatal(err)
	}

	if err := conns.SyncPlatformKinds(t.Context(), kinds); err != nil {
		t.Fatal(err)
	}

	kb := porttest.NewKnowledge(conns, tenancy)
	creds := porttest.NewCredentials(porttest.NewMasterKeys().Key("k1"), tenancy, conns)
	uploads := knowledge.NewUploads(kb, porttest.NewObjectStore(), 0)

	// Its own transport: parallel tests closing their httptest servers call CloseIdleConnections on
	// http.DefaultTransport, which breaks a connection test's request in flight.
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)

	handler, err := api.New(api.Deps{
		Engine: e, Items: store, Factories: cache, Definitions: authors, Versions: defs, Orgs: tenancy, Audit: tenancy,
		Access: keys, OrgRetention: time.Hour, Authorizer: api.AllowAll{}, Auth: api.AuthConfig{Mode: api.AuthNone},
		Platform: porttest.NewPlatform(porttest.NewIdentity(clock.Now)), Feed: api.NewFeed(porttest.NewItemNotices(), defNotices, nil),
		Builder: builder.New(objects, clock, builder.Config{}), Connections: conns, ParseKind: connkind.Parse,
		Credentials: creds, Intake: porttest.NewIntake(porttest.NewMasterKeys().Key("k1"), store), DraftRequests: porttest.NewDraftRequests(tenancy, clock.Now),
		ConnectionTransport: transport, Knowledge: kb, KnowledgeUploads: uploads, KnowledgeSearch: knowledge.NewSearcher(kb, nil, nil),
		UploadMaxBytes: domain.DefaultMaxUpload, Clock: clock,
		OrgPolicies: porttest.NewOrgPolicies(nil, tenancy, clock.Now), BoxSizes: []domain.BoxSize{{Size: domain.SandboxMedium, VCPUs: 2, MemoryBytes: 4 << 30}},
	})
	if err != nil {
		t.Fatal(err)
	}

	g := &defGoald{authors: authors, cache: cache, orgID: o.ID, keys: keys, conns: conns, creds: creds, kb: kb}
	g.sync = func(t *testing.T) {
		t.Helper()

		// The knowledge fake dates sources by the time of day, so the loop's clock is too.
		in := knowledge.NewIngestor(knowledge.IngestorConfig{
			Knowledge: kb, Connections: conns, Uploads: uploads, Clock: porttest.NewClock(time.Now().Add(time.Second)),
		})
		if _, err := in.SyncDue(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/draft/tree") {
			g.mu.Lock()
			hook := g.beforeTree
			g.beforeTree = nil
			g.mu.Unlock()

			if hook != nil {
				hook()
			}
		}

		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(g.srv.Close)

	return g
}

// goal runs one CLI command line against g and returns its output.
func (g *defGoald) goal(t *testing.T, args ...string) (string, error) {
	t.Helper()

	var buf bytes.Buffer

	err := run(t.Context(), args, &printer{w: &buf}, testEnv(t, g.srv.URL, map[string]string{envOrg: orgAcme}), nil)

	return buf.String(), err
}

// mustGoal runs a command line that must succeed.
func (g *defGoald) mustGoal(t *testing.T, args ...string) string {
	t.Helper()

	out, err := g.goal(t, args...)
	if err != nil {
		t.Fatalf("goal %s: %v\n%s", strings.Join(args, " "), err, out)
	}

	return out
}

// saveDraftFile changes a file of the open draft as the builder would, at whatever revision.
func (g *defGoald) saveDraftFile(t *testing.T, path, content string) {
	t.Helper()

	if _, err := g.authors.PutFile(g.ctx(t), factoryID, authoring.FileWrite{Path: path, Content: []byte(content)}); err != nil {
		t.Fatal(err)
	}
}

// ctx is a request of the local user in acme.
func (g *defGoald) ctx(t *testing.T) context.Context {
	t.Helper()

	return domain.WithActor(domain.WithOrg(t.Context(), g.orgID), domain.LocalUser)
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()

	for p, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func readFiles(t *testing.T, dir string) map[string]string {
	t.Helper()

	files, err := readFolder(dir)
	if err != nil {
		t.Fatal(err)
	}

	out := map[string]string{}
	for p, b := range files {
		out[p] = string(b)
	}

	return out
}

func readRecord(t *testing.T, dir string) pulled {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(dir, ".goal", "pulled.json")) //nolint:gosec // the test's own folder
	if err != nil {
		t.Fatal(err)
	}

	var rec pulled
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}

	return rec
}

// newCopy writes the working copy, with whatever else, into a new folder.
func newCopy(t *testing.T, extra map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	files := workingCopy()
	maps.Copy(files, extra)
	writeFiles(t, dir, files)

	return dir
}

func TestFactoryPush_PublishThenRun(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)
	dir := newCopy(t, map[string]string{".git/HEAD": "ref: main", "knowledge/notes/a.md": "# local\n"})

	out := g.mustGoal(t, "factory", "push", dir, "--publish", "--message", "first line")
	for _, want := range []string{"added     " + binaryPath, "added     " + promptPath, "✓ published acme v1"} {
		if !strings.Contains(out, want) {
			t.Errorf("push output lacks %q:\n%s", want, out)
		}
	}

	if strings.Contains(out, ".git") || strings.Contains(out, "knowledge") {
		t.Errorf("push sent hidden files or the knowledge base:\n%s", out)
	}

	if rec := readRecord(t, dir); rec.FactoryID != factoryID || rec.Org != string(g.orgID) || rec.Number != 1 || rec.Revision != 0 ||
		len(rec.Manifest) != 3 {
		t.Errorf("pulled.json after the publish = %+v", rec)
	}

	// goald runs what was published: the cache reloads on the publish's notice, as goald's
	// listener does, and an item starts on the new line.
	if _, err := g.cache.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}

	if out := g.mustGoal(t, "run", "acme", "Write the notes", "--detach"); !strings.Contains(out, "submitted acme#1") {
		t.Errorf("run = %s", out)
	}

	// The files went as they are, binary included, and the paths with slashes came back.
	pulledDir := filepath.Join(t.TempDir(), "pulled")
	g.mustGoal(t, "factory", "pull", "acme", pulledDir)

	if got := readFiles(t, pulledDir); !maps.Equal(got, workingCopy()) {
		t.Errorf("pulled files = %q, want %q", got, workingCopy())
	}
}

// goal run names no work type of its own: without --type the item gets the factory's first.
func TestRun_TypeDefaultsToTheFactorysFirst(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)
	dir := newCopy(t, map[string]string{yamlPath: strings.Replace(factoryYAML, "[feature, fix]", "[fix, chore]", 1)})
	g.mustGoal(t, "factory", "push", dir, "--publish")

	if _, err := g.cache.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}

	g.mustGoal(t, "run", "acme", "Fix the notes", "--detach")

	if out := g.mustGoal(t, "item", "acme#1"); !strings.Contains(out, `"type": "fix"`) {
		t.Errorf("goal item acme#1 = %s", out)
	}
}

func TestFactoryPush_DraftWithoutPublish(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)
	dir := newCopy(t, nil)
	g.mustGoal(t, "factory", "push", dir, "--publish")

	writeFiles(t, dir, map[string]string{promptPath: "# writer, careful\n"})

	if err := os.Remove(filepath.Join(dir, filepath.FromSlash(binaryPath))); err != nil {
		t.Fatal(err)
	}

	out := g.mustGoal(t, "factory", "push", dir)
	for _, want := range []string{"updated   " + promptPath, "deleted   " + binaryPath, "unchanged 1 files", "goal factory publish acme"} {
		if !strings.Contains(out, want) {
			t.Errorf("push output lacks %q:\n%s", want, out)
		}
	}

	if out := g.mustGoal(t, "factory", "list"); !strings.Contains(out, "v1 + draft r2") {
		t.Errorf("list = %s", out)
	}

	// The folder's record says it is the draft now, so pushing it again is allowed.
	if rec := readRecord(t, dir); rec.Number != 1 || rec.Revision != 2 {
		t.Errorf("pulled.json = %+v, want v1, revision 2", rec)
	}

	g.mustGoal(t, "factory", "push", dir)

	if out := g.mustGoal(t, "factory", "publish", "acme", "--message", "careful"); !strings.Contains(out, "✓ published acme v2") {
		t.Errorf("publish = %s", out)
	}

	out = g.mustGoal(t, "factory", "versions", "acme")
	if !strings.Contains(out, "* v2") || !strings.Contains(out, "careful") || !strings.Contains(out, "  v1") {
		t.Errorf("versions = %s", out)
	}

	if out := g.mustGoal(t, "factory", "restore", "acme", "1"); !strings.Contains(out, "acme runs v1 again (it ran v2)") {
		t.Errorf("restore = %s", out)
	}
}

func TestFactoryPush_RefusesAPushStaleSinceItsPull(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)
	g.mustGoal(t, "factory", "push", newCopy(t, nil), "--publish")

	mine, theirs := filepath.Join(t.TempDir(), "mine"), filepath.Join(t.TempDir(), "theirs")
	g.mustGoal(t, "factory", "pull", "acme", mine)
	g.mustGoal(t, "factory", "pull", factoryID, theirs)

	writeFiles(t, theirs, map[string]string{promptPath: "# writer, theirs\n"})
	g.mustGoal(t, "factory", "push", theirs, "--publish")

	writeFiles(t, mine, map[string]string{promptPath: "# writer, mine\n"})

	out, err := g.goal(t, "factory", "push", mine, "--publish")
	if err == nil || !strings.Contains(err.Error(), "acme v2 was published") || !strings.Contains(err.Error(), "pulled from v1") {
		t.Fatalf("stale push = %v\n%s", err, out)
	}

	if out := g.mustGoal(t, "factory", "list"); !strings.Contains(out, "v2") || strings.Contains(out, "draft") {
		t.Errorf("the refused push changed the factory: %s", out)
	}

	// A version holding exactly what the folder pushed is not a reason to refuse.
	g.mustGoal(t, "factory", "push", theirs)
	g.mustGoal(t, "factory", "publish", "acme")
	writeFiles(t, theirs, map[string]string{promptPath: "# writer, theirs again\n"})
	g.mustGoal(t, "factory", "push", theirs)

	// --force pushes over the newer version; the record follows.
	g.mustGoal(t, "factory", "push", mine, "--force", "--replace-draft")

	if rec := readRecord(t, mine); rec.Number != 3 {
		t.Errorf("pulled.json after --force = %+v, want v3", rec)
	}
}

func TestFactoryPush_RefusesAnOpenDraftItWasNotPulledFrom(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)
	dir := newCopy(t, nil)
	g.mustGoal(t, "factory", "push", dir, "--publish")

	// Someone opens the draft in the builder and changes it.
	if _, _, err := g.authors.OpenDraft(g.ctx(t), factoryID, authoring.OpenDraftRequest{}); err != nil {
		t.Fatal(err)
	}

	g.saveDraftFile(t, promptPath, "# writer, from the builder\n")

	writeFiles(t, dir, map[string]string{promptPath: "# writer, from the folder\n"})

	out, err := g.goal(t, "factory", "push", dir)
	if err == nil || !strings.Contains(err.Error(), "open draft with 1 changes") || !strings.Contains(err.Error(), "--replace-draft") {
		t.Fatalf("push over the builder's draft = %v\n%s", err, out)
	}

	// Pulling the draft makes it the folder's base; then the push replaces it.
	drafted := filepath.Join(t.TempDir(), "draft")
	if out := g.mustGoal(t, "factory", "pull", "acme", drafted, "--version", "draft"); !strings.Contains(out, "the draft of acme at revision 2 (3 files)") {
		t.Errorf("pull of the draft = %s", out)
	}

	if rec := readRecord(t, drafted); rec.Number != 1 || rec.Revision != 2 {
		t.Errorf("pulled.json of a draft = %+v, want v1, revision 2", rec)
	}

	writeFiles(t, drafted, map[string]string{promptPath: "# writer, merged\n"})
	g.mustGoal(t, "factory", "push", drafted)

	// --replace-draft archives the open draft and starts one from the folder.
	g.mustGoal(t, "factory", "push", dir, "--replace-draft")

	if got := readFiles(t, mustPull(t, g, "draft"))[promptPath]; got != "# writer, from the folder\n" {
		t.Errorf("draft prompt after --replace-draft = %q", got)
	}
}

func TestFactoryPush_RefusesWhenTheDraftMovesMeanwhile(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)
	dir := newCopy(t, nil)
	g.mustGoal(t, "factory", "push", dir)

	writeFiles(t, dir, map[string]string{promptPath: "# writer, from the folder\n"})

	// The builder saves between the push's read of the draft and its write.
	g.mu.Lock()
	g.beforeTree = func() { g.saveDraftFile(t, promptPath, "# writer, from the builder\n") }
	g.mu.Unlock()

	out, err := g.goal(t, "factory", "push", dir)
	if err == nil || !strings.Contains(err.Error(), "changed while pushing") {
		t.Fatalf("push over a moving draft = %v\n%s", err, out)
	}

	if got := readFiles(t, mustPull(t, g, "draft"))[promptPath]; got != "# writer, from the builder\n" {
		t.Errorf("the builder's save was overwritten: %q", got)
	}
}

func TestFactoryPublish_PrintsWhatBlocksIt(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)
	dir := newCopy(t, map[string]string{yamlPath: strings.Replace(factoryYAML, "kind: human", "kind: agent, agent: nobody", 1)})

	out, err := g.goal(t, "factory", "push", dir, "--publish")
	if err == nil || !strings.Contains(err.Error(), "acme is not published") {
		t.Fatalf("publish of a broken draft = %v\n%s", err, out)
	}

	if want := filepath.Join(dir, yamlPath) + ":12:"; !strings.Contains(out, want) || !strings.Contains(out, "error") {
		t.Errorf("problems are not located in the folder (%s):\n%s", want, out)
	}

	// The draft was saved all the same, so the folder can be fixed and pushed again.
	if rec := readRecord(t, dir); rec.Revision == 0 {
		t.Errorf("pulled.json = %+v, want the draft's revision", rec)
	}

	if out, err := g.goal(t, "factory", "publish", "acme"); err == nil || !strings.Contains(out, "factory.yaml:12:") {
		t.Errorf("goal factory publish of the broken draft = %v\n%s", err, out)
	}
}

func TestFactoryValidate_OnlineAndOffline(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)
	good := newCopy(t, nil)
	broken := newCopy(t, map[string]string{yamlPath: strings.Replace(factoryYAML, "./agents/writer.md", "./agents/missing.md", 1)})

	for _, args := range [][]string{
		{"factory", "validate", good, broken},
		{"factory", "validate", "--offline", good, broken},
		{validateAlias, good, broken},
		// goal validate is goal factory validate --offline, with the same flags anywhere.
		{validateAlias, "--offline", good, broken},
		{validateAlias, good, "--org", "acme", broken},
	} {
		out, err := g.goal(t, args...)
		if err == nil || !strings.Contains(err.Error(), "1 of 2 factories are invalid") {
			t.Errorf("goal %v = %v", args, err)
		}

		if !strings.Contains(out, "✓ acme (0b6f1c9e) — 1 agents, 3 stations") || !strings.Contains(out, "✗ "+broken) ||
			!strings.Contains(out, filepath.Join(broken, yamlPath)+":7:") || !strings.Contains(out, "error prompt-not-found") {
			t.Errorf("goal %v printed:\n%s", args, out)
		}
	}

	// A factory goald knows is validated over its open draft.
	g.mustGoal(t, "factory", "push", good)

	if out := g.mustGoal(t, "factory", "validate", good); !strings.Contains(out, "✓ acme") {
		t.Errorf("validate over the draft = %s", out)
	}

	// Once published, with no draft open, the folder and the current version are the factory's
	// own files: its name is not taken by itself.
	g.mustGoal(t, "factory", "publish", "acme")

	if out := g.mustGoal(t, "factory", "validate", good); !strings.Contains(out, "✓ acme") {
		t.Errorf("validate of a published factory's folder = %s", out)
	}

	if out := g.mustGoal(t, "factory", "list", "--validate"); !strings.Contains(out, "✓ acme") || !strings.Contains(out, "v1") {
		t.Errorf("list --validate = %s", out)
	}
}

// A problem with a quick fix names it under the problem, online and offline, so the YAML can be
// fixed by hand the way the builder would.
func TestFactoryValidate_PrintsQuickFixes(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)
	dir := newCopy(t, map[string]string{yamlPath: strings.Replace(factoryYAML, "modelConnection: claude", "modelConnection: claude, image: ghcr.io/acme/agent:1.0.0", 1)})

	// A form connections replaced is an error since the cutover: the folder is invalid, and the
	// problem says how to fix it.
	for _, validate := range []func() (string, error){
		func() (string, error) { return g.goal(t, "factory", "validate", dir) },
		func() (string, error) { return g.goal(t, validateAlias, dir) },
	} {
		out, err := validate()
		if err == nil {
			t.Fatalf("validate = %q; want the folder invalid", out)
		}

		if all := out + err.Error(); !strings.Contains(all, "error field-removed runtime.image is removed") ||
			!strings.Contains(all, "\n      fix: Remove runtime.image\n") {
			t.Errorf("validate printed:\n%s", all)
		}
	}
}

func TestFactoryPull_RefusesAFolderWithFiles(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)
	dir := newCopy(t, nil)
	g.mustGoal(t, "factory", "push", dir)

	if _, err := g.goal(t, "factory", "pull", "acme", dir); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Errorf("pull into a folder with files = %v", err)
	}

	if _, err := g.goal(t, "factory", "pull", "acme", filepath.Join(t.TempDir(), "x"), "--version", selectCurrent); err == nil ||
		!strings.Contains(err.Error(), "never published") {
		t.Errorf("pull of the current version before a publish = %v", err)
	}

	if _, err := g.goal(t, "factory", "pull", "nobody", filepath.Join(t.TempDir(), "x")); err == nil ||
		!strings.Contains(err.Error(), `no factory "nobody"`) {
		t.Errorf("pull of an unknown factory = %v", err)
	}
}

func TestFactory_RefusesBadCommandLines(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)

	for _, line := range []string{
		"factory", "factory remove x", "factory push", "factory push a b", "factory push a --message m",
		"factory pull a", "factory pull a b --version latest", "factory validate", "factory list x",
		"factory publish", "factory versions", "factory restore a zero", "factory push a --move inbox",
		"factory archive", "factory archive a b", "factory delete", "factory delete a b", "factory restore a 1 2",
		"factory restore a --move inbox=done", "factory list --archived x",
	} {
		if _, err := g.goal(t, strings.Fields(line)...); !errors.Is(err, errUsage) {
			t.Errorf("goal %s = %v, want a usage error", line, err)
		}
	}
}

// mustPull pulls acme's version sel into a new folder and returns it.
func mustPull(t *testing.T, g *defGoald, sel string) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "pulled-"+sel)
	g.mustGoal(t, "factory", "pull", "acme", dir, "--version", sel)

	return dir
}

// publishedAcme pushes and publishes acme and returns g.
func publishedAcme(t *testing.T) *defGoald {
	t.Helper()

	g := newDefGoald(t)
	g.mustGoal(t, "factory", "push", newCopy(t, nil), "--publish")

	return g
}

// pause pauses acme as the UI's pause would; the CLI has no pause.
func (g *defGoald) pause(t *testing.T) {
	t.Helper()

	if _, err := g.authors.Pause(g.ctx(t), factoryID); err != nil {
		t.Fatal(err)
	}
}

// goalTyping runs a command line with typed as the terminal's input (nil: none).
func (g *defGoald) goalTyping(t *testing.T, typed *string, args ...string) (string, error) {
	t.Helper()

	var buf bytes.Buffer

	p := &printer{w: &buf}
	if typed != nil {
		p.in = strings.NewReader(*typed)
	}

	err := run(t.Context(), args, p, testEnv(t, g.srv.URL, map[string]string{envOrg: orgAcme}), nil)

	return buf.String(), err
}

func TestFactoryArchive_ArchivesAndRestores(t *testing.T) {
	t.Parallel()

	g := publishedAcme(t)
	g.pause(t)

	if out := g.mustGoal(t, "factory", "archive", "acme"); !strings.Contains(out, "✓ archived acme ("+factoryID+")") {
		t.Errorf("archive = %s", out)
	}

	// Archived, it is still found by name and by id.
	if out := g.mustGoal(t, "factory", "archive", factoryID); !strings.Contains(out, "✓ archived acme") {
		t.Errorf("archive by id of an archived factory = %s", out)
	}

	out := g.mustGoal(t, "factory", "restore", "acme")
	if !strings.Contains(out, "✓ restored acme ("+factoryID+")") || !strings.Contains(out, "still paused") {
		t.Errorf("restore = %s", out)
	}

	if out := g.mustGoal(t, "factory", "list"); !strings.Contains(out, factoryID) || !strings.Contains(out, "paused") {
		t.Errorf("list after the restore = %s", out)
	}

	// Two arguments still restore a version.
	if out := g.mustGoal(t, "factory", "restore", "acme", "1"); !strings.Contains(out, "acme v1 runs already") {
		t.Errorf("restore of a version = %s", out)
	}
}

func TestFactoryArchive_RefusesARunningFactory(t *testing.T) {
	t.Parallel()

	g := publishedAcme(t)

	out, err := g.goal(t, "factory", "archive", "acme")
	if err == nil || !strings.Contains(err.Error(), "Pause the factory first") {
		t.Errorf("archive of a running factory = %v\n%s", err, out)
	}

	if out := g.mustGoal(t, "factory", "list"); !strings.Contains(out, factoryID) {
		t.Errorf("a refused archive archived acme:\n%s", out)
	}
}

func TestFactoryDelete_ConfirmsByTypedName(t *testing.T) {
	t.Parallel()

	typed := func(s string) *string { return &s }
	yes := []string{"--yes"}

	const deleted = "✓ deleted acme"

	for _, tt := range []struct {
		name      string
		published bool
		archived  bool
		ref       string // the factory as delete names it; acme when empty
		args      []string
		typed     *string
		want      string // in the output on success, in the error otherwise
		deleted   bool
	}{
		{name: "match", published: true, archived: true, typed: typed("acme\n"), want: deleted, deleted: true},
		{name: "mismatch", published: true, archived: true, typed: typed("acne\n"), want: "nothing was deleted"},
		{name: "no input", published: true, archived: true, want: "pass --yes"},
		{name: "end of input", published: true, archived: true, typed: typed(""), want: "read the confirmation"},
		{name: "yes", published: true, archived: true, args: yes, want: deleted, deleted: true},
		{name: "by id", published: true, archived: true, ref: factoryID, args: yes, want: deleted, deleted: true},
		{name: "never published", typed: typed("acme\n"), want: deleted, deleted: true},
		{name: "not archived", published: true, args: yes, want: "`goal factory archive acme`"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := newDefGoald(t)
			if tt.published {
				g.mustGoal(t, "factory", "push", newCopy(t, nil), "--publish")
			} else {
				g.mustGoal(t, "factory", "push", newCopy(t, nil))
			}

			if tt.archived {
				g.pause(t)
				g.mustGoal(t, "factory", "archive", "acme")
			}

			out, err := g.goalTyping(t, tt.typed, strings.Fields("factory delete "+cmp.Or(tt.ref, "acme")+" "+strings.Join(tt.args, " "))...)

			switch {
			case tt.deleted && (err != nil || !strings.Contains(out, tt.want)):
				t.Errorf("delete = %v\n%s", err, out)
			case !tt.deleted && (err == nil || !strings.Contains(err.Error(), tt.want)):
				t.Errorf("delete = %v, want an error with %q\n%s", err, tt.want, out)
			}

			listed := g.mustGoal(t, "factory", "list") + g.mustGoal(t, "factory", "list", "--archived")
			if gone := !strings.Contains(listed, factoryID); gone != tt.deleted {
				t.Errorf("deleted = %v, want %v:\n%s", gone, tt.deleted, listed)
			}
		})
	}
}

func TestFactoryList_Archived(t *testing.T) {
	t.Parallel()

	g := publishedAcme(t)

	other := strings.NewReplacer(factoryID, "6f7a2b1c-0d3e-4f5a-8b9c-1d2e3f4a5b6c", "name: acme", "name: beta").Replace(factoryYAML)
	g.mustGoal(t, "factory", "push", newCopy(t, map[string]string{yamlPath: other}), "--publish")

	g.pause(t)
	g.mustGoal(t, "factory", "archive", "acme")

	out := g.mustGoal(t, "factory", "list", "--archived")
	if !strings.Contains(out, factoryID) || !strings.Contains(out, "· archived") || strings.Contains(out, "beta") ||
		!strings.Contains(out, "1 archived factories") {
		t.Errorf("list --archived = %s", out)
	}

	if out := g.mustGoal(t, "factory", "list"); strings.Contains(out, factoryID) || !strings.Contains(out, "beta") {
		t.Errorf("list = %s", out)
	}
}

// goal validate checks the working copy's rules: a rule naming an agent the factory does not have
// is an error that names the rule, the agent and its code.
func TestFactoryValidate_ReportsRuleProblems(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)
	withRules := strings.Replace(factoryYAML, "  agents:\n", "  rules:\n    - { name: tone, source: ./rules/tone.md }\n  agents:\n", 1)
	rule := func(agent string) string {
		return "---\ndescription: Write plainly.\nappliesTo: { agents: [" + agent + "] }\n---\n\nShort sentences.\n"
	}

	good := newCopy(t, map[string]string{yamlPath: withRules, "rules/tone.md": rule("writer")})
	broken := newCopy(t, map[string]string{yamlPath: withRules, "rules/tone.md": rule("ghost")})

	out, err := g.goal(t, validateAlias, good, broken)
	if err == nil || !strings.Contains(err.Error(), "1 of 2 factories are invalid") {
		t.Errorf("goal validate = %v", err)
	}

	if !strings.Contains(out, "✓ acme") || !strings.Contains(out, "✗ "+broken) ||
		!strings.Contains(out, `rule tone: appliesTo.agents: unknown agent "ghost"`) || !strings.Contains(out, "error unknown-agent") {
		t.Errorf("goal validate printed:\n%s", out)
	}
}
