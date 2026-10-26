import { describe, expect, it } from 'vitest';
import type { FactoryOverview } from '../../api/types';
import {
  bannerDetail,
  budgetLine,
  constraintNote,
  headerSentence,
  spendNote,
  throughputDelta,
  touchesTrend,
  waitingBreakdown,
} from './sentences';

const waiting = (
  items: { type: string; count: number }[],
  tools: { connection: string; count: number }[] = [],
): FactoryOverview['waiting'] => ({
  on_you: items.reduce((n, i) => n + i.count, 0) + tools.reduce((n, t) => n + t.count, 0),
  items_by_type: items,
  tool_approvals: tools,
});

describe('headerSentence', () => {
  it('says what waits and how much of the weekly budget is spent', () => {
    expect(headerSentence('Acme', 3, 71.6, 250)).toBe(
      'Acme’s factories. 3 items are waiting on you, and $71.60 of the $250 weekly budget is spent.',
    );
  });

  it('agrees in number with one item', () => {
    expect(headerSentence('Acme', 1, 5, 60)).toBe(
      'Acme’s factories. 1 item is waiting on you, and $5.00 of the $60 weekly budget is spent.',
    );
  });

  it('says nothing waits when nothing does', () => {
    expect(headerSentence('Acme', 0, 18.4, 60.5)).toBe(
      'Acme’s factories. Nothing is waiting on you, and $18.40 of the $60.50 weekly budget is spent.',
    );
  });

  it('gives only the spend when no factory has a budget', () => {
    expect(headerSentence('Example Labs', 0, 1.06, null)).toBe(
      'Example Labs’ factories. Nothing is waiting on you, and $1.06 was spent this week.',
    );
  });
});

describe('waitingBreakdown', () => {
  it('names tool approvals by connection, then items by work type', () => {
    expect(waitingBreakdown(waiting([{ type: 'change', count: 2 }], [{ connection: 'jira', count: 1 }]))).toBe(
      '1 jira approval, 2 changes',
    );
  });

  it('counts items of a factory without work types as items', () => {
    expect(waitingBreakdown(waiting([{ type: '', count: 2 }]))).toBe('2 items');
  });

  it('pluralizes work types as words', () => {
    expect(
      waitingBreakdown(
        waiting([
          { type: 'fix', count: 2 },
          { type: 'policy', count: 3 },
          { type: 'runbook-draft', count: 1 },
        ]),
      ),
    ).toBe('2 fixes, 3 policies, 1 runbook draft');
  });

  it('says nothing waits when nothing does', () => {
    expect(waitingBreakdown(waiting([]))).toBe('Nothing right now');
  });
});

describe('bannerDetail', () => {
  const factory = (name: string, w: FactoryOverview['waiting']) => ({ factory: { name }, waiting: w });

  it('names the one factory that waits, with its breakdown', () => {
    expect(bannerDetail([factory('goal', waiting([])), factory('sre', waiting([{ type: 'change', count: 3 }]))])).toBe(
      'All in sre: 3 changes',
    );
  });

  it('counts by factory when several wait', () => {
    expect(
      bannerDetail([
        factory('goal', waiting([{ type: 'feature', count: 2 }])),
        factory('sre', waiting([{ type: 'change', count: 3 }])),
        factory('support', waiting([])),
      ]),
    ).toBe('goal 2, sre 3');
  });
});

describe('throughputDelta', () => {
  it('signs a rise and a fall against the previous window', () => {
    expect(throughputDelta(1.2, 0.9)).toEqual({ text: '+0.3 vs last week', trend: 'up' });
    expect(throughputDelta(6.4, 7.2)).toEqual({ text: '−0.8 vs last week', trend: 'down' });
  });

  it('says the same when the change rounds to nothing', () => {
    expect(throughputDelta(1.02, 1)).toEqual({ text: 'Same as last week', trend: 'flat' });
  });
});

describe('constraintNote', () => {
  it('gives the busy share once it is measured', () => {
    expect(constraintNote({ station: 'build', busy: 0.876 })).toBe('Busy 88% this week');
    expect(constraintNote({ station: 'build', busy: null })).toBe('Most work per slot now');
    expect(constraintNote(null)).toBe('None right now');
  });
});

describe('spendNote', () => {
  it('describes the budget and its use for the meter', () => {
    expect(spendNote(18.4, 60)).toEqual({ text: 'of $60 budget', label: '$18.40 of $60 weekly budget, 31%' });
    expect(spendNote(72, 60)).toEqual({ text: '$12 over the $60 budget', label: '$72.00 of $60 weekly budget, 120%' });
    expect(spendNote(3, null)).toEqual({ text: 'No budget set', label: null });
  });
});

describe('budgetLine', () => {
  it('ends the phone page with the weekly spend', () => {
    expect(budgetLine('Acme', 71.6, 250)).toBe('$71.60 of Acme’s $250 weekly budget spent');
    expect(budgetLine('Acme', 1.06, null)).toBe('$1.06 spent this week, no budget set');
  });
});

describe('touchesTrend', () => {
  const week = (finished: number, per_item_mean: number) => ({
    finished,
    per_item_mean,
    per_item_median: per_item_mean,
    touchless_rate: 0.3,
  });

  it('is worse when people touch each finished item more than last week', () => {
    expect(touchesTrend({ current: week(12, 1.6), previous: week(9, 1.3) })).toEqual({ text: '+0.3', tone: 'worse' });
    expect(touchesTrend({ current: week(40, 1.4), previous: week(38, 1.7) })).toEqual({ text: '−0.3', tone: 'better' });
    expect(touchesTrend({ current: week(4, 1.02), previous: week(5, 1) })).toEqual({ text: 'Same', tone: 'flat' });
  });

  it('compares nothing when a week finished no item', () => {
    expect(touchesTrend({ current: week(0, 0), previous: week(5, 1) })).toBeNull();
    expect(touchesTrend({ current: week(3, 1), previous: week(0, 0) })).toBeNull();
  });
});
