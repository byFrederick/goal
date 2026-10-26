// The Item screen's time bar: an item's spans (Item.flow) as segments of time per station and
// state, measured against its lead-time target. Pure, so the arithmetic is tested without a DOM.
import type { ItemSpan, ItemState } from '../../api/types';
import type { HatchTone } from '../../ui/hatch';

/** Agents working teal, waiting on a person violet, blocked rust, waiting in a queue gray. */
const stateTone: Record<ItemState, HatchTone> = {
  in_progress: 'run',
  waiting_human: 'you',
  blocked: 'rework',
  queued: 'target',
  ready: 'target',
  done: 'target',
  cancelled: 'target',
  assembly: 'target',
};

/** The tones in the order the legend lists them. */
export const toneOrder: readonly HatchTone[] = ['run', 'you', 'rework', 'target'];

export const toneLabel: Record<HatchTone, string> = {
  run: 'Working',
  you: 'Waiting on you',
  rework: 'Blocked',
  target: 'Waiting',
};

export interface TimeSegment {
  key: string;
  station: string;
  tone: HatchTone;
  ms: number;
  /** The segment the item is in now. */
  current: boolean;
}

export interface StationTime {
  station: string;
  ms: number;
}

export interface TimeBar {
  /** Created to `end`. */
  elapsedMs: number;
  /** What the bar's full width stands for: the target while under it, else the elapsed time. */
  scaleMs: number;
  targetMs: number | null;
  /** Consecutive spans at one station in one tone, in order; empty stretches left out. */
  segments: TimeSegment[];
  /** Time per station, in the order the item first reached each. */
  stations: StationTime[];
  /** What is left of the target, while under it. */
  leftMs: number | null;
  /** How far past the target, once over it. */
  overMs: number | null;
  /** The tones the segments use, in legend order. */
  tones: HatchTone[];
}

/**
 * Lays an item's spans out against its target. `end` is when the item was done, or now; an open
 * span runs to it.
 */
export function timeBar(
  spans: readonly ItemSpan[],
  { createdAt, end, targetSeconds }: { createdAt: string; end: string; targetSeconds: number | null },
): TimeBar {
  const endMs = Date.parse(end);
  const segments: TimeSegment[] = [];
  const stations = new Map<string, number>();

  for (const s of spans) {
    const ms = Math.max(0, (s.left_at ? Date.parse(s.left_at) : endMs) - Date.parse(s.entered_at));
    if (ms === 0) continue;

    const tone = stateTone[s.state];
    const current = s.left_at === null;
    const last = segments.at(-1);

    if (last && last.station === s.station && last.tone === tone) {
      last.ms += ms;
      last.current ||= current;
    } else {
      segments.push({ key: `${s.station}-${s.entered_at}`, station: s.station, tone, ms, current });
    }

    stations.set(s.station, (stations.get(s.station) ?? 0) + ms);
  }

  const elapsedMs = Math.max(0, endMs - Date.parse(createdAt));
  const targetMs = targetSeconds && targetSeconds > 0 ? targetSeconds * 1000 : null;
  const used = new Set(segments.map((s) => s.tone));

  return {
    elapsedMs,
    scaleMs: Math.max(elapsedMs, targetMs ?? 0, 1),
    targetMs,
    segments,
    stations: [...stations].map(([station, ms]) => ({ station, ms })),
    leftMs: targetMs !== null && elapsedMs <= targetMs ? targetMs - elapsedMs : null,
    overMs: targetMs !== null && elapsedMs > targetMs ? elapsedMs - targetMs : null,
    tones: toneOrder.filter((tone) => used.has(tone)),
  };
}

/**
 * A target as the Item screen says it: "60 minutes" up to two hours, then "6 hours", "2.5 hours";
 * short, as under the bar: "60 min", "6 h".
 */
export function formatTarget(seconds: number, length: 'long' | 'short' = 'long'): string {
  const minutes = Math.round(seconds / 60);
  const hours = Math.round((seconds / 3600) * 10) / 10;
  if (length === 'short') return minutes < 120 ? `${minutes} min` : `${hours} h`;
  if (minutes < 120) return `${minutes} ${minutes === 1 ? 'minute' : 'minutes'}`;
  return `${hours} hours`;
}
