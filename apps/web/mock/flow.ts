// The mock's flow metrics (E §8.1): a factory's done history, ranges of UTC days, the metrics of a
// range with the range before it, and throughput by week, computed as goald computes them from
// work_items. Each factory with a profile gets months of seeded, deterministic history so every
// range shows something; the items the mock moves to done count too.
import type {
  BridgeStep,
  ConstraintReport,
  Factory,
  ItemSpan,
  LeadTimeBridge,
  Metrics,
  MetricsSummary,
  Station,
  Targets,
  TouchReason,
  WeeklyThroughput,
} from '@goal/api-client';
import type { MockFlow, MockItem, MockStationStats } from './fixtures.ts';
import type { MockTouch } from './touches.ts';

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
  /** Its hours in each component of the lead-time bridge, by `<kind>:<station>` (domain.Component). */
  times: Record<string, number>;
  /** The seeded history's human touches; the mock's own items have theirs in their events. */
  touches?: MockTouch[];
}

/** How a factory's history looks: items a day, the longest wait at its drum, rework share, cost per item. */
interface Profile {
  perDay: number;
  drumWaitHours: number;
  rework: number;
  costUsd: number;
}

const profiles: Record<string, Profile> = {
  goal: { perDay: 2.1, drumWaitHours: 14, rework: 0.22, costUsd: 1.4 },
  sre: { perDay: 6.6, drumWaitHours: 11, rework: 0.18, costUsd: 0.35 },
};

/** The key of a bridge component: `waiting:build`, `waiting_on_you:`. */
export const componentKey = (kind: string, station = '') => `${kind}:${station}`;

/** The reasons of the seeded history's unplanned touches; null: none set yet. */
const unplannedReasons: (TouchReason | null)[] = [
  'missing_knowledge',
  'missing_knowledge',
  'wrong_decision',
  'other',
  null,
];

/**
 * A history item's touches: a planned decision at each human station (merged at a merge station,
 * else approved), and now and then a note at the drum or a send-back at the first human station.
 */
function seedTouches(rand: () => number, humans: readonly Station[], drum: string): MockTouch[] {
  const reason = () => unplannedReasons[Math.floor(rand() * unplannedReasons.length)] ?? null;
  const out: MockTouch[] = humans.map((s) => ({
    kind: s.action === 'merge' ? 'merged' : 'approved',
    station: s.id,
    planned: true,
    reason: 'required_gate',
  }));
  if (rand() < 0.25) out.push({ kind: 'note', station: drum, planned: false, reason: reason() });
  const first = humans[0];
  if (first && rand() < 0.12) out.push({ kind: 'rejected', station: first.id, planned: false, reason: reason() });
  return out;
}

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

/**
 * The seeded history of a factory up to now; empty for a factory without a profile. Each item
 * queues at the first station, waits for and works at the drum, may be sent back by the station
 * after it, and waits on a person at the first human station.
 */
export function seedHistory(f: Factory, now: number, drum: string | null): MockDone[] {
  const profile = profiles[f.name];
  if (!profile || f.work_types.length === 0 || !drum) return [];
  const rand = random(f.id);
  // Touches have a generator of their own, so the rest of the history stays as it was.
  const touchRand = random(`${f.id}:touches`);
  const humans = f.stations.filter((s) => s.kind === 'human');
  const today = Math.floor(now / dayMs) * dayMs;
  const queue = f.stations.find((s) => s.kind === 'queue')?.id;
  const human = f.stations.find((s) => s.kind === 'human')?.id;
  const sender = f.stations[f.stations.findIndex((s) => s.id === drum) + 1]?.id ?? drum;
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
      const reworked = rand() < profile.rework;
      const times: Record<string, number> = {
        [componentKey('waiting', queue)]: rand() * 0.8,
        [componentKey('waiting', drum)]: rand() ** 4 * profile.drumWaitHours,
        [componentKey('working', drum)]: 0.5 + rand() * 2,
        ...(human ? { [componentKey('waiting_on_you')]: rand() ** 2 * 3 } : {}),
        ...(reworked ? { [componentKey('rework', sender)]: 1 + rand() * 4 } : {}),
      };
      out.push({
        type,
        done_at: done,
        lead_hours: Object.values(times).reduce((a, b) => a + b, 0),
        reworked,
        cost_usd: Math.round(profile.costUsd * (0.4 + rand() * 1.2) * 100) / 100,
        times,
        touches: seedTouches(touchRand, humans, drum),
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

/** Where a span's time goes, as domain.ComponentOf says. */
function componentOf(sp: ItemSpan): string {
  if (sp.rework_by) return componentKey('rework', sp.rework_by);
  switch (sp.state) {
    case 'waiting_human':
      return componentKey('waiting_on_you');
    case 'blocked':
      return componentKey('blocked', sp.station);
    case 'in_progress':
      return componentKey('working', sp.station);
    default:
      return componentKey('waiting', sp.station);
  }
}

/** An item's hours in each component, from its closed spans. */
function timesOf(spans: readonly ItemSpan[]): Record<string, number> {
  const out: Record<string, number> = {};
  for (const sp of spans) {
    if (!sp.left_at) continue;
    const key = componentOf(sp);
    out[key] = (out[key] ?? 0) + (Date.parse(sp.left_at) - Date.parse(sp.entered_at)) / 3_600_000;
  }
  return out;
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
            times: timesOf(it.spans),
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
    // The mock seeds no assemblies, so none is ever done.
    assembly_lead_time_hours: null,
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
    assembly_lead_time_hours: cur.assembly_lead_time_hours,
    rework_rate: cur.rework_rate,
    wip: open.length,
    spend_usd: round(inRange.reduce((n, d) => n + d.cost_usd, 0)),
    previous,
    targets: targetsOf(flow),
    long_waits: drum
      ? {
          station: drum,
          items:
            inRange.filter((d) => (d.times[componentKey('waiting', drum)] ?? 0) > longWaitHours).length +
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

/** domain.WaitSaved: the mean wait one more slot saves (Sakasegawa's M/M/c), in hours, or null. */
export function waitSaved(busy: number, slots: number, serviceHours: number, waitHours: number, leases: number) {
  if (busy >= 0.99 || leases < 20 || slots < 1 || waitHours <= 0 || busy <= 0) return null;
  const wq = (rho: number, c: number) => (rho ** (Math.sqrt(2 * (c + 1)) - 1) / (c * (1 - rho))) * serviceHours;
  const saved = wq(busy, slots) - wq((busy * slots) / (slots + 1), slots + 1);
  return Math.min(Math.max(saved, 0), waitHours);
}

export interface ConstraintInput {
  factory: Factory;
  range: DayRange;
  constraint: string | null;
  source: ConstraintReport['source'];
  /** Seeded busy shares by station; a station without one has no lease history. */
  stats: Record<string, MockStationStats>;
  agents: Record<string, string[]>;
  /** Each station's capacity (Station.Capacity). */
  slots: Record<string, number>;
  /** Items waiting at each station now. */
  waiting: Record<string, number>;
  now: number;
}

/** A mean lease of the mock's agents, in hours. */
const serviceHours = 1.5;
/** What one of the mock's agent runs costs, in dollars: with 1.2, the software Floor's build adds about $18 a day. */
const runCostUsd = 1.2;

/**
 * GET …/constraint: the agent stations with a seeded busy share, busiest first, at most 3; the
 * items waiting now stand for the time-weighted queue, Little's law gives the mean wait.
 */
export function constraintOf(i: ConstraintInput): ConstraintReport {
  const hours = (Math.min(i.range.to + dayMs, i.now) - i.range.from) / 3_600_000;
  const candidates = i.factory.stations
    .filter((s) => s.kind === 'agent' && i.stats[s.id]?.busy_ratio !== undefined)
    .map((s) => {
      const agents = i.agents[s.id] ?? [];
      const slots = i.slots[s.id] ?? Math.max(agents.length, 1);
      const busy = i.stats[s.id]?.busy_ratio ?? 0;
      const leases = Math.round((busy * slots * hours) / serviceHours);
      const avg = i.waiting[s.id] ?? 0;
      const wait = leases > 0 && avg > 0 ? (avg * hours) / leases : null;
      const saved = waitSaved(busy, slots, serviceHours, wait ?? 0, leases);
      return {
        station: s.id,
        slots,
        agents,
        busy_ratio: busy,
        avg_waiting: avg,
        mean_wait_minutes: wait === null ? null : wait * 60,
        mean_service_minutes: leases > 0 ? serviceHours * 60 : null,
        leases,
        spend_per_slot_day_usd: hours > 0 && leases > 0 ? (leases * runCostUsd) / slots / (hours / 24) : null,
        estimate: saved === null ? null : { extra_slot_saves_hours: saved },
      };
    })
    .sort((a, b) => b.busy_ratio - a.busy_ratio)
    .slice(0, 3);
  return {
    from: iso(i.range.from),
    to: iso(i.range.to),
    coverage_since: candidates.length > 0 ? new Date(i.now - 120 * dayMs).toISOString() : null,
    configured: null,
    constraint: i.constraint,
    source: i.source,
    candidates,
  };
}

const kinds = ['waiting', 'rework', 'waiting_on_you', 'blocked', 'working', 'other'] as const;

/** Splits a component key into the bridge step's component and station. */
function stepOf(key: string): Pick<BridgeStep, 'component' | 'station'> {
  const [kind = 'other', station = ''] = key.split(':');
  return { component: kind as BridgeStep['component'], station: station || null };
}

/**
 * GET …/metrics/lead-time over r, as domain.BridgeLeadTime computes it: slow items (over the
 * target) against fast ones, each component's excess its share of the gap, steps under 3% in
 * other. `busy` is the drum's busy share for the insight.
 */
export function leadTimeOf(input: FlowInput, r: DayRange, busy: number | null): LeadTimeBridge {
  const target = input.flow?.lead_time ? input.flow.lead_time / 3600 : null;
  const { done } = summary(doneOf(input.history, input.items, input.type), r, input.now);
  const leads = done.map((d) => d.lead_hours).sort((a, b) => a - b);
  const actual = leads.length ? percentile(leads, 0.85) : null;
  const out: LeadTimeBridge = {
    from: iso(r.from),
    to: iso(r.to),
    type: input.type || null,
    status: 'over_target',
    target_hours: target,
    actual_p85_hours: actual,
    gap_hours: null,
    items_done: done.length,
    over_target_items: 0,
    steps: [],
    constraint: target !== null && input.drum ? { station: input.drum, busy_ratio: busy } : null,
  };
  if (target === null) return { ...out, status: 'no_target' };
  if (done.length < 5) return { ...out, status: 'not_enough_data' };
  if (actual === null || actual <= target) return { ...out, status: 'on_target' };

  const gap = actual - target;
  const slow = done.filter((d) => d.lead_hours > target);
  const fast = done.filter((d) => d.lead_hours <= target);
  const mean = (ds: MockDone[], key: string) => ds.reduce((n, d) => n + (d.times[key] ?? 0), 0) / ds.length;
  const excess = new Map<string, number>();
  for (const key of new Set(slow.flatMap((d) => Object.keys(d.times)))) {
    const e = fast.length ? mean(slow, key) - mean(fast, key) : key.startsWith('working:') ? 0 : mean(slow, key);
    if (e > 0) excess.set(key, e);
  }
  const total = [...excess.values()].reduce((a, b) => a + b, 0);
  const slowWith = (keys: string[]) => slow.filter((d) => keys.some((k) => (d.times[k] ?? 0) > 0)).length;
  const big = [...excess].filter(([, e]) => e / total >= 0.03);
  const small = [...excess].filter(([, e]) => e / total < 0.03);
  const steps: BridgeStep[] = big
    .sort(
      ([a, ea], [b, eb]) =>
        eb - ea || kinds.indexOf(stepOf(a).component) - kinds.indexOf(stepOf(b).component) || a.localeCompare(b),
    )
    .map(([key, e]) => ({ ...stepOf(key), hours: (gap * e) / total, share: e / total, items: slowWith([key]) }));
  if (small.length > 0) {
    const share = small.reduce((n, [, e]) => n + e, 0) / total;
    steps.push({
      component: 'other',
      station: null,
      hours: gap * share,
      share,
      items: slowWith(small.map(([k]) => k)),
    });
  }
  return { ...out, gap_hours: gap, over_target_items: slow.length, steps };
}
