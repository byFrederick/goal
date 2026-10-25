package gateway

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"slices"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Items reads the item a run works on; ports.Store satisfies it.
type Items interface {
	Item(ctx context.Context, id domain.ItemID) (domain.Item, error)
}

// runBundle is the body of GET /run: what a run's sandbox needs to prepare and run its agent
// (goal-agent prepare writes it to /goal; worker/README.md documents the layout). It holds no
// secret: a git credential names the environment variable that carries its password, and a
// connection's MCP headers name the variables Claude Code expands in the box.
type runBundle struct {
	Spec bundleSpec `json:"spec"`
	// Files are written under /goal at their slash-separated paths (agent/prompt.md, …); their
	// contents are base64 in JSON, so a skill may ship binary files.
	Files map[string][]byte `json:"files"`
	// Repository is cloned into the workspace; nil leaves the workspace empty.
	Repository *bundleRepository `json:"repository,omitempty"`
	// Git configures a credential helper per login: the repository connection's and those of the
	// agent's connections.
	Git []gitCredential `json:"git_credentials"`
	// Tools are mise tool specs installed after the repository's own: the runtime's, then the CLI
	// tools of the agent's connections.
	Tools []string `json:"tools"`
}

// bundleSpec is the task as the worker reads it from /goal/spec.json, less the gateway's URL,
// which the sandbox's own environment gives (prepare adds it).
type bundleSpec struct {
	FactoryID  domain.FactoryID           `json:"factory_id"`
	ItemID     domain.ItemID              `json:"item_id"`
	RunID      domain.RunID               `json:"run_id"`
	Station    string                     `json:"station"`
	Agent      string                     `json:"agent"`
	Model      string                     `json:"model,omitempty"`
	Skills     []string                   `json:"skills"`
	ToolsAllow []string                   `json:"tools_allow"`
	ToolsDeny  []string                   `json:"tools_deny"`
	Purpose    string                     `json:"purpose,omitempty"`
	Brief      string                     `json:"brief"`
	Inputs     map[string]json.RawMessage `json:"inputs,omitempty"`
	Output     string                     `json:"output,omitempty"`
	MaxTurns   int                        `json:"max_turns,omitempty"`
	MaxUSD     float64                    `json:"max_usd,omitempty"`
	// Connections are the agent's organization connections its token grants, in its order: the
	// worker configures their MCP servers and tells the agent about them.
	Connections []bundleConnection `json:"connections"`
	// KnowledgeSources are the knowledge sources the gateway's knowledge tools search for the
	// run, which the worker's prompt names.
	KnowledgeSources []string `json:"knowledge_sources,omitempty"`
}

// bundleConnection is what the agent hears and the worker configures of one connection: the
// names of the variables it sets in the agent's env, never their values, its MCP server and the
// kind's docs.
type bundleConnection struct {
	Name  string   `json:"name"`
	Kind  string   `json:"kind"`
	Title string   `json:"title"`
	Env   []string `json:"env"`
	// MCP is a remote server (url, headers naming env variables as ${NAME}) or a stdio command
	// the worker starts in the box; nil without one.
	MCP  *domain.KindMCP `json:"mcp,omitempty"`
	Docs string          `json:"docs,omitempty"`
}

// bundleRepository is the factory's repository and the item's branch, which prepare checks out:
// the remote branch when it exists, otherwise a new one from the default branch.
type bundleRepository struct {
	URL    string `json:"url"`
	Branch string `json:"branch"`
}

// gitCredential is a login git uses for https://<Host>: Username and the value of the variable
// PasswordEnv, read when git asks, so the password is never written to disk.
type gitCredential struct {
	Host        string `json:"host"`
	Username    string `json:"username"`
	PasswordEnv string `json:"password_env"`
}

// repositoryUser is the user name git sends with a repository token; code hosts read the token
// and accept any name, and GitHub documents this one.
const repositoryUser = "x-access-token"

// ServeRun serves GET /run: the bundle of the run whose task token the request carries, from
// the definition version the run started on and the item it works on. A token that does not
// verify is 401; one whose factory, agent, station or item the version or the organization does
// not have is 403.
func (g *Gateway) ServeRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}

	claims, err := g.authenticate(r)
	if err != nil {
		g.unauthorized(w, r, err)
		return
	}

	ctx := domain.WithOrg(r.Context(), claims.OrgID)

	b, err := g.bundle(ctx, claims)
	switch {
	case errors.Is(err, errForbidden) || errors.Is(err, ports.ErrNotFound):
		g.log.WarnContext(ctx, "run bundle forbidden", claimAttrs(claims, "reason", err)...)
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	case err != nil:
		g.log.ErrorContext(ctx, "run bundle failed", claimAttrs(claims, "error", err)...)
		http.Error(w, "gateway unavailable", http.StatusServiceUnavailable)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")

	if err := json.NewEncoder(w).Encode(b); err != nil {
		g.log.WarnContext(ctx, "write run bundle", claimAttrs(claims, "error", err)...)
		return
	}

	g.log.InfoContext(ctx, "run bundle served", claimAttrs(claims, "files", len(b.Files))...)
}

func (g *Gateway) bundle(ctx context.Context, c domain.TaskClaims) (runBundle, error) {
	f, err := g.defs.Version(ctx, c.VersionID)
	if err != nil {
		return runBundle{}, fmt.Errorf("load factory %s version %s: %w", c.FactoryID, c.VersionID, err)
	}

	if f.Metadata.ID != string(c.FactoryID) {
		return runBundle{}, fmt.Errorf("%w: version %s is not a version of factory %s", errForbidden, c.VersionID, c.FactoryID)
	}

	line := f.Line()

	agent, ok := line.Agent(c.Agent)
	if !ok {
		return runBundle{}, fmt.Errorf("%w: agent %q is not in factory %s", errForbidden, c.Agent, c.FactoryID)
	}

	station, ok := line.Station(c.Station)
	if !ok {
		return runBundle{}, fmt.Errorf("%w: station %q is not in factory %s", errForbidden, c.Station, c.FactoryID)
	}

	it, err := g.items.Item(ctx, c.ItemID)
	if err != nil {
		return runBundle{}, fmt.Errorf("item %s: %w", c.ItemID, err)
	}

	if it.FactoryID != c.FactoryID {
		return runBundle{}, fmt.Errorf("%w: item %s is not in factory %s", errForbidden, c.ItemID, c.FactoryID)
	}

	agentFiles, err := g.defs.AgentFiles(ctx, c.VersionID, agent, station.Output)
	if err != nil {
		return runBundle{}, fmt.Errorf("agent %s files: %w", agent.Name, err)
	}

	maxTurns, maxUSD := agent.Limits(line.Runtime)
	b := runBundle{
		Spec: bundleSpec{
			FactoryID: c.FactoryID, ItemID: it.ID, RunID: c.RunID, Station: station.ID, Agent: agent.Name,
			Model: cmp.Or(agent.Model, line.Runtime.Model), Skills: make([]string, 0, len(agent.Skills)),
			ToolsAllow: nonNil(agent.ToolsAllow), ToolsDeny: nonNil(agent.ToolsDeny), Purpose: line.Purpose,
			Brief: it.Work(), Inputs: it.Outputs, Output: station.Output, MaxTurns: maxTurns, MaxUSD: maxUSD,
			Connections:      []bundleConnection{},
			KnowledgeSources: g.searchable(line),
		},
		Files: make(map[string][]byte, len(agentFiles)),
		Git:   []gitCredential{},
		Tools: append([]string{}, line.Runtime.Tools...),
	}

	for _, sk := range agent.Skills {
		b.Spec.Skills = append(b.Spec.Skills, sk.Name)
	}

	for p, body := range agentFiles {
		b.Files[path.Join("agent", p)] = body
	}

	if err := g.addConnections(ctx, c, line, agent, &b); err != nil {
		return runBundle{}, err
	}

	if err := g.addRepository(ctx, line.Runtime.Repository, it.Branch(), &b); err != nil {
		return runBundle{}, err
	}

	return b, nil
}

// addRepository puts the repository in b with the git login that clones it: its connection's or,
// for a version published before connections, one for the repository token
// (GOAL_REPOSITORY_TOKEN).
func (g *Gateway) addRepository(ctx context.Context, repo domain.Repository, branch string, b *runBundle) error {
	if repo.URL == "" {
		return nil
	}

	b.Repository = &bundleRepository{URL: repo.URL, Branch: branch}

	switch {
	case repo.Connection != "":
		o, _, err := g.outline(ctx, repo.Connection)
		if err != nil {
			return err
		}

		if o.Git != nil {
			b.addGit(gitCredentialOf(*o.Git))
		}
	case repo.AuthRef != "":
		u, err := url.Parse(repo.URL)
		if err != nil {
			return fmt.Errorf("repository url: %w", err)
		}

		b.addGit(gitCredential{Host: u.Host, Username: repositoryUser, PasswordEnv: domain.RunEnvRepositoryToken})
	}

	return nil
}

// addConnections describes the agent's organization connections that its token grants in b, with
// their git logins and CLI tools. The model connection gives the agent's env only, so it is not
// described.
func (g *Gateway) addConnections(ctx context.Context, c domain.TaskClaims, line domain.Line, agent domain.AgentSpec, b *runBundle) error {
	for _, name := range agent.Connections {
		if !slices.Contains(line.Connections, name) || !slices.Contains(c.Connections, name) {
			continue // an inline connection (served at /mcp) or one the token does not grant
		}

		o, k, err := g.outline(ctx, name)
		if err != nil {
			return err
		}

		b.Spec.Connections = append(b.Spec.Connections, bundleConnection{
			Name: name, Kind: k.Name, Title: k.Title, Env: nonNil(o.Env), MCP: o.MCP, Docs: k.Spec.Docs,
		})

		if o.Git != nil {
			b.addGit(gitCredentialOf(*o.Git))
		}

		if o.CLI != "" && !slices.Contains(b.Tools, o.CLI) {
			b.Tools = append(b.Tools, o.CLI)
		}
	}

	return nil
}

// outline is the connection name of the organization in ctx, as its kind renders it without any
// secret value.
func (g *Gateway) outline(ctx context.Context, name string) (domain.ConnectionOutline, domain.ConnectionKind, error) {
	conn, err := g.connections.Get(ctx, name)
	if err != nil {
		return domain.ConnectionOutline{}, domain.ConnectionKind{}, fmt.Errorf("connection %s: %w", name, err)
	}

	k, err := g.connections.Kind(ctx, conn.Kind)
	if err != nil {
		return domain.ConnectionOutline{}, domain.ConnectionKind{}, fmt.Errorf("connection %s: kind %s: %w", name, conn.Kind, err)
	}

	return k.Outline(conn), k, nil
}

func gitCredentialOf(g domain.KindGit) gitCredential {
	return gitCredential{Host: g.Host, Username: g.Username, PasswordEnv: g.PasswordEnv}
}

// addGit adds a git login once.
func (b *runBundle) addGit(c gitCredential) {
	if !slices.Contains(b.Git, c) {
		b.Git = append(b.Git, c)
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}

	return s
}
