package main

import (
	"cmp"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/adapters/postgres"
	"github.com/byFrederick/goal/internal/builder"
	"github.com/byFrederick/goal/internal/definitions"
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
	// often the current definition versions are reconciled with PostgreSQL (GOAL_REFRESH_EVERY,
	// default 30s), a safety net behind the goal_definitions notices that reload them at once.
	TickEvery    time.Duration
	RefreshEvery time.Duration
	// DefinitionCacheBytes bounds the files of the definition versions kept for runs that started
	// on a version no longer current (GOAL_DEFINITION_CACHE_BYTES, default 64 MiB).
	DefinitionCacheBytes int64
	// LegacyImport imports, at startup, the factories of the bucket layout before versions that
	// have no version yet (GOAL_LEGACY_IMPORT, default true).
	LegacyImport bool
	// MaxRuns caps the agent runs this goald has in flight at once (GOAL_MAX_RUNS, default 0:
	// no cap). With a cap, claims are shared out fairly across organizations.
	MaxRuns int
	// OrgRetention is how long a deleted organization is kept before it is purged with its rows
	// and bucket objects (GOAL_ORG_RETENTION, default 7d; a Go duration or a number of days).
	OrgRetention time.Duration
	// GitHubAPIURL is the GitHub REST API goald merges pull requests through
	// (GOAL_GITHUB_API_URL, default https://api.github.com; http only on a loopback address).
	GitHubAPIURL string
	// Models are the models the factory editor offers (GOAL_MODELS: comma-separated model ids,
	// each optionally id=Label; the first is the default; default defaultModels).
	Models []builder.Model
	// LogLevel (GOAL_LOG_LEVEL: debug, info, warn, error) and LogFormat (GOAL_LOG_FORMAT: json or
	// text) shape the structured log on stdout.
	LogLevel  slog.Level
	LogFormat string
}

const (
	// modelsEnv lists the editor's models; defaultModels are the current Claude models, the most
	// capable first.
	modelsEnv      = "GOAL_MODELS"
	defaultModels  = "claude-opus-5-5,claude-sonnet-5-5,claude-haiku-5-5"
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

	if s.OrgRetention, err = duration(or("GOAL_ORG_RETENTION", "7d")); err != nil {
		errs = append(errs, fmt.Errorf("GOAL_ORG_RETENTION: %w", err))
	}

	errs = append(errs, definitionSettings(&s, or)...)

	if s.MaxRuns, err = strconv.Atoi(or("GOAL_MAX_RUNS", "0")); err != nil || s.MaxRuns < 0 {
		errs = append(errs, fmt.Errorf("GOAL_MAX_RUNS %q is not a count of runs (0 for no cap)", getenv("GOAL_MAX_RUNS")))
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

	// errors.Join drops a nil error.
	s.Models, err = models(or(modelsEnv, defaultModels))

	return s, errors.Join(append(errs, err)...)
}

// definitionSettings reads the settings of the definitions cache and the legacy import.
func definitionSettings(s *settings, or func(name, def string) string) []error {
	var (
		errs []error
		err  error
	)

	cacheBytes := or("GOAL_DEFINITION_CACHE_BYTES", strconv.Itoa(definitions.DefaultCacheBytes))
	if s.DefinitionCacheBytes, err = strconv.ParseInt(cacheBytes, 10, 64); err != nil || s.DefinitionCacheBytes <= 0 {
		errs = append(errs, fmt.Errorf("GOAL_DEFINITION_CACHE_BYTES %q is not a positive number of bytes", cacheBytes))
	}

	legacy := or("GOAL_LEGACY_IMPORT", "true")
	if s.LegacyImport, err = strconv.ParseBool(legacy); err != nil {
		errs = append(errs, fmt.Errorf("GOAL_LEGACY_IMPORT %q is not true or false", legacy))
	}

	return errs
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

// duration parses a positive Go duration, or a whole number of days such as 7d.
func duration(s string) (time.Duration, error) {
	if days, ok := strings.CutSuffix(s, "d"); ok {
		if n, err := strconv.Atoi(days); err == nil {
			s = strconv.Itoa(n*24) + "h"
		}
	}

	d, err := time.ParseDuration(s)
	if err == nil && d <= 0 {
		err = fmt.Errorf("%s is not positive", s)
	}

	return d, err
}

// modelID is the shape of runtime.model and agents[].model in schema/factory.schema.json.
var modelID = regexp.MustCompile(`^claude-[a-z0-9-]+$`)

// models parses GOAL_MODELS: comma-separated model ids, each optionally followed by =Label. A
// model without a label gets one from its id (claude-opus-5-5 is Claude Opus 5.5).
func models(raw string) ([]builder.Model, error) {
	var out []builder.Model

	for item := range strings.SplitSeq(raw, ",") {
		id, label, _ := strings.Cut(strings.TrimSpace(item), "=")
		id, label = strings.TrimSpace(id), strings.TrimSpace(label)

		if !modelID.MatchString(id) {
			return nil, fmt.Errorf("GOAL_MODELS: %q is not a model id (claude-...)", id)
		}

		if slices.ContainsFunc(out, func(m builder.Model) bool { return m.ID == id }) {
			return nil, fmt.Errorf("GOAL_MODELS: %s is listed twice", id)
		}

		out = append(out, builder.Model{ID: id, Label: cmp.Or(label, modelLabel(id))})
	}

	return out, nil
}

// modelLabel names a model from its id: words capitalized, version digits joined by dots.
func modelLabel(id string) string {
	var words, version []string

	for part := range strings.SplitSeq(id, "-") {
		if part == "" {
			continue
		}

		if strings.Trim(part, "0123456789") == "" {
			version = append(version, part)
			continue
		}

		words = append(words, strings.ToUpper(part[:1])+part[1:])
	}

	if len(version) > 0 {
		words = append(words, strings.Join(version, "."))
	}

	return strings.Join(words, " ")
}
