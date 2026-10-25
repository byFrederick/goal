// Human touches per item (spec 4 §4.9, §4.10) as goald derives them from item events: one
// person's events on an item at the same instant count once, named by the most specific; API
// keys, agents and the engine never touch, nor do created and lease_taken. Planned touches are
// decisions the line requires (data.planned, else an approval, gate approval or merge) and start
// with the reason required_gate. The metrics count the items finished in a range.
import type {
  ItemEvent,
  Touch,
  TouchCount,
  TouchHeadline,
  TouchReason,
  TouchReasonCount,
  TouchStats,
  VersionTouches,
} from '@goal/api-client';

/** A reason a person set on a touch's event. */
export interface MockReason {
  reason: TouchReason;
  by: string;
}

/** What the metrics read of a touch. */
export type MockTouch = Pick<Touch, 'kind' | 'station' | 'planned' | 'reason'>;

/** A finished item as the touch metrics read it: the version it last ran on and its touches. */
export interface MockFinished {
  type: string;
  done_at: number;
  version: { id: string; number: number } | null;
  touches: readonly MockTouch[];
}

export const touchReasons: readonly TouchReason[] = ['missing_knowledge', 'wrong_decision', 'required_gate', 'other'];

/** The kinds that name a touch, most specific first; any other kind comes after. */
const names = [
  'merged',
  'rejected',
  'gate_rejected',
  'gate_approved',
  'approved',
  'moved',
  'paused',
  'retried',
  'note',
  'released',
];
const rank = (kind: string) => (names.includes(kind) ? names.indexOf(kind) : names.length);

const personOf = (e: ItemEvent) =>
  typeof e.data.actor === 'string' && e.data.actor.startsWith('user:') ? e.data.actor : null;

const isTouchEvent = (e: ItemEvent) => personOf(e) !== null && e.kind !== 'created' && e.kind !== 'lease_taken';

/** data.planned when the event carries it, else whether it is an approval, gate approval or merge. */
export const plannedEvent = (e: ItemEvent) =>
  typeof e.data.planned === 'boolean' ? e.data.planned : ['approved', 'gate_approved', 'merged'].includes(e.kind);

/** An item's touches in time order, from its events and the reasons people set. */
export function touchesOf(events: readonly ItemEvent[], reasons: Readonly<Record<number, MockReason>>): Touch[] {
  const named = new Map<string, ItemEvent>();
  for (const e of events) {
    if (!isTouchEvent(e)) continue;
    const key = `${personOf(e)}|${e.at}`;
    const cur = named.get(key);
    if (!cur || rank(e.kind) < rank(cur.kind) || (rank(e.kind) === rank(cur.kind) && e.id < cur.id)) named.set(key, e);
  }
  return [...named.values()]
    .sort((a, b) => a.at.localeCompare(b.at) || a.id - b.id)
    .map((e): Touch => {
      const planned = plannedEvent(e);
      const set = reasons[e.id];
      return {
        event_id: e.id,
        kind: e.kind,
        station: e.station ?? null,
        at: e.at,
        actor: { kind: 'user', id: (personOf(e) ?? '').slice('user:'.length), name: e.actor },
        planned,
        reason: set?.reason ?? (planned ? 'required_gate' : null),
        reason_by: set?.by ?? null,
      };
    });
}

const ratio = (n: number, d: number) => (d === 0 ? 0 : n / d);

/** The median of values, linearly interpolated; 0 for none. */
function median(values: readonly number[]): number {
  const sorted = [...values].sort((a, b) => a - b);
  if (sorted.length === 0) return 0;
  const pos = 0.5 * (sorted.length - 1);
  const lo = Math.floor(pos);
  const hi = Math.min(lo + 1, sorted.length - 1);
  return (sorted[lo] ?? 0) + (pos - lo) * ((sorted[hi] ?? 0) - (sorted[lo] ?? 0));
}

interface Tally {
  touches: number;
  planned: number;
}

const tally = (m: Map<string | null, Tally>, key: string | null, planned: boolean) => {
  const t = m.get(key) ?? { touches: 0, planned: 0 };
  t.touches += 1;
  if (planned) t.planned += 1;
  m.set(key, t);
};

/** The most touches first, then by key. */
const ranked = (m: Map<string | null, Tally>) =>
  [...m.entries()].sort(
    ([ka, a], [kb, b]) => b.touches - a.touches || String(ka ?? '').localeCompare(String(kb ?? '')),
  );

/** The touch statistics of items, with the line stops people made in the same range. */
export function touchStatsOf(items: readonly MockFinished[], lineStops = 0): TouchStats {
  const stations = new Map<string | null, Tally>();
  const kinds = new Map<string | null, Tally>();
  const reasons = new Map<string | null, Tally>();
  const versions = new Map<string | null, { number: number | null; finished: number; touchless: number } & Tally>();
  let touches = 0;
  let planned = 0;
  for (const it of items) {
    const key = it.version?.id ?? null;
    const v = versions.get(key) ?? {
      number: it.version?.number ?? null,
      finished: 0,
      touchless: 0,
      touches: 0,
      planned: 0,
    };
    v.finished += 1;
    if (it.touches.length === 0) v.touchless += 1;
    for (const t of it.touches) {
      touches += 1;
      v.touches += 1;
      if (t.planned) {
        planned += 1;
        v.planned += 1;
      }
      tally(stations, t.station ?? '', t.planned);
      tally(kinds, t.kind, t.planned);
      tally(reasons, t.reason ?? null, t.planned);
    }
    versions.set(key, v);
  }
  const finished = items.length;
  const count = ([key, t]: [string | null, Tally]): TouchCount => ({
    key: key ?? '',
    touches: t.touches,
    planned: t.planned,
    unplanned: t.touches - t.planned,
  });
  return {
    finished,
    touches,
    planned,
    unplanned: touches - planned,
    touchless_rate: ratio(items.filter((it) => it.touches.length === 0).length, finished),
    per_item_mean: ratio(touches, finished),
    per_item_median: median(items.map((it) => it.touches.length)),
    planned_per_item: ratio(planned, finished),
    unplanned_per_item: ratio(touches - planned, finished),
    by_station: ranked(stations).map(count),
    by_kind: ranked(kinds).map(count),
    by_reason: ranked(reasons).map(
      ([reason, t]): TouchReasonCount => ({
        reason: reason as TouchReason | null,
        touches: t.touches,
        planned: t.planned,
        unplanned: t.touches - t.planned,
      }),
    ),
    by_version: [...versions.entries()]
      .map(
        ([id, v]): VersionTouches => ({
          version_id: id,
          number: v.number,
          finished: v.finished,
          touches: v.touches,
          touchless_rate: ratio(v.touchless, v.finished),
          per_item_mean: ratio(v.touches, v.finished),
          planned_per_item: ratio(v.planned, v.finished),
          unplanned_per_item: ratio(v.touches - v.planned, v.finished),
        }),
      )
      .sort((a, b) => (a.number === null ? 1 : b.number === null ? -1 : b.number - a.number)),
    line_stops: lineStops,
  };
}

/** The touches of items in brief. */
export function touchHeadlineOf(items: readonly MockFinished[]): TouchHeadline {
  const s = touchStatsOf(items);
  return {
    finished: s.finished,
    per_item_mean: s.per_item_mean,
    per_item_median: s.per_item_median,
    touchless_rate: s.touchless_rate,
  };
}

/** The items finished in [from, until). */
export const finishedIn = (items: readonly MockFinished[], from: number, until: number) =>
  items.filter((it) => it.done_at >= from && it.done_at < until);
