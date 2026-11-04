import { describe, expect, it } from 'vitest';
import { estimateDelta, observationChip } from './jonah-format';

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
