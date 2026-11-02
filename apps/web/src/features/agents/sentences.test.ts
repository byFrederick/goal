import { describe, expect, it } from 'vitest';
import { blockedSentence, reasonShare } from './sentences';

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
