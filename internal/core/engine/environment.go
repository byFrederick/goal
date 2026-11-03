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

// secretEnv resolves a run of a version on secrets (ADR 0037 decisions 7 and 10): the {{secret}}
// references of modelKey and of env's values are opened once, through OrgSecrets.Resolve, which
// checks each scope against use's factory, audits each as secret.used and sets its last use. The
// agent gets env's variables with the values filled in, whole or part of a value, and the model
// key's value as domain.ModelKeyEnv names it; every value opened joins the run's secrets, so it
// is scrubbed. A failure names the secret and the environment, never a value. env is zero for a
// run that gets the model key alone (a Jonah run, a definition run, Use credentials off).
//
// repo's token, when it has a URL and one, is opened in the same call and goes to prepare alone,
// as domain.RepositoryTokenEnv: it signs git in for the clone, the base branch's push and, from
// the login prepare keeps, the agent's own push, but it is not in the agent's env unless env names
// the secret too. repo is zero for a run that clones nothing.
func (e *Engine) secretEnv(
	ctx context.Context, use domain.SecretUse, modelKey string, env domain.Environment, repo domain.Repository,
) (runEnv, error) {
	vars := slices.Sorted(maps.Keys(env.Env))
	names := domain.SecretNames(modelKey)

	var tokenNames []string
	if repo.URL != "" {
		tokenNames = domain.SecretNames(repo.Token)
	}

	for _, n := range tokenNames {
		if !slices.Contains(names, n) {
			names = append(names, n)
		}
	}

	for _, v := range vars {
		for _, n := range domain.SecretNames(env.Env[v]) {
			if !slices.Contains(names, n) {
				names = append(names, n)
			}
		}
	}

	where := "runtime.modelKey"
	if len(tokenNames) > 0 {
		where += ", " + repositoryLogin
	}

	if env.Name != "" {
		where += " and environment " + env.Name
	}

	values, err := e.openSecrets(ctx, names, use, env.Name, where)
	if err != nil {
		return runEnv{}, err
	}

	out := runEnv{prepare: map[string]string{}, agent: map[string]string{}, gitHosts: map[string]string{}}

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

	out.agent[keyEnv] = key

	if len(tokenNames) > 0 {
		out.prepare[domain.RepositoryTokenEnv] = domain.FillSecrets(repo.Token, values)
		out.repositoryLogin = true
	}

	return out, nil
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
