// The Trace tab's time axis: which tick marks fit the width the track has.

const steps = [1, 2, 5, 10, 15, 30, 60, 120, 300, 600, 900, 1800, 3600, 7200, 14400, 43200, 86400];

/** Room for one character at the meta text size, and the gap kept between two labels, in px. */
const charPx = 7;
const gapPx = 16;
/** Left padding of a tick label (pl-1). */
const padPx = 4;

export const tickLabel = (seconds: number) =>
  seconds === 0 ? '0' : seconds >= 3600 ? `${seconds / 3600} h` : seconds >= 60 ? `${seconds / 60} m` : `${seconds} s`;

const labelPx = (seconds: number) => tickLabel(seconds).length * charPx + padPx;

function ticksAt(spanMs: number, widthPx: number, step: number): number[] {
  const out = [0];
  for (let s = step; s * 1000 <= spanMs; s += step) {
    if ((s * 1000 * widthPx) / spanMs + labelPx(s) > widthPx) break;
    out.push(s);
  }
  return out;
}

/**
 * Tick positions in seconds: 0 and multiples of one round step, the smallest step whose neighbouring labels
 * keep a gap. Without a measured width (widthPx 0 or less) only 0 is returned.
 */
export function axisTicks(spanMs: number, widthPx: number): number[] {
  if (!(widthPx > 0) || !(spanMs > 0)) return [0];
  const fits = (step: number) => {
    const ticks = ticksAt(spanMs, widthPx, step);
    const widest = Math.max(...ticks.map(labelPx));
    return (step * 1000 * widthPx) / spanMs >= widest + gapPx;
  };
  const step = steps.find(fits) ?? steps[steps.length - 1] ?? 86400;
  return ticksAt(spanMs, widthPx, step);
}
