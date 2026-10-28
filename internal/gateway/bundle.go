package gateway

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"path"
	"slices"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Items reads the item a run works on; ports.Store satisfies it.
type Items interface {
	Item(ctx context.Context, id domain.ItemID) (domain.Item, error)
}

// runBundle is the body of GET /run: what a run's sandbox needs to prepare and run its agent
// (goal-agent prepare writes it to /goal; apps/worker/README.md documents the layout). It holds no
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
	// Rules are the factory rules that apply to the run (ADR 0014), in declaration order, which
	// the worker writes to the agent's rules.
	Rules []bundleRule `json:"rules"`
	// GrantedCalls are the Ask-first calls a person approved for this run (ADR 0031 decision 5),
	// which the worker allows once each when connection, tool and canonical input match.
	GrantedCalls []grantedCall `json:"granted_calls"`
	// Jonah is a Jonah review's definition files, events and rejected proposals (jonah.go); nil
	// for every other run.
	Jonah *bundleJonah `json:"jonah,omitempty"`
}

// grantedCall is one call approved for the run, its input as canonical JSON.
type grantedCall struct {
	Connection string          `json:"connection"`
	Tool       string          `json:"tool"`
	Input      json.RawMessage `json:"input"`
}

// bundleRule is one factory rule of the run: its name, the hash of its rule file in the run's
// definition version, its paths (none: every file) and the file the worker writes, its
// frontmatter (description and paths) and body.
type bundleRule struct {
	Name    string          `json:"name"`
	SHA256  domain.BlobHash `json:"sha256"`
	Paths   []string        `json:"paths,omitempty"`
	Content string          `json:"content"`
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
	// ToolModes are the connection's tools that ask first or are off for the run's agent (ADR
	// 0031 decision 3); a tool not in it is allow.
	ToolModes map[string]domain.ConnectionToolMode `json:"tool_modes"`
	// AlwaysAllowOffered are the tools of ToolModes, sorted, whose ask comes from the
	// organization connection's mode, so a person may answer it with Always allow.
	AlwaysAllowOffered []string `json:"always_allow_offered"`
}

// bundleRepository is the factory's repository and the run's branch, which prepare checks out:
// the remote branch when it exists, otherwise a new one from Base. Base is the branch the run
// builds on and opens its pull request into, an assembly's integration branch for its parts in
// integration mode branch (ADR 0033), and empty for the default branch. FreshBase is how prepare
// brings an existing branch up to date with the default branch (rebase or merge).
type bundleRepository struct {
	URL       string           `json:"url"`
	Branch    string           `json:"branch"`
	Base      string           `json:"base,omitempty"`
	FreshBase domain.FreshBase `json:"fresh_base,omitempty"`
}

// gitCredential is a login git uses for https://<Host>: Username and the value of the variable
// PasswordEnv, read when git asks, so the password is never written to disk.
type gitCredential struct {
	Host        string `json:"host"`
	Username    string `json:"username"`
	PasswordEnv string `json:"password_env"`
}

// ServeRun serves GET /run: the bundle of the run whose task token the request carries, from
// the definition version the run started on and the item it works on, or with notes_after the
// notes left since the run started (serveNotes). A token that does not verify is 401; one whose
// factory, agent, station or item the version or the organization does not have is 403. A jonah
// token is served a Jonah review's bundle (jonahBundle), and 503 without Deps.Jonah.
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

	if r.URL.Query().Has(notesAfterParam) {
		g.serveNotes(ctx, w, r, claims)
		return
	}

	b, err := g.bundleOf(ctx, claims)
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

// bundleOf is the bundle of the run c names: a Jonah review's for a TaskKindJonah token.
func (g *Gateway) bundleOf(ctx context.Context, c domain.TaskClaims) (runBundle, error) {
	if c.Kind == domain.TaskKindJonah {
		return g.jonahBundle(ctx, c)
	}

	return g.bundle(ctx, c)
}

// taskRun is what a task token names, checked against its definition version and organization.
type taskRun struct {
	line    domain.Line
	agent   domain.AgentSpec
	station domain.Station
	item    domain.Item
}

// run checks what c names: its version must be one of its factory's and declare its agent and
// station, and its item must be of that factory in the token's organization (ctx). Anything else
// wraps errForbidden or ports.ErrNotFound.
func (g *Gateway) run(ctx context.Context, c domain.TaskClaims) (taskRun, error) {
	f, err := g.defs.Version(ctx, c.VersionID)
	if err != nil {
		return taskRun{}, fmt.Errorf("load factory %s version %s: %w", c.FactoryID, c.VersionID, err)
	}

	if f.Metadata.ID != string(c.FactoryID) {
		return taskRun{}, fmt.Errorf("%w: version %s is not a version of factory %s", errForbidden, c.VersionID, c.FactoryID)
	}

	line := f.Line()

	agent, ok := line.Agent(c.Agent)
	if !ok {
		return taskRun{}, fmt.Errorf("%w: agent %q is not in factory %s", errForbidden, c.Agent, c.FactoryID)
	}

	station, ok := line.Station(c.Station)
	if !ok {
		return taskRun{}, fmt.Errorf("%w: station %q is not in factory %s", errForbidden, c.Station, c.FactoryID)
	}

	it, err := g.item(ctx, c)
	if err != nil {
		return taskRun{}, err
	}

	return taskRun{line: line, agent: agent, station: station, item: it}, nil
}

func (g *Gateway) bundle(ctx context.Context, c domain.TaskClaims) (runBundle, error) {
	run, err := g.run(ctx, c)
	if err != nil {
		return runBundle{}, err
	}

	line, agent, station, it := run.line, run.agent, run.station, run.item

	agentFiles, err := g.defs.AgentFiles(ctx, c.VersionID, agent, station.Output)
	if err != nil {
		return runBundle{}, fmt.Errorf("agent %s files: %w", agent.Name, err)
	}

	maxTurns, maxUSD := agent.Limits(line.Runtime)
	b := runBundle{
		Spec: bundleSpec{
			FactoryID: c.FactoryID, ItemID: it.ID, RunID: c.RunID, Station: station.ID, Agent: agent.Name,
			// The engine chose the model and read the denied commands from the organization policy at
			// run start (ADR 0024); a token from before the policy names neither.
			Model: cmp.Or(c.Model, agent.Model, line.Runtime.Model), Skills: make([]string, 0, len(agent.Skills)),
			ToolsAllow: nonNil(agent.ToolsAllow), ToolsDeny: withDenied(agent.ToolsDeny, c.DeniedCommands), Purpose: line.Purpose,
			Brief: it.Work(), Inputs: it.Outputs, Output: station.Output, MaxTurns: maxTurns, MaxUSD: maxUSD,
			Connections:      []bundleConnection{},
			KnowledgeSources: g.searchable(line),
			Rules:            []bundleRule{},
			GrantedCalls:     []grantedCall{},
		},
		Files: make(map[string][]byte, len(agentFiles)),
		Git:   []gitCredential{},
		Tools: append([]string{}, line.Runtime.Tools...),
	}

	for _, sk := range agent.Skills {
		b.Spec.Skills = append(b.Spec.Skills, sk.Name)
	}

	if err := forPart(it, agentFiles); err != nil {
		return runBundle{}, fmt.Errorf("agent %s output schema: %w", agent.Name, err)
	}

	for p, body := range agentFiles {
		b.Files[path.Join("agent", p)] = body
	}

	if err := g.addConnections(ctx, c, line, agent, &b); err != nil {
		return runBundle{}, err
	}

	// The engine decided the run's branch and base at run start; a token from before names neither.
	branch, base := it.Branch(), ""
	if c.Branch != "" {
		branch, base = c.Branch, c.Base
	}

	if err := g.addRepository(ctx, line.Runtime.Repository, branch, base, line.Flow.Assemblies.FreshBase, &b); err != nil {
		return runBundle{}, err
	}

	if err := g.addRules(ctx, c, it, &b); err != nil {
		return runBundle{}, err
	}

	if err := g.addGrantedCalls(ctx, c, &b); err != nil {
		return runBundle{}, err
	}

	return b, nil
}

// addGrantedCalls puts the calls approved for the run in b (domain.GrantedCalls).
func (g *Gateway) addGrantedCalls(ctx context.Context, c domain.TaskClaims, b *runBundle) error {
	events, err := g.itemEvents(ctx, c.ItemID)
	if err != nil {
		return err
	}

	for _, call := range domain.GrantedCalls(events, c.RunID) {
		b.Spec.GrantedCalls = append(b.Spec.GrantedCalls, grantedCall{Connection: call.Connection, Tool: call.Tool, Input: call.Input})
	}

	return nil
}

// itemEvents are all of item id's events, in append order.
func (g *Gateway) itemEvents(ctx context.Context, id domain.ItemID) ([]domain.Event, error) {
	var all []domain.Event

	for after := int64(0); ; {
		events, err := g.events.ItemEvents(ctx, id, after, eventPage)
		if err != nil {
			return nil, fmt.Errorf("events of item %s: %w", id, err)
		}

		all = append(all, events...)

		if len(events) < eventPage {
			return all, nil
		}

		after = events[len(events)-1].ID
	}
}

// addRules puts the factory rules that apply to the run in b, matched by its agent, station and
// item's lane and work type, and records them.
func (g *Gateway) addRules(ctx context.Context, c domain.TaskClaims, it domain.Item, b *runBundle) error {
	rules, err := g.defs.Rules(ctx, c.VersionID, domain.RuleTarget{Agent: c.Agent, Station: c.Station, Lane: it.Lane, WorkType: it.Type})
	if err != nil {
		return fmt.Errorf("rules of agent %s: %w", c.Agent, err)
	}

	for _, r := range rules {
		b.Spec.Rules = append(b.Spec.Rules, bundleRule{Name: r.Name, SHA256: r.SHA256, Paths: r.Paths, Content: r.Content})
	}

	return g.recordRules(ctx, c, rules)
}

// eventPage is how many of an item's events recordRules reads at a time.
const eventPage = 500

// recordRules appends the run's EventRulesApplied with the name and hash of each of its rules,
// unless no rule applies or the run has one already: a bundle fetched again records nothing.
func (g *Gateway) recordRules(ctx context.Context, c domain.TaskClaims, rules []domain.RunRule) error {
	if len(rules) == 0 {
		return nil
	}

	for after := int64(0); ; {
		events, err := g.events.ItemEvents(ctx, c.ItemID, after, eventPage)
		if err != nil {
			return fmt.Errorf("events of item %s: %w", c.ItemID, err)
		}

		if slices.ContainsFunc(events, func(e domain.Event) bool { return e.Kind == domain.EventRulesApplied && e.RunID == c.RunID }) {
			return nil
		}

		if len(events) < eventPage {
			break
		}

		after = events[len(events)-1].ID
	}

	applied := make([]any, 0, len(rules))
	for _, r := range rules {
		applied = append(applied, map[string]any{"name": r.Name, "sha256": string(r.SHA256)})
	}

	err := g.events.AppendEvents(ctx, domain.Event{
		FactoryID: c.FactoryID, ItemID: c.ItemID, RunID: c.RunID, At: g.clock.Now(), Kind: domain.EventRulesApplied,
		Station: c.Station, Actor: c.Agent, Data: map[string]any{"rules": applied},
	})
	if err != nil {
		return fmt.Errorf("record the rules of run %s: %w", c.RunID, err)
	}

	return nil
}

// item is the item the run works on, which must be of the run's factory.
func (g *Gateway) item(ctx context.Context, c domain.TaskClaims) (domain.Item, error) {
	it, err := g.items.Item(ctx, c.ItemID)
	if err != nil {
		return domain.Item{}, fmt.Errorf("item %s: %w", c.ItemID, err)
	}

	if it.FactoryID != c.FactoryID {
		return domain.Item{}, fmt.Errorf("%w: item %s is not in factory %s", errForbidden, c.ItemID, c.FactoryID)
	}

	return it, nil
}

// outputSchemaFile is the agent file that holds the station contract's JSON Schema.
const outputSchemaFile = "output.schema.json"

// forPart drops the plan from the output schema in files when it is a part's: a part never plans
// an assembly (ADR 0012), so its structured output cannot hold one.
func forPart(it domain.Item, files map[string][]byte) error {
	schema, ok := files[outputSchemaFile]
	if !ok || it.ParentID == "" {
		return nil
	}

	var err error

	files[outputSchemaFile], err = withoutPlan(schema)

	return err
}

// withoutPlan is schema without its plan property and plan's entry in required; schema as it is
// when it has no plan property.
func withoutPlan(schema []byte) ([]byte, error) {
	var s map[string]json.RawMessage
	if err := json.Unmarshal(schema, &s); err != nil {
		return nil, fmt.Errorf("not a JSON object: %w", err)
	}

	raw, ok := s["properties"]
	if !ok {
		return schema, nil
	}

	var props map[string]json.RawMessage
	if err := json.Unmarshal(raw, &props); err != nil {
		return nil, fmt.Errorf("properties: %w", err)
	}

	if _, ok := props[domain.PlanField]; !ok {
		return schema, nil
	}

	delete(props, domain.PlanField)

	var err error
	if s["properties"], err = json.Marshal(props); err != nil {
		return nil, err
	}

	if raw, ok := s["required"]; ok {
		var required []string
		if err := json.Unmarshal(raw, &required); err != nil {
			return nil, fmt.Errorf("required: %w", err)
		}

		if s["required"], err = json.Marshal(slices.DeleteFunc(required, func(r string) bool { return r == domain.PlanField })); err != nil {
			return nil, err
		}
	}

	return json.Marshal(s)
}

// addRepository puts the repository in b with the git login of its connection, which clones it;
// a public repository needs none.
func (g *Gateway) addRepository(
	ctx context.Context, repo domain.Repository, branch, base string, fresh domain.FreshBase, b *runBundle,
) error {
	if repo.URL == "" {
		return nil
	}

	b.Repository = &bundleRepository{URL: repo.URL, Branch: branch, Base: base, FreshBase: fresh}

	if repo.Connection == "" {
		return nil
	}

	_, o, _, err := g.outline(ctx, repo.Connection)
	if err != nil {
		return err
	}

	if o.Git != nil {
		b.addGit(gitCredentialOf(*o.Git))
	}

	return nil
}

// addConnections describes the agent's organization connections that its token grants in b, with
// their git logins, CLI tools and the effective modes of their tools for the agent. The model
// connection gives the agent's env only, so it is not described.
func (g *Gateway) addConnections(ctx context.Context, c domain.TaskClaims, line domain.Line, agent domain.AgentSpec, b *runBundle) error {
	var rules standingRules

	for _, name := range agent.Connections {
		if !slices.Contains(line.Connections, name) || !slices.Contains(c.Connections, name) {
			continue // one the factory does not list, or the token does not grant
		}

		conn, o, k, err := g.outline(ctx, name)
		if err != nil {
			return err
		}

		active, err := rules.of(ctx, g.connections, conn)
		if err != nil {
			return err
		}

		modes := domain.EffectiveToolModes(conn.ToolModes, active, conn.ID, agent.Name)
		b.Spec.Connections = append(b.Spec.Connections, bundleConnection{
			Name: name, Kind: k.Name, Title: k.Title, Env: nonNil(o.Env), MCP: o.MCP, Docs: k.Spec.Docs,
			ToolModes: modes.Modes, AlwaysAllowOffered: modes.AlwaysAllowOffered,
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

// standingRules are the organization's active standing rules, read once for a bundle and only
// when one of its connections has a tool that asks first.
type standingRules struct {
	rules  []domain.StandingRule
	loaded bool
}

// of returns the active rules when conn has a tool that asks first, and none otherwise.
func (s *standingRules) of(ctx context.Context, connections Connections, conn domain.Connection) ([]domain.StandingRule, error) {
	if s.loaded || !slices.Contains(slices.Collect(maps.Values(conn.ToolModes)), domain.ToolModeAsk) {
		return s.rules, nil
	}

	rules, err := connections.StandingRules(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("standing rules: %w", err)
	}

	s.rules, s.loaded = rules, true

	return s.rules, nil
}

// outline is the connection name of the organization in ctx, and it as its kind renders it
// without any secret value.
func (g *Gateway) outline(ctx context.Context, name string) (domain.Connection, domain.ConnectionOutline, domain.ConnectionKind, error) {
	conn, err := g.connections.Get(ctx, name)
	if err != nil {
		return domain.Connection{}, domain.ConnectionOutline{}, domain.ConnectionKind{}, fmt.Errorf("connection %s: %w", name, err)
	}

	k, err := g.connections.Kind(ctx, conn.Kind)
	if err != nil {
		return domain.Connection{}, domain.ConnectionOutline{}, domain.ConnectionKind{}, fmt.Errorf("connection %s: kind %s: %w", name, conn.Kind, err)
	}

	return conn, k.Outline(conn), k, nil
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

// withDenied is the agent's deny rules followed by the organization's denied commands it does not
// already have, never nil.
func withDenied(agent, org []string) []string {
	out := append([]string{}, agent...)

	for _, d := range org {
		if !slices.Contains(out, d) {
			out = append(out, d)
		}
	}

	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}

	return s
}
