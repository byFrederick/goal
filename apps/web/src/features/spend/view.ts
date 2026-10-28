// The spend screen's arithmetic and words, kept out of the components so they can be tested as plain
// values. A figure goald does not split (null, or an empty list when the answer is not complete) is
// never turned into a 0: `money` and `usdOf` say "—" for it.

import type { OrgSpend, SpendBudget, SpendWindow } from '../../api/types';
import { formatUsd, formatUsdCompact } from '../../shared/format';

export const windowLabel: Record<SpendWindow, string> = {
  week: 'This week',
  month: 'This month',
  '90d': 'Last 90 days',
};

/** The first KPI tile's label: the window's spend. */
export const spendLabel: Record<SpendWindow, string> = {
  week: 'Spend this week',
  month: 'Spend this month',
  '90d': 'Spend, last 90 days',
};

const dash = '—';

/** Whole dollars, as budgets are set; "—" for a figure goald did not give. */
export const money = (n: number | null | undefined): string =>
  n === null || n === undefined ? dash : formatUsdCompact(n);

/** Cents, for a cost per item; "—" for a figure goald did not give. */
export const preciseMoney = (n: number | null | undefined): string =>
  n === null || n === undefined ? dash : formatUsd(n);

/** A share of a whole, as a whole percentage; 0 when the whole is 0. */
export const percentOf = (part: number, whole: number): number =>
  whole > 0 ? Math.round(Math.min(100, Math.max(0, (part / whole) * 100))) : 0;

/** The sum of the figures given; null when none is given (so a missing cap stays a dash). */
export function sumOf(values: readonly (number | null)[]): number | null {
  const given = values.filter((v): v is number => v !== null);
  return given.length > 0 ? given.reduce((a, b) => a + b, 0) : null;
}

const utc = (iso: string) => new Date(iso);

const monthLong = new Intl.DateTimeFormat('en-US', { month: 'long', timeZone: 'UTC' });
const monthDay = new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric', timeZone: 'UTC' });
const weekdayShort = new Intl.DateTimeFormat('en-US', { weekday: 'short', timeZone: 'UTC' });

/** The month's name for a UTC day: "September". */
export const monthName = (day: string): string => monthLong.format(new Date(`${day}T12:00:00Z`));

/** "Oct 1 to 26": from the first of the month the window ends in, to the window's last day. */
export function monthToDate(until: string): string {
  const end = utc(until);
  const first = new Date(Date.UTC(end.getUTCFullYear(), end.getUTCMonth(), 1));
  return `${monthDay.format(first)} to ${monthDay.format(end)}`;
}

/** "Mon" for a UTC day, the bar's label. */
export const weekdayOf = (day: string): string => weekdayShort.format(new Date(`${day}T12:00:00Z`));

/** The budget's segment colour: the first factory takes run, then the palette in turn. */
const series = ['bg-run', 'bg-you', 'bg-ok', 'bg-amber', 'bg-rework'] as const;
export const seriesFill = (index: number): string => series[index % series.length] ?? 'bg-run';

/** Each factory's name and its total over the window, in the order of the budgets. */
export interface FactoryLine {
  id: string;
  name: string;
  title: string;
  fill: string;
  usd: number | null;
}

/**
 * The factories that spend in the window, in budget order. `usd` is the window's sum from the days'
 * by_factory, or null when goald did not split the spend (not complete).
 */
export function factoryLines(spend: OrgSpend): FactoryLine[] {
  return spend.budgets.map((b, i) => {
    const usd = spend.complete
      ? spend.days.reduce((n, d) => n + (d.by_factory.find((f) => f.factory_id === b.factory.id)?.usd ?? 0), 0)
      : null;
    return { id: b.factory.id, name: b.factory.name, title: b.factory.title, fill: seriesFill(i), usd };
  });
}

/** Whether a budget is stopped now: an andon pull is open on the factory. */
export const isStopped = (b: SpendBudget): boolean => b.state === 'stopped';

/** A connection's factories as words: "used by all 3 factories". */
export function usedByText(used: number, total: number): string {
  if (used === total) return `used by all ${total} ${total === 1 ? 'factory' : 'factories'}`;
  return `used by ${used} of ${total} ${total === 1 ? 'factory' : 'factories'}`;
}

/** The model connections of every budget, each once, with the factories that use it. */
export function connectionsOf(budgets: readonly SpendBudget[]): { name: string; kind: string; used: number }[] {
  const byName = new Map<string, { name: string; kind: string; used: number }>();
  for (const b of budgets) {
    for (const c of b.model_connections) {
      const seen = byName.get(c.name);
      if (seen) seen.used += 1;
      else byName.set(c.name, { name: c.name, kind: c.kind, used: 1 });
    }
  }
  return [...byName.values()];
}

/** "Oct 26": a UTC day as the chart's label. */
export const shortDay = (day: string): string => monthDay.format(new Date(`${day}T12:00:00Z`));
