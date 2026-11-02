import { describe, expect, it } from 'vitest';
import { blockedSentence, reasonLabel, reasonShare } from './sentences';

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

describe('reasonLabel', () => {
  it('upper-cases the first letter and leaves the rest', () => {
    expect(reasonLabel('nothing was delivered. after a fresh `git fetch`…')).toBe(
      'Nothing was delivered. after a fresh `git fetch`…',
    );
  });
  it('keeps only the first line of a label that spans several', () => {
    expect(reasonLabel('missing test\nsecond line')).toBe('Missing test');
  });
  it('returns an empty string for an empty label', () => {
    expect(reasonLabel('')).toBe('');
    expect(reasonLabel('\n  ')).toBe('');
  });
});
