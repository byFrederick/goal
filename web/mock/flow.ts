// The mock's flow metrics (E §8.1): a factory's done history, ranges of UTC days, the metrics of a
// range with the range before it, and throughput by week, computed as goald computes them from
// work_items. Each factory with a profile gets months of seeded, deterministic history so every
// range shows something; the items the mock moves to done count too.
import type { Factory, Metrics, MetricsSummary, Targets, WeeklyThroughput } from '@goal/api-client';
import type { MockFlow, MockItem } from './fixtures.ts';

const dayMs = 86_400_000;
const maxRangeDays = 90;
/** Days of history seeded: 26 weeks of weekly bars plus a 90-day range before them. */
const historyDays = 26 * 7 + maxRangeDays;
/** How long a wait in front of the drum must last before the Floor calls it out. */
export const longWaitHours = 24;

/** One item finished in the past, as the metrics read it. */
export interface MockDone {
  type: string;
  done_at: number;
  lead_hours: number;
  reworked: boolean;
  cost_usd: number;
  /** How long it waited in front of the factory's drum, in hours. */
  waited_hours: number;
}

/** How a factory's history looks: items a day, typical lead time, rework share, cost per item. */
interface Profile {
  perDay: number;
  leadHours: number;
  rework: number;
  costUsd: number;
}

const profiles: Record<string, Profile> = {
  goal: { perDay: 2.1, leadHours: 7, rework: 0.22, costUsd: 1.4 },
  sre: { perDay: 6.6, leadHours: 5.8, rework: 0.18, costUsd: 0.35 },
};

/** A small seeded generator (mulberry32), so the history is the same on every reset. */
function random(seed: string): () => number {
  let a = [...seed].reduce((h, c) => Math.imul(h ^ c.charCodeAt(0), 2654435761) >>> 0, 1779033703);
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

/** The seeded history of a factory up to now; empty for a factory without a profile. */
export function seedHistory(f: Factory, now: number): MockDone[] {
  const profile = profiles[f.name];
  if (!profile || f.work_types.length === 0) return [];
  const rand = random(f.id);
  const today = Math.floor(now / dayMs) * dayMs;
  const out: MockDone[] = [];
  for (let back = historyDays; back >= 0; back--) {
    // A slow rise over the months and a day-to-day swing.
    const trend = 0.75 + 0.25 * (1 - back / historyDays);
    const n = Math.round(profile.perDay * trend * (0.4 + 1.2 * rand()));
    for (let i = 0; i < n; i++) {
      const done = today - back * dayMs + Math.floor(rand() * dayMs);
      if (done >= now) continue;
      const type =
        f.work_types[Math.min(f.work_types.length - 1, Math.floor(rand() ** 1.6 * f.work_types.length))] ?? '';
      out.push({
        type,
        done_at: done,
        lead_hours: profile.leadHours * (0.25 - Math.log(1 - rand() * 0.98) * 0.7),
        reworked: rand() < profile.rework,
        cost_usd: Math.round(profile.costUsd * (0.4 + rand() * 1.2) * 100) / 100,
        waited_hours: rand() ** 3 * 40,
      });
    }
  }
  return out;
}

/** A range of whole UTC days, both ends included, as midnights in ms. */
export interface DayRange {
  from: number;
  to: number;
}

const iso = (ms: number) => new Date(ms).toISOString().slice(0, 10);
const midnight = (ms: number) => Math.floor(ms / dayMs) * dayMs;
const parseDay = (s: string | null) => (s && /^\d{4}-\d{2}-\d{2}$/.test(s) ? Date.parse(`${s}T00:00:00Z`) : Number.NaN);
const daysOf = (r: DayRange) => Math.round((r.to - r.from) / dayMs) + 1;

/** A request's range as goald checks it (a window, or from and to), or why it is refused. */
export function rangeOf(params: URLSearchParams, now: number): DayRange | string {
  const today = midnight(now);
  const window = params.get('window');
  const from = params.get('from');
  const to = params.get('to');
  if (window !== null && (from !== null || to !== null)) return 'give a window or a range (from, to), not both';
  if (window !== null) {
    const m = /^([1-9][0-9]?)d$/.exec(window);
    const n = Number(m?.[1]);
    if (!m || n > maxRangeDays) return `window "${window}" is not 1d to ${maxRangeDays}d`;
    return { from: today - (n - 1) * dayMs, to: today };
  }
  const end = to === null ? today : parseDay(to);
  const start = from === null ? end - 6 * dayMs : parseDay(from);
  if (Number.isNaN(end) || Number.isNaN(start)) return 'from and to are dates (YYYY-MM-DD)';
  if (end > today) return `to ${to} is after today`;
  if (start > end) return 'from is after to';
  const r = { from: start, to: end };
  if (daysOf(r) > maxRangeDays) return `a range is at most ${maxRangeDays} days`;
  return r;
}

/** The done items of a factory: its history and the items the mock finished. */
function doneOf(history: readonly MockDone[], items: readonly MockItem[], type: string): MockDone[] {
  const finished = items.flatMap((it): MockDone[] =>
    it.done_at
      ? [
          {
            type: it.type,
            done_at: Date.parse(it.done_at),
            lead_hours: (Date.parse(it.done_at) - Date.parse(it.created_at)) / 3_600_000,
            reworked: it.rework_count > 0,
            cost_usd: it.cost_usd,
            waited_hours: 0,
          },
        ]
      : [],
  );
  return [...history, ...finished].filter((d) => !type || d.type === type);
}

/** PostgreSQL's percentile_cont over sorted values. */
function percentile(sorted: readonly number[], p: number): number {
  if (sorted.length === 0) return 0;
  const pos = p * (sorted.length - 1);
  const lo = Math.floor(pos);
  const hi = Math.min(lo + 1, sorted.length - 1);
  return (sorted[lo] ?? 0) + (pos - lo) * ((sorted[hi] ?? 0) - (sorted[lo] ?? 0));
}

const round = (n: number, places = 2) => Math.round(n * 10 ** places) / 10 ** places;

function summary(done: readonly MockDone[], r: DayRange, now: number): MetricsSummary & { done: MockDone[] } {
  const end = Math.min(r.to + dayMs, now);
  const inRange = done.filter((d) => d.done_at >= r.from && d.done_at < end);
  const lead = inRange.map((d) => d.lead_hours).sort((a, b) => a - b);
  return {
    from: iso(r.from),
    to: iso(r.to),
    items_done: inRange.length,
    throughput_per_day: inRange.length / daysOf(r),
    lead_time_hours: lead.length ? { p50: round(percentile(lead, 0.5)), p85: round(percentile(lead, 0.85)) } : null,
    rework_rate: inRange.length ? inRange.filter((d) => d.reworked).length / inRange.length : 0,
    done: inRange,
  };
}

export function targetsOf(flow: MockFlow | undefined): Targets {
  return {
    throughput_per_day: flow?.throughput_per_day ?? null,
    lead_time_hours: flow?.lead_time ? flow.lead_time / 3600 : null,
    expedite_lead_time_minutes: flow?.expedite_lead_time ? Math.round(flow.expedite_lead_time / 60) : null,
  };
}

export interface FlowInput {
  history: readonly MockDone[];
  items: readonly MockItem[];
  flow: MockFlow | undefined;
  /** The station long waits are counted at; null without one. */
  drum: string | null;
  type: string;
  now: number;
}

/** GET …/metrics over r, with the range before it when compare is set. */
export function metricsOf(input: FlowInput, r: DayRange, compare: boolean): Metrics {
  const { history, items, flow, drum, type, now } = input;
  const done = doneOf(history, items, type);
  const { done: inRange, ...cur } = summary(done, r, now);
  const perDay = new Map<string, number>();
  for (const d of inRange) perDay.set(iso(d.done_at), (perDay.get(iso(d.done_at)) ?? 0) + 1);
  const ofType = items.filter((it) => !type || it.type === type);
  const open = ofType.filter((it) => it.state !== 'done' && it.state !== 'cancelled');
  const waitingNow = drum
    ? open.filter((it) => it.station === drum && (it.state === 'queued' || it.state === 'ready'))
    : [];
  const end = Math.min(r.to + dayMs, now);
  let previous: MetricsSummary | null = null;
  if (compare) {
    const n = daysOf(r) * dayMs;
    const { done: _, ...prev } = summary(done, { from: r.from - n, to: r.to - n }, now);
    previous = prev;
  }
  return {
    window: `${daysOf(r)}d`,
    from: cur.from,
    to: cur.to,
    type: type || null,
    throughput: Array.from({ length: daysOf(r) }, (_, i) => {
      const day = iso(r.from + i * dayMs);
      return { day, done: perDay.get(day) ?? 0 };
    }),
    throughput_per_day: cur.throughput_per_day,
    items_done: cur.items_done,
    lead_time_hours: cur.lead_time_hours,
    rework_rate: cur.rework_rate,
    wip: open.length,
    spend_usd: round(inRange.reduce((n, d) => n + d.cost_usd, 0)),
    previous,
    targets: targetsOf(flow),
    long_waits: drum
      ? {
          station: drum,
          items:
            inRange.filter((d) => d.waited_hours > longWaitHours).length +
            waitingNow.filter((it) => end - Date.parse(it.updated_at) > longWaitHours * 3_600_000).length,
          threshold_hours: longWaitHours,
        }
      : null,
  };
}

/** GET …/metrics/weekly: weeks of 7 days ending on `to`, oldest first, or why it is refused. */
export function weeklyOf(input: FlowInput, params: URLSearchParams): WeeklyThroughput | string {
  const today = midnight(input.now);
  const toParam = params.get('to');
  const to = toParam === null ? today : parseDay(toParam);
  const weeks = params.has('weeks') ? Number(params.get('weeks')) : 12;
  if (Number.isNaN(to) || to > today) return 'to must be a date, not after today';
  if (!Number.isInteger(weeks) || weeks < 1 || weeks > 26) return 'weeks must be in [1, 26]';
  const done = doneOf(input.history, input.items, input.type);
  const end = to + dayMs;
  return {
    to: iso(to),
    type: input.type || null,
    targets: targetsOf(input.flow),
    weeks: Array.from({ length: weeks }, (_, i) => {
      const from = end - (weeks - i) * 7 * dayMs;
      const n = done.filter((d) => d.done_at >= from && d.done_at < from + 7 * dayMs).length;
      return { from: iso(from), to: iso(from + 6 * dayMs), done: n, per_day: n / 7 };
    }),
  };
}
