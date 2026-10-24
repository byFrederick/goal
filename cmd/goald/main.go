// Command goald is Goal's daemon: it runs the current definition version of every factory on one
// flow engine, serves the HTTP API (/v1/) and the MCP gateway agents use (/mcp, on a listener of
// its own with GOAL_GATEWAY_LISTEN), and starts agent tasks in Docker.
//
// All wiring happens here, by hand: PostgreSQL (store, definition versions, item and definition
// notices), the bucket (definition file content), environment secrets, git workspaces, GitHub
// (merges), the Docker runner, checker and MCP sidecars, the system clock, the engine, the gateway,
// definition authoring, the API and its stream feed. Configuration comes from the environment;
// settings.go lists every variable. goald keeps nothing another instance would need: work state
// and definition versions are in PostgreSQL, file content is in the bucket, and its workspaces
// and run folders are scratch, reaped by any instance once their run is over. The exception is
// the gateway's running MCP sidecars, which serve only the runs this instance started (see
// internal/gateway).
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
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moby/moby/client"

	"github.com/byFrederick/goal/internal/adapters/docker"
	"github.com/byFrederick/goal/internal/adapters/envsecrets"
	"github.com/byFrederick/goal/internal/adapters/github"
	"github.com/byFrederick/goal/internal/adapters/gitworkspace"
	"github.com/byFrederick/goal/internal/adapters/postgres"
	"github.com/byFrederick/goal/internal/adapters/s3store"
	"github.com/byFrederick/goal/internal/adapters/sysclock"
	"github.com/byFrederick/goal/internal/api"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/builder"
	"github.com/byFrederick/goal/internal/definitions"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/engine"
	"github.com/byFrederick/goal/internal/factorystore"
	"github.com/byFrederick/goal/internal/gateway"
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

	s3cfg, err := s3store.ConfigFromEnv(os.Getenv)
	if err != nil {
		return err
	}

	objects, err := s3store.New(ctx, s3cfg)
	if err != nil {
		return err
	}

	definitionStore := postgres.NewDefinitions(pool, objects, log)

	// Definition authoring writes versions to PostgreSQL and file content to the bucket; until
	// phase 4 a definition's secret references are checked against goald's environment. The
	// legacy import validates through it too.
	authors := authoring.New(authoring.Deps{
		Store: definitionStore, Loads: store, Templates: objects,
		Credentials: s.credentials(secrets),
		Clock:       clock, DefaultImage: s.DefaultAgentImage,
	})

	if err := importLegacy(ctx, s, objects, definitionStore, store, publishProblems(authors), log); err != nil {
		return err
	}

	dockerClient, egress, err := containers(ctx, s, log)
	if err != nil {
		return err
	}
	defer func() { _ = dockerClient.Close() }()

	// The engine is built after the definitions it reads; the load hook reaches it through eng.
	var eng *engine.Engine

	defs := definitions.New(definitionStore, store, func(_ context.Context, lines []domain.Line) error {
		return eng.SetLines(lines)
	}, definitions.Config{CacheBytes: s.DefinitionCacheBytes}, log)

	// One rule for env:NAME references wherever goald resolves one: clones, the code host, the gateway.
	policy := s.secretPolicy()
	runnerCfg := docker.Config{RunDir: s.RunDir}
	workspaces := gitworkspace.New(secrets, gitworkspace.Config{Root: s.WorkspaceRoot, Hosts: []string{s.GitHubWebHost}, Policy: policy}, log)

	runner := docker.New(dockerClient, defs, egress, runnerCfg, log)

	gw, err := gateway.New(gateway.Deps{
		Definitions: defs, Secrets: secrets, Events: store, Clock: clock, Logger: log,
		Sidecars: docker.NewSidecars(dockerClient, egress, docker.Config{}, log),
	}, gateway.Config{Key: []byte(key), SecretPolicy: policy})
	if err != nil {
		return err
	}
	// Runs after serve returns: HTTP is drained and the engine's runs have ended.
	defer gw.Close()

	codehost := github.New(secrets, github.Config{APIURL: s.GitHubAPIURL, WebHost: s.GitHubWebHost, Policy: policy})

	eng, err = engine.New(engine.Deps{
		Store: store, Orgs: store, Runner: runner, Workspaces: workspaces, Checker: docker.NewChecker(dockerClient, egress, runnerCfg, log),
		Contracts: defs, Secrets: secrets, CodeHost: codehost, Clock: clock, Logger: log, RunEnded: gw.EndRun,
		// The engine reaps what runs that are over left in Docker and on disk, whichever goald on
		// this host started them: at its start and every ReapEvery.
		Reapers: []ports.Reaper{runner, workspaces},
	}, engine.Config{
		TickEvery: s.TickEvery, GatewayURL: s.GatewayURL, GatewayKeyRef: s.GatewayKeyRef, TaskEnv: s.TaskEnv, MaxRuns: s.MaxRuns,
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
		Clock: clock, Logger: log,
	})
	if err != nil {
		return err
	}

	servers := httpServers(s, proxySecret, apiHandler, gw, web.Handler(web.Options{StrictTransport: s.strictTransport()}), log)

	// Deleted organizations go, rows and bucket objects, once their retention has passed.
	purger := retention.New(store, objects, clock, s.OrgRetention, log)

	return serve(ctx, log, s, servers, eng, defs, definitionNotices, feed, jobs{purger: purger, platform: platform, clock: clock})
}

// containers connects to Docker and prepares the egress proxy that agents, networked sidecars and
// checks reach the outside through (ADR 0009); the proxy itself starts with the first container
// that needs it. A missing proxy image is a warning: goald serves everything else without it.
func containers(ctx context.Context, s settings, log *slog.Logger) (*client.Client, *docker.Egress, error) {
	dockerClient, err := client.New(client.FromEnv)
	if err != nil {
		return nil, nil, fmt.Errorf("docker: %w", err)
	}

	if _, err := dockerClient.Ping(ctx, client.PingOptions{}); err != nil {
		_ = dockerClient.Close()
		return nil, nil, fmt.Errorf("docker is not reachable: %w", err)
	}

	egress, err := docker.NewEgress(dockerClient, s.Egress, log)
	if err != nil {
		_ = dockerClient.Close()
		return nil, nil, err
	}

	if err := egress.CheckImage(ctx); err != nil {
		log.WarnContext(ctx, "agent runs, networked sidecars and checks cannot start until the egress proxy's image is built", slog.Any("error", err))
	}

	return dockerClient, egress, nil
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
// the gateway has no listener of its own, the agents' gateway at /mcp. With one (separate), the
// gateway listener serves only /mcp (everything else 404) and the main one answers /mcp with 404,
// so the gateway address agents are given reaches nothing else; gatewayOnly is nil without one.
func routes(separate bool, apiHandler, gw, spa http.Handler) (primary, gatewayOnly http.Handler) {
	mux := http.NewServeMux()
	mux.Handle("/v1/", apiHandler)
	mux.Handle("/", spa)

	if !separate {
		mux.Handle(api.GatewayPath, gw)

		return mux, nil
	}

	mux.Handle(api.GatewayPath, http.NotFoundHandler())

	only := http.NewServeMux()
	only.Handle(api.GatewayPath, gw)

	return mux, only
}

// jobs are goald's scheduled jobs: the purge of deleted organizations and the sign-in clean-up.
type jobs struct {
	purger   *retention.Purger
	platform ports.Platform
	clock    ports.Clock
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

// importLegacy brings the bucket's older layouts up to date before anything reads definitions,
// for the organizations whose legacy import is pending (those from before versions, until it
// ends): factories stored before organizations move into the default one (which holds the rows
// from before organizations too), then every factory with no version yet becomes version 1 of
// itself, or a draft when validate finds a blocking problem (GOAL_LEGACY_IMPORT). Once an
// organization's import ran it is never read again, so the step is a no-op on every later start.
func importLegacy(ctx context.Context, s settings, objects ports.ObjectStore, defs ports.DefinitionStore, orgs definitions.Orgs,
	validate definitions.LegacyValidator, log *slog.Logger,
) error {
	pending, err := defs.LegacyImportPending(domain.WithOrg(ctx, postgres.DefaultOrgID))
	if err != nil {
		return fmt.Errorf("legacy import of the default organization: %w", err)
	}

	if pending {
		moved, err := factorystore.MigrateLegacy(ctx, objects, postgres.DefaultOrgID)
		if err != nil {
			return fmt.Errorf("move legacy factory definitions into the default organization: %w", err)
		}

		log.InfoContext(ctx, "legacy bucket layout migrated", slog.Int("copied", moved.Copied), slog.Int("present", moved.Present),
			slog.Int("gone", moved.Gone), slog.Int("deleted", moved.Deleted))
	}

	if !s.LegacyImport {
		return nil
	}

	res, err := definitions.ImportLegacy(ctx, definitions.Legacy{
		Catalog: factorystore.New(objects), Store: defs, Orgs: orgs, Validate: validate, Log: log,
	})
	if err != nil {
		return fmt.Errorf("import legacy factory definitions: %w", err)
	}

	log.InfoContext(ctx, "legacy definitions imported", slog.Int("factories", res.Factories), slog.Int("versions", res.Published),
		slog.Int("drafts", res.Drafts), slog.Int("present", res.Present), slog.Int("problems", res.Problems))

	return nil
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

// publishProblems is the legacy import's validation: authoring's, as a publish of the files
// would run it.
func publishProblems(authors *authoring.Service) definitions.LegacyValidator {
	return func(ctx context.Context, files map[string][]byte) ([]domain.DefinitionProblem, error) {
		v, err := authors.ValidateFiles(ctx, files)
		if err != nil {
			return nil, err
		}

		return v.Problems, nil
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

// authUnreachableAfter bounds goald's startup look at the identity service.
const authUnreachableAfter = 2 * time.Second

// authentication starts the auth mode (spec 3 §8.3). Under GOAL_AUTH=none it warns that every
// request is the local user. Under session it refuses to start unless the database passes the
// identity self-check (postgres.CheckIdentity), returns the per-request identity lookups, and
// warns, without stopping, when the identity service does not answer (signed-in people keep
// working) or no one has signed in yet.
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

	if signedIn, err := postgres.AnyoneSignedIn(ctx, store); err != nil {
		return nil, "", err
	} else if !signedIn {
		log.WarnContext(ctx, "no one can sign in yet: set GOAL_BOOTSTRAP_OWNER_EMAIL and BETTER_AUTH_SECRET and start the "+
			"auth service (goal up does), or set GOAL_AUTH=none")
	}

	if err := authReachable(ctx, s.AuthUpstream.String()); err != nil {
		log.WarnContext(ctx, "the identity service does not answer: signing in and out waits for it; signed-in people keep working",
			slog.String("upstream", s.AuthUpstream.String()), slog.Any("error", err))
	}

	return postgres.NewIdentity(store), proxySecret, nil
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
