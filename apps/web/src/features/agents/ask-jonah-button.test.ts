import { describe, expect, it } from 'vitest';
import type { AgentStats } from '../../api/types';
import { fixRequestText, requestTextLimit } from './ask-jonah-button';

const stats = (reasons: AgentStats['reasons']): AgentStats =>
  ({
    agent: 'frontend-engineer',
    from: '2026-10-20',
    to: '2026-10-26',
    model: 'claude-haiku-5-5',
    stations: [],
    tasks_finished: 41,
    sent_back: { count: 9, ratio: 0.22 },
    median_turns: 46,
    spend_per_task_usd: 1.9,
    task_time_p85_minutes: 38,
    spend_usd: 77.9,
    reasons,
    denied_tool_calls: { count: 0, tools: [] },
  }) as AgentStats;

describe('fixRequestText', () => {
  it('names the agent, the count and every reason in the order review gave them', () => {
    const text = fixRequestText(
      stats([
        { label: 'No test for a new empty or error state', count: 4 },
        { label: 'Icon button without a label', count: 1 },
      ]),
    );
    expect(text).toContain('Fix the agent frontend-engineer');
    expect(text).toContain('review sent 9 of its tasks back');
    expect(text.indexOf('- No test for a new empty or error state (4 times)')).toBeLessThan(
      text.indexOf('- Icon button without a label (1 time)'),
    );
    expect(text.trim()).toBe(text);
  });

  it('keeps the text within goald’s limit by leaving reasons out and saying how many', () => {
    const reasons = Array.from({ length: 80 }, (_, i) => ({
      label: `Reason number ${i} with a long description that takes up room in the request text`,
      count: 1,
    }));
    const text = fixRequestText(stats(reasons));
    expect(text.length).toBeLessThanOrEqual(requestTextLimit);
    expect(text).toMatch(/- and \d+ more/);
    expect(text).toContain('- Reason number 0 ');
    expect(text).not.toContain('- Reason number 79 ');
  });
});
