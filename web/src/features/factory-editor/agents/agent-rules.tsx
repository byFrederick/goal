// The factory rules that apply to this agent (ADR 0014 §3), on its page: each rule's name, its
// description, and "Always" or the stations, lanes or work types it is kept to. Computed from the
// draft's line and rules, so an edit to either shows at once. Rules are read for their appliesTo
// through the session, so the list shows what is typed; a rule whose file is missing is left out.
import { useId } from 'react';
import { type EffectiveRule, effectiveRules, type RuleForAgent } from '../../../shared/definition/effective-rules';
import { parseRule } from '../../../shared/definition/rule-md';
import { useSection } from '../draft/use-draft';
import { useRuleTexts } from './use-rule-text';

/** The condition a rule is kept to, as the agent page says it: "Only at Build · lanes backend". */
export function conditionLabel(rule: EffectiveRule): string {
  return [
    rule.stations.length > 0 ? `Only at ${rule.stations.join(', ')}` : undefined,
    rule.lanes.length > 0 ? `lanes ${rule.lanes.join(', ')}` : undefined,
    rule.workTypes.length > 0 ? `work types ${rule.workTypes.join(', ')}` : undefined,
  ]
    .filter(Boolean)
    .join(' · ');
}

export function AgentRules({ agent }: { agent: string }) {
  const agents = useSection('agents');
  const line = useSection('line');
  const headingId = useId();
  const rules = agents.rules ?? [];
  const texts = useRuleTexts(rules.map((r) => r.name));
  const loading = rules.some((r) => texts[r.name] === undefined);

  const forAgent: RuleForAgent[] = rules.flatMap((r) => {
    const text = texts[r.name];
    if (typeof text !== 'string') return [];
    const parsed = parseRule(text);
    return [{ name: r.name, description: parsed.description, appliesTo: parsed.appliesTo }];
  });
  const applying = effectiveRules(line, agent, forAgent);

  return (
    <section
      aria-labelledby={headingId}
      aria-busy={loading}
      className="flex min-w-0 flex-col gap-3 rounded-card border border-line bg-card p-3.5"
    >
      <h4 id={headingId} className="m-0 text-[15px] font-medium">
        Rules
      </h4>
      <p className="m-0 text-[13px] leading-snug text-muted">
        The factory rules this agent follows on every run it works, unlike skills, which it loads on demand.
      </p>
      {loading ? (
        <p className="sr-only">Loading the rules</p>
      ) : applying.length > 0 ? (
        <ul className="m-0 flex list-none flex-col p-0">
          {applying.map((rule) => (
            <li key={rule.name} className="flex flex-col gap-0.5 border-t border-line py-3 first:border-t-0 first:pt-0">
              <span translate="no" className="font-mono text-[13px] break-all">
                {rule.name}
              </span>
              <span className="text-[15px]">{rule.description || 'No description'}</span>
              <span className="text-[13px] text-muted">{rule.always ? 'Always' : conditionLabel(rule)}</span>
            </li>
          ))}
        </ul>
      ) : (
        <p className="m-0 text-[15px] text-muted">No factory rule applies to {agent} yet.</p>
      )}
    </section>
  );
}
