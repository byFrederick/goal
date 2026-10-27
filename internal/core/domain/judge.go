package domain

import "slices"

// Judge is a judge of a definition version, read from its evals/judges/<slug>.md (ADR 0025): the
// front matter evals.schema.json describes and, as Rubric, the file's Markdown body. It scores a
// Sample share of the finished production runs of its Agents and never gates them.
type Judge struct {
	// Slug is the judge's file name without .md.
	Slug      string
	Title     string
	Criterion string
	// Agents are the agents whose runs it scores; empty means every agent.
	Agents   []string
	Outcomes []JudgeOutcome
	// PassMark is the lowest score that passes, from 0 to 1.
	PassMark float64
	// Sample is the share of the agents' finished runs it scores, from 0 to 1; 0 pauses it.
	Sample float64
	// Model is the judge run's model; empty means the factory's runtime.model.
	Model string
	// Enabled is the front matter's enabled, true when the file leaves it out.
	Enabled bool
	Rubric  string
}

// JudgeOutcome is one answer a judge may give: its score from 0 to 1, its label and what it means.
type JudgeOutcome struct {
	Score   float64
	Label   string
	Meaning string
}

// Scores reports whether j scores the runs of agent: every agent's when j names none.
func (j Judge) Scores(agent string) bool {
	return len(j.Agents) == 0 || slices.Contains(j.Agents, agent)
}

// Outcome is j's outcome labelled label, matched exactly, and whether j has one.
func (j Judge) Outcome(label string) (JudgeOutcome, bool) {
	i := slices.IndexFunc(j.Outcomes, func(o JudgeOutcome) bool { return o.Label == label })
	if i < 0 {
		return JudgeOutcome{}, false
	}

	return j.Outcomes[i], true
}

// Passes reports whether score reaches j's pass mark.
func (j Judge) Passes(score float64) bool { return score >= j.PassMark }
