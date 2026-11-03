package moveover_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"maps"
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

const refusedUUID = "0b6f1c9e-5d0a-4c39-9a51-6f1f3f0e2a78"

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
	defs        *porttest.Definitions
	conversions *porttest.FactoryConversions
	mover       *moveover.Mover
	logs        *logBuffer
}

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
	w.defs = porttest.NewDefinitions(porttest.DefinitionsDeps{Objects: porttest.NewObjectStore(), Items: w.store, Audit: w.tenancy, Now: w.clock.Now})
	w.conversions = porttest.NewFactoryConversions()

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
		{Name: "github", Kind: "github", Secrets: map[string]string{"token": githubValue}},
		{Name: "cloudflare", Kind: "cloudflare", Fields: map[string]string{"account_id": "acme-account"}, Secrets: map[string]string{"api_token": cfValue}},
		{Name: "context7", Kind: "context7", Secrets: map[string]string{"key": ctx7Value}},
		{Name: "claude", Kind: "claude-subscription", Secrets: map[string]string{"token": oauthValue}},
		{Name: "aws-prod", Kind: "aws", Secrets: map[string]string{"access_key_id": "AKIAEXAMPLE", "secret_access_key": "aws-secret-for-tests"}},
	} {
		if _, err := w.conns.Create(ctx, n, domain.LocalUser); err != nil {
			t.Fatalf("connection %s: %v", n.Name, err)
		}
	}

	if _, err := w.conns.Update(ctx, "context7", domain.ConnectionPatch{
		ToolModes: map[string]domain.ConnectionToolMode{"resolve-library-id": domain.ToolModeAsk},
	}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	authors := authoring.New(authoring.Deps{
		Store: w.defs, Templates: porttest.NewObjectStore(), Credentials: authoring.SecretsCheck{Secrets: w.secrets}, Clock: w.clock,
	})

	w.mover = moveover.New(moveover.Deps{
		Orgs: w.tenancy, Connections: w.conns, Secrets: w.secrets, Definitions: w.defs, Publisher: authors,
		Conversions: w.conversions, Clock: w.clock, Logger: slog.New(slog.NewJSONHandler(w.logs, nil)),
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
	ctx := w.ctx(t)

	v1 := w.publishOld(t, factoryUUID, "acme-software", files(productionFactory))
	refusedV1 := w.publishOld(t, refusedUUID, "acme-cloud",
		files(strings.NewReplacer(factoryUUID, refusedUUID, "acme-software", "acme-cloud", "[github, cloudflare, context7]", "[github, aws-prod]").Replace(productionFactory)))

	// A person's draft on v1, with a changed prompt and the old keys.
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

	if err := w.mover.Run(t.Context()); err != nil {
		t.Fatal(err)
	}

	rec := w.record(t, factoryUUID)
	if rec.Current == nil || rec.Current.Number != 2 || rec.Current.By.Ref() != moveover.Actor.Ref() {
		t.Fatalf("current %+v; want v2 published by Goal", rec.Current)
	}

	v2, err := w.defs.Version(ctx, rec.Current.ID)
	if err != nil {
		t.Fatal(err)
	}

	if v2.Message != moveover.Message {
		t.Errorf("v2's message %q; want %q", v2.Message, moveover.Message)
	}

	if _, ok := w.tree(t, v2)["environments/default.yaml"]; !ok {
		t.Error("v2 has no environments/default.yaml")
	}

	// The draft is on v2, still its author's, converted and with its change.
	draft, err := w.defs.Draft(ctx, factoryUUID)
	if err != nil {
		t.Fatal(err)
	}

	got := w.tree(t, draft)
	if draft.BaseID != v2.ID || draft.UpdatedBy.Ref() != person.Ref() {
		t.Errorf("draft base %s by %v; want v2 by %v", draft.BaseID, draft.UpdatedBy, person)
	}

	if !strings.Contains(string(got["agents/engineer.md"]), "then test it") || got["environments/default.yaml"] == nil ||
		strings.Contains(string(got[config.FileName]), "modelConnection") {
		t.Errorf("the reopened draft lost its change or kept the old keys:\n%s", got[config.FileName])
	}

	if old, err := w.defs.Version(ctx, d.ID); err != nil || old.State == domain.VersionDraft && old.ID == draft.ID {
		t.Errorf("the archived draft %s = %+v, %v; want it readable and no longer open", d.ID, old.State, err)
	}

	// The refused factory keeps v1.
	if r := w.record(t, refusedUUID); r.Current.ID != refusedV1.ID || r.LastVersionNumber != 1 {
		t.Errorf("refused factory at %+v; want its v1", r.Current)
	}

	convs, err := w.conversions.List(ctx)
	if err != nil {
		t.Fatal(err)
	}

	want := map[domain.FactoryID]bool{factoryUUID: false, refusedUUID: true}
	for _, c := range convs {
		if c.Refused() != want[c.Factory] {
			t.Errorf("conversion of %s refused %v; want %v (%v)", c.Factory, c.Refused(), want[c.Factory], c.Problems)
		}

		if c.Factory == factoryUUID && (c.From != v1.ID || c.To != v2.ID || !maps.Equal(c.Names["github"], map[string]string{"token": "github"})) {
			t.Errorf("conversion %+v; want v1 to v2 with github's names", c)
		}

		if c.Factory == refusedUUID && (len(c.Problems) != 1 || !strings.HasPrefix(c.Problems[0], "connection aws-prod uses the aws kind")) {
			t.Errorf("refusal problems %q; want aws-prod's", c.Problems)
		}
	}

	if len(convs) != 2 {
		t.Errorf("%d conversions; want 2", len(convs))
	}

	secrets, err := w.secrets.List(ctx)
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

	// Again: nothing changes.
	if err := w.mover.Run(t.Context()); err != nil {
		t.Fatal(err)
	}

	if again := w.record(t, factoryUUID); again.LastVersionNumber != 2 || again.Current.ID != v2.ID || again.Draft == nil || again.Draft.ID != draft.ID {
		t.Errorf("after a second run %+v; want v2 and the same draft", again)
	}

	logs := w.logs.String()
	if n := strings.Count(logs, `"msg":"moved over"`); n != 2 {
		t.Errorf("%d moved over lines; want one per run:\n%s", n, logs)
	}

	if !strings.Contains(logs, `"converted":1,"refused":1,"secrets":4`) || !strings.Contains(logs, `"converted":0,"refused":0,"secrets":0`) {
		t.Errorf("the lines do not count the first run's work and the second's none:\n%s", logs)
	}

	for _, v := range []string{githubValue, cfValue, ctx7Value, oauthValue, "aws-secret-for-tests"} {
		if strings.Contains(logs, v) {
			t.Errorf("a secret's value reached the logs")
		}
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

	ctx := w.ctx(t)

	v2, err := w.defs.Version(ctx, w.record(t, factoryUUID).Current.ID)
	if err != nil {
		t.Fatal(err)
	}

	fs := fstest.MapFS{}
	for p, b := range w.tree(t, v2) {
		fs[p] = &fstest.MapFile{Data: b}
	}

	f := factoryOf(t, w.tree(t, v2))

	if c7 := environment(t, w.tree(t, v2), "default").MCP["context7"]; c7.URL != "https://mcp.context7.com/mcp" || c7.Headers["CONTEXT7_API_KEY"] != "{{context7}}" {
		t.Errorf("default's context7 MCP server %+v; want its URL and key header", c7)
	}

	if _, err := config.Load(fs); err != nil {
		t.Fatalf("the new version does not load: %v", err)
	}

	line := f.Line()
	line.OrgID, line.VersionID, line.Number = w.org, v2.ID, v2.Number

	if !line.OnSecrets() {
		t.Fatal("the new version is not on secrets")
	}

	logs := &logBuffer{}
	worker := porttest.NewWorker(porttest.Succeed(`{}`))

	e, err := engine.New(engine.Deps{
		Store: w.store, Orgs: w.tenancy, Sandboxes: porttest.NewSandboxes(worker.Exec), Contracts: porttest.NewContracts(), Clock: w.clock,
		Secrets: porttest.Secrets{"GOAL_GATEWAY_KEY": "k3y"}, Connections: w.conns, Credentials: porttest.NewCredentials(w.key, w.tenancy, w.conns),
		OrgSecrets: w.secrets, CodeHosts: map[string]ports.CodeHost{"github": porttest.NewCodeHost()},
		Audit: w.tenancy, NotificationOutbox: porttest.NewNotificationOutbox(), Logger: slog.New(slog.NewJSONHandler(logs, nil)),
	}, engine.Config{
		Lines: []domain.Line{line}, Image: "ghcr.io/acme/goal-agent:test", GatewayURL: "http://gateway.example.com", GatewayKeyRef: "env:GOAL_GATEWAY_KEY",
	})
	if err != nil {
		t.Fatal(err)
	}

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

	env := runs[0].Env
	for name, want := range map[string]string{
		"GH_TOKEN": githubValue, "CLOUDFLARE_ACCOUNT_ID": "acme-account", "CLOUDFLARE_API_TOKEN": cfValue,
		"CONTEXT7_API_KEY": ctx7Value, domain.EnvClaudeOAuthToken: oauthValue,
	} {
		if env[name] != want {
			t.Errorf("the run's %s = %q; want the converted secret's value", name, env[name])
		}
	}

	for _, action := range []string{domain.AuditConnectionUsed, domain.AuditCredentialUsed} {
		entries, err := w.tenancy.AuditLog(ctx, ports.AuditQuery{Action: action, Limit: 10})
		if err != nil || len(entries) != 0 {
			t.Errorf("%s entries %v (%v); want none: the run is on secrets", action, entries, err)
		}
	}
}

var _ io.Writer = (*logBuffer)(nil)
