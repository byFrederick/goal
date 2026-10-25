package engine

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"github.com/byFrederick/goal/internal/domain"
)

// runEnv is what one run's commands get from its connections (spec 4 §4.5), each exec only what
// it needs: prepare the repository's git password, the agent the model's access and its own
// connections' env, a check nothing.
type runEnv struct {
	// prepare is goal-agent prepare's: the password variable of the repository's git login.
	prepare map[string]string
	// agent is goal-agent run's: the model connection's env (or the install's task env for a
	// version without one) and the env of the agent's connections.
	agent map[string]string
	// gitHosts are the hosts the agent's own connections sign git in to, by connection.
	gitHosts map[string]string
	// secrets are every secret value the run's commands get, scrubbed from what they report.
	secrets []string
}

// runEnv resolves the run's connections, each use audited as connection.used: the runtime's model
// and repository connections and the organization connections of the agent's list (an inline
// connection of a version published before phase 4 is the gateway's). A version without a model
// connection gets the model from the install's task env, and one with the repository token of the
// removed form clones with it, until the cutover (4-14).
func (e *Engine) runEnv(ctx context.Context, c claimed, run domain.RunID) (runEnv, error) {
	rt := c.line.Runtime
	own := slices.DeleteFunc(slices.Clone(c.agent.Connections), func(n string) bool { return !slices.Contains(c.line.Connections, n) })

	var names []string

	for _, n := range append([]string{rt.ModelConnection, rt.Repository.Connection}, own...) {
		if n != "" && !slices.Contains(names, n) {
			names = append(names, n)
		}
	}

	conns, err := e.resolve(ctx, names, run)
	if err != nil {
		return runEnv{}, err
	}

	env := runEnv{prepare: map[string]string{}, gitHosts: map[string]string{}}
	for _, rc := range conns {
		env.secrets = append(env.secrets, rc.secrets...)
	}

	if err := e.agentEnv(ctx, rt.ModelConnection, own, conns, &env); err != nil {
		return runEnv{}, err
	}

	if err := e.repositoryEnv(ctx, rt.Repository, conns, &env); err != nil {
		return runEnv{}, err
	}

	if len(names) > 0 {
		e.log.InfoContext(ctx, "connection used", slog.String("run", string(run)), slog.Any("connections", names))
	}

	return env, nil
}

// agentEnv gives the agent the model connection's env, or the install's task env without one,
// and the env of its own connections, refusing a variable two connections set.
func (e *Engine) agentEnv(ctx context.Context, model string, own []string, conns map[string]runConnection, env *runEnv) error {
	setBy := map[string]string{} // variable → the connection that sets it

	if model == "" {
		agent, err := e.taskEnv(ctx)
		if err != nil {
			return err
		}

		env.agent = agent
		env.secrets = append(env.secrets, slices.Collect(maps.Values(agent))...)
	} else {
		m := conns[model]
		if !m.kind.Spec.Model {
			return fmt.Errorf("model connection %s: its kind %s gives no model access", m.name, m.kind.Name)
		}

		env.agent = map[string]string{}
		if err := addEnv(env.agent, setBy, m); err != nil {
			return err
		}
	}

	for _, n := range own {
		if err := addEnv(env.agent, setBy, conns[n]); err != nil {
			return err
		}

		if g := conns[n].Git; g != nil {
			env.gitHosts[n] = g.Host
		}
	}

	return nil
}

// repositoryEnv gives prepare what clones the repository: its connection's git password or, for a
// version published before connections, the repository token (GOAL_REPOSITORY_TOKEN).
func (e *Engine) repositoryEnv(ctx context.Context, r domain.Repository, conns map[string]runConnection, env *runEnv) error {
	switch {
	case r.Connection != "":
		rc := conns[r.Connection]
		if rc.Git == nil {
			return fmt.Errorf("repository connection %s: its kind %s signs git in to no host", rc.name, rc.kind.Name)
		}

		env.prepare[rc.Git.PasswordEnv] = rc.Env[rc.Git.PasswordEnv]
	case r.AuthRef != "":
		token, err := e.repositoryToken(ctx, r)
		if err != nil {
			return err
		}

		env.prepare[domain.RunEnvRepositoryToken] = token
		env.secrets = append(env.secrets, token)
	}

	return nil
}

// runConnection is one connection rendered for a run.
type runConnection struct {
	domain.RunConnection

	name string
	kind domain.ConnectionKind
	// secrets are its secret field values.
	secrets []string
}

// resolve opens and renders the named connections for run, by name.
func (e *Engine) resolve(ctx context.Context, names []string, run domain.RunID) (map[string]runConnection, error) {
	out := make(map[string]runConnection, len(names))
	if len(names) == 0 {
		return out, nil
	}

	resolved, err := e.connections.Resolve(ctx, names, run)
	if err != nil {
		return nil, fmt.Errorf("connections: %w", err)
	}

	for _, r := range resolved {
		rc := runConnection{name: r.Connection.Name, kind: r.Kind}

		for _, f := range r.Kind.Spec.Fields {
			if v := r.Values[f.Name]; f.Secret && v != "" {
				rc.secrets = append(rc.secrets, v)
			}
		}

		// Minting per-run credentials comes with ports.Minter (spec 4 §5.4).
		if r.Kind.Spec.Mint != "" {
			return nil, fmt.Errorf("connection %s: its kind %s mints its credentials (%s), which this goald does not do",
				rc.name, r.Kind.Name, r.Kind.Spec.Mint)
		}

		if rc.RunConnection, err = r.Kind.Render(r.Values, nil); err != nil {
			return nil, fmt.Errorf("connection %s: %w", rc.name, err)
		}

		out[rc.name] = rc
	}

	return out, nil
}

// addEnv adds rc's env to env, refusing a variable another connection of the run sets.
func addEnv(env, setBy map[string]string, rc runConnection) error {
	for _, name := range slices.Sorted(maps.Keys(rc.Env)) {
		if other, ok := setBy[name]; ok && other != rc.name {
			return fmt.Errorf("connections %s and %s both set %s; a run gets one value per variable", other, rc.name, name)
		}

		env[name], setBy[name] = rc.Env[name], rc.name
	}

	return nil
}

// pushNote tells the agent whether git can push to the repository's host: with one of its own
// connections that signs git in there, or not at all.
func (env runEnv) pushNote(host string) string {
	for _, n := range slices.Sorted(maps.Keys(env.gitHosts)) {
		if strings.EqualFold(env.gitHosts[n], host) {
			return fmt.Sprintf("Git signs in to %s with your connection `%s`.", host, n)
		}
	}

	return "It holds no git credentials for " + host + "."
}
