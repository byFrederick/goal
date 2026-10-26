import { describe, expect, it } from 'vitest';
import { agentSentence, blockedSentence, reasonShare } from './sentences';

describe('agentSentence', () => {
  it('says how many copies run where, whether it is the constraint, and on which model', () => {
    expect(
      agentSentence({ stations: [{ id: 'investigate', slots: 3, constraint: true }], model: 'claude-sonnet-5-5' }),
    ).toBe('Three copies run at Investigate, the current constraint, on claude-sonnet-5-5.');
    expect(agentSentence({ stations: [{ id: 'build', slots: 1, constraint: false }], model: null })).toBe(
      'One copy runs at Build.',
    );
    expect(agentSentence({ stations: [{ id: 'build', slots: 12, constraint: false }], model: 'm' })).toBe(
      '12 copies run at Build, on m.',
    );
  });

  it('names every station of an agent that works at several, and says when it works at none', () => {
    expect(
      agentSentence({
        stations: [
          { id: 'spec', slots: 1, constraint: false },
          { id: 'review', slots: 2, constraint: true },
        ],
        model: 'm',
      }),
    ).toBe('Works at Spec and Review, the current constraint, on m.');
    expect(agentSentence({ stations: [], model: null })).toBe('No station of the line uses this agent.');
  });
});

describe('blockedSentence', () => {
  it('counts the calls its policy blocked and names the tool when it is always the same', () => {
    expect(blockedSentence({ count: 0, tools: [] })).toBe('Blocked this week: no calls.');
    expect(blockedSentence({ count: 1, tools: [{ tool: 'Bash', count: 1 }] })).toBe('Blocked this week: 1 call, Bash.');
    expect(blockedSentence({ count: 2, tools: [{ tool: 'aws ssm put-parameter', count: 2 }] })).toBe(
      'Blocked this week: 2 calls, both aws ssm put-parameter.',
    );
    expect(blockedSentence({ count: 4, tools: [{ tool: 'Bash', count: 4 }] })).toBe(
      'Blocked this week: 4 calls, all Bash.',
    );
    expect(
      blockedSentence({
        count: 5,
        tools: [
          { tool: 'Bash', count: 3 },
          { tool: 'github__merge_pr', count: 2 },
        ],
      }),
    ).toBe('Blocked this week: 5 calls, most often Bash (3).');
  });
});

describe('reasonShare', () => {
  it('sizes each reason against the most frequent one', () => {
    const reasons = [
      { label: 'a', count: 4 },
      { label: 'b', count: 1 },
    ];
    expect(reasonShare({ count: 4 }, reasons)).toBe(1);
    expect(reasonShare({ count: 1 }, reasons)).toBe(0.25);
  });
});
