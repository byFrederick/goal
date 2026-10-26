package domain

import "slices"

// Rule is a factory rule (ADR 0014): always-on guidance the factory gives the agents its
// AppliesTo targets, a rules/<name>.md file of the definition declared in spec.rules.
type Rule struct {
	Name string
	// Description is the rule's one line.
	Description string
	AppliesTo   RuleScope
	// Paths are globs that scope the rule to the files an agent works on, kept as Claude Code's
	// path scoping; none means every file.
	Paths []string
	// Body is the rule's text, the markdown after its frontmatter.
	Body string
}

// RuleScope is the runs a rule applies to. A list that is empty is not given and matches every
// run; a list given matches a run whose value it contains.
type RuleScope struct {
	Agents    []string
	Stations  []string
	Lanes     []string
	WorkTypes []string
}

// RuleTarget is what a run is matched on: its agent, its station and its item's lane and work
// type, "" when the item has none.
type RuleTarget struct {
	Agent    string
	Station  string
	Lane     string
	WorkType string
}

// EffectiveRules are the rules that apply to run, in declaration order: those where every list of
// AppliesTo that is given contains the run's value.
func EffectiveRules(rules []Rule, run RuleTarget) []Rule {
	var out []Rule

	for _, r := range rules {
		if r.AppliesTo.matches(run) {
			out = append(out, r)
		}
	}

	return out
}

func (s RuleScope) matches(run RuleTarget) bool {
	return within(s.Agents, run.Agent) && within(s.Stations, run.Station) &&
		within(s.Lanes, run.Lane) && within(s.WorkTypes, run.WorkType)
}

// within reports whether v is in list, or list is not given.
func within(list []string, v string) bool {
	return len(list) == 0 || slices.Contains(list, v)
}
