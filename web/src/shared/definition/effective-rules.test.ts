import { describe, expect, it } from 'vitest';
import { effectiveRules, type RuleForAgent } from './effective-rules';
import type { LineSection } from './model';

const line: LineSection = {
  stations: [
    { id: 'investigate', name: 'Investigate', kind: 'agent', agent: 'investigator' },
    { id: 'build', name: 'Build', kind: 'agent', route: { backend: 'engineer', frontend: 'web-engineer' } },
    { id: 'review', name: 'Review', kind: 'agent', agent: 'reviewer' },
    { id: 'done', name: 'Done', kind: 'done' },
  ],
  librarian: 'librarian',
};

const rule = (name: string, appliesTo?: RuleForAgent['appliesTo']): RuleForAgent => ({
  name,
  description: `${name} rule`,
  ...(appliesTo ? { appliesTo } : {}),
});

describe('effectiveRules', () => {
  it('gives a rule with no appliesTo to every agent, always', () => {
    const [found] = effectiveRules(line, 'engineer', [rule('secrets')]);
    expect(found).toEqual({
      name: 'secrets',
      description: 'secrets rule',
      always: true,
      stations: [],
      lanes: [],
      workTypes: [],
    });
  });

  it('keeps a rule whose agents list names the agent, always', () => {
    const rules = [rule('delivery', { agents: ['engineer', 'reviewer'] })];
    expect(effectiveRules(line, 'engineer', rules).map((r) => [r.name, r.always])).toEqual([['delivery', true]]);
  });

  it('leaves out a rule whose agents list does not name the agent', () => {
    const rules = [rule('delivery', { agents: ['reviewer'] })];
    expect(effectiveRules(line, 'engineer', rules)).toEqual([]);
  });

  it('keeps a stations rule as conditional on the one station the agent works at', () => {
    const rules = [rule('tests', { stations: ['build'] })];
    expect(effectiveRules(line, 'engineer', rules)).toEqual([
      { name: 'tests', description: 'tests rule', always: false, stations: ['Build'], lanes: [], workTypes: [] },
    ]);
  });

  it('keeps a stations rule conditional on only the agent’s stations among several listed', () => {
    const rules = [rule('review', { stations: ['build', 'review'] })];
    expect(effectiveRules(line, 'reviewer', rules)[0]).toMatchObject({ always: false, stations: ['Review'] });
    expect(effectiveRules(line, 'engineer', rules)[0]).toMatchObject({ always: false, stations: ['Build'] });
  });

  it('leaves out a stations rule at no station the agent works at', () => {
    const rules = [rule('review-only', { stations: ['review'] })];
    expect(effectiveRules(line, 'engineer', rules)).toEqual([]);
  });

  it('leaves out a stations rule for an agent not on the line', () => {
    const rules = [rule('build-only', { stations: ['build'] })];
    expect(effectiveRules(line, 'nobody', rules)).toEqual([]);
  });

  it('keeps a lanes or workTypes rule as conditional, naming them', () => {
    const rules = [rule('backend', { lanes: ['backend'] }), rule('feature', { workTypes: ['feature'] })];
    expect(effectiveRules(line, 'engineer', rules)).toEqual([
      { name: 'backend', description: 'backend rule', always: false, stations: [], lanes: ['backend'], workTypes: [] },
      { name: 'feature', description: 'feature rule', always: false, stations: [], lanes: [], workTypes: ['feature'] },
    ]);
  });

  it('keeps a lanes rule for an agent not on the line, since it depends on the item', () => {
    const rules = [rule('backend', { lanes: ['backend'] })];
    expect(effectiveRules(line, 'nobody', rules)[0]).toMatchObject({ always: false, lanes: ['backend'] });
  });

  it('combines a station with a lane: the rule stays conditional on both', () => {
    const rules = [rule('tests', { stations: ['build'], lanes: ['backend'] })];
    expect(effectiveRules(line, 'engineer', rules)[0]).toMatchObject({
      always: false,
      stations: ['Build'],
      lanes: ['backend'],
    });
  });

  it('keeps the rules in the order given', () => {
    const rules = [rule('b'), rule('a'), rule('c', { agents: ['reviewer'] })];
    expect(effectiveRules(line, 'engineer', rules).map((r) => r.name)).toEqual(['b', 'a']);
  });
});
