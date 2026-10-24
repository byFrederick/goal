package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/adapters/postgres"
)

// settings is goald's configuration, read from the environment (12-factor). Secrets appear here
// only as references (env:NAME), resolved through the Secrets port.
type settings struct {
	// MigrateDatabaseURL connects as goal_owner, which owns the schema, to apply migrations at
	// startup (GOAL_MIGRATE_DATABASE_URL). DatabaseURL connects as goal_app for everything else
	// (GOAL_DATABASE_URL); goald refuses a role that bypasses row-level security. Both default to
	// the compose.yaml database with GOAL_PG_OWNER_PASSWORD and GOAL_PG_APP_PASSWORD, the
	// passwords `goal db init` gives the roles.
	MigrateDatabaseURL string
	DatabaseURL        string
	// Listen is the API and gateway address (GOAL_LISTEN, default 127.0.0.1:8080). Phase 1 has no
	// API authentication, so it must be a loopback address.
	Listen string
	// GatewayURL is the MCP gateway as agent containers reach it (GOAL_GATEWAY_URL, default
	// http://<listen>/mcp; the Docker runner rewrites loopback to host.docker.internal).
	GatewayURL string
	// GatewayKeyRef names the secret that signs task tokens (GOAL_GATEWAY_KEY_REF, default
	// env:GOAL_GATEWAY_KEY).
	GatewayKeyRef string
	// TaskEnv maps variables of every agent task to secret references (GOAL_TASK_ENV,
	// NAME=ref pairs separated by commas; default CLAUDE_CODE_OAUTH_TOKEN=env:CLAUDE_CODE_OAUTH_TOKEN).
	TaskEnv map[string]string
	// WorkspaceRoot holds the runs' git checkouts (GOAL_WORKSPACE_ROOT, default
	// $TMPDIR/goal-workspaces); RunDir the runs' agent files (GOAL_RUN_DIR, default
	// $TMPDIR/goal-runs). Docker must be able to bind-mount both.
	WorkspaceRoot string
	RunDir        string
	// TickEvery is how often the engine pulls work (GOAL_TICK_EVERY, default 5s); RefreshEvery how
	// often factory definitions are re-read from the bucket (GOAL_REFRESH_EVERY, default 30s).
	TickEvery    time.Duration
	RefreshEvery time.Duration
	// GitHubAPIURL is the GitHub REST API goald merges pull requests through
	// (GOAL_GITHUB_API_URL, default https://api.github.com; http only on a loopback address).
	GitHubAPIURL string
	// LogLevel (GOAL_LOG_LEVEL: debug, info, warn, error) and LogFormat (GOAL_LOG_FORMAT: json or
	// text) shape the structured log on stdout.
	LogLevel  slog.Level
	LogFormat string
}

const (
	defaultListen  = "127.0.0.1:8080"
	defaultTaskEnv = "CLAUDE_CODE_OAUTH_TOKEN=env:CLAUDE_CODE_OAUTH_TOKEN"
	defaultKeyRef  = "env:GOAL_GATEWAY_KEY"
)

// forbiddenTaskEnv never reaches a task: on a laptop the model is reached with the subscription
// token, and an API key in a container would silently take over billing.
var forbiddenTaskEnv = []string{"ANTHROPIC_API_KEY", "GOAL_GATEWAY_KEY", "GOAL_GITHUB_TOKEN"}

func settingsFrom(getenv func(string) string) (settings, error) {
	or := func(name, def string) string {
		if v := strings.TrimSpace(getenv(name)); v != "" {
			return v
		}

		return def
	}

	s := settings{
		MigrateDatabaseURL: or("GOAL_MIGRATE_DATABASE_URL",
			localDatabase(postgres.OwnerRole, or("GOAL_PG_OWNER_PASSWORD", postgres.DevOwnerPassword))),
		DatabaseURL: or("GOAL_DATABASE_URL",
			localDatabase(postgres.AppRole, or("GOAL_PG_APP_PASSWORD", postgres.DevAppPassword))),
		Listen:        or("GOAL_LISTEN", defaultListen),
		GatewayKeyRef: or("GOAL_GATEWAY_KEY_REF", defaultKeyRef),
		WorkspaceRoot: or("GOAL_WORKSPACE_ROOT", filepath.Join(os.TempDir(), "goal-workspaces")),
		RunDir:        or("GOAL_RUN_DIR", filepath.Join(os.TempDir(), "goal-runs")),
		LogFormat:     or("GOAL_LOG_FORMAT", "json"),
		GitHubAPIURL:  or("GOAL_GITHUB_API_URL", "https://api.github.com"),
	}

	var errs []error

	if err := loopback(s.Listen); err != nil {
		errs = append(errs, err)
	}

	s.GatewayURL = or("GOAL_GATEWAY_URL", "http://"+s.Listen+"/mcp")

	var err error
	if s.TaskEnv, err = taskEnv(or("GOAL_TASK_ENV", defaultTaskEnv)); err != nil {
		errs = append(errs, err)
	}

	if s.TickEvery, err = duration(or("GOAL_TICK_EVERY", "5s")); err != nil {
		errs = append(errs, fmt.Errorf("GOAL_TICK_EVERY: %w", err))
	}

	if s.RefreshEvery, err = duration(or("GOAL_REFRESH_EVERY", "30s")); err != nil {
		errs = append(errs, fmt.Errorf("GOAL_REFRESH_EVERY: %w", err))
	}

	if err := s.LogLevel.UnmarshalText([]byte(or("GOAL_LOG_LEVEL", "info"))); err != nil {
		errs = append(errs, fmt.Errorf("GOAL_LOG_LEVEL: %w", err))
	}

	if err := apiURL(s.GitHubAPIURL); err != nil {
		errs = append(errs, err)
	}

	if s.LogFormat != "json" && s.LogFormat != "text" {
		errs = append(errs, fmt.Errorf("GOAL_LOG_FORMAT %q is not json or text", s.LogFormat))
	}

	return s, errors.Join(errs...)
}

// localDatabase is compose.yaml's database as role.
func localDatabase(role, password string) string {
	return (&url.URL{
		Scheme: "postgres", User: url.UserPassword(role, password),
		Host: "127.0.0.1:5432", Path: "goal", RawQuery: "sslmode=disable",
	}).String()
}

// loopback refuses listen addresses other machines could reach: the phase 1 API has no
// authentication.
func loopback(listen string) error {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("GOAL_LISTEN %q: %w", listen, err)
	}

	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("GOAL_LISTEN %q: the API has no authentication yet, so goald listens on a loopback address only", listen)
	}

	return nil
}

// apiURL refuses a GitHub API that a repository token would reach in clear text, except on
// this machine (a stand-in API for tests).
func apiURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("GOAL_GITHUB_API_URL %q is not a URL", raw)
	}

	if u.Scheme == "https" {
		return nil
	}

	if ip := net.ParseIP(u.Hostname()); u.Scheme != "http" || (u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback())) {
		return fmt.Errorf("GOAL_GITHUB_API_URL %q must be https (http only on a loopback address)", raw)
	}

	return nil
}

// taskEnv parses NAME=ref pairs. Values must be references (env:NAME), never secret literals,
// and some names may never reach a task.
func taskEnv(raw string) (map[string]string, error) {
	out := map[string]string{}

	for pair := range strings.SplitSeq(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}

		name, ref, ok := strings.Cut(pair, "=")
		if !ok || name == "" || !strings.HasPrefix(ref, "env:") {
			return nil, fmt.Errorf("GOAL_TASK_ENV: %q is not NAME=env:REF", name)
		}

		for _, f := range forbiddenTaskEnv {
			if strings.EqualFold(name, f) {
				return nil, fmt.Errorf("GOAL_TASK_ENV: %s must never reach an agent task", name)
			}
		}

		out[name] = ref
	}

	return out, nil
}

func duration(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err == nil && d <= 0 {
		err = fmt.Errorf("%s is not positive", s)
	}

	return d, err
}
