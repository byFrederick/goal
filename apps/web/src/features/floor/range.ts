// The Floor's range of UTC days and work type, kept in the URL (?from&to&type): absent `to` is
// today, absent `from` six days before it, absent `type` every type, as goald reads them. The
// Touches panel's grouping is there too (?touches, absent: by station).
import { isTouchGrouping, type TouchGrouping } from '../../shared/touches';

/** The range and work type the Floor's statistics are read over. */
export interface FloorRange {
  from?: string;
  to?: string;
  type?: string;
}

export interface FloorSearch extends FloorRange {
  touches?: Exclude<TouchGrouping, 'station'>;
}

const isDay = (v: unknown): v is string => typeof v === 'string' && /^\d{4}-\d{2}-\d{2}$/.test(v);

/** The Floor's search params, keeping only well-formed ones (goald checks the rest). */
export function floorSearch(search: Record<string, unknown>): FloorSearch {
  return {
    ...(isDay(search.from) ? { from: search.from } : {}),
    ...(isDay(search.to) ? { to: search.to } : {}),
    ...(typeof search.type === 'string' && search.type !== '' ? { type: search.type } : {}),
    ...(isTouchGrouping(search.touches) && search.touches !== 'station' ? { touches: search.touches } : {}),
  };
}

/** The range of the search, without what only arranges the page: the statistics' query keys. */
export function rangeOf({ from, to, type }: FloorSearch): FloorRange {
  return { ...(from ? { from } : {}), ...(to ? { to } : {}), ...(type ? { type } : {}) };
}

/** The range of the same length just before, as goald compares the metrics with. */
export function previousRange(range: FloorRange, now = Date.now()): FloorRange {
  const to = range.to ?? todayUtc(now);
  const from = range.from ?? addDays(to, -6);
  const days = daysIn(from, to);
  return { from: addDays(from, -days), to: addDays(from, -1), ...(range.type ? { type: range.type } : {}) };
}

const dayMs = 86_400_000;

/** Today's UTC date, as goald's days are UTC (review decision 7). */
export function todayUtc(now = Date.now()): string {
  return new Date(now).toISOString().slice(0, 10);
}

/** `day` moved by `n` days. */
export function addDays(day: string, n: number): string {
  return new Date(Date.parse(`${day}T00:00:00Z`) + n * dayMs).toISOString().slice(0, 10);
}

/** How many days a range covers, both ends included. */
export function daysIn(from: string, to: string): number {
  return Math.round((Date.parse(`${to}T00:00:00Z`) - Date.parse(`${from}T00:00:00Z`)) / dayMs) + 1;
}

/** The windows of the header's segmented control: the last N days, ending today. */
export const windows = [1, 7, 30] as const;

/**
 * The search params of the last `days` days, keeping the type: the last 7 days are goald's
 * default, so they leave the range out of the URL.
 */
export function lastDaysSearch(days: number, type: string | undefined, now = Date.now()): FloorSearch {
  const range = days === 7 ? {} : { from: addDays(todayUtc(now), 1 - days) };
  return { ...range, ...(type ? { type } : {}) };
}

/** Which window a range is, or null for a custom one. */
export function windowOf(from: string, to: string, now = Date.now()): number | null {
  if (to !== todayUtc(now)) return null;
  const days = daysIn(from, to);
  return windows.find((w) => w === days) ?? null;
}

const month = new Intl.DateTimeFormat('en-US', { month: 'short', timeZone: 'UTC' });
const dayOf = (day: string) => Number(day.slice(8, 10));

/** "Oct 17 to 23", "Sep 28 to Oct 4", "Oct 23" for one day. */
export function rangeLabel(from: string, to: string): string {
  const [f, t] = [new Date(`${from}T12:00:00Z`), new Date(`${to}T12:00:00Z`)];
  const start = `${month.format(f)} ${dayOf(from)}`;
  if (from === to) return start;
  const end =
    month.format(f) === month.format(t) && from.slice(0, 4) === to.slice(0, 4)
      ? `${dayOf(to)}`
      : `${month.format(t)} ${dayOf(to)}`;
  return `${start} to ${end}`;
}

/** What the previous range is called: "last week" for the 7 days to today, else "the N days before". */
export function previousName(from: string, to: string, now = Date.now()): string {
  const days = daysIn(from, to);
  if (days === 7 && to === todayUtc(now)) return 'last week';
  return days === 1 ? 'the day before' : `the ${days} days before`;
}
