// The numbers step 5 shows (docs/design/screens/get-started/budget.jpg): the slider ranges and their
// defaults, the hint under each budget, and the estimate. Goal has no per-model cost yet and a new
// organization has no spend history, so every hint and the estimate come from one average cost per item.
// The average is the picture's figure, not a measured one: change it here when a measured average exists.
import { plural } from '../../shared/format';

/** The average cost of one item, in US dollars. */
export const averageItemUsd = 3.1;

/** The budget ranges, the slider steps and the value a field shows while the draft leaves it unset. */
export const budgetRanges = {
  day: { min: 10, max: 500, step: 5, fallback: 150 },
  week: { min: 50, max: 2000, step: 50, fallback: 800 },
  perItem: { min: 2, max: 50, step: 1, fallback: 12 },
} as const;

/** The flow targets' fallbacks: items a day and the lead time in hours. */
export const targetFallbacks = { throughputPerDay: 10, leadTimeHours: 48 } as const;

/** "fits about 48 items": how many items a day's budget pays for at the average cost. */
export function itemsForDay(day: number): number {
  return Math.round(day / averageItemUsd);
}

/** "covers 5 full days": how many whole days of the day's budget the week's budget holds. */
export function fullDaysInWeek(day: number, week: number): number {
  return Math.floor(week / day);
}

/** "4 times the average": how far the most one item may cost is above the average cost. */
export function timesAverage(perItem: number): number {
  return Math.round(perItem / averageItemUsd);
}

/** "~$31": what the day's target of items costs at the average cost. */
export function estimateForTarget(itemsPerDay: number): number {
  return Math.round(itemsPerDay * averageItemUsd);
}

export function itemsHint(day: number): string {
  return `fits about ${plural(itemsForDay(day), 'item')}`;
}

export function fullDaysHint(day: number, week: number): string {
  return `covers ${plural(fullDaysInWeek(day, week), 'full day')}`;
}

export function timesAverageHint(perItem: number): string {
  const times = timesAverage(perItem);
  return `${times} ${times === 1 ? 'time' : 'times'} the average`;
}

/** The share of the half-circle gauge the day's budget fills: the day out of the largest day. */
export function gaugeFraction(day: number): number {
  return Math.min(Math.max(day / budgetRanges.day.max, 0), 1);
}
