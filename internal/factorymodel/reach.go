package factorymodel

import (
	"cmp"
	"encoding/json"
	"path"
	"slices"
	"strings"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
)

// StationsReached are the agent stations of to, in line order, that the change from from reaches
// (ADR 0039 decision 14): those where what the station's agent gets differs, its agent file, a
// rule that applies to it, a skill it loads, its contract or its model. A station new in to is
// reached. When from does not load, every agent station of to is; when to does not, none is.
func StationsReached(from, to Snapshot) []string {
	b, _ := config.Check(to.Files)
	if b == nil {
		return nil
	}

	a, _ := config.Check(from.Files)

	var out []string

	for _, st := range b.Spec.Board.Stations {
		agents := stationAgents(st)
		if len(agents) == 0 {
			continue
		}

		if a == nil || stationInputs(a, from.Manifest, st.ID) != stationInputs(b, to.Manifest, st.ID) {
			out = append(out, st.ID)
		}
	}

	return out
}

// stationAgents are the agents st runs, sorted: its agent and its route's.
func stationAgents(st config.Station) []string {
	var out []string
	if st.Agent != "" {
		out = append(out, st.Agent)
	}

	for _, r := range st.Route {
		out = append(out, r.Agent)
	}

	slices.Sort(out)

	return slices.Compact(out)
}

// stationInputs is what station's agents get in f, as one comparable string: each agent's
// entry, effective model and prompt file, the skills it loads with their files, the rules that
// apply to it there, and the station's contract. "" when f has no such station.
func stationInputs(f *config.Factory, m domain.Manifest, station string) string {
	i := slices.IndexFunc(f.Spec.Board.Stations, func(s config.Station) bool { return s.ID == station })
	if i < 0 {
		return ""
	}

	st := f.Spec.Board.Stations[i]

	var b strings.Builder

	add := func(parts ...string) {
		for _, p := range parts {
			b.WriteString(p)
			b.WriteByte(0)
		}

		b.WriteByte('\n')
	}

	add("contract", st.Output, string(m[clean(f.Spec.Contracts[st.Output])]))

	for _, name := range stationAgents(st) {
		j := slices.IndexFunc(f.Spec.Agents, func(a config.Agent) bool { return a.Name == name })
		if j < 0 {
			add("agent", name, "missing")

			continue
		}

		agent := f.Spec.Agents[j]
		entry, _ := json.Marshal(agent)
		add("agent", name, string(entry), cmp.Or(agent.Model, f.Spec.Runtime.Model), string(m[clean(agent.Prompt)]))

		for _, skill := range agent.Skills {
			add(append([]string{"skill", skill}, skillInputs(f, m, skill)...)...)
		}

		for _, r := range f.Spec.Rules {
			if within(r.AppliesTo.Agents, name) && within(r.AppliesTo.Stations, station) {
				add("rule", r.Name, string(m[clean(r.Source)]))
			}
		}
	}

	return b.String()
}

// skillInputs are the source of skill name and, for a local one, every file under it with its
// hash.
func skillInputs(f *config.Factory, m domain.Manifest, name string) []string {
	i := slices.IndexFunc(f.Spec.Skills, func(s config.Skill) bool { return s.Name == name })
	if i < 0 {
		return []string{"missing"}
	}

	s := f.Spec.Skills[i]
	out := []string{s.Source, s.Origin}

	if !s.Local() {
		return out
	}

	dir := clean(s.Source) + "/"

	paths := make([]string, 0)
	for p := range m {
		if strings.HasPrefix(p, dir) {
			paths = append(paths, p)
		}
	}

	slices.Sort(paths)

	for _, p := range paths {
		out = append(out, p, string(m[p]))
	}

	return out
}

// clean is a definition-relative path as the manifest keys it: "./agents/a.md" is agents/a.md.
func clean(p string) string {
	if p == "" {
		return ""
	}

	return path.Clean(strings.TrimPrefix(p, "./"))
}

// within reports whether v is in list, or list is not given, as a rule's scope reads.
func within(list []string, v string) bool { return len(list) == 0 || slices.Contains(list, v) }
