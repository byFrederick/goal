package moveover_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/connkind"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/engine"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/moveover"
)

var t0 = time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)

const (
	refusedUUID = "0b6f1c9e-5d0a-4c39-9a51-6f1f3f0e2a78"
	awsSecret   = "aws-secret-for-tests"
	defaultFile = "environments/default.yaml"
)

// logBuffer is a log sink safe for the engine's goroutines.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// world is one install on the fakes: an organization with the goal organization's four
// connections, its factories' definitions, secrets and conversions, and the mover over them.
type world struct {
	org         domain.OrgID
	clock       *porttest.Clock
	store       *porttest.Store
	tenancy     *porttest.Tenancy
	key         ports.MasterKey
	conns       *porttest.Connections
	secrets     *porttest.OrgSecrets
	kb          *porttest.Knowledge
	defs        *porttest.Definitions
	conversions *porttest.FactoryConversions
	hosts       *porttest.CodeHost
	tools       *porttest.ToolLister
	mover       *moveover.Mover
	logs        *logBuffer
}

// repoURL is the production factory's repository.
const (
	repoURL      = "https://github.com/acme/app.git"
	githubMCPURL = "https://api.githubcopilot.com/mcp/"
)

// canPush is a token's access that can read the repository and push to it.
var canPush = ports.RepositoryAccess{Read: ports.AccessCheck{OK: true, Reason: "can read"}, Push: ports.AccessCheck{OK: true, Reason: "can push"}}

var person = domain.Actor{Kind: domain.ActorUser, ID: "0b6f1c9e-0000-7000-8000-00000000aaaa", Name: "Ada"}

func newWorld(t *testing.T) *world {
	t.Helper()

	w := &world{clock: porttest.NewClock(t0), store: porttest.NewStore(), logs: &logBuffer{}, key: porttest.NewMasterKeys().Key("k1")}
	w.tenancy = porttest.NewTenancy(w.clock.Now).WithStore(w.store)

	o, err := w.tenancy.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: "acme", Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}

	w.org = o.ID
	w.conns = porttest.NewConnections(w.key, w.tenancy)
	w.secrets = porttest.NewOrgSecrets(w.key, w.tenancy, w.conns)
	w.kb = porttest.NewKnowledge(w.conns, w.tenancy)
	w.kb.ReadSecrets(w.secrets)
	w.defs = porttest.NewDefinitions(porttest.DefinitionsDeps{Objects: porttest.NewObjectStore(), Items: w.store, Audit: w.tenancy, Now: w.clock.Now})
	w.conversions = porttest.NewFactoryConversions()

	// The converted versions' checks pass unless a test says otherwise: the repository token can
	// read and push, and the github and context7 MCP servers answer tools/list.
	w.hosts, w.tools = porttest.NewCodeHost(), porttest.NewToolLister()
	w.hosts.SetAccess(repoURL, githubValue, canPush)
	w.tools.Serve(context7URL, "query-docs", "resolve-library-id")
	w.tools.Serve(githubMCPURL, "create_pull_request")

	platform, err := connkind.Platform()
	if err != nil {
		t.Fatal(err)
	}

	if err := w.conns.SyncPlatformKinds(t.Context(), platform); err != nil {
		t.Fatal(err)
	}

	ctx := w.ctx(t)

	for _, raw := range []string{cloudflareKind, context7Kind} {
		k, err := connkind.Parse([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}

		if err := w.conns.PutKind(ctx, k, domain.LocalUser); err != nil {
			t.Fatal(err)
		}
	}

	for _, n := range []domain.NewConnection{
		{Name: connGitHub, Kind: connGitHub, Secrets: map[string]string{fieldToken: githubValue}},
		{Name: connCloudflare, Kind: connCloudflare, Fields: map[string]string{"account_id": accountID}, Secrets: map[string]string{"api_token": cfValue}},
		{Name: connContext7, Kind: connContext7, Secrets: map[string]string{"key": ctx7Value}},
		{Name: "claude", Kind: "claude-subscription", Secrets: map[string]string{"token": oauthValue}},
		{Name: "aws-prod", Kind: "aws", Secrets: map[string]string{"access_key_id": "AKIAEXAMPLE", "secret_access_key": awsSecret}},
	} {
		if _, err := w.conns.Create(ctx, n, domain.LocalUser); err != nil {
			t.Fatalf("connection %s: %v", n.Name, err)
		}
	}

	if _, err := w.conns.Update(ctx, connContext7, domain.ConnectionPatch{
		ToolModes: map[string]domain.ConnectionToolMode{"resolve-library-id": domain.ToolModeAsk},
	}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	authors := authoring.New(authoring.Deps{
		Store: w.defs, Templates: porttest.NewObjectStore(), Credentials: authoring.SecretsCheck{Secrets: w.secrets}, Clock: w.clock,
	})

	w.mover = moveover.New(moveover.Deps{
		Orgs: w.tenancy, Connections: w.conns, Secrets: w.secrets, Knowledge: w.kb, Definitions: w.defs, Publisher: authors,
		Conversions: w.conversions, CodeHosts: map[string]ports.CodeHost{connGitHub: w.hosts}, Tools: w.tools,
		Clock: w.clock, Logger: slog.New(slog.NewJSONHandler(w.logs, nil)),
	})

	return w
}

func (w *world) ctx(t *testing.T) context.Context {
	t.Helper()

	return domain.WithActor(domain.WithOrg(t.Context(), w.org), person)
}

// publishOld publishes tree as factory id's first version, as goald did before ADR 0037: through
// the store, which the old keys do not stop.
func (w *world) publishOld(t *testing.T, id domain.FactoryID, name string, tree map[string][]byte) domain.DefinitionVersion {
	t.Helper()

	ctx := w.ctx(t)

	m, err := w.defs.PutBlobs(ctx, tree)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := w.defs.CreateFactory(ctx, ports.NewFactory{ID: id, Name: name, Manifest: m, By: person}); err != nil {
		t.Fatal(err)
	}

	v, err := w.defs.Publish(ctx, ports.Publish{Factory: id, DraftRevision: 1, Name: name, Message: "first", Start: true, By: person})
	if err != nil {
		t.Fatal(err)
	}

	w.conversions.AddFactory(w.org, id)

	return v
}

// tree reads version v's files.
func (w *world) tree(t *testing.T, v domain.DefinitionVersion) map[string][]byte {
	t.Helper()

	out := map[string][]byte{}

	for p, h := range v.Manifest {
		b, err := w.defs.Blob(w.ctx(t), h)
		if err != nil {
			t.Fatal(err)
		}

		out[p] = b
	}

	return out
}

func (w *world) record(t *testing.T, id domain.FactoryID) ports.FactoryRecord {
	t.Helper()

	rec, err := w.defs.FactoryRecord(w.ctx(t), id)
	if err != nil {
		t.Fatal(err)
	}

	return rec
}

// The conversion publishes each factory's new version as Goal, creates each connection's
// secrets, reopens an open draft on the new version for its author, converted, refuses a factory
// it cannot convert with its problems and keeps its version, and logs one line per organization.
// A second run changes nothing.
func TestRun_Idempotent(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	v1 := w.publishOld(t, factoryUUID, "acme-software", files(productionFactory))
	refusedV1 := w.publishOld(t, refusedUUID, "acme-cloud",
		files(strings.NewReplacer(factoryUUID, refusedUUID, "acme-software", "acme-cloud", "[github, cloudflare, context7]", "[github, aws-prod]").Replace(productionFactory)))
	d := w.personDraft(t)

	if err := w.mover.Run(t.Context()); err != nil {
		t.Fatal(err)
	}

	v2 := w.checkPublished(t)
	draft := w.checkReopened(t, d, v2)

	if r := w.record(t, refusedUUID); r.Current.ID != refusedV1.ID || r.LastVersionNumber != 1 {
		t.Errorf("refused factory at %+v; want its v1", r.Current)
	}

	w.checkConversions(t, v1, v2)
	w.checkSecrets(t)

	// Again: nothing changes.
	if err := w.mover.Run(t.Context()); err != nil {
		t.Fatal(err)
	}

	if again := w.record(t, factoryUUID); again.LastVersionNumber != 2 || again.Current.ID != v2.ID || again.Draft == nil || again.Draft.ID != draft.ID {
		t.Errorf("after a second run %+v; want v2 and the same draft", again)
	}

	w.checkLogs(t)
}

// personDraft opens a person's draft on the production factory's v1, with a changed prompt and
// the old keys.
func (w *world) personDraft(t *testing.T) domain.DefinitionVersion {
	t.Helper()

	ctx := w.ctx(t)

	d, _, err := w.defs.OpenDraft(ctx, ports.OpenDraft{Factory: factoryUUID, By: person})
	if err != nil {
		t.Fatal(err)
	}

	edited := files(productionFactory)
	edited["agents/engineer.md"] = []byte("# engineer\n\nBuild the change, then test it.\n")

	m, err := w.defs.PutBlobs(ctx, edited)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := w.defs.SaveDraft(ctx, ports.DraftChange{Factory: factoryUUID, ExpectedRevision: d.Revision, Set: m, Replace: true, By: person}); err != nil {
		t.Fatal(err)
	}

	return d
}

// checkPublished checks the production factory's v2, published by Goal with the message and
// environment files, and returns it.
func (w *world) checkPublished(t *testing.T) domain.DefinitionVersion {
	t.Helper()

	rec := w.record(t, factoryUUID)
	if rec.Current == nil || rec.Current.Number != 2 || rec.Current.By.Ref() != moveover.Actor.Ref() {
		t.Fatalf("current %+v; want v2 published by Goal", rec.Current)
	}

	v2, err := w.defs.Version(w.ctx(t), rec.Current.ID)
	if err != nil {
		t.Fatal(err)
	}

	if v2.Message != moveover.Message {
		t.Errorf("v2's message %q; want %q", v2.Message, moveover.Message)
	}

	if _, ok := w.tree(t, v2)[defaultFile]; !ok {
		t.Error("v2 has no environments/default.yaml")
	}

	return v2
}

// checkReopened checks the person's draft d is reopened on v2, still theirs, converted and with
// its change, and returns it.
func (w *world) checkReopened(t *testing.T, d, v2 domain.DefinitionVersion) domain.DefinitionVersion {
	t.Helper()

	draft, err := w.defs.Draft(w.ctx(t), factoryUUID)
	if err != nil {
		t.Fatal(err)
	}

	if draft.BaseID != v2.ID || draft.UpdatedBy.Ref() != person.Ref() {
		t.Errorf("draft base %s by %v; want v2 by %v", draft.BaseID, draft.UpdatedBy, person)
	}

	got := w.tree(t, draft)
	if !strings.Contains(string(got["agents/engineer.md"]), "then test it") || got[defaultFile] == nil ||
		strings.Contains(string(got[config.FileName]), "modelConnection") {
		t.Errorf("the reopened draft lost its change or kept the old keys:\n%s", got[config.FileName])
	}

	if old, err := w.defs.Version(w.ctx(t), d.ID); err != nil || old.ID == draft.ID {
		t.Errorf("the archived draft %s = %+v, %v; want it readable and no longer open", d.ID, old.State, err)
	}

	return draft
}

// checkConversions checks one conversion per factory: v1 to v2 with the names, and the refusal
// naming aws-prod.
func (w *world) checkConversions(t *testing.T, v1, v2 domain.DefinitionVersion) {
	t.Helper()

	convs, err := w.conversions.List(w.ctx(t))
	if err != nil || len(convs) != 2 {
		t.Fatalf("conversions %+v, %v; want 2", convs, err)
	}

	for _, c := range convs {
		switch c.Factory {
		case factoryUUID:
			if c.Refused() || c.From != v1.ID || c.To != v2.ID || !maps.Equal(c.Names[connGitHub], map[string]string{fieldToken: connGitHub}) {
				t.Errorf("conversion %+v; want v1 to v2 with github's names", c)
			}
		case refusedUUID:
			if !c.Refused() || len(c.Problems) != 1 || !strings.HasPrefix(c.Problems[0], "connection aws-prod uses the aws kind") {
				t.Errorf("refusal %+v; want aws-prod's problem", c)
			}
		}
	}
}

// checkSecrets checks the four connections' secrets and none of aws-prod.
func (w *world) checkSecrets(t *testing.T) {
	t.Helper()

	secrets, err := w.secrets.List(w.ctx(t))
	if err != nil {
		t.Fatal(err)
	}

	names := []string{}
	for _, s := range secrets {
		names = append(names, s.Name)
	}

	if strings.Join(names, ",") != "claude,cloudflare,context7,github" {
		t.Errorf("secrets %v; want the four connections' and none of aws-prod", names)
	}
}

// checkLogs checks one moved over line per run, the first counting its work and the second none,
// and no value.
func (w *world) checkLogs(t *testing.T) {
	t.Helper()

	logs := w.logs.String()
	if n := strings.Count(logs, `"msg":"moved over"`); n != 2 {
		t.Errorf("%d moved over lines; want one per run:\n%s", n, logs)
	}

	if !strings.Contains(logs, `"converted":1,"refused":1,"secrets":4`) || !strings.Contains(logs, `"converted":0,"refused":0,"secrets":0`) {
		t.Errorf("the lines do not count the first run's work and the second's none:\n%s", logs)
	}

	if slices.ContainsFunc([]string{githubValue, cfValue, ctx7Value, oauthValue, awsSecret}, func(v string) bool { return strings.Contains(logs, v) }) {
		t.Error("a secret's value reached the logs")
	}
}

// The goal organization's production factory, with github, cloudflare, context7 and the
// claude subscription, converts, passes the publish checks and starts runs on secrets: the
// engine claims an item on the new version and its run gets GH_TOKEN, the cloudflare variables,
// the context7 key the MCP server's header names and CLAUDE_CODE_OAUTH_TOKEN, with no
// connection in the run request.
func TestConvert_GoalProductionFactory(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.publishOld(t, factoryUUID, "acme-software", files(productionFactory))

	if err := w.mover.Run(t.Context()); err != nil {
		t.Fatal(err)
	}

	env := w.oneRun(t, w.convertedLine(t))
	for name, want := range map[string]string{
		envGHToken: githubValue, "CLOUDFLARE_ACCOUNT_ID": accountID, "CLOUDFLARE_API_TOKEN": cfValue,
		envContext7: ctx7Value, domain.EnvClaudeOAuthToken: oauthValue,
	} {
		if env[name] != want {
			t.Errorf("the run's %s = %q; want the converted secret's value", name, env[name])
		}
	}

	for _, action := range []string{domain.AuditConnectionUsed, domain.AuditCredentialUsed} {
		entries, err := w.tenancy.AuditLog(w.ctx(t), ports.AuditQuery{Action: action, Limit: 10})
		if err != nil || len(entries) != 0 {
			t.Errorf("%s entries %v (%v); want none: the run is on secrets", action, entries, err)
		}
	}
}

// convertedLine is the production factory's converted version as the engine runs it, failing
// unless it passes the publish checks, declares the context7 MCP server and is on secrets.
func (w *world) convertedLine(t *testing.T) domain.Line {
	t.Helper()

	v2, err := w.defs.Version(w.ctx(t), w.record(t, factoryUUID).Current.ID)
	if err != nil {
		t.Fatal(err)
	}

	tree := w.tree(t, v2)
	f := factoryOf(t, tree)

	if c7 := environment(t, tree, config.DefaultEnvironment).MCP[connContext7]; c7.URL != context7URL || c7.Headers[envContext7] != refContext7 {
		t.Errorf("default's context7 MCP server %+v; want its URL and key header", c7)
	}

	fs := fstest.MapFS{}
	for p, b := range tree {
		fs[p] = &fstest.MapFile{Data: b}
	}

	if _, err := config.Load(fs); err != nil {
		t.Fatalf("the new version does not load: %v", err)
	}

	line := f.Line()
	line.OrgID, line.VersionID, line.Number = w.org, v2.ID, v2.Number

	if !line.OnSecrets() {
		t.Fatal("the new version is not on secrets")
	}

	return line
}

// oneRun submits an item to line, runs one tick on an engine over w's fakes and returns the env
// of the one agent run it started.
func (w *world) oneRun(t *testing.T, line domain.Line) map[string]string {
	t.Helper()

	logs := &logBuffer{}
	worker := porttest.NewWorker(porttest.Succeed(`{}`))

	e, err := engine.New(engine.Deps{
		Store: w.store, Orgs: w.tenancy, Sandboxes: porttest.NewSandboxes(worker.Exec), Contracts: porttest.NewContracts(), Clock: w.clock,
		Secrets: porttest.Secrets{"GOAL_GATEWAY_KEY": "k3y"}, Connections: w.conns, Credentials: porttest.NewCredentials(w.key, w.tenancy, w.conns),
		OrgSecrets: w.secrets, CodeHosts: map[string]ports.CodeHost{connGitHub: porttest.NewCodeHost()},
		Audit: w.tenancy, NotificationOutbox: porttest.NewNotificationOutbox(), Logger: slog.New(slog.NewJSONHandler(logs, nil)),
	}, engine.Config{
		Lines: []domain.Line{line}, Image: "ghcr.io/acme/goal-agent:test", GatewayURL: "http://gateway.example.com", GatewayKeyRef: "env:GOAL_GATEWAY_KEY",
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := w.ctx(t)

	if _, err := e.Submit(ctx, domain.NewItem{FactoryID: factoryUUID, Type: "feature", Title: "Add the flag"}); err != nil {
		t.Fatal(err)
	}

	if err := e.Tick(ctx); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})

	go func() { e.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the run did not finish")
	}

	runs := worker.Runs()
	if len(runs) != 1 {
		t.Fatalf("%d agent runs; want 1:\n%s", len(runs), logs.String())
	}

	return runs[0].Env
}

var _ io.Writer = (*logBuffer)(nil)

// connWiki is a confluence connection, and the source that reads it.
const connWiki = "wiki"

// docsPairKind syncs with gitdocs and has two secret fields, neither of them the token.
const docsPairKind = `apiVersion: goal/v1
kind: ConnectionKind
metadata: {name: docs-pair, title: Docs pair}
spec:
  fields:
    - {name: key_id, title: Key ID, secret: true, required: true}
    - {name: key_secret, title: Key secret, secret: true, required: true}
  sync: gitdocs
`

// Moving over points each synced source still on a connection at the secret its connection's
// token became, with the reader and the connection's other values in its config; a second run
// changes nothing, and a source it cannot map is logged once, naming it and never a value.
func TestRun_PointsKnowledgeSourcesAtSecrets(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.addSources(t)

	if err := w.mover.Run(t.Context()); err != nil {
		t.Fatal(err)
	}

	want := map[string]struct {
		secret string
		config map[string]string
	}{
		"docs":   {connGitHub, map[string]string{"repository": "https://github.com/acme/docs", domain.SourceReaderKey: "gitdocs", "code_host": "github"}},
		connWiki: {connWiki, map[string]string{"spaces": "ENG", domain.SourceReaderKey: "confluence", "site": "acme.atlassian.net", "email": "ada@example.com"}},
		"paired": {"", nil},
		"notes":  {"", map[string]string{}},
	}

	got := map[string]domain.KnowledgeSource{}

	for name, wt := range want {
		src := w.source(t, name)
		got[name] = src

		if src.Secret != wt.secret || (wt.config != nil && !maps.Equal(src.Config, wt.config)) {
			t.Errorf("source %s = secret %q, config %v; want %q, %v", name, src.Secret, src.Config, wt.secret, wt.config)
		}
	}

	if err := w.mover.Run(t.Context()); err != nil {
		t.Fatal(err)
	}

	for name, before := range got {
		if after := w.source(t, name); !after.UpdatedAt.Equal(before.UpdatedAt) {
			t.Errorf("source %s changed on the second run", name)
		}
	}

	w.unmappedLogged(t)
}

// source is the knowledge source name.
func (w *world) source(t *testing.T, name string) domain.KnowledgeSource {
	t.Helper()

	src, err := w.kb.Source(w.ctx(t), name)
	if err != nil {
		t.Fatal(err)
	}

	return src
}

// unmappedLogged checks that two runs logged paired once each, and no value.
func (w *world) unmappedLogged(t *testing.T) {
	t.Helper()

	logs := w.logs.String()
	if n := strings.Count(logs, `"msg":"knowledge source not moved over"`); n != 2 || !strings.Contains(logs, `"source":"paired"`) {
		t.Errorf("logs hold %d lines for an unmapped source; want one a run, naming paired:\n%s", n, logs)
	}

	for _, v := range []string{githubValue, "confluence-token-for-tests", "pair-id-for-tests", "pair-secret-for-tests"} {
		if strings.Contains(logs, v) {
			t.Errorf("the logs hold a value")
		}
	}
}

// addSources gives w a confluence connection (wiki), one of a kind with two secret fields and no
// token (pair), and four sources: docs on github, wiki on wiki, paired on pair and an upload.
func (w *world) addSources(t *testing.T) {
	t.Helper()

	ctx := w.ctx(t)

	k, err := connkind.Parse([]byte(docsPairKind))
	if err != nil {
		t.Fatal(err)
	}

	if err := w.conns.PutKind(ctx, k, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	for _, n := range []domain.NewConnection{
		{
			Name: connWiki, Kind: "confluence", Fields: map[string]string{"site": "acme.atlassian.net", "email": "ada@example.com"},
			Secrets: map[string]string{fieldToken: "confluence-token-for-tests"},
		},
		{Name: "pair", Kind: "docs-pair", Secrets: map[string]string{"key_id": "pair-id-for-tests", "key_secret": "pair-secret-for-tests"}},
	} {
		if _, err := w.conns.Create(ctx, n, domain.LocalUser); err != nil {
			t.Fatalf("connection %s: %v", n.Name, err)
		}
	}

	for _, n := range []domain.NewKnowledgeSource{
		{Name: "docs", Title: "Docs", Kind: domain.SourceSynced, Connection: connGitHub, Config: map[string]string{"repository": "https://github.com/acme/docs"}},
		{Name: connWiki, Title: "Wiki", Kind: domain.SourceSynced, Connection: connWiki, Config: map[string]string{"spaces": "ENG"}},
		{Name: "paired", Title: "Paired", Kind: domain.SourceSynced, Connection: "pair"},
		{Name: "notes", Title: "Notes", Kind: domain.SourceUpload},
	} {
		if _, err := w.kb.CreateSource(ctx, n, person); err != nil {
			t.Fatalf("source %s: %v", n.Name, err)
		}
	}
}

// A converted version whose repository token can read and push, and whose remote MCP servers
// answer tools/list, is published once, as Goal, after both were tested with the converted
// secrets' values.
func TestRun_PublishesWhenChecksPass(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	v1 := w.publishOld(t, factoryUUID, "acme-software", files(productionFactory))

	if err := w.mover.Run(t.Context()); err != nil {
		t.Fatal(err)
	}

	v2 := w.checkPublished(t)

	if c := w.conversion(t, factoryUUID); c.Refused() || c.From != v1.ID || c.To != v2.ID {
		t.Errorf("conversion %+v; want v1 to v2", c)
	}

	if got := w.hosts.AccessTokens(); !slices.Equal(got, []string{githubValue}) {
		t.Errorf("RepositoryAccess was given %d tokens; want the github secret's value once", len(got))
	}

	called := w.tools.Called()
	if len(called) == 0 || !slices.ContainsFunc(called, func(e ports.MCPEndpoint) bool {
		return e.URL == context7URL && e.Headers[envContext7] == ctx7Value
	}) {
		t.Errorf("tools/list was not asked of context7 with its key's value: %d calls", len(called))
	}
}

// A converted version whose token can read but not push is not published: the factory keeps its
// version, and its refusal names the repository and the missing push with the host's reason,
// scrubbed of the token.
func TestRun_TokenWithoutPushNotPublished(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	v1 := w.publishOld(t, factoryUUID, "acme-software", files(productionFactory))
	w.hosts.SetAccess(repoURL, githubValue, ports.RepositoryAccess{
		Read: ports.AccessCheck{OK: true, Reason: "can read"},
		Push: ports.AccessCheck{Reason: "token " + githubValue + " has\nno write access"},
	})

	if err := w.mover.Run(t.Context()); err != nil {
		t.Fatal(err)
	}

	w.checkKept(t, v1)

	c := w.conversion(t, factoryUUID)
	if !c.Refused() || len(c.Problems) != 1 || !strings.Contains(c.Problems[0], repoURL) || !strings.Contains(c.Problems[0], "push") ||
		!strings.Contains(c.Problems[0], "token [secret] has no write access") {
		t.Errorf("refusal %+v; want one problem naming %s, the push and the host's scrubbed reason", c, repoURL)
	}

	w.checkNoValue(t, c)
}

// A remote MCP server whose tools/list fails keeps the version from publishing; the refusal
// names the environment file, the server and its error, scrubbed of the key.
func TestRun_MCPServerFailureBlocksPublish(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	v1 := w.publishOld(t, factoryUUID, "acme-software", files(productionFactory))
	w.tools.Fail(context7URL, errors.New("401 Unauthorized: bad key "+ctx7Value))

	if err := w.mover.Run(t.Context()); err != nil {
		t.Fatal(err)
	}

	w.checkKept(t, v1)

	c := w.conversion(t, factoryUUID)
	if !c.Refused() || !slices.ContainsFunc(c.Problems, func(p string) bool {
		return strings.Contains(p, defaultFile) && strings.Contains(p, connContext7) && strings.Contains(p, "401 Unauthorized: bad key [secret]")
	}) {
		t.Errorf("refusal %+v; want a problem naming %s, context7 and its scrubbed error", c, defaultFile)
	}

	w.checkNoValue(t, c)
}

// After a refusal, fixing its cause and running again publishes exactly once and replaces the
// refusal with the conversion; a third run changes nothing.
func TestRun_RetriesAfterFix(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	v1 := w.publishOld(t, factoryUUID, "acme-software", files(productionFactory))
	w.hosts.SetAccess(repoURL, githubValue, ports.RepositoryAccess{Read: ports.AccessCheck{OK: true}, Push: ports.AccessCheck{Reason: "no write access"}})

	if err := w.mover.Run(t.Context()); err != nil {
		t.Fatal(err)
	}

	w.checkKept(t, v1)

	w.hosts.SetAccess(repoURL, githubValue, canPush)

	for range 2 {
		if err := w.mover.Run(t.Context()); err != nil {
			t.Fatal(err)
		}
	}

	v2 := w.checkPublished(t)

	if c := w.conversion(t, factoryUUID); c.Refused() || c.To != v2.ID || len(c.Problems) != 0 {
		t.Errorf("conversion %+v; want the refusal replaced by v1 to v2", c)
	}

	if !strings.Contains(w.logs.String(), `"converted":1,"refused":0`) {
		t.Errorf("no run counted the conversion:\n%s", w.logs.String())
	}
}

// conversion is factory id's one conversion.
func (w *world) conversion(t *testing.T, id domain.FactoryID) ports.FactoryConversion {
	t.Helper()

	convs, err := w.conversions.List(w.ctx(t))
	if err != nil {
		t.Fatal(err)
	}

	i := slices.IndexFunc(convs, func(c ports.FactoryConversion) bool { return c.Factory == id })
	if i < 0 {
		t.Fatalf("no conversion of %s in %+v", id, convs)
	}

	return convs[i]
}

// checkKept checks the production factory is still at v1, with no draft of Goal's left.
func (w *world) checkKept(t *testing.T, v1 domain.DefinitionVersion) {
	t.Helper()

	if r := w.record(t, factoryUUID); r.Current == nil || r.Current.ID != v1.ID || r.LastVersionNumber != 1 || r.Draft != nil {
		t.Errorf("factory at %+v (draft %+v); want its v1 and no draft", r.Current, r.Draft)
	}
}

// checkNoValue checks no secret's value is in c's problems or the logs.
func (w *world) checkNoValue(t *testing.T, c ports.FactoryConversion) {
	t.Helper()

	all := strings.Join(c.Problems, "\n") + w.logs.String()

	if slices.ContainsFunc([]string{githubValue, cfValue, ctx7Value, oauthValue}, func(v string) bool { return strings.Contains(all, v) }) {
		t.Error("a secret's value reached a problem or the logs")
	}
}

// approvedOn is the item the seeded standing rules were approved on; ruleAgent is the production
// factory's agent role they let.
const (
	approvedOn = domain.ItemID("0b6f1c9e-0000-7000-8000-0000000000bb")
	ruleAgent  = "engineer"
)

// A converted factory gets a copy of each active connection's rule on every environment whose
// file has a server named after the connection, made by Goal with the rule's item; the rules
// stay, a revoked rule, a connection with no server and a refused factory get no copy, and a
// second run copies nothing.
func TestRun_ReKeysStandingRulesOnce(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.publishOld(t, factoryUUID, "acme-software", files(productionFactory))
	w.publishOld(t, refusedUUID, "acme-cloud",
		files(strings.NewReplacer(factoryUUID, refusedUUID, "acme-software", "acme-cloud", "[github, cloudflare, context7]", "[github, aws-prod]").Replace(productionFactory)))

	ctx := w.ctx(t)
	for _, n := range []domain.NewStandingRule{
		{Connection: connContext7, Tool: "resolve-library-id", Agent: ruleAgent, Item: approvedOn},
		{Connection: connGitHub, Tool: "create_pull_request", Agent: "reviewer", Item: approvedOn},
		{Connection: connCloudflare, Tool: "list_zones", Agent: ruleAgent, Item: approvedOn}, // no server
		{Connection: connGitHub, Tool: "merge_pull_request", Agent: ruleAgent, Item: approvedOn},
	} {
		if _, err := w.conns.CreateStandingRule(ctx, n, person); err != nil {
			t.Fatal(err)
		}
	}

	w.revokeRule(t, "merge_pull_request")

	if err := w.mover.Run(t.Context()); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"default context7 resolve-library-id engineer",
		"default github create_pull_request reviewer",
		"reviewer github create_pull_request reviewer",
	}
	if got := w.serverRules(t); !slices.Equal(got, want) {
		t.Errorf("server rules after the conversion = %v; want %v", got, want)
	}

	if err := w.mover.Run(t.Context()); err != nil {
		t.Fatal(err)
	}

	if got := w.serverRules(t); !slices.Equal(got, want) {
		t.Errorf("server rules after a second run = %v; want %v unchanged", got, want)
	}

	all, err := w.conns.StandingRules(ctx, false)
	if err != nil {
		t.Fatal(err)
	}

	if byConn := slices.DeleteFunc(all, func(r domain.StandingRule) bool { return r.ConnectionID == "" }); len(byConn) != 3 {
		t.Errorf("active connection rules after the conversion = %+v; want the three kept", byConn)
	}
}

// revokeRule revokes the active connection rule for tool.
func (w *world) revokeRule(t *testing.T, tool string) {
	t.Helper()

	rules, err := w.conns.StandingRules(w.ctx(t), false)
	if err != nil {
		t.Fatal(err)
	}

	i := slices.IndexFunc(rules, func(r domain.StandingRule) bool { return r.Tool == tool })
	if i < 0 {
		t.Fatalf("no rule for %s", tool)
	}

	if _, err := w.conns.RevokeStandingRule(w.ctx(t), rules[i].ID, person); err != nil {
		t.Fatal(err)
	}
}

// serverRules are the active rules keyed by server, as "<environment> <server> <tool> <agent>",
// sorted, failing the test unless each is the production factory's, made by Goal on approvedOn.
func (w *world) serverRules(t *testing.T) []string {
	t.Helper()

	rules, err := w.conns.StandingRules(w.ctx(t), false)
	if err != nil {
		t.Fatal(err)
	}

	var out []string

	for _, r := range rules {
		if r.ConnectionID != "" {
			continue
		}

		if r.Factory != factoryUUID || r.CreatedBy.Ref() != moveover.Actor.Ref() || r.ItemID != approvedOn {
			t.Errorf("server rule %+v; want the production factory's, by Goal, on %s", r, approvedOn)
		}

		out = append(out, strings.Join([]string{r.Environment, r.Server, r.Tool, r.Agent}, " "))
	}

	slices.Sort(out)

	return out
}
