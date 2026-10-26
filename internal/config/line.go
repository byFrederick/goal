package config

import (
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/domain"
)

// Line converts a validated factory into the line the engine runs. It reads only the parsed
// definition, never the filesystem: prompt, skill and contract paths stay relative to the
// factory's root, wherever its files are stored.
func (f *Factory) Line() domain.Line {
	s := f.Spec
	l := domain.Line{
		FactoryID: domain.FactoryID(f.Metadata.ID),
		Purpose:   f.Purpose(),
		Runtime: domain.Runtime{
			Model: s.Runtime.Model, ModelConnection: s.Runtime.ModelConnection,
			Size: domain.SandboxSize(s.Runtime.Size), Tools: slices.Clone(s.Runtime.Tools),
		},
		Stations: make([]domain.Station, 0, len(s.Board.Stations)),
		Agents:   make([]domain.AgentSpec, 0, len(s.Agents)),
	}

	if r := s.Runtime.Repository; r != nil {
		l.Runtime.Repository = domain.Repository{URL: r.URL, Connection: r.Connection}
	}

	for _, c := range s.Connections {
		if !c.Inline() {
			l.Connections = append(l.Connections, c.Name)
		}
	}

	if s.Knowledge != nil {
		l.KnowledgeSources = slices.Clone(s.Knowledge.Sources)
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
		l.Agents = append(l.Agents, agent(a, sources))
	}

	var fl Flow
	if s.Board.Flow != nil {
		fl = *s.Board.Flow
	}

	l.Flow = flowPolicy(fl)
	if fl.Expedite != nil {
		l.ExpediteTypes = slices.Clone(fl.Expedite.Types)
	}

	return l
}

// flowPolicy maps board.flow; an unset release is push, unset assembly settings rebase,
// rework, serialize and materialize, an unset transient retry cap domain.DefaultTransientAttempts.
func flowPolicy(fl Flow) domain.FlowPolicy {
	p := domain.FlowPolicy{
		Release: domain.ReleasePush, Constraint: fl.Constraint,
		Assemblies: domain.AssemblyPolicy{
			FreshBase: domain.FreshBaseRebase, OnConflict: domain.ConflictRework,
			Overlap: domain.OverlapSerialize, Materialize: true,
		},
		Retry: domain.RetryPolicy{TransientAttempts: domain.DefaultTransientAttempts},
	}
	if r := fl.Retry; r != nil && r.Transient != nil && r.Transient.Attempts > 0 {
		p.Retry.TransientAttempts = r.Transient.Attempts
	}

	if a := fl.Assemblies; a != nil {
		if a.FreshBase != "" {
			p.Assemblies.FreshBase = domain.FreshBase(a.FreshBase)
		}

		if a.OnConflict != "" {
			p.Assemblies.OnConflict = domain.ConflictPolicy(a.OnConflict)
		}

		if a.Overlap != "" {
			p.Assemblies.Overlap = domain.OverlapPolicy(a.Overlap)
		}

		if a.Materialize != nil {
			p.Assemblies.Materialize = *a.Materialize
		}
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

func station(st Station) domain.Station {
	out := domain.Station{
		ID: st.ID, Kind: domain.StationKind(st.Kind), Agent: st.Agent,
		Agents: st.Agents, WIP: st.WIP, Buffer: st.Buffer, Output: st.Output, Action: domain.StationAction(st.Action),
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

func agent(a Agent, sources map[string]string) domain.AgentSpec {
	out := domain.AgentSpec{
		Name: a.Name, PromptPath: a.Prompt, Model: a.Model, Connections: slices.Clone(a.Connections),
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
