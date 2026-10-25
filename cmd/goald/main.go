// Command goald is Goal's daemon: it runs the current definition version of every factory on one
// flow engine, serves the HTTP API (/v1/) and the gateway agents use (/mcp and /run, on a listener of
// its own with GOAL_GATEWAY_LISTEN), and runs every agent run in a sandbox of its own in Docker.
//
// All wiring happens here, by hand: PostgreSQL (store, definition versions, item and definition
// notices, connections), the bucket (definition file content), environment secrets, GitHub and
// GitLab (merges), Docker's sandboxes, the system clock, the engine, the gateway, definition
// authoring, the API and its stream feed. Configuration comes from the environment; settings.go
// lists every variable. goald keeps nothing another instance would need: work state, connections
// and definition versions are in PostgreSQL, file content is in the bucket, and its runs'
// sandboxes are scratch, reaped by any instance once their run is over.
package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moby/moby/client"

	"github.com/byFrederick/goal/internal/adapters/awssts"
	"github.com/byFrederick/goal/internal/adapters/confluence"
	"github.com/byFrederick/goal/internal/adapters/docker"
	"github.com/byFrederick/goal/internal/adapters/envsecrets"
	"github.com/byFrederick/goal/internal/adapters/gitdocs"
	"github.com/byFrederick/goal/internal/adapters/masterkey"
	"github.com/byFrederick/goal/internal/adapters/openaiembed"
	"github.com/byFrederick/goal/internal/adapters/postgres"
	"github.com/byFrederick/goal/internal/adapters/s3store"
	"github.com/byFrederick/goal/internal/adapters/sysclock"
	"github.com/byFrederick/goal/internal/api"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/builder"
	"github.com/byFrederick/goal/internal/connkind"
	"github.com/byFrederick/goal/internal/definitions"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/egress"
	"github.com/byFrederick/goal/internal/engine"
	"github.com/byFrederick/goal/internal/gateway"
	"github.com/byFrederick/goal/internal/knowledge"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/retention"
	"github.com/byFrederick/goal/web"
)

// minKeyBytes is the shortest gateway key goald accepts (openssl rand -hex 32 gives 64).
const minKeyBytes = 32

// shutdownGrace bounds draining HTTP requests at shutdown; agent runs are released to the queue.
const shutdownGrace = 10 * time.Second

// purgeEvery is how often goald looks for deleted organizations whose retention has passed.
const purgeEvery = time.Hour

func main() { os.Exit(goald()) }

// goald runs the daemon and returns its exit code: 2 for bad configuration, 1 for a failure.
func goald() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	s, err := settingsFrom(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "goald:", err)
		return 2
	}

	log := newLogger(s)

	for _, r := range s.Ignored {
		log.Warn("setting ignored: it was removed (spec 4 §5.13); remove it from the environment",
			slog.String("variable", r.name), slog.String("instead", r.instead))
	}

	if err := run(ctx, s, log); err != nil {
		log.Error("goald stopped", slog.Any("error", err))
		return 1
	}

	log.Info("goald stopped")

	return 0
}

func newLogger(s settings) *slog.Logger {
	opts := &slog.HandlerOptions{Level: s.LogLevel}
	if s.LogFormat == "text" {
		return slog.New(slog.NewTextHandler(os.Stdout, opts))
	}

	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}

func run(ctx context.Context, s settings, log *slog.Logger) error {
	secrets := envsecrets.New(os.LookupEnv)
	clock := sysclock.Clock{}

	key, err := gatewayKey(ctx, s, secrets)
	if err != nil {
		return err
	}

	pool, err := openDatabase(ctx, s, log)
	if err != nil {
		return err
	}
	defer pool.Close()

	// One value is the Store, Tenancy and Audit. Each binds its transactions to the organization
	// in ctx: the engine takes it from each line, the API from the request.
	store := postgres.New(pool)

	identity, proxySecret, err := authentication(ctx, s, store, secrets, log)
	if err != nil {
		return err
	}

	// Connections and their kinds (spec 4): definitions are validated against them, the API serves
	// them, runs get them and the gateway describes them in run bundles.
	connections, err := openConnections(ctx, store, os.Getenv, log)
	if err != nil {
		return err
	}

	s3cfg, err := s3store.ConfigFromEnv(os.Getenv)
	if err != nil {
		return err
	}

	objects, err := s3store.New(ctx, s3cfg)
	if err != nil {
		return err
	}

	definitionStore := postgres.NewDefinitions(pool, objects, log)
	kb := openKnowledge(s, store, connections, objects, clock, log)

	// Definition authoring writes versions to PostgreSQL and file content to the bucket. A
	// definition's connections and knowledge sources are checked against the organization's.
	authors := authoring.New(authoring.Deps{
		Store: definitionStore, Loads: store, Templates: objects,
		Credentials: authoring.ConnectionsCheck{Connections: connections, Knowledge: kb.searcher},
		Clock:       clock,
	})

	dockerClient, err := containers(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = dockerClient.Close() }()

	// The engine is built after the definitions it reads; the load hook reaches it through eng.
	var eng *engine.Engine

	defs := definitions.New(definitionStore, store, func(_ context.Context, lines []domain.Line) error {
		return eng.SetLines(lines)
	}, definitions.Config{CacheBytes: s.DefinitionCacheBytes}, log)

	// Boxes reach goald's gateway through the egress proxy, at the gateway's paths.
	boxGateway, err := docker.BoxGatewayURL(s.GatewayURL)
	if err != nil {
		return err
	}

	gw, err := gateway.New(gateway.Deps{
		Definitions: defs, Items: store, Connections: connections, Events: store, Clock: clock, Logger: log,
		Knowledge: kb.searcher,
	}, gateway.Config{Key: []byte(key)})
	if err != nil {
		return err
	}

	// The engine runs each claim in a sandbox with the run's connections, merges with the
	// factory's code host connections and, at its start and every ReapEvery, deletes the
	// sandboxes of runs that are over, whichever goald on this Docker host started them.
	eng, err = engine.New(engine.Deps{
		Store: store, Orgs: store, Sandboxes: docker.NewSandboxes(dockerClient, s.SandboxNetwork, log),
		Contracts: defs, Secrets: secrets, Connections: connections, CodeHosts: codeHosts(s.AllowPrivateUpstreams), Clock: clock,
		Minters: minters(s), Logger: log,
	}, engine.Config{
		TickEvery: s.TickEvery, Image: s.AgentImage, GatewayURL: boxGateway, GatewayKeyRef: s.GatewayKeyRef, MaxRuns: s.MaxRuns,
	})
	if err != nil {
		return err
	}

	loaded, err := defs.Refresh(ctx)
	if err != nil {
		return err
	}

	log.InfoContext(ctx, "factories loaded", slog.Int("count", len(loaded)), slog.String("bucket", objects.Bucket()))

	definitionNotices := postgres.NewDefinitionNotices(store, log)

	feed := newFeed(s, store, eng, log)

	// Phase 4 sets the capabilities in its wiring PRs; until then the editor keeps their controls hidden.
	editor := builder.New(objects, clock, builder.Config{Models: s.Models, Capabilities: builder.Capabilities{}, Logger: log})

	// Platform admins, the security log and the sign-in clean-up: install-level, in no organization.
	platform := postgres.NewPlatform(store)

	apiHandler, err := api.New(api.Deps{
		Engine: eng, Items: store, Factories: defs, Definitions: authors, Versions: definitionStore, Orgs: store, Audit: store, OrgRetention: s.OrgRetention,
		Authorizer: authorizer(s.Auth, clock), Auth: api.AuthConfig{Mode: api.AuthMode(s.Auth), Google: s.Google, SecureCookies: s.strictTransport()},
		Identity: identity, Access: postgres.NewAccess(store), Platform: platform, TrustedProxies: s.TrustedProxies, Feed: feed, Builder: editor,
		Connections: connections, ParseKind: connkind.Parse, ConnectionTransport: connectionTransport(s.AllowPrivateUpstreams),
		Knowledge: kb.kb, KnowledgeUploads: kb.uploads, KnowledgeSearch: kb.searcher, UploadMaxBytes: s.UploadMaxBytes,
		Clock: clock, Logger: log,
	})
	if err != nil {
		return err
	}

	servers := httpServers(s, proxySecret, apiHandler, gatewayHandler(gw), web.Handler(web.Options{StrictTransport: s.strictTransport()}), log)

	// Deleted organizations go, rows and bucket objects, once their retention has passed.
	purger := retention.New(store, objects, clock, s.OrgRetention, log)

	return serve(ctx, log, s, servers, eng, defs, definitionNotices, feed,
		jobs{purger: purger, platform: platform, clock: clock, identity: newIdentityWatch(s, store), ingestor: kb.ingestor})
}

// knowledgeBase is the knowledge base as goald wires it (spec 4 §4.8): its sources and documents
// in PostgreSQL, uploads in the bucket, the search the API and the gateway's tools run, and the
// ingestion loop.
type knowledgeBase struct {
	kb       *postgres.Knowledge
	uploads  *knowledge.Uploads
	searcher *knowledge.Searcher
	ingestor *knowledge.Ingestor
}

// openKnowledge wires the knowledge base: uploads of at most GOAL_UPLOAD_MAX_BYTES each, read
// from the bucket; embeddings from GOAL_EMBED_URL's model or, without one, keyword search only;
// GOAL_KB_WORKERS workers syncing due sources. A synced source is read by its connection kind's
// sync reader through the public-only transport: gitdocs (GitHub and GitLab repositories) or
// confluence (the spaces of a Confluence Cloud site).
func openKnowledge(s settings, store *postgres.Store, connections *postgres.Connections, objects ports.ObjectStore,
	clock ports.Clock, log *slog.Logger,
) knowledgeBase {
	kb := postgres.NewKnowledge(store)

	var embedder ports.Embedder

	if s.Embed.URL == "" {
		log.Info("knowledge base: keyword search only; set " + embedURLEnv + " (and " + embedModelEnv + ", " + embedKeyEnv +
			") for an embedding model")
	} else {
		cfg := s.Embed
		cfg.Transport = connectionTransport(s.AllowPrivateUpstreams)

		client, err := openaiembed.New(cfg) // settingsFrom checked it
		if err != nil {
			log.Error("knowledge base: keyword search only", slog.Any("error", err))
		} else {
			embedder = client
			log.Info("knowledge base: hybrid search", slog.String("embed_model", client.Model()))
		}
	}

	readers := map[string]ports.SourceReader{
		"gitdocs":    gitdocs.New(gitdocs.Config{Transport: connectionTransport(s.AllowPrivateUpstreams)}),
		"confluence": confluence.New(confluence.Config{Transport: connectionTransport(s.AllowPrivateUpstreams)}),
	}
	uploads := knowledge.NewUploads(kb, objects, s.UploadMaxBytes)

	return knowledgeBase{
		kb: kb, uploads: uploads, searcher: knowledge.NewSearcher(kb, embedder, log),
		ingestor: knowledge.NewIngestor(knowledge.IngestorConfig{
			Knowledge: kb, Orgs: store, Connections: connections, Uploads: uploads,
			Readers: readers, Embedder: embedder, Clock: clock, Log: log, Workers: s.KBWorkers,
		}),
	}
}

// openConnections loads the platform's master keys (GOAL_MASTER_KEYS or GOAL_MASTER_KEY_FILE),
// refuses to start while a connection is sealed under a key id they lack (spec 4 §5.2, threat row
// 12), and makes the kinds goald ships the platform's. Without a master key goald serves
// everything but connections with secrets, and says so.
func openConnections(ctx context.Context, store *postgres.Store, getenv func(string) string, log *slog.Logger) (*postgres.Connections, error) {
	keys, err := masterkey.Load(getenv)
	if err != nil {
		return nil, err
	}

	sealed, err := postgres.ConnectionKeyIDs(ctx, store)
	if err != nil {
		return nil, err
	}

	have := keys.IDs()

	if missing := slices.DeleteFunc(sealed, func(id string) bool { return slices.Contains(have, id) }); len(missing) > 0 {
		return nil, fmt.Errorf("connections are sealed under master key %s, which %s and %s do not hold: "+
			"give goald every key a connection is sealed under (goal db rekey reseals them under the active one, "+
			"after which an old key can go)", strings.Join(missing, ", "), masterkey.KeysEnv, masterkey.FileEnv)
	}

	if len(have) == 0 {
		log.WarnContext(ctx, "no master key: connections with secrets are unavailable until one is set; add "+
			masterkey.KeysEnv+"=k1:<the output of openssl rand -base64 32> to .env and keep a copy offline")
	} else {
		log.InfoContext(ctx, "master keys loaded", slog.String("active", keys.Active()), slog.Int("count", len(have)))
	}

	kinds, err := connkind.Platform()
	if err != nil {
		return nil, err
	}

	connections := postgres.NewConnections(store, keys)
	if err := connections.SyncPlatformKinds(ctx, kinds); err != nil {
		return nil, err
	}

	log.InfoContext(ctx, "connection kinds synced", slog.Int("platform", len(kinds)))

	return connections, nil
}

// minters are the minters by the name connection kinds give them (mint): AWS STS for the aws and
// bedrock kinds, over the connection transport.
func minters(s settings) map[string]ports.Minter {
	return map[string]ports.Minter{
		"aws-sts": awssts.New(awssts.Config{HTTPClient: &http.Client{Transport: connectionTransport(s.AllowPrivateUpstreams)}}),
	}
}

// connectionTransport carries every request goald makes for a connection (its test, STS, merges,
// sync and embeddings): to public addresses only (egress.PublicDialer, threat row 4), unless
// GOAL_ALLOW_PRIVATE_UPSTREAMS lets a laptop reach its own test servers. It uses no proxy, so a
// proxy variable cannot route around the address check.
func connectionTransport(allowPrivate bool) *http.Transport {
	return &http.Transport{
		DialContext: egress.PublicDialer{AllowPrivate: allowPrivate}.DialContext, ForceAttemptHTTP2: true,
		TLSHandshakeTimeout: egress.DialTimeout, MaxIdleConns: 16, IdleConnTimeout: 90 * time.Second,
	}
}

// containers connects to Docker. The agents' network and its egress proxy are compose.yaml's
// (ADR 0009 and its phase 4 amendment); a container that would run without them says so.
func containers(ctx context.Context) (*client.Client, error) {
	dockerClient, err := client.New(client.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("docker: %w", err)
	}

	if _, err := dockerClient.Ping(ctx, client.PingOptions{}); err != nil {
		_ = dockerClient.Close()
		return nil, fmt.Errorf("docker is not reachable: %w", err)
	}

	return dockerClient, nil
}

// httpServers are goald's HTTP servers: the main listener and, with GOAL_GATEWAY_LISTEN, the
// gateway's own. Under GOAL_AUTH=session the main one also serves the identity service's
// endpoints (/api/auth/*, api.AuthProxy). Every request to the main one but the health check and
// the agents' gateway must name goald (DNS rebinding); the gateway's serves nothing that a Host
// could matter to.
func httpServers(s settings, proxySecret string, apiHandler, gw, spa http.Handler, log *slog.Logger) []*http.Server {
	primary, gatewayOnly := routes(s.GatewayListen != "", apiHandler, gw, spa)
	if s.Auth == authSession {
		primary = api.AuthProxy(api.AuthProxyConfig{Upstream: s.AuthUpstream, TrustedProxies: s.TrustedProxies, Secret: proxySecret, Logger: log}, primary)
	}

	servers := []*http.Server{httpServer(s.Listen, api.CheckHost(s.hosts(), primary))}
	if gatewayOnly != nil {
		servers = append(servers, httpServer(s.GatewayListen, gatewayOnly))
	}

	return servers
}

// routes are the handlers of goald's listeners: the main one serves the API, the SPA and, when
// the gateway has no listener of its own, the agents' gateway at /mcp and /run (gw serves both).
// With one (separate), the gateway listener serves only those (everything else 404) and the main
// one answers them with 404, so the gateway address agents are given reaches nothing else;
// gatewayOnly is nil without one.
func routes(separate bool, apiHandler, gw, spa http.Handler) (primary, gatewayOnly http.Handler) {
	mux := http.NewServeMux()
	mux.Handle("/v1/", apiHandler)
	mux.Handle("/", spa)

	gatewayPaths := []string{api.GatewayPath, api.RunPath}

	if !separate {
		for _, p := range gatewayPaths {
			mux.Handle(p, gw)
		}

		return mux, nil
	}

	only := http.NewServeMux()

	for _, p := range gatewayPaths {
		mux.Handle(p, http.NotFoundHandler())
		only.Handle(p, gw)
	}

	return mux, only
}

// gatewayHandler serves the gateway's paths: the MCP endpoint and the run bundle.
func gatewayHandler(gw *gateway.Gateway) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(api.GatewayPath, gw)
	mux.HandleFunc(api.RunPath, gw.ServeRun)

	return mux
}

// jobs are goald's scheduled jobs: the purge of deleted organizations, the sign-in clean-up, the
// knowledge base's ingestion and under GOAL_AUTH=session the startup watch of the identity service.
type jobs struct {
	purger   *retention.Purger
	platform ports.Platform
	clock    ports.Clock
	identity *identityWatch
	ingestor *knowledge.Ingestor
}

// serve runs the HTTP servers, the engine, the stream feed, the definitions reloads and the
// scheduled jobs until ctx ends, then shuts them down: the feed ends every stream, HTTP drains,
// then the engine releases the items its runs held.
func serve(ctx context.Context, log *slog.Logger, s settings, servers []*http.Server, eng *engine.Engine, defs *definitions.Cache,
	notices ports.DefinitionNotices, feed *api.Feed, j jobs,
) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg      sync.WaitGroup
		srvErr  error
		errOnce sync.Once
	)

	fail := func(err error) {
		errOnce.Do(func() { srvErr = err })
		cancel()
	}

	log.InfoContext(ctx, "goald listening", slog.String("addr", s.Listen), slog.String("gateway_listen", cmp.Or(s.GatewayListen, s.Listen)),
		slog.String("gateway", s.GatewayURL))

	for _, srv := range servers {
		wg.Go(func() {
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				fail(fmt.Errorf("http %s: %w", srv.Addr, err))
			}
		})
	}

	wg.Go(func() {
		if err := eng.Run(ctx); err != nil {
			fail(fmt.Errorf("engine: %w", err))
		}
	})

	wg.Go(func() {
		if err := defs.Listen(ctx, notices); err != nil {
			fail(fmt.Errorf("definition notices: %w", err))
		}
	})

	wg.Go(func() { refresh(ctx, log, defs, s.RefreshEvery) })
	wg.Go(func() { j.purger.Run(ctx, purgeEvery) })
	wg.Go(func() { cleanUpAuth(ctx, log, j.platform, j.clock, authCleanUpEvery) })

	if s.KBWorkers > 0 {
		wg.Go(func() { j.ingestor.Run(ctx) })
	}

	if j.identity != nil {
		wg.Go(func() { j.identity.run(ctx, log) })
	}

	wg.Go(func() {
		if err := feed.Run(ctx); err != nil {
			fail(fmt.Errorf("stream feed: %w", err))
		}
	})

	<-ctx.Done()
	log.Info("goald shutting down")

	shutdownCtx, done := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)
	defer done()

	for _, srv := range servers {
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Warn("http shutdown", slog.String("addr", srv.Addr), slog.Any("error", err))
		}
	}

	wg.Wait()

	return srvErr
}

// openDatabase applies pending migrations as goal_owner on a pool of its own, closed before goald
// serves anything, then opens goald's pool as goal_app and refuses a role that row-level security
// would not bind. The caller closes the pool.
func openDatabase(ctx context.Context, s settings, log *slog.Logger) (*pgxpool.Pool, error) {
	owner, err := postgres.Open(ctx, s.MigrateDatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("GOAL_MIGRATE_DATABASE_URL: %w (goal db init creates the roles)", err)
	}

	err = postgres.Migrate(ctx, owner)
	owner.Close()

	if err != nil {
		return nil, err
	}

	pool, err := postgres.Open(ctx, s.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("GOAL_DATABASE_URL: %w", err)
	}

	if err := postgres.CheckAppRole(ctx, pool); err != nil {
		pool.Close()

		return nil, fmt.Errorf("GOAL_DATABASE_URL: %w", err)
	}

	log.InfoContext(ctx, "database ready", slog.String("role_check", "passed"))

	return pool, nil
}

// HTTP server bounds: a client has readHeaderTimeout to send its headers and readTimeout for the
// whole request (a definition tree is at most 24 MiB), and an idle keep-alive connection is
// closed after idleTimeout. There is no write timeout: the realtime stream is one response that
// lasts as long as its client listens.
const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 2 * time.Minute
	idleTimeout       = 2 * time.Minute
)

// httpServer is one of goald's HTTP servers, on addr.
func httpServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr: addr, Handler: h, ReadHeaderTimeout: readHeaderTimeout, ReadTimeout: readTimeout, IdleTimeout: idleTimeout,
	}
}

// refresh reconciles the current definition versions with PostgreSQL until ctx ends, behind the
// notices that reload them at once.
func refresh(ctx context.Context, log *slog.Logger, defs *definitions.Cache, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}

		if _, err := defs.Refresh(ctx); err != nil && ctx.Err() == nil {
			log.WarnContext(ctx, "refresh factory definitions", slog.Any("error", err))
		}
	}
}

// authorizer is the API's Authorizer for the auth mode: AllowAll, everything allowed, only under
// GOAL_AUTH=none (a loopback listen, one local user); under session the permission table
// (api.Permissions), dating sessions by clock for the fresh rule. Any other mode has none, which
// api.New refuses: goald fails closed.
func authorizer(mode authMode, clock ports.Clock) api.Authorizer {
	switch mode {
	case authNone:
		return api.AllowAll{}
	case authSession:
		return api.NewPermissions(clock)
	default:
		return nil
	}
}

// gatewayKey is the secret that signs task tokens: at least minKeyBytes long.
func gatewayKey(ctx context.Context, s settings, secrets ports.Secrets) (string, error) {
	key, err := secrets.Resolve(ctx, s.GatewayKeyRef)
	if err != nil {
		return "", fmt.Errorf("gateway key (%s): %w; generate one with `openssl rand -hex 32`", s.GatewayKeyRef, err)
	}

	if len(key) < minKeyBytes {
		return "", fmt.Errorf("gateway key (%s) is shorter than %d bytes", s.GatewayKeyRef, minKeyBytes)
	}

	return key, nil
}

// newFeed is the stream feed. The engine hears every item notice, so a person's move, pause or
// send-back of an item this instance runs stops its run at once; under GOAL_AUTH=session a stream
// ends when its person's access changes, and is checked again when it reconnects.
func newFeed(s settings, store *postgres.Store, eng *engine.Engine, log *slog.Logger) *api.Feed {
	feed := api.NewFeed(postgres.NewNotices(store, log), postgres.NewDefinitionNotices(store, log), log, eng.Interrupt)
	if s.Auth == authSession {
		feed.EndOnAccessChanges(postgres.NewAccessNotices(store, log))
	}

	return feed
}

// The startup look at the identity service: each health check has authUnreachableAfter, goald
// asks every authCheckEvery and, while it does not answer, waits authStartGrace (goal up starts it
// beside goald) before it warns.
const (
	authUnreachableAfter = 2 * time.Second
	authCheckEvery       = 500 * time.Millisecond
	authStartGrace       = 30 * time.Second
)

// authentication starts the auth mode (spec 3 §8.3). Under GOAL_AUTH=none it warns that every
// request is the local user. Under session it refuses to start unless the database passes the
// identity self-check (postgres.CheckIdentity) and returns the per-request identity lookups;
// whether the identity service answers, and whether anyone has signed in, watchIdentity says
// while goald serves.
func authentication(ctx context.Context, s settings, store *postgres.Store, secrets ports.Secrets, log *slog.Logger) (api.Identity, string, error) {
	log.InfoContext(ctx, "authentication", slog.String("mode", string(s.Auth)), slog.String("public_url", s.PublicURL))

	if s.Auth == authNone {
		log.WarnContext(ctx, "GOAL_AUTH=none: sign-in is off and every request to "+s.Listen+" acts as the local user, "+
			"agent containers included; one developer's laptop only")

		return nil, "", nil
	}

	proxySecret, err := secrets.Resolve(ctx, authProxySecretRef)
	if err != nil || len(proxySecret) < minKeyBytes {
		return nil, "", fmt.Errorf("GOAL_AUTH=session needs %s, at least %d bytes: the secret goald sends the identity service with "+
			"every request it forwards, which refuses any other (goal up generates one for both; when you start them yourself, "+
			"give both the same: openssl rand -hex 32)", authProxySecretEnv, minKeyBytes)
	}

	if err := postgres.CheckIdentity(ctx, store); err != nil {
		return nil, "", fmt.Errorf("GOAL_AUTH=session: %w (goald migrates the database at start; check the roles with goal db init)", err)
	}

	log.InfoContext(ctx, "identity self-check passed")

	return postgres.NewIdentity(store), proxySecret, nil
}

// identityWatch is what watchIdentity asks: the identity service's health check at upstream, and
// whether anyone has signed in to goald at publicURL.
type identityWatch struct {
	upstream, publicURL string
	answers             func(context.Context) error
	signedIn            func(context.Context) (bool, error)
}

// newIdentityWatch watches the identity service of s, under GOAL_AUTH=session only (nil else).
func newIdentityWatch(s settings, store *postgres.Store) *identityWatch {
	if s.Auth != authSession {
		return nil
	}

	upstream := s.AuthUpstream.String()

	return &identityWatch{
		upstream: upstream, publicURL: s.PublicURL,
		answers:  func(ctx context.Context) error { return authReachable(ctx, upstream) },
		signedIn: func(ctx context.Context) (bool, error) { return postgres.AnyoneSignedIn(ctx, store) },
	}
}

// run follows the identity service from goald's start until it answers or ctx ends, without
// stopping goald (signed-in people keep working through an auth outage): while it does not answer
// it says goald waits for it, and warns once authStartGrace has passed; once it answers it says
// so and, while nobody has signed in yet, who signs in first.
func (w identityWatch) run(ctx context.Context, log *slog.Logger) {
	began := time.Now()
	upstream := slog.String("upstream", w.upstream)
	waiting, warned := false, false

	for err := w.answers(ctx); err != nil; err = w.answers(ctx) {
		switch waited := time.Since(began); {
		case !waiting:
			waiting = true

			log.InfoContext(ctx, "waiting for the identity service to answer: signing in and out waits for it", upstream)
		case waited >= authStartGrace && !warned:
			warned = true

			log.WarnContext(ctx, "the identity service does not answer: signing in and out waits for it; signed-in people keep working",
				upstream, slog.Duration("waited", waited.Round(time.Second)), slog.Any("error", err))
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(authCheckEvery):
		}
	}

	log.InfoContext(ctx, "identity service answers", upstream)

	signedIn, err := w.signedIn(ctx)
	if err != nil {
		log.WarnContext(ctx, "could not tell whether anyone has signed in", slog.Any("error", err))

		return
	}

	if !signedIn {
		log.InfoContext(ctx, "nobody has signed in yet: the first owner signs in at "+w.publicURL+" with the identity service's "+
			"GOAL_BOOTSTRAP_OWNER_EMAIL, which goal up requires (without it nobody can sign in; or set GOAL_AUTH=none)")
	}
}

// authReachable asks the identity service's health check (GET /api/auth/ok).
func authReachable(ctx context.Context, upstream string) error {
	ctx, cancel := context.WithTimeout(ctx, authUnreachableAfter)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstream+"/api/auth/ok", http.NoBody)
	if err != nil {
		return err
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	_ = res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("GET /api/auth/ok: %s", res.Status)
	}

	return nil
}
