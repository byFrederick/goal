package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"github.com/byFrederick/goal/internal/core/domain"
)

// errNoOrgSecrets fails a run on secrets on a goald built without them.
var errNoOrgSecrets = errors.New("this goald has no secrets store")

// runEnv is what one run's commands get from its version's secrets (ADR 0037), each exec only
// what it needs: prepare the repository tokens, the agent the model key and its environment, a
// check nothing.
type runEnv struct {
	// prepare is goal-agent prepare's: the repository token and the entry tokens (secretEnv).
	prepare map[string]string
	// repositoryLogin is whether prepare got the repository token, which signs git in to the
	// repository's host for the agent's push too.
	repositoryLogin bool
	// agent is goal-agent run's: the model key and the environment's env.
	agent map[string]string
	// secrets are every secret value the run's commands get, scrubbed from what they report.
	secrets []string
	// files are the paths of the environment's files with the variable of agent each one's
	// content is in (domain.FileEnv), and mcp its MCP servers rendered to name GOAL_SECRET_<NAME>
	// variables of agent instead of values (domain.MCPServer.Rendered); the run bundle carries
	// them to the worker.
	files map[string]string
	mcp   map[string]domain.MCPServer
}

// runEnv opens what req's run gets from its version's secrets (secretEnv): of its runs a
// station's opens the repository token and the entry tokens its code picks, and a check's the
// entry tokens alone, since it has no repository; a Jonah review clones nothing. A request with no
// model key is of a version on connections, which no run reads any more (ADR 0037 decision 19):
// it fails with domain.ErrNotOnSecrets, opening no connection or credential.
func (e *Engine) runEnv(ctx context.Context, req domain.RunRequest, run domain.RunID) (runEnv, error) {
	if req.ModelKey == "" {
		return runEnv{}, fmt.Errorf("version %s: %w", req.Version, domain.ErrNotOnSecrets)
	}

	repo, code := req.Repository, req.Code
	if req.Kind == domain.RunKindJonah {
		repo, code = domain.Repository{}, domain.RunCode{}
	}

	return e.secretEnv(ctx, domain.SecretUse{Factory: req.FactoryID, Agent: req.Agent, Run: run}, req.ModelKey, req.Environment, repo, code)
}

// pushNote tells the agent whether git can push to the repository's host: with the repository
// token, or not at all.
func (env runEnv) pushNote(host string) string {
	if env.repositoryLogin {
		return "Git signs in to " + host + " with the repository token."
	}

	return "It holds no git credentials for " + host + "."
}

// secretEnv resolves a run of a version on secrets (ADR 0037 decisions 7 and 10): the {{secret}}
// references of modelKey and of the environment's env, files, MCP headers and stdio servers' env
// are opened together, once, through OrgSecrets.Resolve, which checks each scope against use's
// factory, audits each as secret.used and sets its last use. The agent gets env's variables with
// the values filled in, whole or part of a value, each file's content in its GOAL_FILE_<n>
// variable (domain.FileEnv), each secret an MCP server names in its GOAL_SECRET_<NAME> variable
// (domain.SecretEnv), and the model key's value as domain.ModelKeyEnv names it. The MCP servers
// are kept rendered (domain.MCPServer.Rendered), naming those variables and never a value. Every
// value opened joins the run's secrets, so it is scrubbed. A failure names the secret and the
// environment, never a value. env is zero for a run that gets the model key alone (a Jonah run, a
// definition run), and has no env, files or MCP servers with Use credentials off.
//
// repo's token, when it has a URL and one, is opened in the same call and goes to prepare alone,
// as domain.RepositoryTokenEnv: it signs git in for the clone, the base branch's push and, from
// the login prepare keeps, the agent's own push, but it is not in the agent's env unless env names
// the secret too. When code clones a repository, the token the agent's code picks for the first
// (domain.RunClone.Token) takes repo's place. repo is zero for a run that clones nothing.
//
// Each entry token code's logins pick is opened in the same call too, once however many logins
// pick it, and goes to prepare alone in its entry's domain.RepositoryEntryTokenEnv variable, for
// the credential helper per path (ADR 0041 decision 5). code is zero for every run but a station's
// and a check's.
func (e *Engine) secretEnv(
	ctx context.Context, use domain.SecretUse, modelKey string, env domain.Environment, repo domain.Repository, code domain.RunCode,
) (runEnv, error) {
	vars := slices.Sorted(maps.Keys(env.Env))
	files := domain.FileEnv(env.Files)

	token, entryTokens := repositoryToken(repo, code), pickedTokens(code)
	tokenNames := domain.SecretNames(token)
	names, mcpSecrets := secretNames(modelKey, append([]string{token}, slices.Collect(maps.Values(entryTokens))...), env)

	values, err := e.openSecrets(ctx, names, use, env.Name, runSecretsWhere(len(tokenNames) > 0, len(entryTokens) > 0, env.Name))
	if err != nil {
		return runEnv{}, err
	}

	out := runEnv{prepare: map[string]string{}, agent: map[string]string{}}

	for _, n := range names {
		if v := values[n]; v != "" {
			out.secrets = append(out.secrets, v)
		}
	}

	key := domain.FillSecrets(modelKey, values)
	keyEnv := domain.ModelKeyEnv(key)

	for _, v := range vars {
		if v == keyEnv {
			return runEnv{}, fmt.Errorf("environment %s sets %s, which the model key sets; a run gets one value per variable", env.Name, v)
		}

		out.agent[v] = domain.FillSecrets(env.Env[v], values)
	}

	for p, v := range files {
		out.agent[v] = domain.FillSecrets(env.Files[p], values)
	}

	for _, n := range mcpSecrets {
		out.agent[domain.SecretEnv(n)] = values[n]
	}

	if len(files) > 0 {
		out.files = files
	}

	if len(env.MCP) > 0 {
		out.mcp = make(map[string]domain.MCPServer, len(env.MCP))
		for name, s := range env.MCP {
			out.mcp[name] = s.Rendered()
		}
	}

	out.agent[keyEnv] = key

	out.repositoryLogin = len(tokenNames) > 0
	prepareTokens(out.prepare, token, entryTokens, values)

	return out, nil
}

// repositoryToken is the token prepare's stored login signs git in with: repo's, when it has a
// URL, or the one code picks for the item's first repository when it has one.
func repositoryToken(repo domain.Repository, code domain.RunCode) string {
	if repo.URL == "" {
		return ""
	}

	if len(code.Clones) > 0 && code.Clones[0].Token != "" {
		return code.Clones[0].Token
	}

	return repo.Token
}

// prepareTokens puts token, when it names a secret, in prepare as domain.RepositoryTokenEnv, and
// each of entries in its entry's domain.RepositoryEntryTokenEnv, filled from values.
func prepareTokens(prepare map[string]string, token string, entries, values map[string]string) {
	if len(domain.SecretNames(token)) > 0 {
		prepare[domain.RepositoryTokenEnv] = domain.FillSecrets(token, values)
	}

	for name, t := range entries {
		prepare[domain.RepositoryEntryTokenEnv(name)] = domain.FillSecrets(t, values)
	}
}

// pickedTokens are the {{secret}} references of the entry tokens code's logins pick, by the
// entry's name, once each.
func pickedTokens(code domain.RunCode) map[string]string {
	out := map[string]string{}
	for _, l := range code.Logins {
		out[l.TokenEntry] = l.Token
	}

	return out
}

// secretNames are the secrets a run on modelKey, the repository tokens and env opens, each once:
// modelKey's, the tokens' in sorted order after the first, then env's, files' and the MCP
// servers', each in sorted key order; mcp are the MCP servers' alone.
func secretNames(modelKey string, tokens []string, env domain.Environment) (names, mcp []string) {
	add := func(to *[]string, ns []string) {
		for _, n := range ns {
			if !slices.Contains(*to, n) {
				*to = append(*to, n)
			}
		}
	}

	add(&names, domain.SecretNames(modelKey))

	if len(tokens) > 0 {
		add(&names, domain.SecretNames(tokens[0]))

		for _, t := range slices.Sorted(slices.Values(tokens[1:])) {
			add(&names, domain.SecretNames(t))
		}
	}

	for _, m := range []map[string]string{env.Env, env.Files} {
		for _, k := range slices.Sorted(maps.Keys(m)) {
			add(&names, domain.SecretNames(m[k]))
		}
	}

	for _, s := range slices.Sorted(maps.Keys(env.MCP)) {
		add(&mcp, env.MCP[s].SecretNames())
	}

	add(&names, mcp)

	return names, mcp
}

// runSecretsWhere names the keys a run's secrets were named in, for openSecrets's error: the
// model key, the repository token when repo, the entries' tokens when entries, and the
// environment env, if any.
func runSecretsWhere(repo, entries bool, env string) string {
	where := "runtime.modelKey"
	if repo {
		where += ", " + repositoryLogin
	}

	if entries {
		where += ", runtime.repositories tokens"
	}

	if env != "" {
		where += " and environment " + env
	}

	return where
}

// openSecrets opens names for use, by name; none opens nothing. env is the environment the run
// gets, if any, for the log. Its error names where, the keys the secrets were named in, and wraps
// the port's, which names the secret.
func (e *Engine) openSecrets(ctx context.Context, names []string, use domain.SecretUse, env, where string) (map[string]string, error) {
	if len(names) == 0 {
		return nil, nil
	}

	if e.orgSecrets == nil {
		return nil, fmt.Errorf("secrets of %s: %w", where, errNoOrgSecrets)
	}

	opened, err := e.orgSecrets.Resolve(ctx, names, use)
	if err != nil {
		return nil, fmt.Errorf("secrets of %s: %w", where, err)
	}

	values := make(map[string]string, len(opened))
	for _, rs := range opened {
		values[rs.Secret.Name] = rs.Value
	}

	e.log.InfoContext(ctx, "secret used", slog.String("run", string(use.Run)), slog.String("agent", use.Agent),
		slog.String("environment", env), slog.String("secrets", strings.Join(names, ", ")))

	return values, nil
}
