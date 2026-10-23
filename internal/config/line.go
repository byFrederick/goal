package config

import (
	"maps"
	"slices"

	"github.com/byFrederick/goal/internal/domain"
)

// Line converts a validated factory into the line the engine runs. It reads only the parsed
// definition, never the filesystem: prompt, skill and contract paths stay relative to the
// factory's root, wherever its files are stored.
func (f *Factory) Line() domain.Line {
	s := f.Spec
	l := domain.Line{
		FactoryID: domain.FactoryID(f.Metadata.ID),
		Runtime:   domain.Runtime{Image: s.Runtime.Image, Model: s.Runtime.Model},
		Stations:  make([]domain.Station, 0, len(s.Board.Stations)),
		Agents:    make([]domain.AgentSpec, 0, len(s.Agents)),
	}

	if r := s.Runtime.Repository; r != nil {
		l.Runtime.Repository = domain.Repository{URL: r.URL, AuthRef: r.Token}
	}

	if b := s.Runtime.Budget; b != nil {
		l.Runtime.MaxTurnsPerTask, l.Runtime.PerItemUSD = b.MaxTurnsPerTask, b.PerItemUSD
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

	if fl := s.Board.Flow; fl != nil && fl.Expedite != nil {
		l.ExpediteTypes = slices.Clone(fl.Expedite.Types)
	}

	return l
}

func station(st Station) domain.Station {
	out := domain.Station{
		ID: st.ID, Kind: domain.StationKind(st.Kind), Agent: st.Agent, Route: maps.Clone(st.Route),
		Agents: st.Agents, WIP: st.WIP, Output: st.Output,
	}

	for _, c := range st.Checks {
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
	}

	for _, name := range a.Skills {
		out.Skills = append(out.Skills, domain.SkillRef{Name: name, Source: sources[name]})
	}

	if a.Tools != nil {
		out.ToolsAllow, out.ToolsDeny = slices.Clone(a.Tools.Allow), slices.Clone(a.Tools.Deny)
	}

	return out
}
