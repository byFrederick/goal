package config

import (
	"cmp"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// Line converts a validated factory into the line the engine runs. It reads only the parsed
// definition, never the filesystem: prompt, skill and contract paths stay relative to the
// factory's root, wherever its files are stored.
func (f *Factory) Line() domain.Line {
	s := f.Spec
	l := domain.Line{
		FactoryID: domain.FactoryID(f.Metadata.ID),
		Name:      f.Metadata.Name,
		Purpose:   f.Purpose(),
		Runtime: domain.Runtime{
			Model: s.Runtime.Model, ModelConnection: s.Runtime.ModelConnection, ModelKey: s.Runtime.ModelKey,
			Size: domain.SandboxSize(s.Runtime.Size), Tools: slices.Clone(s.Runtime.Tools),
			BuildCache: s.Runtime.BuildCache,
		},
		Stations: make([]domain.Station, 0, len(s.Board.Stations)),
		Agents:   make([]domain.AgentSpec, 0, len(s.Agents)),
	}

	l.Runtime.Repository, l.Runtime.Repositories = s.Runtime.lineRepositories()

	l.Environments = environments(f.environments)

	for _, c := range s.Connections {
		if !c.Inline() {
			l.Connections = append(l.Connections, c.Name)
		}
	}

	l.Knowledge, l.Memory = f.knowledge()

	for _, r := range s.Rules {
		l.Rules = append(l.Rules, domain.Rule{
			Name: r.Name, Description: r.Description, AppliesTo: r.AppliesTo, Paths: slices.Clone(r.Paths), Body: r.Body,
		})
	}

	l.Removed = slices.Clone(f.removed)

	if b := s.Runtime.Budget; b != nil {
		l.Runtime.MaxTurnsPerTask, l.Runtime.PerItemUSD = b.MaxTurnsPerTask, b.PerItemUSD
		l.Runtime.WeeklyUSD, l.Runtime.OnExhausted = b.WeeklyUSD, domain.BudgetPolicy(b.OnExhausted)
	}

	for _, st := range s.Board.Stations {
		l.Stations = append(l.Stations, station(st))
	}

	sources := make(map[string]string, len(s.Skills))
	for _, sk := range s.Skills {
		sources[sk.Name] = sk.Source
	}

	for _, a := range s.Agents {
		ag := agent(a, sources)
		ag.Code = f.AgentCode(a)
		l.Agents = append(l.Agents, ag)
	}

	var fl Flow
	if s.Board.Flow != nil {
		fl = *s.Board.Flow
	}

	for _, t := range s.Board.Triggers {
		l.Triggers = append(l.Triggers, f.trigger(t))
	}

	l.WorkTypes = slices.Clone(s.Board.WorkTypes)

	l.Flow = flowPolicy(fl)
	if fl.Expedite != nil {
		l.ExpediteTypes = slices.Clone(fl.Expedite.Types)
	}

	l.Jonah = jonahPolicy(s.Learning, s.Runtime.Model)

	return l
}

// environments maps the environment files, every key, to what the engine gives a run of them.
func environments(files []Environment) []domain.Environment {
	var out []domain.Environment

	for _, e := range files {
		s := e.Spec
		env := domain.Environment{
			Name: e.Name, Size: domain.SandboxSize(s.Size), Image: s.Image, Tools: slices.Clone(s.Tools), Setup: slices.Clone(s.Setup),
			Env: maps.Clone(s.Env), Files: maps.Clone(s.Files),
		}

		if s.MCP != nil {
			env.MCP = make(map[string]domain.MCPServer, len(s.MCP))
			for name, m := range s.MCP {
				env.MCP[name] = domain.MCPServer{
					URL: m.URL, Headers: maps.Clone(m.Headers), Command: slices.Clone(m.Command), Env: maps.Clone(m.Env), Tools: maps.Clone(m.Tools),
				}
			}
		}

		out = append(out, env)
	}

	return out
}

// jonahPolicy maps learning.jonah: a key left out takes domain.DefaultJonahPolicy's value, and an
// unset model the runtime's.
func jonahPolicy(learning *Learning, runtimeModel string) domain.JonahPolicy {
	p := domain.DefaultJonahPolicy()
	p.Model = runtimeModel

	if learning == nil || learning.Jonah == nil {
		return p
	}

	j := learning.Jonah

	if j.Enabled != nil {
		p.Enabled = *j.Enabled
	}

	if j.ProposeAfter != nil {
		p.ProposeAfter = *j.ProposeAfter
	}

	if share, ok := percent(j.Share); ok {
		p.Share = share
	}

	if j.Model != "" {
		p.Model = j.Model
	}

	return p
}

// percent reads a percent string such as "5%" or "2.5%" as a fraction; false for anything else.
// The schema's pattern keeps it to 0% to 100%.
func percent(s string) (float64, bool) {
	n, ok := strings.CutSuffix(s, "%")
	if !ok {
		return 0, false
	}

	v, err := strconv.ParseFloat(n, 64)
	if err != nil || v < 0 || v > 100 {
		return 0, false
	}

	return v / 100, true
}

// flowPolicy maps board.flow; an unset release is push, unset assembly settings rebase,
// rework, serialize and materialize, an unset transient retry cap domain.DefaultTransientAttempts
// and unset dependency and output fields map no output field.
func flowPolicy(fl Flow) domain.FlowPolicy {
	p := domain.FlowPolicy{
		Release: domain.ReleasePush, Constraint: fl.Constraint,
		Assemblies: assemblyPolicy(fl.Assemblies),
		Retry:      domain.RetryPolicy{TransientAttempts: domain.DefaultTransientAttempts},
	}
	if r := fl.Retry; r != nil && r.Transient != nil && r.Transient.Attempts > 0 {
		p.Retry.TransientAttempts = r.Transient.Attempts
	}

	if d := fl.Dependencies; d != nil {
		p.Dependencies.After = d.After
		p.Dependencies.Paths = d.Paths
	}

	if o := fl.Outputs; o != nil {
		p.Outputs.FollowUps, p.Outputs.Close, p.Outputs.Repositories = o.FollowUps, o.Close, o.Repositories
	}

	for _, id := range fl.FollowUpsFrom {
		p.FollowUpsFrom = append(p.FollowUpsFrom, domain.FactoryID(id))
	}

	if fl.Release != "" {
		p.Release = domain.ReleasePolicy(fl.Release)
	}

	if fl.Expedite != nil {
		p.ExpediteWIP = fl.Expedite.WIP
	}

	if a := fl.Andon; a != nil {
		p.Andon = &domain.AndonPolicy{ReworkRate: a.ReworkRate, Window: a.Window}
	}

	if t := fl.Targets; t != nil {
		p.Targets = domain.Targets{
			ThroughputPerDay: t.ThroughputPerDay,
			LeadTime:         time.Duration(t.LeadTimeHours * float64(time.Hour)),
			ExpediteLeadTime: time.Duration(t.ExpediteLeadTimeMinutes) * time.Minute,
		}
	}

	return p
}

// assemblyPolicy maps board.flow.assemblies; unset settings are rebase, rework, serialize,
// materialize, no changedSince and integration trunk.
func assemblyPolicy(a *Assemblies) domain.AssemblyPolicy {
	p := domain.AssemblyPolicy{
		FreshBase: domain.FreshBaseRebase, OnConflict: domain.ConflictRework,
		Overlap: domain.OverlapSerialize, Materialize: true,
	}
	if a == nil {
		p.Integration = integrationPolicy(nil)
		return p
	}

	p.Integration = integrationPolicy(a.Integration)

	if a.FreshBase != "" {
		p.FreshBase = domain.FreshBase(a.FreshBase)
	}

	if a.OnConflict != "" {
		p.OnConflict = domain.ConflictPolicy(a.OnConflict)
	}

	if a.Overlap != "" {
		p.Overlap = domain.OverlapPolicy(a.Overlap)
	}

	if a.Materialize != nil {
		p.Materialize = *a.Materialize
	}

	p.ChangedSince = a.ChangedSince

	return p
}

// integrationPolicy maps board.flow.assemblies.integration; unset settings are trunk, onLand,
// no minimum and no landAt.
func integrationPolicy(in *Integration) domain.IntegrationPolicy {
	p := domain.IntegrationPolicy{Mode: domain.IntegrationModeTrunk, Refresh: domain.RefreshOnLand}
	if in == nil {
		return p
	}

	if in.Mode != "" {
		p.Mode = domain.IntegrationMode(in.Mode)
	}

	if in.Refresh != "" {
		p.Refresh = domain.IntegrationRefresh(in.Refresh)
	}

	p.MinParts, p.LandAt = in.MinParts, in.LandAt

	return p
}

// ciWait maps a station's waitFor.ci, a field left out taking its default.
func ciWait(c CIWait) domain.CIWait {
	grace, _ := time.ParseDuration(c.Grace) // the schema refuses one that does not parse
	limit, _ := time.ParseDuration(c.Max)

	return domain.CIWait{
		Grace: cmp.Or(grace, domain.DefaultCIGrace), Max: cmp.Or(limit, domain.DefaultCIMax),
		Rounds: cmp.Or(c.Rounds, domain.DefaultCIRounds),
	}
}

func station(st Station) domain.Station {
	out := domain.Station{
		ID: st.ID, Kind: domain.StationKind(st.Kind), Agent: st.Agent,
		Agents: st.Agents, WIP: st.WIP, Buffer: st.Buffer, Output: st.Output, Action: domain.StationAction(st.Action),
		Merges: domain.MergesPolicy(st.Merges),
	}

	if st.WaitFor != nil && st.WaitFor.CI != nil {
		out.WaitFor = ciWait(*st.WaitFor.CI)
	}

	for lane, r := range st.Route {
		if out.Route == nil {
			out.Route = map[string]string{}
		}

		out.Route[lane] = r.Agent

		if r.Agents > 0 {
			if out.RouteAgents == nil {
				out.RouteAgents = map[string]int{}
			}

			out.RouteAgents[lane] = r.Agents
		}
	}

	for _, c := range st.Context {
		out.Context = append(out.Context, domain.StationContext(c))
	}

	for _, c := range st.Checks { // shorthand command lines decode as command checks
		if c.Kind == CheckCommand && c.Run != "" {
			out.Checks = append(out.Checks, c.Run)
		}
	}

	if st.Gate != nil {
		out.Gate = &domain.Gate{OnReject: st.Gate.OnReject, MaxRework: st.Gate.MaxRework}
	}

	return out
}

// trigger maps one of board.triggers; an unset enabled is enabled. The keys triggers wrote before
// ADR 0040 decision 14 read as its forms: github.check_run.failed as github.check_run with
// conclusion failure, github.issues.labeled as github.issues with action labeled, and
// webhook.<endpoint>[.<event>] as <endpoint>[.<event>]. An event trigger without limits.perHour
// takes domain.DefaultPerHour, and a check without limits.perCheck domain.DefaultCheckPerCheck.
// trigger is t as the engine runs it, its create and check brief files read.
func (f *Factory) trigger(t Trigger) domain.Trigger {
	tr := trigger(t)
	if file, ok := BriefFile(t.Create.Brief); ok {
		tr.Create.Brief, _ = f.briefText(file)
	}

	if file, ok := BriefFile(tr.CheckBrief()); ok {
		tr.Check.Brief, _ = f.briefText(file)
	}

	return tr
}

func trigger(t Trigger) domain.Trigger {
	out := domain.Trigger{
		Name: t.Name, Enabled: t.Enabled == nil || *t.Enabled, On: t.On, Cron: t.Cron, TimeZone: t.TimeZone,
		Create: domain.TriggerCreate{
			Type: t.Create.Type, Station: t.Create.Station, Lane: t.Create.Lane, Expedite: t.Create.Expedite,
			Title: t.Create.Title, Brief: t.Create.Brief, Repositories: slices.Clone(t.Create.Repositories), Trusted: t.Create.Trusted,
		},
	}

	if f := t.Filter; f != nil {
		out.Filter = domain.TriggerFilter{
			Branch: f.Branch, Repo: slices.Clone(f.Repo), Label: f.Label, Action: slices.Clone(f.Action),
			Conclusion: slices.Clone(f.Conclusion), Paths: slices.Clone(f.Paths), Authors: slices.Clone(f.Authors), SkipBots: f.SkipBots,
		}

		for path, vs := range f.Match {
			if out.Filter.Match == nil {
				out.Filter.Match = map[string][]string{}
			}

			out.Filter.Match[path] = slices.Clone(vs)
		}
	}

	switch {
	case t.On == domain.OnCheckRunFailed:
		out.On = "github.check_run"
		out.Filter.Conclusion = append(out.Filter.Conclusion, "failure")
	case t.On == domain.OnIssuesLabeled:
		out.On = "github.issues"
		out.Filter.Action = append(out.Filter.Action, "labeled")
	case strings.HasPrefix(t.On, webhookPrefix):
		out.On = strings.TrimPrefix(t.On, webhookPrefix)
	}

	if o := t.Only; o != nil {
		out.Only = domain.TriggerOnly{FailuresInARow: o.FailuresInARow, Dedupe: o.Dedupe}
		out.Only.ModelAgrees = o.ModelAgrees != nil
	}

	if l := t.Limits; l != nil {
		out.Limits = domain.TriggerLimits{PerDay: l.PerDay, PerHour: l.PerHour, PerCheck: l.PerCheck, PauseOnBudget: l.PauseOnBudget}
	}

	if ch := t.Check; ch != nil {
		timeout, _ := time.ParseDuration(ch.Timeout) // Check refuses one that does not parse
		out.Check = &domain.TriggerCheck{
			Agent: ch.Agent, Brief: ch.Brief, MaxUSD: cmp.Or(ch.MaxUSD, domain.DefaultCheckMaxUSD),
			Timeout: cmp.Or(timeout, domain.DefaultCheckTimeout),
		}
		out.Limits.PerCheck = cmp.Or(out.Limits.PerCheck, domain.DefaultCheckPerCheck)
	}

	if out.On != domain.OnSchedule && out.Limits.PerHour == 0 {
		out.Limits.PerHour = domain.DefaultPerHour
	}

	return out
}

// webhookPrefix starts the on: key of a factory endpoint's trigger as written before ADR 0040.
const webhookPrefix = "webhook."

func agent(a Agent, sources map[string]string) domain.AgentSpec {
	out := domain.AgentSpec{
		Name: a.Name, PromptPath: a.Prompt, Model: a.Model, Connections: slices.Clone(a.Connections), Environment: a.Environment,
		MaxTurns: a.MaxTurns, PerItemUSD: a.PerItemUSD,
	}

	for _, name := range a.Skills {
		out.Skills = append(out.Skills, domain.SkillRef{Name: name, Source: sources[name]})
	}

	switch {
	case a.Builtins != nil:
		out.ToolsAllow, out.ToolsDeny = a.Builtins.rules()
	case a.Tools != nil:
		out.ToolsAllow, out.ToolsDeny = slices.Clone(a.Tools.Allow), slices.Clone(a.Tools.Deny)
	}

	return out
}

// knowledge are the factory's knowledge.sources and its memory.yaml's facts as the line has them.
func (f *Factory) knowledge() ([]domain.KnowledgeEntry, []domain.MemoryFact) {
	var (
		entries []domain.KnowledgeEntry
		facts   []domain.MemoryFact
	)

	if k := f.Spec.Knowledge; k != nil {
		for _, s := range k.Sources {
			entries = append(entries, domain.KnowledgeEntry{Name: s.Name, AppliesTo: s.AppliesTo, ReadWhen: s.ReadWhen})
		}
	}

	for _, m := range f.memory {
		facts = append(facts, domain.MemoryFact{Text: m.Text, From: slices.Clone(m.From)})
	}

	return entries, facts
}

// builtinGroups are the Claude Code tools of each built-in group but run.
var builtinGroups = []struct {
	tools []string
	mode  func(Builtins) string
}{
	{[]string{"Read", "Grep", "Glob"}, func(b Builtins) string { return b.Read }},
	{[]string{"Edit", "Write"}, func(b Builtins) string { return b.Edit }},
	{[]string{"WebFetch", "WebSearch"}, func(b Builtins) string { return b.Web }},
	{[]string{"Task"}, func(b Builtins) string { return b.Subagents }},
}

// rules compiles the groups into the worker's permission rules. An allowed group's tools are
// allowed; any other group's are denied, since the worker grants read-only tools to an agent
// whose allow list is empty and deny rules always win. ask is served as off (Check warns).
// run with commands allows Bash(<command>:*) for each and leaves other commands unapproved,
// which the worker refuses.
func (b Builtins) rules() (allow, deny []string) {
	for _, g := range builtinGroups {
		if g.mode(b) == BuiltinAllowed {
			allow = append(allow, g.tools...)
		} else {
			deny = append(deny, g.tools...)
		}
	}

	switch {
	case b.Run.Mode != BuiltinAllowed:
		deny = append(deny, "Bash")
	case len(b.Run.Commands) == 0:
		allow = append(allow, "Bash")
	default:
		for _, cmd := range b.Run.Commands {
			allow = append(allow, "Bash("+cmd+":*)")
		}
	}

	return allow, deny
}

// lineRepositories are the line's repository and every runtime.repositories entry. The line
// keeps one repository, the default entry, until runs start with an item's repositories
// (ADR 0041); the shorthand keeps its connection and forge.
func (r Runtime) lineRepositories() (domain.Repository, []domain.RepositoryEntry) {
	var repo domain.Repository

	switch short := r.Repository; {
	case short != nil:
		repo = domain.Repository{URL: short.URL, Connection: short.Connection, Token: short.Token, Forge: short.Forge}
	default:
		if e, ok := r.DefaultRepository(); ok {
			repo = domain.Repository{URL: e.URL, Token: e.Token}
		}
	}

	return repo, lineEntries(r.RepositoryEntries())
}

// lineEntries are entries as the line has them.
func lineEntries(entries []RepositoryEntry) []domain.RepositoryEntry {
	var out []domain.RepositoryEntry
	for _, e := range entries {
		out = append(out, domain.RepositoryEntry{
			Name: e.Name, URL: e.URL, Token: e.Token, Access: e.Access, Default: e.Default,
		})
	}

	return out
}
