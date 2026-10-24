// Command goald is Goal's daemon: it runs every factory in the bucket on one flow engine, serves
// the HTTP API (/v1/) and the MCP gateway agents use (/mcp), and starts agent tasks in Docker.
//
// All wiring happens here, by hand: PostgreSQL (store and item notices), the bucket (factory
// definitions), environment secrets, git workspaces, GitHub (merges), the Docker runner, checker
// and MCP sidecars, the system clock, the engine, the gateway, the API and its stream feed.
// Configuration comes from the environment; settings.go lists every variable. goald keeps nothing
// another instance would need: work state is in PostgreSQL, definitions are in the bucket, and its
// workspaces and run folders are scratch. The exception is the gateway's running MCP sidecars,
// which serve only the runs this instance started (see internal/gateway).
package main

import (
	"context"
	"encoding/json"
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
	"github.com/byFrederick/goal/internal/definitions"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/engine"
	"github.com/byFrederick/goal/internal/factorystore"
	"github.com/byFrederick/goal/internal/gateway"
	"github.com/byFrederick/goal/web"
)

// minKeyBytes is the shortest gateway key goald accepts (openssl rand -hex 32 gives 64).
const minKeyBytes = 32

// shutdownGrace bounds draining HTTP requests at shutdown; agent runs are released to the queue.
const shutdownGrace = 10 * time.Second

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

	key, err := secrets.Resolve(ctx, s.GatewayKeyRef)
	if err != nil {
		return fmt.Errorf("gateway key (%s): %w; generate one with `openssl rand -hex 32`", s.GatewayKeyRef, err)
	}

	if len(key) < minKeyBytes {
		return fmt.Errorf("gateway key (%s) is shorter than %d bytes", s.GatewayKeyRef, minKeyBytes)
	}

	pool, err := openDatabase(ctx, s, log)
	if err != nil {
		return err
	}
	defer pool.Close()

	// One value is the Store, Tenancy and Audit. Each binds its transactions to the organization
	// in ctx: the engine takes it from each line, the API from the request.
	store := postgres.New(pool)

	s3cfg, err := s3store.ConfigFromEnv(os.Getenv)
	if err != nil {
		return err
	}

	objects, err := s3store.New(ctx, s3cfg)
	if err != nil {
		return err
	}

	// Factories stored before organizations move into the default one, which holds the rows
	// from before organizations too.
	moved, err := factorystore.MigrateLegacy(ctx, objects, postgres.DefaultOrgID)
	if err != nil {
		return fmt.Errorf("move legacy factory definitions into the default organization: %w", err)
	}

	log.InfoContext(ctx, "legacy bucket layout migrated", slog.Int("copied", moved.Copied), slog.Int("present", moved.Present),
		slog.Int("gone", moved.Gone), slog.Int("deleted", moved.Deleted))

	dockerClient, err := client.New(client.FromEnv)
	if err != nil {
		return fmt.Errorf("docker: %w", err)
	}
	defer func() { _ = dockerClient.Close() }()

	if _, err := dockerClient.Ping(ctx, client.PingOptions{}); err != nil {
		return fmt.Errorf("docker is not reachable: %w", err)
	}

	// The engine is built after the definitions it reads; the load hook reaches it through eng.
	var eng *engine.Engine

	defs := definitions.New(factorystore.New(objects), store, func(ctx context.Context, fs []definitions.Loaded) error {
		return load(ctx, store, eng, fs)
	}, log)

	runnerCfg := docker.Config{RunDir: s.RunDir}
	workspaces := gitworkspace.New(secrets, gitworkspace.Config{Root: s.WorkspaceRoot}, log)
	runner := docker.New(dockerClient, defs, runnerCfg, log)

	gw, err := gateway.New(gateway.Deps{
		Definitions: defs, Secrets: secrets, Events: store, Clock: clock, Logger: log,
		Sidecars: docker.NewSidecars(dockerClient, docker.Config{}, log),
	}, gateway.Config{Key: []byte(key)})
	if err != nil {
		return err
	}
	// Runs after serve returns: HTTP is drained and the engine's runs have ended.
	defer gw.Close()

	codehost := github.New(secrets, github.Config{APIURL: s.GitHubAPIURL})

	eng, err = engine.New(engine.Deps{
		Store: store, Orgs: store, Runner: runner, Workspaces: workspaces, Checker: docker.NewChecker(dockerClient, runnerCfg, log),
		Contracts: defs, Secrets: secrets, CodeHost: codehost, Clock: clock, Logger: log, RunEnded: gw.EndRun,
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

	reap(ctx, log, runner, workspaces)

	feed := api.NewFeed(postgres.NewNotices(pool.Config().ConnConfig, log), log)

	apiHandler, err := api.New(api.Deps{
		Engine: eng, Items: store, Factories: defs, Feed: feed, Clock: clock, LegacyOrg: postgres.DefaultOrgID, Logger: log,
	})
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.Handle("/v1/", apiHandler)
	mux.Handle("/mcp", gw)
	mux.Handle("/", web.Handler())

	srv := &http.Server{Addr: s.Listen, Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	return serve(ctx, log, s, srv, eng, defs, feed)
}

// serve runs the HTTP server, the engine, the stream feed and the definitions refresh until ctx
// ends, then shuts them down: the feed ends every stream, HTTP drains, then the engine releases
// the items its runs held.
func serve(ctx context.Context, log *slog.Logger, s settings, srv *http.Server, eng *engine.Engine, defs *definitions.Cache,
	feed *api.Feed,
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

	wg.Go(func() {
		log.InfoContext(ctx, "goald listening", slog.String("addr", s.Listen), slog.String("gateway", s.GatewayURL))

		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fail(fmt.Errorf("http: %w", err))
		}
	})

	wg.Go(func() {
		if err := eng.Run(ctx); err != nil {
			fail(fmt.Errorf("engine: %w", err))
		}
	})

	wg.Go(func() { refresh(ctx, log, defs, s.RefreshEvery) })

	wg.Go(func() {
		if err := feed.Run(ctx); err != nil {
			fail(fmt.Errorf("stream feed: %w", err))
		}
	})

	<-ctx.Done()
	log.Info("goald shutting down")

	shutdownCtx, done := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)
	defer done()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Warn("http shutdown", slog.Any("error", err))
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

// refresh re-reads factory definitions from the bucket until ctx ends.
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

// load registers every loaded factory in the store, in its organization (items reference their
// factory row), and hands their lines to the engine.
func load(ctx context.Context, store *postgres.Store, eng *engine.Engine, fs []definitions.Loaded) error {
	lines := make([]domain.Line, 0, len(fs))

	for _, l := range fs {
		f := l.Factory

		def, err := json.Marshal(f)
		if err != nil {
			return fmt.Errorf("factory %s: %w", f.Metadata.ID, err)
		}

		err = store.SaveFactory(domain.WithOrg(ctx, l.Org), postgres.Factory{
			ID: domain.FactoryID(f.Metadata.ID), Name: f.Metadata.Name, Title: f.Metadata.Title, Definition: def,
		})
		if err != nil {
			return err
		}

		lines = append(lines, l.Line())
	}

	return eng.SetLines(lines)
}

// reap removes containers, run folders and workspaces left by an earlier goald that died
// mid-run. On a laptop one goald owns the local Docker, so at startup no run here is live; the
// items those runs held return to their queue when their leases expire.
func reap(ctx context.Context, log *slog.Logger, runner *docker.Runner, workspaces *gitworkspace.Workspaces) {
	none := func(domain.RunID) bool { return false }

	if n, err := runner.Reap(ctx, none); err != nil || n > 0 {
		log.InfoContext(ctx, "reaped orphaned agent runs", slog.Int("runs", n), slog.Any("error", err))
	}

	if n, err := workspaces.Reap(none); err != nil || n > 0 {
		log.InfoContext(ctx, "reaped orphaned workspaces", slog.Int("workspaces", n), slog.Any("error", err))
	}
}
