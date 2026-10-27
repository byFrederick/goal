package engine

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// runEnv is what one run's commands get from its connections (spec 4 §4.5), each exec only what
// it needs: prepare the repository's git password, the agent the model's access and its own
// connections' env, a check nothing.
type runEnv struct {
	// prepare is goal-agent prepare's: the password variable of the repository's git login.
	prepare map[string]string
	// agent is goal-agent run's: the model connection's env and the env of the agent's
	// connections.
	agent map[string]string
	// gitHosts are the hosts the agent's own connections sign git in to, by connection.
	gitHosts map[string]string
	// secrets are every secret value the run's commands get, scrubbed from what they report.
	secrets []string
}

// runEnv resolves the run's connections, each use audited as connection.used: the runtime's model
// and repository connections and the organization connections of the agent's list. It opens the
// credentials they refer to and the ones listed for the run's agent (ADR 0017 decisions 5 to 7),
// each use audited as credential.used by the port. The line runs (domain.Line.Refusal), so it has
// a model connection.
func (e *Engine) runEnv(ctx context.Context, c claimed, run domain.RunID) (runEnv, error) {
	rt := c.line.Runtime
	own := slices.DeleteFunc(slices.Clone(c.agent.Connections), func(n string) bool { return !slices.Contains(c.line.Connections, n) })

	var names []string

	for _, n := range append([]string{rt.ModelConnection, rt.Repository.Connection}, own...) {
		if n != "" && !slices.Contains(names, n) {
			names = append(names, n)
		}
	}

	use := domain.CredentialUse{Factory: c.line.FactoryID, Agent: c.agent.Name, Run: run}

	conns, opened, err := e.resolve(ctx, names, use)
	if err != nil {
		return runEnv{}, err
	}

	env := runEnv{prepare: map[string]string{}, gitHosts: map[string]string{}}
	for _, rc := range conns {
		env.secrets = append(env.secrets, rc.secrets...)
	}

	for _, rc := range opened {
		if rc.Value != "" {
			env.secrets = append(env.secrets, rc.Value)
		}
	}

	if err := agentEnv(rt.ModelConnection, own, conns, &env); err != nil {
		return runEnv{}, err
	}

	if err := credentialEnv(use, opened, conns, &env); err != nil {
		return runEnv{}, err
	}

	if err := repositoryEnv(rt.Repository, conns, &env); err != nil {
		return runEnv{}, err
	}

	if len(names) > 0 {
		e.log.InfoContext(ctx, "connection used", slog.String("run", string(run)), slog.Any("connections", names))
	}

	return env, nil
}

// agentEnv gives the agent the model connection's env and the env of its own connections,
// refusing a variable two connections set.
func agentEnv(model string, own []string, conns map[string]runConnection, env *runEnv) error {
	setBy := map[string]string{} // variable → the connection that sets it

	m := conns[model]
	if !m.kind.Spec.Model {
		return fmt.Errorf("model connection %s: its kind %s gives no model access", m.name, m.kind.Name)
	}

	env.agent = map[string]string{}
	if err := addEnv(env.agent, setBy, m); err != nil {
		return err
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

// credentialEnv gives the agent each opened credential whose agent list names it for the run's
// factory, as the variable domain.CredentialEnvName (ADR 0017 decision 6). It refuses a variable a
// connection of the run sets, since a run gets one value per variable.
func credentialEnv(use domain.CredentialUse, opened []domain.ResolvedCredential, conns map[string]runConnection, env *runEnv) error {
	for _, rc := range opened {
		cred := rc.Credential
		if !cred.ForAgent(use.Factory, use.Agent) {
			continue
		}

		name, err := domain.CredentialEnvName(cred.Name)
		if err != nil {
			return fmt.Errorf("credential %s: %w", cred.Name, err)
		}

		for _, n := range slices.Sorted(maps.Keys(conns)) {
			if _, ok := conns[n].Env[name]; ok {
				return fmt.Errorf("credential %s and connection %s both set %s; a run gets one value per variable", cred.Name, n, name)
			}
		}

		env.agent[name] = rc.Value
	}

	return nil
}

// repositoryEnv gives prepare what clones the repository: its connection's git password; a public
// repository needs none.
func repositoryEnv(r domain.Repository, conns map[string]runConnection, env *runEnv) error {
	if r.Connection == "" {
		return nil
	}

	rc := conns[r.Connection]
	if rc.Git == nil {
		return fmt.Errorf("repository connection %s: its kind %s signs git in to no host", rc.name, rc.kind.Name)
	}

	env.prepare[rc.Git.PasswordEnv] = rc.Env[rc.Git.PasswordEnv]

	return nil
}

// runConnection is one connection rendered for a run.
type runConnection struct {
	domain.RunConnection

	name string
	kind domain.ConnectionKind
	// secrets are its secret field values and the values minted for the run.
	secrets []string
}

// resolve opens and renders the named connections for use's run, by name, each secret field that
// refers to a credential filled with its value before its kind renders it (ADR 0017 decision 5),
// and returns the credentials it opened: those and the ones listed for use's agent.
func (e *Engine) resolve(ctx context.Context, names []string, use domain.CredentialUse) (map[string]runConnection, []domain.ResolvedCredential, error) {
	out := make(map[string]runConnection, len(names))

	var resolved []domain.ResolvedConnection

	if len(names) > 0 {
		var err error
		if resolved, err = e.connections.Resolve(ctx, names, domain.ConnectionUse{Run: use.Run}); err != nil {
			return nil, nil, fmt.Errorf("connections: %w", err)
		}
	}

	opened, err := e.openCredentials(ctx, resolved, use)
	if err != nil {
		return nil, nil, err
	}

	values := make(map[string]string, len(opened))
	for _, rc := range opened {
		values[rc.Credential.Name] = rc.Value
	}

	for _, r := range resolved {
		if len(r.SecretRefs) > 0 {
			r.Values = maps.Clone(r.Values)
			for field, cred := range r.SecretRefs {
				r.Values[field] = values[cred]
			}
		}

		rc := runConnection{name: r.Connection.Name, kind: r.Kind, secrets: r.SecretValues()}

		minted, err := e.mint(ctx, r, use.Run, &rc)
		if err != nil {
			return nil, nil, err
		}

		if rc.RunConnection, err = r.Kind.Render(r.Values, minted); err != nil {
			// A kind stored before Check refused it can make a value an env name the error quotes;
			// its secrets hold the credentials' values its fields refer to.
			return nil, nil, fmt.Errorf("connection %s: %w", rc.name, newScrubber(rc.secrets...).err(err))
		}

		out[rc.name] = rc
	}

	return out, opened, nil
}

// openCredentials opens, once each, the credentials resolved's connections refer to and the ones
// whose agent list names use's agent for its factory. A referred credential that is missing or
// does not allow the factory fails with an error that names it and the connections that refer to
// it, wrapping ports.ErrNotFound or ports.ErrCredentialNotAllowed. Resolve audits each use as
// credential.used and sets its last use; no error holds a value.
func (e *Engine) openCredentials(ctx context.Context, resolved []domain.ResolvedConnection, use domain.CredentialUse) ([]domain.ResolvedCredential, error) {
	refs := map[string][]string{} // credential → the connections that refer to it

	for _, r := range resolved {
		for _, cred := range r.SecretRefs {
			if !slices.Contains(refs[cred], r.Connection.Name) {
				refs[cred] = append(refs[cred], r.Connection.Name)
			}
		}
	}

	all, err := e.credentials.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("credentials: %w", err)
	}

	byName := make(map[string]domain.Credential, len(all))

	var names []string

	for _, c := range all {
		byName[c.Name] = c
		if c.ForAgent(use.Factory, use.Agent) {
			names = append(names, c.Name)
		}
	}

	for _, cred := range slices.Sorted(maps.Keys(refs)) {
		users := strings.Join(slices.Sorted(slices.Values(refs[cred])), ", ")

		c, ok := byName[cred]
		switch {
		case !ok:
			return nil, fmt.Errorf("connection %s: credential %s: %w", users, cred, ports.ErrNotFound)
		case !c.Scope.Allows(use.Factory):
			return nil, fmt.Errorf("connection %s: credential %s: %w", users, cred, ports.ErrCredentialNotAllowed)
		}

		if !slices.Contains(names, cred) {
			names = append(names, cred)
		}
	}

	if len(names) == 0 {
		return nil, nil
	}

	opened, err := e.credentials.Resolve(ctx, names, use)
	if err != nil {
		return nil, fmt.Errorf("credentials %s: %w", strings.Join(names, ", "), err)
	}

	e.log.InfoContext(ctx, "credential used", slog.String("run", string(use.Run)), slog.String("agent", use.Agent),
		slog.Any("credentials", names))

	return opened, nil
}

// mintMargin is how much longer than a run's task timeout minted credentials live (spec 4 §4.5).
const mintMargin = 15 * time.Minute

// mint mints r's credentials for run when its kind names a minter (spec 4 §5.4), from its values
// with its kind's defaults, to live the task timeout and mintMargin more; nil when the kind mints
// nothing. The minted values join rc's secrets, so they are scrubbed like the stored ones, and the
// error is scrubbed of the stored ones.
func (e *Engine) mint(ctx context.Context, r domain.ResolvedConnection, run domain.RunID, rc *runConnection) (map[string]string, error) {
	name := r.Kind.Spec.Mint
	if name == "" {
		return nil, nil
	}

	m, ok := e.minters[name]
	if !ok {
		return nil, fmt.Errorf("connection %s: its kind %s mints with %q, which this goald has no minter for", rc.name, r.Kind.Name, name)
	}

	minted, _, err := m.Mint(ctx, r.Kind.WithDefaults(r.Values), run, e.cfg.TaskTimeout+mintMargin)
	if err != nil {
		return nil, fmt.Errorf("connection %s: mint: %w", rc.name, newScrubber(rc.secrets...).err(err))
	}

	for _, v := range minted {
		if v != "" {
			rc.secrets = append(rc.secrets, v)
		}
	}

	return minted, nil
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
