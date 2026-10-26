// The factory rules that can apply to one agent (ADR 0014 §3): a rule applies to a run when every
// list its appliesTo gives contains the run's value, so for an agent an `agents` list that does not
// name it excludes the rule, and a `stations` list keeps it only at the agent's workplaces in that
// list. `lanes` and `workTypes` depend on the item, so they make the rule conditional rather than
// deciding it here. A rule with no appliesTo applies to every agent. `paths` are not computed: they
// are Claude Code's file scoping, shown as part of the rule only.
import type { LineSection } from './model';
import type { RuleScope } from './rule-md';
import { whereAgentWorks } from './where';

export interface RuleForAgent {
  name: string;
  description: string;
  appliesTo?: RuleScope | undefined;
}

export interface EffectiveRule {
  name: string;
  description: string;
  /** True when nothing narrows the rule: it applies to the agent on every run. */
  always: boolean;
  /** The agent's workplaces (their labels) the rule is kept at, when its appliesTo names stations. */
  stations: string[];
  /** The lanes the rule is kept to, as appliesTo names them. */
  lanes: string[];
  /** The work types the rule is kept to, as appliesTo names them. */
  workTypes: string[];
}

/** The rules that can apply to `agent` on `line`, in the order given; a rule that cannot is left out. */
export function effectiveRules(line: LineSection, agent: string, rules: readonly RuleForAgent[]): EffectiveRule[] {
  const places = whereAgentWorks(line, agent);
  const out: EffectiveRule[] = [];

  for (const rule of rules) {
    const scope = rule.appliesTo ?? {};
    if (scope.agents && !scope.agents.includes(agent)) continue;

    let stations: string[] = [];
    if (scope.stations && scope.stations.length > 0) {
      const named = scope.stations;
      stations = places.filter((p) => named.includes(p.station)).map((p) => p.label);
      if (stations.length === 0) continue;
    }

    const lanes = scope.lanes ?? [];
    const workTypes = scope.workTypes ?? [];
    out.push({
      name: rule.name,
      description: rule.description,
      always: stations.length === 0 && lanes.length === 0 && workTypes.length === 0,
      stations,
      lanes,
      workTypes,
    });
  }

  return out;
}
