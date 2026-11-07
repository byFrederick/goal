// The numbers step 5 shows (docs/design/screens/get-started/budget.jpg): the slider ranges and their
// defaults, the hint under each budget, and the estimate. Goal has no per-model cost yet and a new
// organization has no spend history, so every hint and the estimate come from one average cost per item.
// The average is the picture's figure, not a measured one: change it here when a measured average exists.
import { formatUsdCompact } from '../../shared/format';

/** The average cost of one item, in US dollars. */
export const averageItemUsd = 3.1;

/** The budget ranges, the slider steps and the value a field shows while the draft leaves it unset. */
export const budgetRanges = {
  week: { min: 50, max: 2000, step: 50, fallback: 800 },
  perItem: { min: 2, max: 50, step: 1, fallback: 12 },
} as const;

/** The flow targets' fallbacks: items a day and the lead time in hours. */
export const targetFallbacks = { throughputPerDay: 10, leadTimeHours: 48 } as const;

/** "about $114": the week's budget as a day's share, week ÷ 7, the figure Home shows as Spend today. */
export function dayShare(week: number): number {
  return Math.round(week / 7);
}

/** "4 times the average": how far the most one agent run may cost is above the average cost. */
export function timesAverage(perItem: number): number {
  return Math.round(perItem / averageItemUsd);
}

/** "~$31": what the day's target of items costs at the average cost. */
export function estimateForTarget(itemsPerDay: number): number {
  return Math.round(itemsPerDay * averageItemUsd);
}

export function dayShareHint(week: number): string {
  return `about ${formatUsdCompact(dayShare(week))} a day`;
}

export function timesAverageHint(perItem: number): string {
  const times = timesAverage(perItem);
  return `one agent run, ${times} ${times === 1 ? 'time' : 'times'} the average item`;
}

/** The share of the half-circle gauge the week's budget fills: the week out of the largest week. */
export function gaugeFraction(week: number): number {
  return Math.min(Math.max(week / budgetRanges.week.max, 0), 1);
}
