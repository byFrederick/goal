package jonah

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// ModelStore is what Try another model reads and writes; *postgres.Store and *porttest.Store
// satisfy it, as ports.JonahStore and ports.PastTries.
type ModelStore interface {
	Proposals(ctx context.Context, q ports.ProposalQuery) ([]domain.Proposal, error)
	JonahSpendSince(ctx context.Context, factory domain.FactoryID, since time.Time) (domain.JonahSpend, error)
	AgentDoneItems(ctx context.Context, factory domain.FactoryID, agent string, limit int) ([]domain.AgentItem, error)
	ProposeModel(ctx context.Context, factory domain.FactoryID, version domain.DefinitionVersionID, agent string, n domain.NewProposal,
		tries []domain.NewPastTry, at time.Time) (domain.Proposal, []domain.PastTry, error)
	KeptReads
	RunTargets
}

// ModelDefinitions reads the version a model proposal is made against; *definitions.Cache
// satisfies it.
type ModelDefinitions interface {
	// Line is version id's line; ErrNotFound when the organization has no such version.
	Line(ctx context.Context, id domain.DefinitionVersionID) (domain.Line, error)
	// VersionFiles are every file of version id by its path in the definition.
	VersionFiles(ctx context.Context, id domain.DefinitionVersionID) (map[string][]byte, error)
}

// ModelDeps are the ports Try another model works through. Log may be nil.
type ModelDeps struct {
	Store       ModelStore
	Definitions ModelDefinitions
	// Check checks the factory.yaml the proposal gives (ADR 0039 decision 6).
	Check DefinitionCheck
	// Policies are the organization's policy, whose allowed models the agent's may change to.
	Policies Policies
	Audit    ports.Audit
	Clock    ports.Clock
	Log      *slog.Logger
}

// ModelProposals makes Try another model's proposals (ADR 0039 decision 13), as the person in
// ctx.
type ModelProposals struct {
	d   ModelDeps
	log *slog.Logger
}

// NewModelProposals returns ModelProposals over d.
func NewModelProposals(d ModelDeps) *ModelProposals {
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &ModelProposals{d: d, log: log}
}

// ModelRequest asks to try Model for Agent of Version, the factory's current version: an estimate
// only, or with Confirm the proposal too.
type ModelRequest struct {
	Version domain.DefinitionVersionID
	Agent   string
	Model   string
	Confirm bool
}

// ModelEstimate is what a model proposal will try and cost: the agent's newest finished items and
// the kept tests at its stations (ADR 0039 decision 12), domain.ModelTriesPerItem tries of each,
// priced at each one's original run's cost per try, and what is left of Jonah's budget (ADR 0039
// decision 16).
type ModelEstimate struct {
	Agent         string
	CurrentModel  string
	Model         string
	Items         []domain.AgentItem
	Tries         int
	EstimateUSD   float64
	BudgetLeftUSD float64
	// kept are the kept tests the proposal tries, with the runs their tries rerun.
	kept []keptTarget
}

// modelItemsRead is how many of the agent's done items are read to find domain.MaxPastTryItems
// at stations of the current line that can be tried.
const modelItemsRead = 4 * domain.MaxPastTryItems

// Propose estimates a model proposal of r.Agent and, with r.Confirm, stores it: an ordinary
// proposal, made against r.Version, whose one edit is a replacement in factory.yaml that changes
// agents[r.Agent].model to r.Model, with domain.ModelTriesPerItem queued tries of each of the
// agent's newest finished items (at most domain.MaxPastTryItems), each at the item's station and
// run with the new model. Its evidence is the items with how each went on the line. It supersedes
// the open proposals it overlaps, as Undo does, and is audited.
//
// It refuses with domain.ErrAgentNotFound for an agent the version does not have,
// domain.ErrModelNotAllowed for a model the organization's policy does not allow or the agent's
// model already, domain.ErrAgentNotTriable for an agent that needs MCP servers or secrets,
// domain.ErrNoFinishedItems when no done item has a first run of a claim by the agent at a
// station of the line that can be tried, and domain.ErrLearningBudgetExhausted, with the
// estimate, when the estimate is more than what is left of Jonah's budget. The estimate comes
// with every answer that got that far.
func (m *ModelProposals) Propose(ctx context.Context, f domain.FactoryID, r ModelRequest) (ModelEstimate, domain.Proposal, error) {
	line, err := m.d.Definitions.Line(ctx, r.Version)
	if err != nil {
		return ModelEstimate{}, domain.Proposal{}, fmt.Errorf("model proposal: read version %s: %w", r.Version, err)
	}

	policy, err := m.d.Policies.Get(ctx)
	if err != nil {
		return ModelEstimate{}, domain.Proposal{}, fmt.Errorf("model proposal: read the organization's policy: %w", err)
	}

	est, err := m.estimate(ctx, f, line, policy, r)
	if err != nil || !r.Confirm {
		return est, domain.Proposal{}, err
	}

	p, err := m.propose(ctx, f, line, policy, est)

	return est, p, err
}

// estimate checks r against line and policy and prices it.
func (m *ModelProposals) estimate(ctx context.Context, f domain.FactoryID, line domain.Line, policy domain.OrgPolicy, r ModelRequest,
) (ModelEstimate, error) {
	a, ok := line.Agent(r.Agent)
	if !ok {
		return ModelEstimate{}, fmt.Errorf("model proposal: agent %q: %w", r.Agent, domain.ErrAgentNotFound)
	}

	est := ModelEstimate{Agent: a.Name, CurrentModel: cmp.Or(a.Model, line.Runtime.Model, policy.DefaultModel), Model: r.Model}

	switch {
	case !slices.Contains(policy.AllowedModels, r.Model):
		return ModelEstimate{}, fmt.Errorf("model proposal: model %q is not allowed: %w", r.Model, domain.ErrModelNotAllowed)
	case r.Model == est.CurrentModel:
		return ModelEstimate{}, fmt.Errorf("model proposal: agent %s runs on %s already: %w", a.Name, r.Model, domain.ErrModelNotAllowed)
	case !line.TriableAgent(a.Name):
		return ModelEstimate{}, fmt.Errorf("model proposal: agent %s: %w", a.Name, domain.ErrAgentNotTriable)
	}

	items, err := m.d.Store.AgentDoneItems(ctx, f, a.Name, modelItemsRead)
	if err != nil {
		return ModelEstimate{}, fmt.Errorf("model proposal: read the done items of agent %s: %w", a.Name, err)
	}

	items = slices.DeleteFunc(items, func(it domain.AgentItem) bool { return !stationTriable(line, it.Run.Station) })
	if len(items) == 0 {
		return ModelEstimate{}, fmt.Errorf("model proposal: agent %s: %w", a.Name, domain.ErrNoFinishedItems)
	}

	est.Items = items[:min(len(items), domain.MaxPastTryItems)]

	if est.kept, _, err = keptTargets(ctx, m.d.Store, m.d.Store, f, line, agentStations(line, a.Name)); err != nil {
		return ModelEstimate{}, fmt.Errorf("model proposal: %w", err)
	}

	est.Tries = domain.ModelTriesPerItem * (len(est.Items) + len(est.kept))

	// The estimate sums the run costs as they are, then each answers whole cents.
	for i, it := range est.Items {
		est.EstimateUSD += domain.ModelTriesPerItem * it.Run.Result.CostUSD
		est.Items[i].Run.Result.CostUSD = domain.RoundCents(it.Run.Result.CostUSD)
	}

	for _, k := range est.kept {
		est.EstimateUSD += domain.ModelTriesPerItem * k.run.Result.CostUSD
	}

	spend, err := m.d.Store.JonahSpendSince(ctx, f, m.d.Clock.Now().UTC().Add(-BudgetWindow))
	if err != nil {
		return ModelEstimate{}, fmt.Errorf("model proposal: read spend: %w", err)
	}

	est.EstimateUSD = domain.RoundCents(est.EstimateUSD)
	est.BudgetLeftUSD = line.Jonah.Budget(spend).LeftUSD()

	if est.EstimateUSD > est.BudgetLeftUSD {
		return est, fmt.Errorf("model proposal: the estimate $%.2f is more than the $%.2f left of Jonah's budget: %w", est.EstimateUSD,
			est.BudgetLeftUSD, domain.ErrLearningBudgetExhausted)
	}

	return est, nil
}

// maxModelTitle is the longest title a model proposal's item keeps in its evidence quote.
const maxModelTitle = 200

// propose stores est as a model proposal of line's version, with its tries, and audits it.
func (m *ModelProposals) propose(ctx context.Context, f domain.FactoryID, line domain.Line, policy domain.OrgPolicy, est ModelEstimate,
) (domain.Proposal, error) {
	by, err := actor(ctx)
	if err != nil {
		return domain.Proposal{}, err
	}

	files, err := m.d.Definitions.VersionFiles(ctx, line.VersionID)
	if err != nil {
		return domain.Proposal{}, fmt.Errorf("model proposal: read the files of version %s: %w", line.VersionID, err)
	}

	edits, err := m.modelEdit(files, est, policy.AllowedModels)
	if err != nil {
		return domain.Proposal{}, err
	}

	n := domain.NewProposal{
		Title: truncateRunes("Try "+est.Model+" for "+est.Agent, maxUndoTitle),
		Summary: fmt.Sprintf("Changes agent %s from %s to %s, tried %d times on each of its %d newest finished items.", est.Agent,
			est.CurrentModel, est.Model, domain.ModelTriesPerItem, len(est.Items)),
		Edits: edits,
	}

	var tries []domain.NewPastTry

	for _, it := range est.Items {
		quote := truncateRunes(fmt.Sprintf("#%d %s: %s", it.ItemNumber, truncateRunes(it.ItemTitle, maxModelTitle), it.LineResult()), maxEvidenceQuote)
		n.Evidence = append(n.Evidence, domain.ProposalEvidence{ItemID: it.ItemID, Station: it.Run.Station, Quote: quote})

		for attempt := 1; attempt <= domain.ModelTriesPerItem; attempt++ {
			tries = append(tries, domain.NewPastTry{
				ItemID: it.ItemID, Attempt: attempt, Station: it.Run.Station, OriginalRunID: it.Run.ID, Model: est.Model,
			})
		}
	}

	kept, err := keptModelTries(line, files, edits, est)
	if err != nil {
		return domain.Proposal{}, err
	}

	tries = append(tries, kept...)

	if n.Supersedes, err = m.overlapping(ctx, f, edits, files); err != nil {
		return domain.Proposal{}, err
	}

	now := m.d.Clock.Now().UTC()

	p, queued, err := m.d.Store.ProposeModel(ctx, f, line.VersionID, est.Agent, n, tries, now)
	if err != nil {
		return domain.Proposal{}, fmt.Errorf("model proposal: %w", err)
	}

	if err := m.d.Audit.Append(ctx, domain.AuditEntry{
		At: now, Actor: by, Action: domain.AuditProposalModel, TargetKind: domain.AuditTargetProposal, TargetID: string(p.ID), FactoryID: f,
		Data: map[string]any{"number": p.Number, "agent": est.Agent, "model": est.Model, "tries": len(queued), "estimate_usd": est.EstimateUSD},
	}); err != nil {
		return domain.Proposal{}, fmt.Errorf("audit proposal %d: %w", p.Number, err)
	}

	m.log.InfoContext(ctx, "jonah model proposal", slog.String("factory", string(f)), slog.Int64("proposal", p.Number),
		slog.String("agent", est.Agent), slog.String("model", est.Model), slog.Int("tries", len(queued)))

	return p, nil
}

// keptModelTries are domain.ModelTriesPerItem tries of each of est's kept tests, on est's model
// for a run of its agent, with the content key of files with edits applied.
func keptModelTries(line domain.Line, files map[string][]byte, edits []domain.ProposalEdit, est ModelEstimate) ([]domain.NewPastTry, error) {
	if len(est.kept) == 0 {
		return nil, nil
	}

	edited, err := domain.ApplyEdits(files, edits)
	if err != nil {
		return nil, fmt.Errorf("model proposal: apply the edit: %w", err)
	}

	key := domain.ContentKey(edited)

	var out []domain.NewPastTry

	for _, k := range est.kept {
		model := ""
		if k.run.Agent == est.Agent {
			model = est.Model
		}

		for attempt := 1; attempt <= domain.ModelTriesPerItem; attempt++ {
			out = append(out, k.try(line, key, attempt, model))
		}
	}

	return out, nil
}

// agentStations are the agent stations of l that agent works, as its own or in a route: what a
// change of its model reaches.
func agentStations(l domain.Line, agent string) []string {
	var out []string

	for _, st := range l.Stations {
		if st.Kind != domain.KindAgent {
			continue
		}

		if st.Agent == agent || slices.Contains(slices.Collect(maps.Values(st.Route)), agent) {
			out = append(out, st.ID)
		}
	}

	return out
}

// maxEvidenceQuote is the longest evidence quote a proposal stores (domain.NewProposal.Check).
const maxEvidenceQuote = 500

// modelEdit is the replacement in files' factory.yaml that sets est.Agent's model to est.Model,
// checked as a review's edits are (ADR 0039 decision 6): it applies, changes only paths a proposal
// may change among models, gives a definition with no new blocking problem, and sets the agent's
// model. domain.ErrInvalid when no such replacement can be written.
func (m *ModelProposals) modelEdit(files map[string][]byte, est ModelEstimate, models []string) ([]domain.ProposalEdit, error) {
	raw, ok := files[factoryFile]
	if !ok {
		return nil, fmt.Errorf("model proposal: the version has no %s: %w", factoryFile, domain.ErrInvalid)
	}

	rep, ok := modelReplacement(string(raw), est.Agent, est.Model)
	if !ok {
		return nil, fmt.Errorf("model proposal: the entry of agent %s in %s can't be edited: %w", est.Agent, factoryFile, domain.ErrInvalid)
	}

	edits := []domain.ProposalEdit{{Path: factoryFile, Replacements: []domain.Replacement{rep}}}

	checker := definitionChecker{in: reviewInput{files: files, check: m.d.Check, models: models}}
	if reason, detail := checker.edits(edits); reason != "" {
		return nil, fmt.Errorf("model proposal: %s: %s: %w", reason, detail, domain.ErrInvalid)
	}

	after, err := edits[0].Apply(raw, true)
	if err != nil {
		return nil, fmt.Errorf("model proposal: %w: %w", domain.ErrInvalid, err)
	}

	if got, err := m.agentModel([]byte(after), est.Agent); err != nil || got != est.Model {
		return nil, fmt.Errorf("model proposal: the edit gives agent %s model %q, not %s: %w", est.Agent, got, est.Model, errors.Join(domain.ErrInvalid, err))
	}

	return edits, nil
}

// agentModel is spec.agents[agent].model in factory.yaml's raw content, "" when it has none.
func (m *ModelProposals) agentModel(raw []byte, agent string) (string, error) {
	tree, err := m.d.Check.ParseFactory(raw)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", factoryFile, err)
	}

	root, _ := tree.(map[string]any)
	spec, _ := root["spec"].(map[string]any)
	list, _ := spec["agents"].([]any)

	for _, a := range list {
		if e, ok := a.(map[string]any); ok && e["name"] == agent {
			model, _ := e["model"].(string)

			return model, nil
		}
	}

	return "", nil
}

// overlapping are f's open proposals whose edits overlap edits in files: those a new proposal of
// edits supersedes.
func (m *ModelProposals) overlapping(ctx context.Context, f domain.FactoryID, edits []domain.ProposalEdit, files map[string][]byte,
) ([]domain.ProposalID, error) {
	open, err := m.d.Store.Proposals(ctx, ports.ProposalQuery{Factory: f, State: domain.ProposalProposed, Limit: reviewProposalLimit})
	if err != nil {
		return nil, fmt.Errorf("model proposal: read open proposals: %w", err)
	}

	var out []domain.ProposalID

	for _, o := range open {
		if domain.EditsOverlap(o.Edits, edits, files) {
			out = append(out, o.ID)
		}
	}

	return out, nil
}

// Lines of factory.yaml that modelReplacement reads: an agent entry's name or model key (after
// the entry's "- " when it starts the entry), a key and its indent, and an entry on one line in
// flow style.
var (
	nameLine  = regexp.MustCompile(`^( *)(- +)?name: *["']?([^"'#]*?)["']? *(#.*)?$`)
	modelLine = regexp.MustCompile(`^( *)(- +)?model: *`)
	keyLine   = regexp.MustCompile(`^( *)([A-Za-z_][A-Za-z0-9_]*): *(#.*)?$`)
	flowEntry = regexp.MustCompile(`^( *)- *\{.*\} *(#.*)?$`)
	flowName  = regexp.MustCompile(`[{,] *name: *["']?([^,"'}]+?)["']? *[,}]`)
	flowModel = regexp.MustCompile(`([{,] *model: *)("[^"]*"|'[^']*'|[^,}]*?)( *[,}])`)
	plainYAML = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/@:+-]*$`)
)

// modelReplacement is the replacement of raw, a factory.yaml, that sets the model of agent's entry
// under spec.agents to model, its old text occurring once in raw: the entry's model line changed,
// or a model line added after its name line. false when raw has no such entry this can edit.
func modelReplacement(raw, agent, model string) (domain.Replacement, bool) {
	lines := strings.SplitAfter(raw, "\n")
	value := model

	if !plainYAML.MatchString(model) || strings.Contains(model, ": ") {
		value = strconv.Quote(model)
	}

	for i, l := range lines {
		text := strings.TrimRight(l, "\n")

		if flowEntry.MatchString(text) {
			if rep, ok := flowReplacement(raw, text, agent, value); ok && underAgents(lines, i, len(flowEntry.FindStringSubmatch(text)[1])+2) {
				return rep, true
			}

			continue
		}

		g := nameLine.FindStringSubmatch(text)
		if g == nil || strings.TrimSpace(g[3]) != agent {
			continue
		}

		key := len(g[1]) + len(g[2])
		start := entryStart(lines, i, key, g[2] != "")

		if start < 0 || !underAgents(lines, start, key) {
			continue
		}

		if rep, ok := blockReplacement(raw, lines, start, i, key, value); ok {
			return rep, true
		}
	}

	return domain.Replacement{}, false
}

// flowReplacement is the replacement of entry, one line of raw in flow style, that sets its model
// to value, when it is agent's.
func flowReplacement(raw, entry, agent, value string) (domain.Replacement, bool) {
	g := flowName.FindStringSubmatch(entry)
	if g == nil || strings.TrimSpace(g[1]) != agent || strings.Count(raw, entry) != 1 {
		return domain.Replacement{}, false
	}

	repl := flowModel.ReplaceAllString(entry, "${1}"+strings.ReplaceAll(value, "$", "$$")+"${3}")
	if repl == entry {
		end := strings.LastIndex(entry, "}")
		repl = strings.TrimRight(entry[:end], " ") + ", model: " + value + " " + entry[end:]
	}

	return domain.Replacement{Old: entry, New: repl}, true
}

// entryStart is the line of lines that starts the list entry whose key at column key is on line
// i: i itself when it starts with "- ", else the nearest line above at the entry's indent that
// does; -1 when there is none.
func entryStart(lines []string, i, key int, dash bool) int {
	if dash {
		return i
	}

	for j := i - 1; j >= 0; j-- {
		text := strings.TrimRight(lines[j], "\n")
		if strings.TrimSpace(text) == "" {
			continue
		}

		ind := indent(text)

		switch {
		case ind == key-2 && strings.HasPrefix(text[ind:], "- "):
			return j
		case ind < key:
			return -1
		}
	}

	return -1
}

// underAgents reports whether the list entry starting on line start, its keys at column key, is
// an entry of agents: the nearest key above it at a lower indent is agents.
func underAgents(lines []string, start, key int) bool {
	for j := start - 1; j >= 0; j-- {
		text := strings.TrimRight(lines[j], "\n")
		if strings.TrimSpace(text) == "" || strings.HasPrefix(strings.TrimSpace(text), "#") {
			continue
		}

		if indent(text) < key-2 || (indent(text) == key-2 && !strings.HasPrefix(text[indent(text):], "- ")) {
			g := keyLine.FindStringSubmatch(text)

			return g != nil && g[2] == "agents"
		}
	}

	return false
}

// blockReplacement is the replacement that sets the model of the entry starting on line start,
// its name on line name and its keys at column key, to value: from the name line to the model
// line, or the name line with a model line after it.
func blockReplacement(raw string, lines []string, start, name, key int, value string) (domain.Replacement, bool) {
	end := start + 1
	for end < len(lines) {
		text := strings.TrimRight(lines[end], "\n")
		if strings.TrimSpace(text) != "" && indent(text) < key {
			break
		}

		end++
	}

	span := slices.Clone(lines[start:end])
	lo, hi := name-start, name-start
	set := func() {
		span[lo] = strings.TrimRight(span[lo], "\n") + "\n" + strings.Repeat(" ", key) + "model: " + value + newline(span[lo])
	}

	for m := start; m < end; m++ {
		if g := modelLine.FindStringSubmatch(strings.TrimRight(lines[m], "\n")); g != nil && len(g[1])+len(g[2]) == key {
			lo, hi = min(m, name)-start, max(m, name)-start
			set = func() { span[m-start] = g[0] + value + newline(lines[m]) }

			break
		}
	}

	set()

	// The lines from the name to the model, or the whole entry when they occur more than once.
	if rep, ok := unique(raw, strings.Join(lines[start+lo:start+hi+1], ""), strings.Join(span[lo:hi+1], "")); ok {
		return rep, true
	}

	return unique(raw, strings.Join(lines[start:end], ""), strings.Join(span, ""))
}

// unique is the replacement of old by repl when old occurs once in raw.
func unique(raw, old, repl string) (domain.Replacement, bool) {
	if strings.Count(raw, old) != 1 {
		return domain.Replacement{}, false
	}

	return domain.Replacement{Old: old, New: repl}, true
}

// indent is the number of spaces text starts with.
func indent(text string) int { return len(text) - len(strings.TrimLeft(text, " ")) }

// newline is line's newline: "\n", or "" for a last line without one.
func newline(line string) string {
	if strings.HasSuffix(line, "\n") {
		return "\n"
	}

	return ""
}
