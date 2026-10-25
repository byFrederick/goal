package authoring

import (
	"context"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Problem codes of the connections check (spec 4 §4.4). All are errors: a version that has them
// cannot run.
const (
	// CodeConnectionUnknown: the organization has no connection of a name the definition uses.
	CodeConnectionUnknown = "connection-unknown"
	// CodeConnectionNotModel: runtime.modelConnection is of a kind that gives no model access.
	CodeConnectionNotModel = "connection-not-model"
	// CodeRepositoryHostMismatch: runtime.repository.connection does not sign git in to the
	// repository's host.
	CodeRepositoryHostMismatch = "repository-host-mismatch"
	// CodeConnectionEnvConflict: two connections an agent's runs get set the same variable.
	CodeConnectionEnvConflict = "connection-env-conflict"
	// CodeKnowledgeSourceUnknown: the organization has no knowledge source of a name the
	// definition may search.
	CodeKnowledgeSourceUnknown = "knowledge-source-unknown"
)

// KnowledgeSources names the organization of ctx's knowledge sources (spec 4 §4.8).
type KnowledgeSources interface {
	SourceNames(ctx context.Context) ([]string, error)
}

// ConnectionsCheck is the credentials check from phase 4 on (spec 4 §4.4): every organization
// connection the definition names exists (connection-unknown); its model connection gives model
// access (connection-not-model); its repository's connection signs git in to the repository's
// host (repository-host-mismatch); no two connections an agent's runs get set the same variable
// (connection-env-conflict); and every knowledge source it may search exists
// (knowledge-source-unknown). It reads names, kinds and non-secret fields, never a secret.
type ConnectionsCheck struct {
	Connections ports.Connections
	// Knowledge names the organization's knowledge sources; nil until the knowledge base ships
	// (spec 4, 4-16), when an organization has none.
	Knowledge KnowledgeSources
	// Env checks the env: references of the forms phase 4 removed, which versions published
	// before still run on until the cutover (spec 4, 4-14); nil checks none.
	Env *EnvReferences
}

var _ Credentials = ConnectionsCheck{}

// Check implements Credentials.
func (c ConnectionsCheck) Check(ctx context.Context, f *config.Factory) ([]domain.DefinitionProblem, error) {
	conns, err := c.Connections.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list connections: %w", err)
	}

	kinds, err := c.Connections.Kinds(ctx)
	if err != nil {
		return nil, fmt.Errorf("list connection kinds: %w", err)
	}

	var sources []string
	if c.Knowledge != nil {
		if sources, err = c.Knowledge.SourceNames(ctx); err != nil {
			return nil, fmt.Errorf("list knowledge sources: %w", err)
		}
	}

	r := &connectionsRun{f: f, conns: map[string]domain.Connection{}, kinds: map[string]domain.ConnectionKind{}}
	for _, cn := range conns {
		r.conns[cn.Name] = cn
	}

	for _, k := range kinds {
		r.kinds[k.Name] = k
	}

	r.listed()
	r.model()
	r.repository()
	r.envConflicts()
	r.knowledge(sources)

	if c.Env != nil {
		env, err := c.Env.Check(ctx, f)
		if err != nil {
			return nil, err
		}

		r.out = append(r.out, env...)
	}

	return r.out, nil
}

// specPath is the factory.yaml path of a key below spec.
func specPath(keys ...string) []string { return append([]string{"spec"}, keys...) }

// connectionsRun is one definition checked against one organization's connections.
type connectionsRun struct {
	f     *config.Factory
	conns map[string]domain.Connection
	kinds map[string]domain.ConnectionKind
	out   []domain.DefinitionProblem
}

func (r *connectionsRun) add(path []string, code, msg string, fixes ...domain.ProblemFix) {
	p := r.f.Locate("/"+strings.Join(path, "/")).Problem(CheckConnections, code, domain.SeverityError, msg)
	p.Fixes = fixes
	r.out = append(r.out, p)
}

// kind is the kind of the organization's connection name; ok is false when either is missing.
func (r *connectionsRun) kind(name string) (domain.Connection, domain.ConnectionKind, bool) {
	cn, ok := r.conns[name]
	if !ok {
		return domain.Connection{}, domain.ConnectionKind{}, false
	}

	k, ok := r.kinds[cn.Kind]

	return cn, k, ok
}

// outline is what a run gets from the organization's connection name, nothing when it has none.
func (r *connectionsRun) outline(name string) domain.ConnectionOutline {
	cn, k, ok := r.kind(name)
	if !ok {
		return domain.ConnectionOutline{}
	}

	return k.Outline(cn)
}

// listed reports the entries of spec.connections that name no connection of the organization;
// one no agent names can be removed.
func (r *connectionsRun) listed() {
	for i, c := range r.f.Spec.Connections {
		if c.Inline() || r.exists(c.Name) {
			continue
		}

		var fixes []domain.ProblemFix

		p := specPath("connections", strconv.Itoa(i))
		if !slices.ContainsFunc(r.f.Spec.Agents, func(a config.Agent) bool { return slices.Contains(a.Connections, c.Name) }) {
			fixes = append(fixes, config.RemoveFix("Remove "+c.Name, p))
		}

		r.add(p, CodeConnectionUnknown, fmt.Sprintf(
			"connection %s: the organization has no connection of that name; create it on the Connections page or remove it", c.Name), fixes...)
	}
}

func (r *connectionsRun) exists(name string) bool {
	_, ok := r.conns[name]

	return ok
}

// model checks runtime.modelConnection; the fixes offer each connection with model access.
func (r *connectionsRun) model() {
	name := r.f.Spec.Runtime.ModelConnection
	if name == "" {
		return
	}

	p := specPath("runtime", "modelConnection")

	var (
		fixes  []domain.ProblemFix
		titles []string
	)

	for _, n := range slices.Sorted(maps.Keys(r.conns)) {
		if _, k, ok := r.kind(n); ok && k.Spec.Model {
			fixes = append(fixes, config.ReplaceFix("Use "+n, p, n))
		}
	}

	for _, k := range r.kinds {
		if k.Spec.Model {
			titles = append(titles, k.Title)
		}
	}

	slices.Sort(titles)

	cn, k, ok := r.kind(name)

	switch {
	case !ok && !r.exists(name):
		r.add(p, CodeConnectionUnknown, "runtime.modelConnection: the organization has no connection named "+name, fixes...)
	case !ok || !k.Spec.Model:
		r.add(p, CodeConnectionNotModel, fmt.Sprintf(
			"runtime.modelConnection: %s is a %s connection, which gives no model access; choose one of a kind that does (%s)",
			name, kindTitle(cn, k), strings.Join(titles, ", ")), fixes...)
	}
}

func kindTitle(cn domain.Connection, k domain.ConnectionKind) string {
	if k.Title != "" {
		return k.Title
	}

	return cn.Kind
}

// repository checks that runtime.repository.connection signs git in to the repository's host;
// the fixes offer each connection that does, and cloning without one.
func (r *connectionsRun) repository() {
	repo := r.f.Spec.Runtime.Repository
	if repo == nil || repo.Connection == "" {
		return
	}

	host := ""
	if u, err := url.Parse(repo.URL); err == nil {
		host = u.Host
	}

	p := specPath("runtime", "repository", "connection")

	var fixes []domain.ProblemFix

	for _, n := range slices.Sorted(maps.Keys(r.conns)) {
		if n != repo.Connection && host != "" && strings.EqualFold(r.outline(n).GitHost, host) {
			fixes = append(fixes, config.ReplaceFix("Use "+n, p, n))
		}
	}

	fixes = append(fixes, config.RemoveFix("Clone without a connection", p))

	if !r.exists(repo.Connection) {
		r.add(p, CodeConnectionUnknown, "runtime.repository: the organization has no connection named "+repo.Connection, fixes...)

		return
	}

	switch got := r.outline(repo.Connection).GitHost; {
	case got == "":
		r.add(p, CodeRepositoryHostMismatch, fmt.Sprintf(
			"runtime.repository: %s signs git in to no host, so it cannot clone the repository on %s", repo.Connection, host), fixes...)
	case !strings.EqualFold(got, host):
		r.add(p, CodeRepositoryHostMismatch, fmt.Sprintf(
			"runtime.repository: %s signs git in to %s, not to %s, the repository's host", repo.Connection, got, host), fixes...)
	}
}

// runConnection is one connection an agent's runs get: the runtime's model and repository
// connections, and the agent's own list, where index is its place (-1 for the runtime's).
type runConnection struct {
	name  string
	index int
}

// envConflicts reports two connections an agent's runs get that set the same variable, at the
// later one of the agent's list. Each fix keeps one, taking the other out of the agent's list.
func (r *connectionsRun) envConflicts() {
	var shared []runConnection

	for _, n := range []string{r.f.Spec.Runtime.ModelConnection, r.repositoryConnection()} {
		if n != "" && r.exists(n) && !slices.ContainsFunc(shared, func(c runConnection) bool { return c.name == n }) {
			shared = append(shared, runConnection{name: n, index: -1})
		}
	}

	if len(shared) == 2 {
		if vars := r.shared(shared[0].name, shared[1].name); len(vars) > 0 {
			r.add(specPath("runtime", "repository", "connection"), CodeConnectionEnvConflict, fmt.Sprintf(
				"runtime: %s and %s both set %s; a run gets one value per variable", shared[0].name, shared[1].name, strings.Join(vars, ", ")))
		}
	}

	for i, a := range r.f.Spec.Agents {
		runs := slices.Clone(shared)

		for j, n := range a.Connections {
			if r.orgConnection(n) && !slices.ContainsFunc(runs, func(c runConnection) bool { return c.name == n }) {
				runs = append(runs, runConnection{name: n, index: j})
			}
		}

		for y := range runs {
			for x := range y {
				r.envConflict(i, a.Name, runs[x], runs[y])
			}
		}
	}
}

func (r *connectionsRun) repositoryConnection() string {
	if repo := r.f.Spec.Runtime.Repository; repo != nil {
		return repo.Connection
	}

	return ""
}

// orgConnection reports whether an agent's connection is one of the organization's that exists.
func (r *connectionsRun) orgConnection(name string) bool {
	i := slices.IndexFunc(r.f.Spec.Connections, func(c config.Connection) bool { return c.Name == name })

	return i >= 0 && !r.f.Spec.Connections[i].Inline() && r.exists(name)
}

// envConflict reports a and b of agent i (a before b) when they set a variable both.
func (r *connectionsRun) envConflict(i int, agent string, a, b runConnection) {
	if b.index < 0 {
		return // both are the runtime's, reported once for the factory
	}

	vars := r.shared(a.name, b.name)
	if len(vars) == 0 {
		return
	}

	entry := func(c runConnection) []string {
		return specPath("agents", strconv.Itoa(i), "connections", strconv.Itoa(c.index))
	}

	fixes := []domain.ProblemFix{config.RemoveFix("Keep "+a.name, entry(b))}
	if a.index >= 0 {
		fixes = append(fixes, config.RemoveFix("Keep "+b.name, entry(a)))
	}

	r.add(entry(b), CodeConnectionEnvConflict, fmt.Sprintf(
		"agent %s: %s and %s both set %s; a run gets one value per variable, so keep one", agent, a.name, b.name, strings.Join(vars, ", ")),
		fixes...)
}

// shared lists the variables connections a and b both set, sorted.
func (r *connectionsRun) shared(a, b string) []string {
	theirs := r.outline(b).Env

	var out []string

	for _, v := range r.outline(a).Env {
		if slices.Contains(theirs, v) {
			out = append(out, v)
		}
	}

	return out
}

// knowledge reports the knowledge sources the definition may search that the organization lacks.
func (r *connectionsRun) knowledge(sources []string) {
	k := r.f.Spec.Knowledge
	if k == nil {
		return
	}

	for i, s := range k.Sources {
		if slices.Contains(sources, s) {
			continue
		}

		p := specPath("knowledge", "sources", strconv.Itoa(i))
		r.add(p, CodeKnowledgeSourceUnknown, "knowledge.sources: the organization has no knowledge source named "+s,
			config.RemoveFix("Remove "+s, p))
	}
}
