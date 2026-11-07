import { describe, expect, it } from 'vitest';
import {
  dayShare,
  dayShareHint,
  estimateForTarget,
  gaugeFraction,
  timesAverage,
  timesAverageHint,
} from './budget-hints';

// The picture's values: $800 a week, $12 per run and a target of 10 items a day.
describe('the hints under the budgets', () => {
  it('shows the week’s day share as week ÷ 7, the figure Home shows', () => {
    expect(dayShare(800)).toBe(114);
    expect(dayShareHint(800)).toBe('about $114 a day');
    expect(dayShareHint(1400)).toBe('about $200 a day');
  });

  it('says how many times the average item the most per run is', () => {
    expect(timesAverage(12)).toBe(4);
    expect(timesAverageHint(12)).toBe('one agent run, 4 times the average item');
    expect(timesAverageHint(3)).toBe('one agent run, 1 time the average item');
  });
});

describe('the estimate and the gauge', () => {
  it('estimates a day at the target of items, at the average cost', () => {
    expect(estimateForTarget(10)).toBe(31);
  });

  it('fills the gauge by the week’s budget out of $2,000, and no further', () => {
    expect(gaugeFraction(800)).toBeCloseTo(0.4);
    expect(gaugeFraction(2000)).toBe(1);
    expect(gaugeFraction(3000)).toBe(1);
  });
});
