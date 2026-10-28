import { describe, expect, it } from 'vitest';
import {
  estimateForTarget,
  fullDaysHint,
  fullDaysInWeek,
  gaugeFraction,
  itemsForDay,
  itemsHint,
  timesAverage,
  timesAverageHint,
} from './budget-hints';

// The picture's values: $150 a day, $800 a week, $12 per item and a target of 10 items a day.
describe('the hints under the budgets', () => {
  it('says how many items a day fits at the average cost', () => {
    expect(itemsForDay(150)).toBe(48);
    expect(itemsHint(150)).toBe('fits about 48 items');
  });

  it('says how many full days the week covers at the day’s budget', () => {
    expect(fullDaysInWeek(150, 800)).toBe(5);
    expect(fullDaysHint(150, 800)).toBe('covers 5 full days');
    expect(fullDaysHint(500, 500)).toBe('covers 1 full day');
  });

  it('says how many times the average the most per item is', () => {
    expect(timesAverage(12)).toBe(4);
    expect(timesAverageHint(12)).toBe('4 times the average');
    expect(timesAverageHint(3)).toBe('1 time the average');
  });
});

describe('the estimate and the gauge', () => {
  it('estimates a day at the target of items, at the average cost', () => {
    expect(estimateForTarget(10)).toBe(31);
  });

  it('fills the gauge by the day’s budget out of $500, and no further', () => {
    expect(gaugeFraction(150)).toBeCloseTo(0.3);
    expect(gaugeFraction(500)).toBe(1);
    expect(gaugeFraction(900)).toBe(1);
  });
});
