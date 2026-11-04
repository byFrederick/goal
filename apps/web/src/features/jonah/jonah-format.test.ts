import { describe, expect, it } from 'vitest';
import {
  addedFacts,
  durationText,
  estimateDelta,
  itemReason,
  itemVerdict,
  latestTry,
  observationChip,
  proposalKind,
  watchText,
} from './jonah-format';

describe('estimateDelta', () => {
  it('says a falling rate in points, and good', () => {
    expect(
      estimateDelta({ metric: 'sent_back_rate', station: 'frontend', before: 0.18, after: 0.1, cost_per_item_usd: 0 }),
    ).toEqual({
      text: '−8 pts',
      good: true,
    });
  });
  it('says a rising lead time in hours, and not good', () => {
    expect(
      estimateDelta({ metric: 'lead_time_p85', station: 'frontend', before: 31, after: 36, cost_per_item_usd: 0 }),
    ).toEqual({
      text: '+5 h',
      good: false,
    });
  });
});

describe('observationChip', () => {
  it('says how far a watched cause has come, Proposed once one addresses it, and Not Jonah’s', () => {
    expect(observationChip('watching', 3, 5)).toBe('Watching, 3 of 5');
    expect(observationChip('proposed', 5, 5)).toBe('Proposed');
    expect(observationChip('not_jonahs', 2, 5)).toBe("Not Jonah's");
  });
});

describe('proposalKind', () => {
  it('tells memory by its path and a model proposal by its items carrying a line result', () => {
    expect(proposalKind({ paths: ['memory.yaml'] })).toBe('memory');
    expect(proposalKind({ paths: ['rules/a.md'] })).toBe('definition');
    const item = { item_ref: 'x#1', item_title: 't', station: 's', tries: [] };
    expect(
      proposalKind({
        paths: ['factory.yaml'],
        tried: {
          state: 'done',
          items: [{ ...item, line_result: 'passed first time, $0.42' }],
          cost_usd: 0,
          kept_tests: [],
        },
      }),
    ).toBe('model');
    expect(
      proposalKind({ paths: ['factory.yaml'], tried: { state: 'done', items: [item], cost_usd: 0, kept_tests: [] } }),
    ).toBe('definition');
  });
});

describe('addedFacts', () => {
  it('lists the facts the content has and the base lacks, with the refs they come from', () => {
    const base = '- text: One.\n  from: [a#1]\n  added: 2026-11-01\n';
    const content = `${base}- text: Two.\n  from: [a#2, a#3]\n  added: 2026-11-02\n`;
    expect(addedFacts(base, content)).toEqual([{ text: 'Two.', from: ['a#2', 'a#3'] }]);
  });
});

describe('durationText', () => {
  it('says seconds, minutes and hours', () => {
    expect(durationText(30)).toBe('30 s');
    expect(durationText(41 * 60)).toBe('41 min');
    expect(durationText(3900)).toBe('1 h 05 min');
  });
});

describe('watchText', () => {
  it('writes a lead time in hours and a cost in dollars', () => {
    const side = (value: number) => ({ items: 5, value });
    const base = { station: 'architect', interval: null, verdict: 'worse', opened_at: '', closed_at: null } as const;
    expect(watchText({ ...base, metric: 'lead_time_p85', before: side(31 * 3600), after: side(34 * 3600) })).toBe(
      'Lead time p85 at architect: 31 h → 34 h',
    );
    expect(watchText({ ...base, metric: 'cost_per_item', before: side(2.38), after: side(1.91) })).toBe(
      'Cost per item at architect: $2.38 → $1.91',
    );
  });
});

describe('an item with more than one try', () => {
  const item = {
    item_ref: 'software#511',
    item_title: 'Board Done cards overflow',
    station: 'frontend',
    tries: [
      { attempt: 1, state: 'done' as const, verdict: 'not_fixed' as const, reason: 'Still overflows.', cost_usd: 1 },
      { attempt: 2, state: 'done' as const, verdict: 'fixed' as const, cost_usd: 1 },
    ],
  };
  it('reads its newest attempt, the one with the highest number', () => {
    expect(latestTry(item)?.attempt).toBe(2);
    expect(latestTry({ ...item, tries: [...item.tries].reverse() })?.attempt).toBe(2);
    expect(itemVerdict(item)).toBe('fixed');
    expect(itemReason(item)).toBeUndefined();
  });
});
