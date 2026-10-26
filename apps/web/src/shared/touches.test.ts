import { describe, expect, it } from 'vitest';
import type { TouchStats } from '../api/types';
import { headlineText, mostTouched, reasonLabel, stationRates, touchRows } from './touches';

// 45 finished items with 63 touches: 22 planned approvals, the rest people stepping in.
const stats: TouchStats = {
  finished: 45,
  touches: 63,
  planned: 22,
  unplanned: 41,
  touchless_rate: 0.38,
  per_item_mean: 1.4,
  per_item_median: 1,
  planned_per_item: 0.49,
  unplanned_per_item: 0.91,
  by_station: [
    { key: 'approve', touches: 22, planned: 22, unplanned: 0 },
    { key: 'investigate', touches: 17, planned: 0, unplanned: 17 },
    { key: 'review', touches: 11, planned: 3, unplanned: 8 },
    { key: '', touches: 1, planned: 0, unplanned: 1 },
  ],
  by_kind: [{ key: 'gate_rejected', touches: 8, planned: 0, unplanned: 8 }],
  by_reason: [
    { reason: 'missing_knowledge', touches: 12, planned: 0, unplanned: 12 },
    { reason: null, touches: 5, planned: 0, unplanned: 5 },
  ],
  by_version: [
    {
      version_id: 'v9',
      number: 9,
      finished: 30,
      touches: 36,
      touchless_rate: 0.4,
      per_item_mean: 1.2,
      planned_per_item: 0.5,
      unplanned_per_item: 0.7,
    },
    {
      version_id: null,
      number: null,
      finished: 2,
      touches: 0,
      touchless_rate: 1,
      per_item_mean: 0,
      planned_per_item: 0,
      unplanned_per_item: 0,
    },
  ],
  line_stops: 1,
};

const none: TouchStats = { ...stats, finished: 0, touches: 0, by_station: [], by_kind: [], by_reason: [] };

describe('touchRows', () => {
  it('says each station with its share per finished item and what its touches were', () => {
    expect(
      touchRows(stats, 'station').map(({ label, perItem, caption }) => [label, perItem.toFixed(2), caption]),
    ).toEqual([
      ['Approve', '0.49', 'All planned'],
      ['Investigate', '0.38', 'All unplanned'],
      ['Review', '0.24', '3 planned, 8 unplanned'],
      ['No station', '0.02', 'All unplanned'],
    ]);
  });

  it('names kinds, reasons and versions in words', () => {
    expect(touchRows(stats, 'kind')[0]?.label).toBe('Sent back at a gate');
    expect(touchRows(stats, 'reason').map((r) => r.label)).toEqual(['Missing knowledge', 'No reason set']);
    expect(touchRows(stats, 'version').map((r) => [r.label, r.caption])).toEqual([
      ['v9', '30 items, 1.2 per item, 40% touchless'],
      ['No agent run', '2 items, 0.0 per item, 100% touchless'],
    ]);
  });
});

describe('the headline and its sentences', () => {
  it('says nothing per item when no item finished', () => {
    expect(headlineText({ finished: 45, per_item_mean: 1.4, per_item_median: 1, touchless_rate: 0.38 })).toBe(
      '1.4 touches per item, 38% touchless',
    );
    expect(headlineText({ finished: 0, per_item_mean: 0, per_item_median: 0, touchless_rate: 0 })).toBeNull();
    expect(mostTouched(stats)).toBe('Most touches: Approve 22, Investigate 17.');
    expect(mostTouched(none)).toBeNull();
  });

  it('rates stations only over finished items, leaving out touches with no station', () => {
    expect([...stationRates(stats).keys()]).toEqual(['approve', 'investigate', 'review']);
    expect(stationRates(none).size).toBe(0);
    expect(reasonLabel('wrong_decision')).toBe('Wrong decision');
  });
});
