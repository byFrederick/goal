// The Runs page's URL state (frontend.md: every filter lives in the URL): the factory, the outcome
// chip, the day range and the search text. Unknown or malformed values are dropped, never thrown on.
import type { RunStatus } from '../../api/types';

/** Today is the UTC day the API uses when since and until are absent; yesterday is the day before. */
export type RunsRange = 'today' | 'yesterday';

export interface RunsSearch {
  /** A factory's name (the API takes a name or an id). Absent: every factory the person sees. */
  factory?: string | undefined;
  /** Only runs with this status. Absent: every run in the window. */
  status?: RunStatus | undefined;
  range?: RunsRange | undefined;
  /** Text the item title, agent or model contains, or the item number it names. */
  q?: string | undefined;
}

const statuses: readonly RunStatus[] = ['running', 'succeeded', 'failed', 'cancelled'];
const ranges: readonly RunsRange[] = ['today', 'yesterday'];

const text = (v: unknown, max = 100): string | undefined =>
  typeof v === 'string' && v.trim() !== '' && v.length <= max ? v : undefined;

export function runsSearch(search: Record<string, unknown>): RunsSearch {
  const factory = text(search.factory);
  const status = statuses.find((s) => s === search.status);
  const range = ranges.find((r) => r === search.range);
  const q = text(search.q, 200);
  return {
    ...(factory ? { factory } : {}),
    ...(status ? { status } : {}),
    ...(range && range !== 'today' ? { range } : {}),
    ...(q ? { q } : {}),
  };
}

/** The window a range covers: [since, until) at UTC midnights, as the API takes them. */
export function runsWindow(range: RunsRange | undefined, now = new Date()): { since: string; until: string } {
  const day = 86_400_000;
  const today = Math.floor(now.getTime() / day) * day;
  const start = range === 'yesterday' ? today - day : today;
  return { since: new Date(start).toISOString(), until: new Date(start + day).toISOString() };
}

/** The footer's word for the range: "Showing 9 of 64 runs today." */
export function rangeWord(range: RunsRange | undefined): RunsRange {
  return range === 'yesterday' ? 'yesterday' : 'today';
}
