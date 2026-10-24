// How the Floor reads a range against the one before it and against its targets: signed deltas
// that say whether the change is better or worse (shown with an arrow and the sign, never colour
// alone), and the sentences under the throughput numeral.
import type { Metrics } from '../../api/types';
import { formatDuration, humanize, plural } from '../../shared/format';

export interface Delta {
  /** Signed, with its unit: "−0.8", "+1 h 24 min", "−4 pts", or "Same". */
  text: string;
  direction: 'up' | 'down' | 'flat';
  tone: 'better' | 'worse' | 'flat';
}

/** A change rounded to `places` decimals, better when `higherIsBetter` matches its sign. */
function delta(change: number, places: number, unit: (abs: number) => string, higherIsBetter: boolean): Delta {
  const rounded = Math.round(change * 10 ** places) / 10 ** places;
  if (rounded === 0) return { text: 'Same', direction: 'flat', tone: 'flat' };
  const up = rounded > 0;
  return {
    text: `${up ? '+' : '−'}${unit(Math.abs(rounded))}`,
    direction: up ? 'up' : 'down',
    tone: up === higherIsBetter ? 'better' : 'worse',
  };
}

/** Items finished a day against the previous range, to one decimal: more is better. */
export function throughputDelta(perDay: number, previous: number): Delta {
  return delta(perDay - previous, 1, (n) => n.toFixed(1), true);
}

/** Lead time p85 against the previous range (hours), to the second: longer is worse. */
export function leadTimeDelta(hours: number | null | undefined, previous: number | null | undefined): Delta | null {
  if (hours == null || previous == null) return null;
  return delta((hours - previous) * 3600, 0, (s) => formatDuration(s * 1000), false);
}

/** Rework rate against the previous range, in percentage points: more is worse. */
export function reworkDelta(rate: number, previous: number): Delta {
  return delta((rate - previous) * 100, 0, (n) => (n === 1 ? '1 pt' : `${n} pts`), false);
}

const perDayNumber = (n: number) => (Number.isInteger(n) ? `${n}` : n.toFixed(1));

/** "1.6 under the target of 8 a day." */
export function targetSentence(perDay: number, target: number): string {
  const gap = Math.round((perDay - target) * 10) / 10;
  if (gap === 0) return `On the target of ${perDayNumber(target)} a day.`;
  return `${Math.abs(gap).toFixed(1)} ${gap < 0 ? 'under' : 'over'} the target of ${perDayNumber(target)} a day.`;
}

/** "11 items waited more than a day in front of Investigate.", or null when none did. */
export function longWaitSentence(waits: Metrics['long_waits']): string | null {
  if (!waits || waits.items === 0) return null;
  return `${plural(waits.items, 'item')} waited more than a day in front of ${humanize(waits.station)}.`;
}
